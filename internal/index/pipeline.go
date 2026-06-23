package index

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/Lordymine/codegraph/internal/graph"
	"github.com/Lordymine/codegraph/internal/memory"
	"github.com/Lordymine/codegraph/internal/scip"
)

// Result summarizes an indexing run.
type Result struct {
	Project       string
	Files         int
	Nodes         int
	EdgesKept     int
	EdgesDropped  int
	Reused        bool // nothing changed since the last index; the pipeline was skipped
	ScipScopes    int
	ScipPeakRSS   uint64
	ScipHeapCapMB int
}

// BuildingSuffix is the suffix RunAtomic uses for in-progress index files.
const BuildingSuffix = ".building"

// Run indexes root into an already-open store (tests/temp dirs). Prefer RunAtomic
// for CLI/MCP so a failed re-index does not wipe the previous graph.
func Run(store *graph.Store, root string) (Result, error) {
	in, reused, err := prepareIndexing(store, root)
	if err != nil {
		return Result{}, err
	}
	if reused != nil {
		return *reused, nil
	}
	reuseFrom, err := graph.Open(store.DBPath())
	if err != nil {
		return Result{}, fmt.Errorf("open reuse store: %w", err)
	}
	defer reuseFrom.Close()
	if err := reuseFrom.BeginReadSnapshot(); err != nil {
		return Result{}, fmt.Errorf("begin read snapshot: %w", err)
	}
	defer reuseFrom.EndReadSnapshot()
	in.reuseFrom = reuseFrom
	return runPipeline(store, in)
}

// RunAtomic builds into dbPath+BuildingSuffix and renames on success, leaving the
// previous graph at dbPath intact when indexing fails. Do not run `codegraph index`
// on the same repo while an MCP server is auto-indexing it — both use this path and
// can race on the store file.
func RunAtomic(dbPath, root string) (Result, error) {
	main, err := graph.Open(dbPath)
	if err != nil {
		return Result{}, err
	}
	in, reused, err := prepareIndexing(main, root)
	if err != nil {
		main.Close()
		return Result{}, err
	}
	if reused != nil {
		main.Close()
		return *reused, nil
	}
	in.reuseFrom = main

	building := dbPath + BuildingSuffix
	_ = os.Remove(building)
	store, err := graph.Open(building)
	if err != nil {
		main.Close()
		return Result{}, err
	}
	res, err := runPipeline(store, in)
	store.Close()
	main.Close()
	if err != nil {
		_ = os.Remove(building)
		return res, err
	}
	if err := commitBuiltIndex(building, dbPath); err != nil {
		_ = os.Remove(building)
		return res, fmt.Errorf("commit index: %w", err)
	}
	return res, nil
}

func commitBuiltIndex(building, dbPath string) error {
	for _, suffix := range []string{"-wal", "-shm"} {
		_ = os.Remove(dbPath + suffix)
	}
	if err := os.Remove(dbPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(building, dbPath); err != nil {
		return err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		_ = os.Remove(building + suffix)
	}
	return nil
}

func runPipeline(store *graph.Store, in pipelineInput) (Result, error) {
	if err := pipelinePreflight(); err != nil {
		return Result{}, err
	}
	files, err := Discover(in.root)
	if err != nil {
		return Result{}, err
	}

	if err := store.ReplaceProject(in.project); err != nil {
		return Result{}, err
	}

	nodeCount, defEdges, err := indexDefinitionsBatched(store, in.project, files)
	if err != nil {
		return Result{}, err
	}
	k, d, err := store.InsertEdges(defEdges)
	if err != nil {
		return Result{}, fmt.Errorf("insert defines edges: %w", err)
	}
	edgesKept, edgesDropped := k, d
	defEdges = nil
	memory.Gate()

	importEdges, err := collectImportsStreaming(in.project, files)
	if err != nil {
		return Result{}, err
	}
	k, d, err = store.InsertEdges(importEdges)
	if err != nil {
		return Result{}, err
	}
	edgesKept += k
	edgesDropped += d
	importEdges = nil
	memory.Gate()

	spans, err := store.FunctionSpans(in.project)
	if err != nil {
		return Result{}, err
	}
	enc := scip.BuildEnclosingFromSpans(spans)

	scipRep, err := resolveTSCalls(store, in.project, in.root, enc, in.changed)
	if err != nil {
		return Result{}, fmt.Errorf("ts calls: %w", err)
	}
	memory.Gate()

	goEdges, err := resolveGoCalls(in.project, in.root, files, enc, in.changed)
	if err != nil {
		return Result{}, fmt.Errorf("go calls: %w", err)
	}
	k, d, err = store.InsertEdges(goEdges)
	if err != nil {
		return Result{}, fmt.Errorf("insert go call edges: %w", err)
	}
	edgesKept += k
	edgesDropped += d
	goEdges = nil
	memory.Gate()

	if in.reuseFrom != nil {
		k, d, err = insertReusedCallEdges(store, in.reuseFrom, in.project, in.changed, in.tsdirs)
		if err != nil {
			return Result{}, fmt.Errorf("insert reused call edges: %w", err)
		}
		edgesKept += k
		edgesDropped += d
	}
	enc = nil
	memory.Gate()

	if !memory.SkipSimilar() {
		simEdges, err := resolveSimilarFromSpans(in.project, in.root, spans)
		if err != nil {
			return Result{}, err
		}
		k, d, err = store.InsertEdges(simEdges)
		if err != nil {
			return Result{}, fmt.Errorf("insert similar edges: %w", err)
		}
		edgesKept += k
		edgesDropped += d
	}
	spans = nil
	memory.Gate()

	return Result{
		Project: in.project, Files: len(files), Nodes: nodeCount,
		EdgesKept: edgesKept, EdgesDropped: edgesDropped,
		ScipScopes: scipRep.ScopesRun, ScipPeakRSS: scipRep.PeakRSS, ScipHeapCapMB: scipRep.HeapCapMB,
	}, nil
}

// indexDefinitionsBatched extracts definitions with bounded parallelism, flushes
// nodes to SQLite per batch, and returns DEFINES edges to insert in one shot (edges
// are tiny vs nodes — holding them all is cheap; reloading idByQN per batch is not).
func indexDefinitionsBatched(store *graph.Store, project string, files []SourceFile) (nodes int, defEdges []graph.Edge, err error) {
	workers := memory.MaxWorkers()
	batchSize := memory.BatchSize()

	for start := 0; start < len(files); start += batchSize {
		end := start + batchSize
		if end > len(files) {
			end = len(files)
		}
		batch := files[start:end]

		type out struct {
			nodes []graph.Node
			edges []graph.Edge
		}
		results := make([]out, len(batch))
		sem := make(chan struct{}, workers)
		var wg sync.WaitGroup
		for i, f := range batch {
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

		var batchNodes []graph.Node
		var batchEdges []graph.Edge
		for _, r := range results {
			batchNodes = append(batchNodes, r.nodes...)
			batchEdges = append(batchEdges, r.edges...)
		}
		results = nil
		if err := store.InsertNodes(batchNodes); err != nil {
			return nodes, defEdges, fmt.Errorf("insert nodes: %w", err)
		}
		nodes += len(batchNodes)
		defEdges = append(defEdges, batchEdges...)
		batchNodes, batchEdges = nil, nil
		memory.Gate()
	}
	return nodes, defEdges, nil
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
