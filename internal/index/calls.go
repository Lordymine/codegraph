package index

import (
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/Lordymine/codegraph/internal/gocalls"
	"github.com/Lordymine/codegraph/internal/graph"
	"github.com/Lordymine/codegraph/internal/memory"
	"github.com/Lordymine/codegraph/internal/scip"
)

// ScipReport summarizes scip-typescript resource use across all TS scopes in one
// index run. PeakRSS is the max child RSS observed (Linux/WSL); zero elsewhere.
type ScipReport struct {
	ScopesRun int
	PeakRSS   uint64
	HeapCapMB int
}

// resolveTSCalls runs scip-typescript per tsconfig scope in isolation: one scope at
// a time, CALLS edges flushed to SQLite immediately, protobuf and Node heap released
// via memory.Gate() before the next scope or the Go VTA pass — same pattern as Go.
func resolveTSCalls(store *graph.Store, project, root string, enc scip.Enclosing, changed map[string]bool) (ScipReport, error) {
	var rep ScipReport

	for _, dir := range tsconfigDirs(root) {
		if changed != nil && !changed[dir] {
			continue
		}
		abs := filepath.Join(root, filepath.FromSlash(dir))
		name := dir
		if name == "" {
			name = "root"
		}
		out := filepath.Join(os.TempDir(), "codegraph-"+strings.ReplaceAll(name, "/", "-")+".scip")
		idx, st, err := scip.RunAndRead(abs, out)
		_ = os.Remove(out)
		if rep.HeapCapMB == 0 {
			rep.HeapCapMB = st.NodeHeapMB
		}
		if st.PeakRSSBytes > rep.PeakRSS {
			rep.PeakRSS = st.PeakRSSBytes
		}
		if err != nil {
			continue // best-effort per scope (same contract as resolveGoCalls)
		}
		rep.ScopesRun++
		scopeEdges := scip.CallEdges(idx, project, dir, enc)
		idx = nil
		if _, _, err := store.InsertEdges(scopeEdges); err != nil {
			return rep, err
		}
		scopeEdges = nil
		memory.Gate()
	}
	return rep, nil
}

// resolveGoCalls runs the in-process Go VTA resolver. Call only after TS scopes are
// done and gated — avoids overlapping the two largest memory spikes.
func resolveGoCalls(project, root string, files []SourceFile, enc scip.Enclosing, changed map[string]bool) ([]graph.Edge, error) {
	if changed != nil && !changed["go"] {
		return nil, nil
	}
	if !hasGo(files) {
		return nil, nil
	}
	edges, err := gocalls.CallEdges(project, root, enc.Has)
	memory.Gate()
	if err != nil {
		// Best-effort per scope, matching resolveTSCalls: a resolver failure must not
		// abort the whole index — log and continue without Go CALLS for this run.
		log.Printf("codegraph: go calls skipped for %s: %v", root, err)
		return nil, nil
	}
	return edges, nil
}

func hasGo(files []SourceFile) bool {
	for _, f := range files {
		if f.Lang == LangGo {
			return true
		}
	}
	return false
}

// tsconfigDirs finds the repo-relative directories scip-typescript should index,
// one per tsconfig.json. node_modules and hidden dirs are skipped. Monorepos have
// their tsconfigs in subprojects (apps/api, packages/x); a single-package repo
// (e.g. a TS library) has only a root tsconfig — in that case we return [""] to run
// scip at the root, since otherwise such repos would get zero CALLS edges. When
// subprojects exist we use them and skip the root (a root solution-style tsconfig
// would otherwise double-index).
func tsconfigDirs(root string) []string {
	var subDirs []string
	rootHas := false
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if hardIgnoreDir[d.Name()] || (strings.HasPrefix(d.Name(), ".") && d.Name() != ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == "tsconfig.json" {
			rel, _ := filepath.Rel(root, filepath.Dir(path))
			if rel = filepath.ToSlash(rel); rel == "." {
				rootHas = true
			} else {
				subDirs = append(subDirs, rel)
			}
		}
		return nil
	})
	if len(subDirs) == 0 && rootHas {
		return []string{""}
	}
	return subDirs
}
