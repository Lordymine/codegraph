package index

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Lordymine/codegraph/internal/graph"
)

// TestDetectChanges pins the M3 foundation: after indexing, Run records a per-file
// content hash, and DetectChanges reports exactly what changed on disk since — the
// basis for the no-op-when-unchanged path and the scope-gated CALLS re-resolution.
func TestDetectChanges(t *testing.T) {
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
	write("a.ts", "export const a = 1\n")
	write("b.ts", "export const b = 2\n")

	store, err := graph.Open(filepath.Join(dir, "g.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	project := ProjectName(dir)
	if _, err := Run(store, dir); err != nil {
		t.Fatalf("run: %v", err)
	}

	has := func(xs []string, p string) bool {
		for _, x := range xs {
			if x == p {
				return true
			}
		}
		return false
	}

	// 1) Immediately after indexing, nothing has changed on disk.
	ch, err := DetectChanges(store, project, dir)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if len(ch.Changed)+len(ch.Added)+len(ch.Deleted) != 0 {
		t.Fatalf("expected no changes right after indexing, got %+v", ch)
	}

	// 2) Edit a.ts, add c.ts, delete b.ts.
	write("a.ts", "export const a = 999\n")
	write("c.ts", "export const c = 3\n")
	if err := os.Remove(filepath.Join(dir, "b.ts")); err != nil {
		t.Fatal(err)
	}

	ch, err = DetectChanges(store, project, dir)
	if err != nil {
		t.Fatalf("detect after edits: %v", err)
	}
	if !has(ch.Changed, "a.ts") {
		t.Errorf("a.ts should be Changed; got %+v", ch)
	}
	if !has(ch.Added, "c.ts") {
		t.Errorf("c.ts should be Added; got %+v", ch)
	}
	if !has(ch.Deleted, "b.ts") {
		t.Errorf("b.ts should be Deleted; got %+v", ch)
	}
}
