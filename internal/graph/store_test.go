package graph

import (
	"path/filepath"
	"testing"
)

// TestReplaceProject_AllowsReindex is a regression test for the contentless-FTS5
// bug: ReplaceProject used `DELETE FROM nodes_fts`, which SQLite rejects on a
// contentless FTS5 table, so the SECOND index of a repo failed with
// "cannot DELETE from contentless fts5 table". A re-index must succeed and leave
// the FTS index searchable.
func TestReplaceProject_AllowsReindex(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	nodes := []Node{{
		Project: "p", Label: LabelFunction, Name: "Foo",
		QualifiedName: "p:f.go.Foo", FilePath: "f.go", StartLine: 1, EndLine: 3,
	}}
	if err := s.InsertNodes(nodes); err != nil {
		t.Fatalf("first insert: %v", err)
	}

	// The re-index path that used to fail.
	if err := s.ReplaceProject("p"); err != nil {
		t.Fatalf("replace project: %v", err)
	}
	if err := s.InsertNodes(nodes); err != nil {
		t.Fatalf("second insert: %v", err)
	}

	hits, err := s.Search("p", "Foo", "", 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("got %d hits, want 1 (FTS must survive a reindex)", len(hits))
	}
}
