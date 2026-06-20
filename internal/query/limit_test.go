package query

import (
	"path/filepath"
	"strconv"
	"testing"

	"github.com/Lordymine/codegraph/internal/graph"
)

// TestEngine_Callers_DefaultLimitDoesNotTruncateHubs pins that the default limit
// (limit=0) does not silently cap an exhaustive relationship answer at 50. callers
// is meant to be complete — truncating a hub like gh-cli's iostreams.Test (448
// callers) at 50 turns a recall ceiling into a wrong answer. A 60-caller symbol
// must come back whole by default.
func TestEngine_Callers_DefaultLimitDoesNotTruncateHubs(t *testing.T) {
	store, err := graph.Open(filepath.Join(t.TempDir(), "g.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	const project = "proj"
	const n = 60
	nodes := []graph.Node{{
		Project: project, Label: graph.LabelFunction, Name: "hub",
		QualifiedName: project + ":a.go.hub", FilePath: "a.go", StartLine: 1, EndLine: 2,
	}}
	var edges []graph.Edge
	for i := range n {
		name := "c" + strconv.Itoa(i)
		nodes = append(nodes, graph.Node{
			Project: project, Label: graph.LabelFunction, Name: name,
			QualifiedName: project + ":a.go." + name, FilePath: "a.go", StartLine: 10 + i, EndLine: 11 + i,
		})
		edges = append(edges, graph.Edge{
			Project: project, SourceQN: project + ":a.go." + name, TargetQN: project + ":a.go.hub", Type: graph.EdgeCalls,
		})
	}
	if err := store.InsertNodes(nodes); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.InsertEdges(edges); err != nil {
		t.Fatal(err)
	}

	eng := NewEngine(store, project, t.TempDir())
	refs, err := eng.Callers("a.go.hub", 0) // 0 = use the default limit
	if err != nil {
		t.Fatalf("Callers: %v", err)
	}
	if len(refs) != n {
		t.Errorf("default limit truncated an exhaustive answer: got %d callers, want %d", len(refs), n)
	}
}
