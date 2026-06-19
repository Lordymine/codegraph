package gocalls

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lordymine/codegraph/internal/graph"
)

// TestCallEdges_InterfacePrecision pins the precision contract: an interface call
// dispatched on a concrete type must NOT spray edges to every implementation of the
// interface. CHA (sound) over-approximates and links caller->Cat.Speak even though
// Cat is never used; the precise resolver (VTA) keeps only caller->Dog.Speak.
func TestCallEdges_InterfacePrecision(t *testing.T) {
	root, err := filepath.Abs("testdata/iface")
	if err != nil {
		t.Fatal(err)
	}
	edges, err := CallEdges("test", root, func(string) bool { return true })
	if err != nil {
		t.Fatalf("CallEdges: %v", err)
	}

	if !hasEdge(edges, "caller", "Dog.Speak") {
		t.Errorf("missing the real call caller->Dog.Speak; edges:%s", dumpEdges(edges))
	}
	if hasEdge(edges, "caller", "Cat.Speak") {
		t.Errorf("over-approximation: caller->Cat.Speak must not exist (Cat is never used); edges:%s", dumpEdges(edges))
	}
}

// TestCallEdges_IncludesTestCallers pins that calls made from *_test.go produce
// edges (packages Tests:true). Test functions are the dominant caller set for
// library code, so dropping them would gut "who calls X" recall.
func TestCallEdges_IncludesTestCallers(t *testing.T) {
	root, err := filepath.Abs("testdata/withtest")
	if err != nil {
		t.Fatal(err)
	}
	edges, err := CallEdges("test", root, func(string) bool { return true })
	if err != nil {
		t.Fatalf("CallEdges: %v", err)
	}
	if !hasEdge(edges, "TestTarget", "Target") {
		t.Errorf("expected test-origin edge TestTarget->Target; edges:%s", dumpEdges(edges))
	}
}

func hasEdge(edges []graph.Edge, srcTail, dstTail string) bool {
	for _, e := range edges {
		if strings.HasSuffix(e.SourceQN, srcTail) && strings.HasSuffix(e.TargetQN, dstTail) {
			return true
		}
	}
	return false
}

func dumpEdges(edges []graph.Edge) string {
	var b strings.Builder
	for _, e := range edges {
		b.WriteString("\n  " + e.SourceQN + " -> " + e.TargetQN)
	}
	return b.String()
}
