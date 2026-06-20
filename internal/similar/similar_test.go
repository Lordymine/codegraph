package similar

import (
	"testing"

	"github.com/Lordymine/codegraph/internal/graph"
)

// TestEdges_FindsNearClones pins the SIMILAR_TO pass: LSH surfaces the near-clone
// pair (1 of 10 tokens differs -> Jaccard ~0.78) and EstJaccard >= threshold keeps it,
// while the dissimilar symbol stays isolated. One symmetric edge per pair.
func TestEdges_FindsNearClones(t *testing.T) {
	docs := []Doc{
		{QN: "p:a.go.foo", Tokens: toks("a b c d e f g h i j")},
		{QN: "p:a.go.bar", Tokens: toks("a b c d e f g h i X")}, // near-clone of foo
		{QN: "p:a.go.baz", Tokens: toks("z y w v u t s r q p")}, // unrelated
	}
	edges := Edges("p", docs, 0.7)
	if len(edges) != 1 {
		t.Fatalf("want exactly 1 SIMILAR_TO edge (foo~bar), got %d: %+v", len(edges), edges)
	}
	e := edges[0]
	if e.Type != graph.EdgeSimilarTo {
		t.Errorf("edge type = %q, want SIMILAR_TO", e.Type)
	}
	ends := map[string]bool{e.SourceQN: true, e.TargetQN: true}
	if !ends["p:a.go.foo"] || !ends["p:a.go.bar"] {
		t.Errorf("edge should connect foo and bar, got %s -> %s", e.SourceQN, e.TargetQN)
	}
}
