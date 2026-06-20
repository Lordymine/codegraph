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

// TestCallEdges_GenericsDoNotCrash guards the RuntimeTypes-free call graph (cha.go):
// x/tools' cha.CallGraph/AllFunctions panic on generic instantiations, which used to
// silently zero a whole repo's Go CALLS. CallEdges must build edges on generic code
// without erroring.
func TestCallEdges_GenericsDoNotCrash(t *testing.T) {
	root, err := filepath.Abs("testdata/generics")
	if err != nil {
		t.Fatal(err)
	}
	edges, err := CallEdges("test", root, func(string) bool { return true })
	if err != nil {
		t.Fatalf("CallEdges on generic code errored: %v", err)
	}
	// The old code panicked in RuntimeTypes and dropped the WHOLE repo's edges. A
	// non-generic call in a file that also contains generics must still resolve.
	if !hasEdge(edges, "use", "helper") {
		t.Errorf("generic code poisoned the graph: use->helper missing; edges:%s", dumpEdges(edges))
	}
}

// TestCallEdges_AttributesClosureCallsToEnclosing pins closure call attribution:
// a call written inside a function literal must be credited to the enclosing named
// function. Without it, the edge's source is the anonymous "outer$1", which is not
// a graph node, so the call is dropped — the dominant recall hole on closure-heavy
// code like cobra (Run: func(){...}).
func TestCallEdges_AttributesClosureCallsToEnclosing(t *testing.T) {
	root, err := filepath.Abs("testdata/closures")
	if err != nil {
		t.Fatal(err)
	}
	edges, err := CallEdges("test", root, func(string) bool { return true })
	if err != nil {
		t.Fatalf("CallEdges: %v", err)
	}
	if !hasEdge(edges, "closures.go.outer", "closures.go.target") {
		t.Errorf("call inside a closure must be attributed to the enclosing function (outer->target); edges:%s", dumpEdges(edges))
	}
}

// TestCallEdges_EmitsRecursiveSelfEdge pins that a function calling itself yields a
// self-edge. Recursion is a genuine call the eval oracle counts as a caller (e.g.
// cobra's FlagErrorFunc calls itself on c.parent); dropping it understates callers.
func TestCallEdges_EmitsRecursiveSelfEdge(t *testing.T) {
	root, err := filepath.Abs("testdata/recursion")
	if err != nil {
		t.Fatal(err)
	}
	edges, err := CallEdges("test", root, func(string) bool { return true })
	if err != nil {
		t.Fatalf("CallEdges: %v", err)
	}
	if !hasEdge(edges, "recursion.go.fact", "recursion.go.fact") {
		t.Errorf("recursive call must emit a self-edge (fact->fact); edges:%s", dumpEdges(edges))
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
