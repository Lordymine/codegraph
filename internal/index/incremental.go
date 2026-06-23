package index

import (
	"os"
	"strings"

	"github.com/Lordymine/codegraph/internal/graph"
)

// Changes is the set of files that differ from the indexed snapshot.
type Changes struct {
	Changed []string // indexed, but content hash differs now
	Added   []string // on disk, absent from the index
	Deleted []string // in the index, gone from disk
}

// Any reports whether anything changed since the last index.
func (c Changes) Any() bool {
	return len(c.Changed)+len(c.Added)+len(c.Deleted) > 0
}

// Summary renders the change set as the compact wire format the detect_changes tool
// returns: one `status<TAB>path` line per file (changed, then added, then deleted).
func (c Changes) Summary() string {
	var b strings.Builder
	for _, p := range c.Changed {
		b.WriteString("changed\t" + p + "\n")
	}
	for _, p := range c.Added {
		b.WriteString("added\t" + p + "\n")
	}
	for _, p := range c.Deleted {
		b.WriteString("deleted\t" + p + "\n")
	}
	return b.String()
}

// DetectChanges compares the source files currently under root against the per-file
// content hashes recorded at the last index (Store.FileHashes). It is the basis for
// skipping a re-index when nothing changed, and later for re-resolving only the
// scopes whose files moved. A never-indexed project reports every file as Added.
func DetectChanges(store *graph.Store, project, root string) (Changes, error) {
	files, err := Discover(root)
	if err != nil {
		return Changes{}, err
	}
	stored, err := store.FileHashes(project)
	if err != nil {
		return Changes{}, err
	}

	var ch Changes
	seen := make(map[string]bool, len(files))
	for _, f := range files {
		seen[f.RelPath] = true
		data, err := os.ReadFile(f.AbsPath)
		if err != nil {
			continue // unreadable now: leave it to the next pass
		}
		switch prev, ok := stored[f.RelPath]; {
		case !ok:
			ch.Added = append(ch.Added, f.RelPath)
		case prev != hashBytes(data):
			ch.Changed = append(ch.Changed, f.RelPath)
		}
	}
	for path := range stored {
		if !seen[path] {
			ch.Deleted = append(ch.Deleted, path)
		}
	}
	return ch, nil
}

// scopeOf returns the CALLS scope a repo-relative file belongs to. Go files share
// the one "go" scope (go/packages + VTA is whole-module); a TS/JS file belongs to
// the tsconfig-project directory that most tightly encloses it, or "" (the repo-root
// scip run) when no subproject does. Scopes are the unit of incremental re-resolution.
func scopeOf(rel string, tsconfigDirs []string) string {
	if strings.HasSuffix(rel, ".go") {
		return "go"
	}
	best, bestLen := "", -1
	for _, d := range tsconfigDirs {
		if d != "" && (rel == d || strings.HasPrefix(rel, d+"/")) && len(d) > bestLen {
			best, bestLen = d, len(d)
		}
	}
	return best
}

const reusedCallEdgeBatch = 2048

// forEachReusableCallEdge invokes fn for each stored CALLS edge whose caller scope
// is not being re-resolved. source must still hold the pre-reindex graph.
func forEachReusableCallEdge(source *graph.Store, project string, changed map[string]bool, tsconfigDirs []string, fn func(graph.Edge) error) error {
	return source.ForEachCallEdge(project, func(e graph.CallEdge) error {
		if changed[scopeOf(e.SourceFile, tsconfigDirs)] {
			return nil
		}
		return fn(graph.Edge{
			Project: project, SourceQN: e.SourceQN, TargetQN: e.TargetQN,
			Type: graph.EdgeCalls, Props: e.Props,
		})
	})
}

// insertReusedCallEdges streams reusable CALLS edges from source into target in batches.
// source must hold a pre-wipe graph snapshot (second connection + BeginReadSnapshot for
// Run, or the main store file for RunAtomic).
func insertReusedCallEdges(target, source *graph.Store, project string, changed map[string]bool, tsconfigDirs []string) (inserted, dropped int, err error) {
	var batch []graph.Edge
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		k, d, e := target.InsertEdges(batch)
		inserted += k
		dropped += d
		batch = batch[:0]
		return e
	}
	err = forEachReusableCallEdge(source, project, changed, tsconfigDirs, func(e graph.Edge) error {
		batch = append(batch, e)
		if len(batch) >= reusedCallEdgeBatch {
			return flush()
		}
		return nil
	})
	if err != nil {
		return inserted, dropped, err
	}
	if err := flush(); err != nil {
		return inserted, dropped, err
	}
	return inserted, dropped, nil
}

// changedScopes is the set of CALLS scopes touched by a change set — exactly the
// scopes whose resolver must re-run. A scope absent from the result has no changed
// file and reuses its stored edges.
func changedScopes(ch Changes, tsconfigDirs []string) map[string]bool {
	out := map[string]bool{}
	for _, group := range [][]string{ch.Changed, ch.Added, ch.Deleted} {
		for _, rel := range group {
			out[scopeOf(rel, tsconfigDirs)] = true
		}
	}
	return out
}
