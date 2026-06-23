package index

import (
	"path/filepath"

	"github.com/Lordymine/codegraph/internal/graph"
)

// pipelineInput carries incremental state shared by Run and RunAtomic.
type pipelineInput struct {
	project   string
	root      string
	changed   map[string]bool
	tsdirs    []string
	reuseFrom *graph.Store // pre-reindex DB; CALLS streamed via insertReusedCallEdges
}

// prepareIndexing detects changes and builds pipelineInput. When the repo is
// unchanged and already indexed, reused is non-nil and input is nil.
func prepareIndexing(store *graph.Store, root string) (input pipelineInput, reused *Result, err error) {
	root, _ = filepath.Abs(root)
	project := ProjectName(root)

	changes, err := DetectChanges(store, project, root)
	if err != nil {
		return pipelineInput{}, nil, err
	}
	if !changes.Any() {
		if n, e, err := store.Stats(project); err == nil && n > 0 {
			files, _ := store.FileHashes(project)
			return pipelineInput{}, &Result{
				Project: project, Files: len(files), Nodes: n, EdgesKept: e, Reused: true,
			}, nil
		}
	}

	tsdirs := tsconfigDirs(root)
	changed := changedScopes(changes, tsdirs)
	return pipelineInput{
		project: project, root: root, changed: changed, tsdirs: tsdirs,
	}, nil, nil
}
