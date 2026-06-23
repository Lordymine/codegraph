package graph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSnippet_ValidRelativePath(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "a.go"), []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Snippet(repo, "a.go", 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got != "two" {
		t.Fatalf("got %q, want %q", got, "two")
	}
}

func TestSnippet_RejectsTraversal(t *testing.T) {
	repo := t.TempDir()
	neighbor := t.TempDir()
	if err := os.WriteFile(filepath.Join(neighbor, "secret.txt"), []byte("nope\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "inside.go"), []byte("ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []string{
		filepath.Join("..", filepath.Base(neighbor), "secret.txt"),
		"../" + filepath.Base(neighbor) + "/secret.txt",
		filepath.Join("pkg", "..", "..", filepath.Base(neighbor), "secret.txt"),
	}
	for _, p := range cases {
		p = filepath.ToSlash(p)
		if _, err := Snippet(repo, p, 1, 1); err == nil || !strings.Contains(err.Error(), "outside repository root") {
			t.Fatalf("path %q should be rejected, err=%v", p, err)
		}
	}
}

func TestSnippet_RejectsAbsolutePath(t *testing.T) {
	repo := t.TempDir()
	abs := filepath.Join(repo, "x.go")
	if err := os.WriteFile(abs, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Snippet(repo, abs, 1, 1); err == nil || !strings.Contains(err.Error(), "absolute paths") {
		t.Fatalf("absolute path should be rejected, err=%v", err)
	}
}

func TestResolveRepoFile_RejectsDotDot(t *testing.T) {
	repo := t.TempDir()
	if _, err := resolveRepoFile(repo, ".."); err == nil {
		t.Fatal("expected rejection for ..")
	}
}
