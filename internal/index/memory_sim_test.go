package index

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/Lordymine/codegraph/internal/graph"
	"github.com/Lordymine/codegraph/internal/memory"
)

// TestMemorySimulation_SyntheticTS compares peak heap of the memory-budget pipeline
// against a RAM-first replica on a synthetic TS corpus (no Go VTA / no scip). This
// isolates the definitions + imports + similar passes — the ones that used to hold
// the whole codebase in RAM at once.
func TestMemorySimulation_SyntheticTS(t *testing.T) {
	dir := t.TempDir()
	const fileCount = 1200
	var sourceBytes int64
	for i := 0; i < fileCount; i++ {
		body := syntheticTSFile(i)
		sourceBytes += int64(len(body))
		path := filepath.Join(dir, fmt.Sprintf("pkg/f%d.ts", i))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	t.Setenv("CODEGRAPH_SKIP_SIMILAR", "1") // test-only: isolate defs+imports; SIMILAR edge volume dwarfs the A/B

	files, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != fileCount {
		t.Fatalf("discovered %d files, want %d", len(files), fileCount)
	}
	project := ProjectName(dir)

	var legacyPeak uint64
	{
		store, err := graph.Open(filepath.Join(dir, "legacy.db"))
		if err != nil {
			t.Fatal(err)
		}
		legacyPeak = memory.PeakHeap(func() {
			if err := runLegacyRAMFirst(store, project, dir, files); err != nil {
				t.Fatal(err)
			}
		})
		store.Close()
	}

	var budgetPeak uint64
	var res Result
	{
		store, err := graph.Open(filepath.Join(dir, "budget.db"))
		if err != nil {
			t.Fatal(err)
		}
		budgetPeak = memory.PeakHeap(func() {
			var err error
			res, err = Run(store, dir)
			if err != nil {
				t.Fatal(err)
			}
		})
		store.Close()
	}

	t.Logf("corpus: %d files, %.2f MB source", fileCount, float64(sourceBytes)/(1024*1024))
	t.Logf("legacy RAM-first peak heap: %.1f MB", float64(legacyPeak)/(1024*1024))
	t.Logf("memory-budget peak heap:     %.1f MB", float64(budgetPeak)/(1024*1024))
	t.Logf("budget index: nodes=%d edges_kept=%d", res.Nodes, res.EdgesKept)
	t.Logf("peak reduction: %.1f%% (legacy/budget=%.2f×)",
		(1-float64(budgetPeak)/float64(legacyPeak))*100, float64(legacyPeak)/float64(budgetPeak))

	if budgetPeak >= legacyPeak {
		t.Fatalf("budget peak (%.1f MB) should be lower than legacy (%.1f MB)",
			float64(budgetPeak)/(1024*1024), float64(legacyPeak)/(1024*1024))
	}
	ratio := float64(legacyPeak) / float64(budgetPeak)
	if ratio < 1.15 {
		t.Fatalf("expected ≥15%% peak reduction; got legacy/budget=%.2f× (legacy=%.1f MB budget=%.1f MB)",
			ratio, float64(legacyPeak)/(1024*1024), float64(budgetPeak)/(1024*1024))
	}
	// Budget peak should not scale with full corpus duplication: cap at legacyImports
	// equivalent (~2× source) plus fixed tree-sitter/runtime overhead (~32 MB).
	overhead := uint64(32 * 1024 * 1024)
	cap := overhead + uint64(sourceBytes*2)
	if budgetPeak > cap {
		t.Fatalf("budget peak %.1f MB exceeds ceiling %.1f MB (32 MB overhead + 2× source)",
			float64(budgetPeak)/(1024*1024), float64(cap)/(1024*1024))
	}
}

// TestMemorySimulation_CodegraphSelf indexes this repo and logs peak heap. It is a
// smoke test for real Go+TS workloads (includes VTA). Fails if peak exceeds 2.5GB —
// generous enough for dev machines but catches runaway growth vs the old design.
func TestMemorySimulation_CodegraphSelf(t *testing.T) {
	if os.Getenv("CODEGRAPH_MEMSIM") == "" {
		t.Skip("set CODEGRAPH_MEMSIM=1 to run the self-index memory simulation")
	}
	root, err := repoRoot()
	if err != nil {
		t.Skip(err)
	}
	dir := t.TempDir()
	store, err := graph.Open(filepath.Join(dir, "self.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	peak := memory.PeakHeap(func() {
		if _, err := Run(store, root); err != nil {
			t.Fatal(err)
		}
	})
	const maxPeak = 2500 * 1024 * 1024 // 2.5 GB heap-inuse ceiling
	t.Logf("codegraph self-index peak heap: %.1f MB", float64(peak)/(1024*1024))
	if peak > maxPeak {
		t.Fatalf("peak %.1f MB exceeds %.1f MB ceiling — memory budget may be insufficient for WSL",
			float64(peak)/(1024*1024), float64(maxPeak)/(1024*1024))
	}
}

// runLegacyRAMFirst reproduces the pre-budget pipeline (accumulate everything, then
// insert once) for A/B peak comparison in tests.
func runLegacyRAMFirst(store *graph.Store, project, root string, files []SourceFile) error {
	if err := store.ReplaceProject(project); err != nil {
		return err
	}
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
	allEdges = append(allEdges, ResolveImports(project, files)...)
	if err := store.InsertNodes(allNodes); err != nil {
		return err
	}
	_, _, err := store.InsertEdges(allEdges)
	return err
}

func repoRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("cannot locate test file")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		return "", fmt.Errorf("go.mod not found at %s", root)
	}
	return root, nil
}
