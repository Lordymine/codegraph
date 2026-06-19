package index

import (
	"os"

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
