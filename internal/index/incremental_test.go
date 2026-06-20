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

// TestChangedScopes pins the gating brain of scope-incremental CALLS: from a change
// set, which CALLS scopes must re-resolve. A Go file touches the one "go" scope; a TS
// file touches its enclosing tsconfig-project; untouched scopes are absent (reused).
func TestChangedScopes(t *testing.T) {
	tsdirs := []string{"apps/api", "apps/web"}

	// Touch Go, app-api and app-web -> all three scopes re-resolve.
	all := changedScopes(Changes{
		Changed: []string{"apps/api/src/x.ts", "cmd/gh/main.go"},
		Added:   []string{"apps/web/y.tsx"},
		Deleted: []string{"apps/api/old.ts"},
	}, tsdirs)
	if !sameSet(all, map[string]bool{"go": true, "apps/api": true, "apps/web": true}) {
		t.Errorf("all scopes: got %v", all)
	}

	// Edit only an app-api file -> app-web and go stay untouched (reused).
	one := changedScopes(Changes{Changed: []string{"apps/api/src/x.ts"}}, tsdirs)
	if !sameSet(one, map[string]bool{"apps/api": true}) {
		t.Errorf("single scope: got %v, want {apps/api}", one)
	}

	// A TS file outside any tsconfig-project maps to the root ("") scope.
	root := changedScopes(Changes{Changed: []string{"loose.ts"}}, tsdirs)
	if !sameSet(root, map[string]bool{"": true}) {
		t.Errorf("root scope: got %v, want {\"\"}", root)
	}
}

func sameSet(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

// TestRun_NoOpWhenUnchanged pins the first user-facing incremental win: re-running
// Run on an unchanged repo skips the whole pipeline (instant) instead of re-resolving
// CALLS. Proven with a sentinel node that a real re-index (ReplaceProject) would wipe:
// if it survives, the pipeline did not run.
func TestRun_NoOpWhenUnchanged(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, content string) {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(rel)), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.ts", "export const a = 1\n")

	store, err := graph.Open(filepath.Join(dir, "g.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	project := ProjectName(dir)

	res1, err := Run(store, dir)
	if err != nil {
		t.Fatalf("run1: %v", err)
	}
	if res1.Reused {
		t.Fatal("first index must do work, not reuse")
	}
	n0, _, _ := store.Stats(project)

	// A sentinel node that a real re-index (ReplaceProject) would wipe.
	if err := store.InsertNodes([]graph.Node{{
		Project: project, Label: "Sentinel", Name: "s", QualifiedName: project + ":__sentinel__",
	}}); err != nil {
		t.Fatal(err)
	}

	// Unchanged re-run -> no-op: Reused, and the sentinel survives (pipeline skipped).
	res2, err := Run(store, dir)
	if err != nil {
		t.Fatalf("run2: %v", err)
	}
	if !res2.Reused {
		t.Error("unchanged re-index should be a no-op (Reused=true)")
	}
	if n, _, _ := store.Stats(project); n != n0+1 {
		t.Errorf("no-op touched the store: nodes=%d, want %d (sentinel intact)", n, n0+1)
	}

	// Change a.ts -> full re-index: Reused false, sentinel wiped, rebuilt from files.
	write("a.ts", "export const a = 2\n")
	res3, err := Run(store, dir)
	if err != nil {
		t.Fatalf("run3: %v", err)
	}
	if res3.Reused {
		t.Error("changed re-index must do work (Reused=false)")
	}
	if n, _, _ := store.Stats(project); n != n0 {
		t.Errorf("re-index should rebuild from files (sentinel wiped): nodes=%d, want %d", n, n0)
	}
}
