package index

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestDiscover_RespectsGitignore pins that discovery honors the repo's .gitignore:
// a gitignored directory (tmp/ — where a Go module cache or build output often
// lives) and a gitignored file glob (*.gen.go) are skipped, while real source is
// kept. Without this, a repo's vendored deps/build artifacts flood the graph.
func TestDiscover_RespectsGitignore(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".gitignore", "tmp/\n*.gen.go\n# comment\n")
	writeFile(t, dir, "main.go", "package main")
	writeFile(t, dir, "internal/svc.go", "package internal")
	writeFile(t, dir, "tmp/gomodcache/dep.go", "package dep") // gitignored dir
	writeFile(t, dir, "schema.gen.go", "package main")        // gitignored glob

	files, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}
	var rels []string
	for _, f := range files {
		rels = append(rels, f.RelPath)
	}

	if !slices.Contains(rels, "main.go") || !slices.Contains(rels, "internal/svc.go") {
		t.Errorf("real source dropped: %v", rels)
	}
	for _, gone := range []string{"tmp/gomodcache/dep.go", "schema.gen.go"} {
		if slices.Contains(rels, gone) {
			t.Errorf("gitignored path %q was indexed: %v", gone, rels)
		}
	}
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
