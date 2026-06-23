package index

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Lordymine/codegraph/internal/graph"
)

func TestRunAtomic_PreservesGraphOnFailure(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "main.go")
	if err := os.WriteFile(p, []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dir, "g.db")

	if _, err := RunAtomic(dbPath, dir); err != nil {
		t.Fatalf("first index: %v", err)
	}
	project := ProjectName(dir)
	st, err := graph.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	nBefore, eBefore, err := st.Stats(project)
	st.Close()
	if err != nil || nBefore == 0 {
		t.Fatalf("expected indexed graph, nodes=%d err=%v", nBefore, err)
	}

	if err := os.WriteFile(p, []byte("package main\nfunc main() { panic(1) }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pipelinePreflightErr = errors.New("injected pipeline failure")
	defer func() { pipelinePreflightErr = nil }()

	if _, err := RunAtomic(dbPath, dir); err == nil {
		t.Fatal("expected injected failure")
	}
	if _, err := os.Stat(dbPath + BuildingSuffix); !os.IsNotExist(err) {
		t.Fatalf("building file should be removed on failure, stat err=%v", err)
	}

	st, err = graph.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	nAfter, eAfter, err := st.Stats(project)
	if err != nil {
		t.Fatal(err)
	}
	if nAfter != nBefore || eAfter != eBefore {
		t.Fatalf("live graph changed on failed re-index: before=%d/%d after=%d/%d", nBefore, eBefore, nAfter, eAfter)
	}
}

func TestRunAtomic_CommitsOnSuccess(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte("package x\nfunc F() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dir, "g.db")
	if _, err := RunAtomic(dbPath, dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dbPath + BuildingSuffix); !os.IsNotExist(err) {
		t.Fatalf("building suffix should not remain after success: %v", err)
	}
}
