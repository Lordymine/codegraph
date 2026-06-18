package index

import (
	"fmt"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/Lordymine/codegraph/internal/graph"
)

// Result summarizes an indexing run.
type Result struct {
	Project      string
	Files        int
	Nodes        int
	EdgesKept    int
	EdgesDropped int
}

// Run indexes root into store under a derived project name. The definitions
// pass runs in parallel across files (one of the cheap wins of the RAM-first
// design); call resolution is a TODO (see calls.go).
func Run(store *graph.Store, root string) (Result, error) {
	root, _ = filepath.Abs(root)
	project := ProjectName(root)

	files, err := Discover(root)
	if err != nil {
		return Result{}, err
	}

	if err := store.ReplaceProject(project); err != nil {
		return Result{}, err
	}

	// Parallel definitions pass.
	type out struct {
		nodes []graph.Node
		edges []graph.Edge
	}
	results := make([]out, len(files))
	sem := make(chan struct{}, runtime.NumCPU())
	var wg sync.WaitGroup
	for i, f := range files {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, f SourceFile) {
			defer wg.Done()
			defer func() { <-sem }()
			n, e := ExtractDefinitions(project, f)
			results[i] = out{n, e}
		}(i, f)
	}
	wg.Wait()

	var allNodes []graph.Node
	var allEdges []graph.Edge
	for _, r := range results {
		allNodes = append(allNodes, r.nodes...)
		allEdges = append(allEdges, r.edges...)
	}

	// IMPORTS edges (TS/JS) + CALLS edges (scip-typescript per subproject).
	allEdges = append(allEdges, ResolveImports(project, files)...)
	allEdges = append(allEdges, ResolveCalls(project, root, files, allNodes)...)

	if err := store.InsertNodes(allNodes); err != nil {
		return Result{}, fmt.Errorf("insert nodes: %w", err)
	}
	kept, dropped, err := store.InsertEdges(allEdges)
	if err != nil {
		return Result{}, fmt.Errorf("insert edges: %w", err)
	}

	return Result{
		Project: project, Files: len(files), Nodes: len(allNodes),
		EdgesKept: kept, EdgesDropped: dropped,
	}, nil
}

// ProjectName derives a stable project key from the repo root (matches the
// upstream convention of slugging the absolute path).
func ProjectName(root string) string {
	slug := filepath.ToSlash(root)
	repl := func(r rune) rune {
		switch r {
		case '/', ':', '\\', ' ':
			return '-'
		}
		return r
	}
	out := []rune{}
	for _, r := range slug {
		out = append(out, repl(r))
	}
	s := string(out)
	for len(s) > 0 && s[0] == '-' {
		s = s[1:]
	}
	return s
}
