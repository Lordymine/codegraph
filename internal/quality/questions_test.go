package quality

import (
	"fmt"
	"testing"

	"github.com/Lordymine/codegraph/internal/graph"
)

func mkNodes(n int) []graph.Node {
	out := make([]graph.Node, n)
	for i := range out {
		out[i] = graph.Node{QualifiedName: fmt.Sprintf("q%03d", i)}
	}
	return out
}

func TestStratifiedSampleSpansDistribution(t *testing.T) {
	in := mkNodes(100)
	got := stratifiedSample(in, 6)
	if len(got) != 6 {
		t.Fatalf("want 6, got %d", len(got))
	}
	// Must include the very top (index 0) and the very bottom (index 99) — i.e.
	// a hub AND a leaf, not just the top of the ranking.
	if got[0].QualifiedName != "q000" {
		t.Fatalf("first should be the top hub, got %q", got[0].QualifiedName)
	}
	if got[len(got)-1].QualifiedName != "q099" {
		t.Fatalf("last should be the bottom (leaf), got %q", got[len(got)-1].QualifiedName)
	}
	// Strictly increasing indices (spread, no duplicates).
	for i := 1; i < len(got); i++ {
		if got[i].QualifiedName <= got[i-1].QualifiedName {
			t.Fatalf("not strictly spread at %d: %q after %q", i, got[i].QualifiedName, got[i-1].QualifiedName)
		}
	}
}

func TestStratifiedSampleEdgeCases(t *testing.T) {
	if stratifiedSample(nil, 6) != nil {
		t.Fatal("nil input -> nil")
	}
	if got := stratifiedSample(mkNodes(3), 6); len(got) != 3 {
		t.Fatalf("fewer than n -> all, got %d", len(got))
	}
	if got := stratifiedSample(mkNodes(10), 0); got != nil {
		t.Fatalf("n=0 -> nil, got %d", len(got))
	}
}
