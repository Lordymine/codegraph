package query

import (
	"path/filepath"
	"testing"

	"github.com/Lordymine/codegraph/internal/graph"
)

// TestEngine_Similar_FiltersToSimilarToBothDirections pins the `similar` query:
// it returns a symbol's near-clones (SIMILAR_TO edges) in BOTH directions — the
// edge is stored once as smaller-QN -> larger-QN, so a clone can sit on either
// side — and it must NOT leak other edge kinds. The fixture gives alpha one
// outbound clone (beta), one inbound clone (gamma), and a CALLS neighbor (delta);
// Similar(alpha) must be {beta, gamma}, never delta.
func TestEngine_Similar_FiltersToSimilarToBothDirections(t *testing.T) {
	store, err := graph.Open(filepath.Join(t.TempDir(), "g.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	const project = "proj"
	fn := func(name string) graph.Node {
		return graph.Node{
			Project: project, Label: graph.LabelFunction, Name: name,
			QualifiedName: project + ":a.ts." + name, FilePath: "a.ts", StartLine: 1, EndLine: 2,
		}
	}
	if err := store.InsertNodes([]graph.Node{fn("alpha"), fn("beta"), fn("gamma"), fn("delta")}); err != nil {
		t.Fatal(err)
	}
	edge := func(src, tgt string, typ graph.EdgeType) graph.Edge {
		return graph.Edge{Project: project, SourceQN: project + ":a.ts." + src, TargetQN: project + ":a.ts." + tgt, Type: typ}
	}
	if _, _, err := store.InsertEdges([]graph.Edge{
		edge("alpha", "beta", graph.EdgeSimilarTo),  // outbound clone
		edge("gamma", "alpha", graph.EdgeSimilarTo), // inbound clone
		edge("alpha", "delta", graph.EdgeCalls),     // must be filtered out
	}); err != nil {
		t.Fatal(err)
	}

	eng := NewEngine(store, project, t.TempDir())
	refs, err := eng.Similar("a.ts.alpha", 10)
	if err != nil {
		t.Fatalf("Similar: %v", err)
	}
	got := map[string]bool{}
	for _, r := range refs {
		got[r.Name] = true
	}
	if !got["beta"] || !got["gamma"] {
		t.Errorf("expected both clones beta and gamma, got %v", got)
	}
	if got["delta"] {
		t.Errorf("delta is a CALLS neighbor, not a clone; SIMILAR_TO filter leaked it: %v", got)
	}
}
