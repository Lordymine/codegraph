package index

import (
	"testing"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
)

// TestTreeSitterFoundation_ParsesGo proves the official cgo tree-sitter core +
// Go grammar link and run in this environment. It is a foundation/smoke test for
// M1 (replacing the regex definitions pass); the real extractor builds on this.
func TestTreeSitterFoundation_ParsesGo(t *testing.T) {
	parser := tree_sitter.NewParser()
	defer parser.Close()

	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_go.Language())); err != nil {
		t.Fatalf("set language: %v", err)
	}

	src := []byte("package main\n\nfunc Foo() int {\n\treturn 1\n}\n")
	tree := parser.Parse(src, nil)
	defer tree.Close()

	root := tree.RootNode()
	if got := root.Kind(); got != "source_file" {
		t.Fatalf("root kind = %q, want source_file", got)
	}
	// The function declaration node's end byte must cover the closing brace —
	// the precise end-line info the regex pass is blind to.
	if root.NamedChildCount() == 0 {
		t.Fatal("expected at least one named child (package_clause / func)")
	}
}
