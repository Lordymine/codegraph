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

func TestForEachCallEdge_Streams(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	nodes := []Node{
		{Project: "p", Label: LabelFunction, Name: "A", QualifiedName: "p:a.go.A", FilePath: "a.go"},
		{Project: "p", Label: LabelFunction, Name: "B", QualifiedName: "p:a.go.B", FilePath: "a.go"},
	}
	if err := s.InsertNodes(nodes); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.InsertEdges([]Edge{
		{Project: "p", SourceQN: "p:a.go.A", TargetQN: "p:a.go.B", Type: EdgeCalls},
	}); err != nil {
		t.Fatal(err)
	}

	var n int
	if err := s.ForEachCallEdge("p", func(ce CallEdge) error {
		n++
		if ce.SourceQN != "p:a.go.A" || ce.TargetQN != "p:a.go.B" || ce.SourceFile != "a.go" {
			t.Fatalf("unexpected edge %+v", ce)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("got %d edges, want 1", n)
	}
}

// TestStore_ReadSnapshotPreservesPreWipeCalls pins the Run reuse path: a second
// connection with an active read snapshot still sees CALLS edges after ReplaceProject
// wipes them on the writer connection.
func TestStore_ReadSnapshotPreservesPreWipeCalls(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.db")
	writer, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()

	nodes := []Node{
		{Project: "p", Label: LabelFunction, Name: "A", QualifiedName: "p:a.go.A", FilePath: "a.go"},
		{Project: "p", Label: LabelFunction, Name: "B", QualifiedName: "p:a.go.B", FilePath: "a.go"},
	}
	if err := writer.InsertNodes(nodes); err != nil {
		t.Fatal(err)
	}
	if _, _, err := writer.InsertEdges([]Edge{
		{Project: "p", SourceQN: "p:a.go.A", TargetQN: "p:a.go.B", Type: EdgeCalls},
	}); err != nil {
		t.Fatal(err)
	}

	reader, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if err := reader.BeginReadSnapshot(); err != nil {
		t.Fatal(err)
	}
	defer reader.EndReadSnapshot()

	if err := writer.ReplaceProject("p"); err != nil {
		t.Fatal(err)
	}
	if err := writer.InsertNodes(nodes[:1]); err != nil {
		t.Fatal(err)
	}

	var n int
	if err := reader.ForEachCallEdge("p", func(ce CallEdge) error {
		n++
		if ce.SourceQN != "p:a.go.A" || ce.TargetQN != "p:a.go.B" {
			t.Fatalf("unexpected edge %+v", ce)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("snapshot saw %d CALLS edges, want 1 (pre-wipe graph)", n)
	}
}

func TestStore_Reopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	nodes := []Node{{
		Project: "p", Label: LabelFunction, Name: "Foo",
		QualifiedName: "p:f.go.Foo", FilePath: "f.go",
	}}
	if err := s.InsertNodes(nodes); err != nil {
		t.Fatal(err)
	}
	if s.DBPath() != path {
		t.Fatalf("DBPath = %q, want %q", s.DBPath(), path)
	}
	if err := s.Reopen(path); err != nil {
		t.Fatal(err)
	}
	n, _, err := s.Stats("p")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("after Reopen: nodes=%d, want 1", n)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}
