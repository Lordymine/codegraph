package index

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/Lordymine/codegraph/internal/gocalls"
	"github.com/Lordymine/codegraph/internal/graph"
	"github.com/Lordymine/codegraph/internal/scip"
)

// ResolveCalls emits CALLS edges. For TS/JS it delegates to scip-typescript
// (batch, type-checker-accurate) per tsconfig subproject and attributes each
// resolved reference to the function/method that encloses it (the caller). It is
// best-effort: a subproject whose scip run fails contributes no edges rather than
// failing the whole index. Go call resolution (go/packages + callgraph) is TODO.
func ResolveCalls(project, root string, files []SourceFile, nodes []graph.Node) []graph.Edge {
	enc := scip.BuildEnclosing(nodes)
	var edges []graph.Edge

	// TS/JS: scip-typescript per tsconfig subproject. dir is repo-relative; "" means
	// the repo root (a single-package repo whose only tsconfig is at the top).
	for _, dir := range tsconfigDirs(root) {
		abs := filepath.Join(root, filepath.FromSlash(dir))
		name := dir
		if name == "" {
			name = "root"
		}
		out := filepath.Join(os.TempDir(), "codegraph-"+strings.ReplaceAll(name, "/", "-")+".scip")
		idx, err := scip.RunAndRead(abs, out)
		if err != nil {
			continue // best-effort per subproject
		}
		edges = append(edges, scip.CallEdges(idx, project, dir, enc)...)
	}

	// Go: in-process go/packages + CHA call graph.
	if hasGo(files) {
		if goEdges, err := gocalls.CallEdges(project, root, enc.Has); err == nil {
			edges = append(edges, goEdges...)
		}
	}
	return edges
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
		return []string{""} // single-package repo: index at the root
	}
	return subDirs
}
