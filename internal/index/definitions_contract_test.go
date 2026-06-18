package index

import (
	"testing"

	"github.com/Lordymine/codegraph/internal/graph"
)

// Contract tests for the definitions pass. They describe what a CORRECT extractor
// must produce — independent of how it's implemented. The regex MVP fails most of
// them on purpose (it's scope-blind); they are the green target for the
// tree-sitter extractor (M1). Each test says what the regex gets wrong.
//
// Run just these:  go test ./internal/index -run TestDefs -v

func findDef(nodes []graph.Node, name string) (graph.Node, bool) {
	for _, n := range nodes {
		if n.Name == name {
			return n, true
		}
	}
	return graph.Node{}, false
}

func nodesNamed(nodes []graph.Node, name string) []graph.Node {
	var out []graph.Node
	for _, n := range nodes {
		if n.Name == name {
			out = append(out, n)
		}
	}
	return out
}

// Baseline: the file node exists with the right label/start. Already passes with
// the regex pass — proof the harness works and not everything is red.
// (NB: the file node's EndLine currently counts a phantom line for the trailing
// newline — a small off-by-one the tree-sitter root-node span will fix for free.)
func TestDefs_FileNodeExists(t *testing.T) {
	src := "package main\nfunc Foo() {}\n"
	nodes, _ := extractDefsFromSource("p", "x.go", LangGo, []byte(src))
	f, ok := findDef(nodes, "x.go")
	if !ok {
		t.Fatal("file node not found")
	}
	if f.Label != graph.LabelFile {
		t.Errorf("label = %s, want File", f.Label)
	}
	if f.StartLine != 1 {
		t.Errorf("StartLine = %d, want 1", f.StartLine)
	}
	if _, ok := findDef(nodes, "Foo"); !ok {
		t.Error("Foo not found (regex baseline)")
	}
}

// CONTRACT: a function node's EndLine covers its body (the closing brace line).
// Regex is blind to the body and reports EndLine == StartLine.
func TestDefs_Go_FunctionEndLineCoversBody(t *testing.T) {
	src := "package main\n\nfunc Foo() int {\n\treturn 1\n}\n"
	//      1            2  3                4           5
	nodes, _ := extractDefsFromSource("p", "x.go", LangGo, []byte(src))
	foo, ok := findDef(nodes, "Foo")
	if !ok {
		t.Fatal("Foo not found")
	}
	if foo.StartLine != 3 {
		t.Errorf("StartLine = %d, want 3", foo.StartLine)
	}
	if foo.EndLine != 5 {
		t.Errorf("EndLine = %d, want 5 (regex reports the start line — blind to body)", foo.EndLine)
	}
}

// CONTRACT: is_exported reflects Go capitalization (Foo exported, foo not).
// Regex never sets it.
func TestDefs_Go_ExportedFlag(t *testing.T) {
	src := "package main\nfunc Foo() {}\nfunc bar() {}\n"
	nodes, _ := extractDefsFromSource("p", "x.go", LangGo, []byte(src))
	foo, _ := findDef(nodes, "Foo")
	bar, _ := findDef(nodes, "bar")
	if foo.Props["is_exported"] != true {
		t.Errorf("Foo is_exported = %v, want true", foo.Props["is_exported"])
	}
	if bar.Props["is_exported"] != false {
		t.Errorf("bar is_exported = %v, want false", bar.Props["is_exported"])
	}
}

// CONTRACT: two methods with the same name on different receivers get DISTINCT
// qualified names, so callers/callees can disambiguate them. This is the core of
// the homonym-disambiguation goal. Regex strips the receiver, so both collapse to
// the same qualified name.
func TestDefs_Go_SameNameMethodsDisambiguated(t *testing.T) {
	src := "package main\n" +
		"type A struct{}\n" +
		"func (a A) Close() {}\n" +
		"type B struct{}\n" +
		"func (b B) Close() {}\n"
	nodes, _ := extractDefsFromSource("p", "x.go", LangGo, []byte(src))
	closes := nodesNamed(nodes, "Close")
	if len(closes) != 2 {
		t.Fatalf("found %d Close nodes, want 2", len(closes))
	}
	if closes[0].QualifiedName == closes[1].QualifiedName {
		t.Errorf("both Close share qualified_name %q; must be distinct per receiver", closes[0].QualifiedName)
	}
}

// CONTRACT: a TS class node spans to its closing brace and its methods are found.
// Regex reports the class EndLine as the declaration line.
func TestDefs_TS_ClassSpansAndMethods(t *testing.T) {
	src := "export class A {\n  foo() {\n    return 1\n  }\n  bar() {}\n}\n"
	//      1                2         3             4    5          6
	nodes, _ := extractDefsFromSource("p", "x.ts", LangTS, []byte(src))
	a, ok := findDef(nodes, "A")
	if !ok {
		t.Fatal("class A not found")
	}
	if a.Label != graph.LabelClass {
		t.Errorf("A label = %s, want Class", a.Label)
	}
	if a.EndLine != 6 {
		t.Errorf("class EndLine = %d, want 6 (regex reports declaration line)", a.EndLine)
	}
	if _, ok := findDef(nodes, "foo"); !ok {
		t.Error("method foo not found")
	}
	if _, ok := findDef(nodes, "bar"); !ok {
		t.Error("method bar not found")
	}
}

// CONTRACT: NestJS decorators are captured so @Injectable/@Controller are visible
// to the graph. Regex cannot see decorators at all.
func TestDefs_TS_NestJSDecoratorsCaptured(t *testing.T) {
	src := "@Injectable()\nexport class UserService {}\n"
	nodes, _ := extractDefsFromSource("p", "x.ts", LangTS, []byte(src))
	us, ok := findDef(nodes, "UserService")
	if !ok {
		t.Fatal("UserService not found")
	}
	decs, _ := us.Props["decorators"].([]string)
	found := false
	for _, d := range decs {
		if d == "Injectable" {
			found = true
		}
	}
	if !found {
		t.Errorf("decorators = %v, want to contain \"Injectable\"", us.Props["decorators"])
	}
}

// CONTRACT: is_exported for TS reflects the `export` keyword.
func TestDefs_TS_ExportedFlag(t *testing.T) {
	src := "export function foo() {}\nfunction bar() {}\n"
	nodes, _ := extractDefsFromSource("p", "x.ts", LangTS, []byte(src))
	foo, _ := findDef(nodes, "foo")
	bar, _ := findDef(nodes, "bar")
	if foo.Props["is_exported"] != true {
		t.Errorf("foo is_exported = %v, want true", foo.Props["is_exported"])
	}
	if bar.Props["is_exported"] != false {
		t.Errorf("bar is_exported = %v, want false", bar.Props["is_exported"])
	}
}
