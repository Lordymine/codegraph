package query

import (
	"path/filepath"
	"testing"

	"github.com/Lordymine/codegraph/internal/graph"
)

// TestEngine_DeadCode_FlagsUnusedPrivateOnly pins the dead-code hint: a
// Function/Method with zero inbound CALLS is flagged ONLY when it is not an entry
// point. Because the graph drops external/dynamic calls by design, the hint stays
// high-precision by excluding things that legitimately have no in-graph caller:
// exported symbols (public API / called from outside), decorated members
// (framework-invoked), main/init, and test functions. Of the fixture, only the
// private, uncalled, non-test `helper` is dead.
func TestEngine_DeadCode_FlagsUnusedPrivateOnly(t *testing.T) {
	store, err := graph.Open(filepath.Join(t.TempDir(), "g.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	const project = "proj"
	node := func(name, file string, label graph.NodeLabel, props map[string]any) graph.Node {
		return graph.Node{
			Project: project, Label: label, Name: name,
			QualifiedName: project + ":" + file + "." + name, FilePath: file,
			StartLine: 1, EndLine: 2, Props: props,
		}
	}
	priv := map[string]any{"is_exported": false}
	if err := store.InsertNodes([]graph.Node{
		node("helper", "a.go", graph.LabelFunction, priv),                                            // dead: private, uncalled
		node("used", "a.go", graph.LabelFunction, priv),                                              // has an inbound CALLS
		node("Exported", "a.go", graph.LabelFunction, map[string]any{"is_exported": true}),           // public API
		node("main", "a.go", graph.LabelFunction, priv),                                              // entry point
		node("TestThing", "a_test.go", graph.LabelFunction, priv),                                    // test runner entry
		node("handler", "ctrl.ts", graph.LabelMethod, map[string]any{"decorators": []string{"Get"}}), // framework-invoked
	}); err != nil {
		t.Fatal(err)
	}
	// main calls used -> `used` has an inbound CALLS (not dead); main itself stays
	// uncalled but is excluded by name.
	if _, _, err := store.InsertEdges([]graph.Edge{{
		Project: project, SourceQN: project + ":a.go.main", TargetQN: project + ":a.go.used", Type: graph.EdgeCalls,
	}}); err != nil {
		t.Fatal(err)
	}

	eng := NewEngine(store, project, t.TempDir())
	refs, err := eng.DeadCode(50)
	if err != nil {
		t.Fatalf("DeadCode: %v", err)
	}
	got := map[string]bool{}
	for _, r := range refs {
		got[r.Name] = true
	}
	if !got["helper"] {
		t.Errorf("helper is private and uncalled; expected it flagged, got %v", got)
	}
	for _, excluded := range []string{"used", "Exported", "main", "TestThing", "handler"} {
		if got[excluded] {
			t.Errorf("%s is an entry point or is called; must not be flagged dead, got %v", excluded, got)
		}
	}
}

// TestEngine_DeadCode_SelfCallDoesNotKeepAlive pins that a recursive call does not
// rescue a function from the dead-code hint: a private function whose ONLY inbound
// CALLS edge is its own recursion is still unreachable from the rest of the repo,
// so it must still be flagged. (Self-edges are kept in the graph for callers, but
// they don't count as "someone else calls it".)
func TestEngine_DeadCode_SelfCallDoesNotKeepAlive(t *testing.T) {
	store, err := graph.Open(filepath.Join(t.TempDir(), "g.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	const project = "proj"
	if err := store.InsertNodes([]graph.Node{{
		Project: project, Label: graph.LabelFunction, Name: "loop",
		QualifiedName: project + ":a.go.loop", FilePath: "a.go", StartLine: 1, EndLine: 2,
		Props: map[string]any{"is_exported": false},
	}}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.InsertEdges([]graph.Edge{{
		Project: project, SourceQN: project + ":a.go.loop", TargetQN: project + ":a.go.loop", Type: graph.EdgeCalls,
	}}); err != nil {
		t.Fatal(err)
	}

	eng := NewEngine(store, project, t.TempDir())
	refs, err := eng.DeadCode(50)
	if err != nil {
		t.Fatalf("DeadCode: %v", err)
	}
	found := false
	for _, r := range refs {
		if r.Name == "loop" {
			found = true
		}
	}
	if !found {
		t.Errorf("a only-self-recursive private function is still dead; expected loop flagged, got %v", refs)
	}
}
