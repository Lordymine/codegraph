package index

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Lordymine/codegraph/internal/graph"
	"github.com/Lordymine/codegraph/internal/memory"
)

// Stress tests exercise the production memory-budget pipeline (not a simulation).
// They run when CODEGRAPH_STRESS=1 or when `go test` is invoked without -short.
// Peak heap is compared to a host-aware ceiling so regressions blow up in CI/local.

func TestStress_TS_LargeCorpus(t *testing.T) {
	if testing.Short() && !stressEnabled() {
		t.Skip("skipped in -short mode (set CODEGRAPH_STRESS=1 to force)")
	}

	const fileCount = 1500
	dir, sourceBytes := writeSyntheticTSCorpus(t, fileCount)
	t.Setenv("CODEGRAPH_SKIP_SIMILAR", "1") // SIMILAR edge volume is orthogonal to RAM shape

	store, err := graph.Open(filepath.Join(dir, "stress.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	var res Result
	peak := memory.PeakHeap(func() {
		var err error
		res, err = Run(store, dir)
		if err != nil {
			t.Fatal(err)
		}
	})

	ceiling := stressHeapCeiling(memory.HostRAMBytes(), "ts")
	if ceiling < 128*1024*1024 {
		ceiling = 128 * 1024 * 1024
	}
	// TS-only (no tsconfig → no scip): bound relative to corpus + fixed overhead.
	if rel := uint64(sourceBytes*3) + 64*1024*1024; rel < ceiling {
		ceiling = rel
	}

	t.Logf("TS stress: %d files, %.2f MB source, %d nodes, %d edges",
		fileCount, float64(sourceBytes)/(1024*1024), res.Nodes, res.EdgesKept)
	t.Logf("TS stress peak heap: %.1f MB (ceiling %.1f MB)",
		float64(peak)/(1024*1024), float64(ceiling)/(1024*1024))

	if peak > ceiling {
		t.Fatalf("TS stress peak %.1f MB exceeds ceiling %.1f MB",
			float64(peak)/(1024*1024), float64(ceiling)/(1024*1024))
	}
	if res.Nodes < fileCount {
		t.Fatalf("expected at least %d nodes, got %d", fileCount, res.Nodes)
	}
}

func TestStress_Go_VTA_LargeModule(t *testing.T) {
	if testing.Short() && !stressEnabled() {
		t.Skip("skipped in -short mode (set CODEGRAPH_STRESS=1 to force)")
	}

	const nFiles = 280
	dir, sourceBytes := writeSyntheticGoModule(t, nFiles)

	store, err := graph.Open(filepath.Join(dir, "stress.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	var res Result
	peak := memory.PeakHeap(func() {
		var err error
		res, err = Run(store, dir)
		if err != nil {
			t.Fatal(err)
		}
	})

	ceiling := stressHeapCeiling(memory.HostRAMBytes(), "go")
	t.Logf("Go stress: %d files, %.2f MB source, %d nodes, %d edges",
		nFiles, float64(sourceBytes)/(1024*1024), res.Nodes, res.EdgesKept)
	t.Logf("Go stress peak heap: %.1f MB (ceiling %.1f MB), profile=%+v",
		float64(peak)/(1024*1024), float64(ceiling)/(1024*1024), memory.ActiveProfile())

	if peak > ceiling {
		t.Fatalf("Go VTA stress peak %.1f MB exceeds ceiling %.1f MB",
			float64(peak)/(1024*1024), float64(ceiling)/(1024*1024))
	}
	if res.Nodes < nFiles {
		t.Fatalf("expected at least %d nodes, got %d", nFiles, res.Nodes)
	}
	// VTA must have produced some CALLS edges in a connected module.
	if res.EdgesKept < nFiles {
		t.Fatalf("expected meaningful CALLS/DEFINES volume, edges_kept=%d", res.EdgesKept)
	}
}

func TestStress_CodegraphSelf_GoAndTS(t *testing.T) {
	if !stressEnabled() {
		t.Skip("set CODEGRAPH_STRESS=1 to index the codegraph repo (Go VTA + real workload)")
	}
	root, err := repoRoot()
	if err != nil {
		t.Skip(err)
	}

	store, err := graph.Open(filepath.Join(t.TempDir(), "self.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	var res Result
	peak := memory.PeakHeap(func() {
		var err error
		res, err = Run(store, root)
		if err != nil {
			t.Fatal(err)
		}
	})

	ceiling := stressHeapCeiling(memory.HostRAMBytes(), "go")
	t.Logf("self-index: %d files, %d nodes, %d edges, scip_scopes=%d scip_peak_rss=%d MB",
		res.Files, res.Nodes, res.EdgesKept, res.ScipScopes, res.ScipPeakRSS/(1024*1024))
	t.Logf("self-index peak heap: %.1f MB (ceiling %.1f MB)",
		float64(peak)/(1024*1024), float64(ceiling)/(1024*1024))

	if peak > ceiling {
		t.Fatalf("self-index peak %.1f MB exceeds ceiling %.1f MB",
			float64(peak)/(1024*1024), float64(ceiling)/(1024*1024))
	}
	if res.Nodes < 50 {
		t.Fatalf("self-index suspiciously small: nodes=%d", res.Nodes)
	}
}

// TestStress_TS_WithScip runs only when a real TS project is available via
// CODEGRAPH_STRESS_TS_ROOT — otherwise skipped. Exercises scip-typescript + Node cap.
func TestStress_TS_WithScip(t *testing.T) {
	root := os.Getenv("CODEGRAPH_STRESS_TS_ROOT")
	if root == "" {
		t.Skip("set CODEGRAPH_STRESS_TS_ROOT to a repo with tsconfig.json + node_modules")
	}
	if _, err := os.Stat(filepath.Join(root, "tsconfig.json")); err != nil {
		t.Skip("CODEGRAPH_STRESS_TS_ROOT has no tsconfig.json")
	}

	store, err := graph.Open(filepath.Join(t.TempDir(), "ts-scip.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	var res Result
	peak := memory.PeakHeap(func() {
		var err error
		res, err = Run(store, root)
		if err != nil {
			t.Fatal(err)
		}
	})

	ceiling := stressHeapCeiling(memory.HostRAMBytes(), "go") // scip+VTA may both run
	t.Logf("TS+scip stress: files=%d nodes=%d edges=%d scip_scopes=%d heap_cap=%d MB peak_rss=%d MB",
		res.Files, res.Nodes, res.EdgesKept, res.ScipScopes, res.ScipHeapCapMB, res.ScipPeakRSS/(1024*1024))
	t.Logf("TS+scip peak heap: %.1f MB (ceiling %.1f MB)", float64(peak)/(1024*1024), float64(ceiling)/(1024*1024))

	if peak > ceiling {
		t.Fatalf("TS+scip peak %.1f MB exceeds ceiling %.1f MB",
			float64(peak)/(1024*1024), float64(ceiling)/(1024*1024))
	}
	if res.ScipScopes == 0 {
		t.Fatal("expected at least one scip-typescript scope")
	}
}
