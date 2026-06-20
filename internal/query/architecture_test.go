package query

import (
	"path/filepath"
	"testing"

	"github.com/Lordymine/codegraph/internal/graph"
)

// TestEngine_Architecture pins the repo map: from graph aggregates it reports
// languages (by File node), node/edge counts, top packages (by symbols per dir),
// and the two hotspot rankings — by cyclomatic complexity (the M4 data) and by
// inbound CALLS (call hubs).
func TestEngine_Architecture(t *testing.T) {
	store, err := graph.Open(filepath.Join(t.TempDir(), "g.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	const p = "proj"
	file := func(path, lang string) graph.Node {
		return graph.Node{Project: p, Label: graph.LabelFile, Name: path, QualifiedName: p + ":" + path,
			FilePath: path, Props: map[string]any{"lang": lang}}
	}
	fn := func(name, path string, cx int) graph.Node {
		return graph.Node{Project: p, Label: graph.LabelFunction, Name: name, QualifiedName: p + ":" + path + "." + name,
			FilePath: path, StartLine: 1, EndLine: 2, Props: map[string]any{"complexity": cx}}
	}
	if err := store.InsertNodes([]graph.Node{
		file("a.go", "go"), file("b.ts", "ts"), file("internal/util.go", "go"),
		fn("simple", "a.go", 1), fn("branchy", "a.go", 5), fn("hub", "b.ts", 2),
		fn("helper", "internal/util.go", 1),
	}); err != nil {
		t.Fatal(err)
	}
	// simple and branchy both call hub → hub is the call hub (2 inbound).
	if _, _, err := store.InsertEdges([]graph.Edge{
		{Project: p, SourceQN: p + ":a.go.simple", TargetQN: p + ":b.ts.hub", Type: graph.EdgeCalls},
		{Project: p, SourceQN: p + ":a.go.branchy", TargetQN: p + ":b.ts.hub", Type: graph.EdgeCalls},
	}); err != nil {
		t.Fatal(err)
	}

	eng := NewEngine(store, p, t.TempDir())
	arch, err := eng.Architecture(10)
	if err != nil {
		t.Fatalf("Architecture: %v", err)
	}

	if arch.Languages["go"] != 2 || arch.Languages["ts"] != 1 {
		t.Errorf("languages = %v, want go=2 ts=1", arch.Languages)
	}
	if arch.NodeCounts["Function"] != 4 || arch.NodeCounts["File"] != 3 {
		t.Errorf("node counts = %v, want Function=4 File=3", arch.NodeCounts)
	}
	if arch.EdgeCounts["CALLS"] != 2 {
		t.Errorf("edge counts = %v, want CALLS=2", arch.EdgeCounts)
	}
	if len(arch.ComplexityHotspots) == 0 || arch.ComplexityHotspots[0].Ref.Name != "branchy" || arch.ComplexityHotspots[0].Metric != 5 {
		t.Errorf("top complexity hotspot = %+v, want branchy/5", arch.ComplexityHotspots)
	}
	if len(arch.CallHubs) == 0 || arch.CallHubs[0].Ref.Name != "hub" || arch.CallHubs[0].Metric != 2 {
		t.Errorf("top call hub = %+v, want hub/2", arch.CallHubs)
	}
	pkg := map[string]int{}
	for _, ps := range arch.Packages {
		pkg[ps.Dir] = ps.Symbols
	}
	if pkg["internal"] != 1 {
		t.Errorf("packages = %v, want dir 'internal' with 1 symbol", arch.Packages)
	}
}
