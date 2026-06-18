package index

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Lordymine/codegraph/internal/graph"
)

// TestImports_EndToEndGraph runs the full pipeline (discover -> definitions ->
// resolveImports -> store) on a tiny TS repo and confirms the IMPORTS edge is
// queryable from the graph. This is the end-to-end proof that complements the
// unit contracts, since codegraph's own repo is Go-only and emits no IMPORTS.
func TestImports_EndToEndGraph(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("src/foo.ts", "import { B } from './bar'\nexport class Foo {}\n")
	write("src/bar.ts", "export class B {}\n")

	store, err := graph.Open(filepath.Join(dir, "g.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if _, err := Run(store, dir); err != nil {
		t.Fatalf("run: %v", err)
	}

	project := ProjectName(dir)
	nbrs, err := store.Neighbors(project, project+":src/foo.ts", "out", string(graph.EdgeImports), 10)
	if err != nil {
		t.Fatalf("neighbors: %v", err)
	}
	for _, n := range nbrs {
		if n.QualifiedName == project+":src/bar.ts" {
			return // found the IMPORTS edge in the graph
		}
	}
	t.Fatalf("expected IMPORTS foo.ts->bar.ts in graph; got neighbors=%+v", nbrs)
}
