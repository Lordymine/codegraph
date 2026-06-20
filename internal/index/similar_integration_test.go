package index

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Lordymine/codegraph/internal/graph"
)

// TestSimilar_EndToEnd runs the full pipeline on a tiny TS repo with two near-clone
// functions (one literal differs) plus an unrelated one, and confirms the graph holds
// a SIMILAR_TO edge between the clones and not to the unrelated function.
func TestSimilar_EndToEnd(t *testing.T) {
	dir := t.TempDir()
	src := `export function alpha(x: number) {
  const a = x + 1;
  const b = a * 2;
  const c = b - 3;
  const d = c * c;
  const e = d + a;
  const f = e - b;
  return f * 10 + a - c;
}
export function beta(x: number) {
  const a = x + 1;
  const b = a * 2;
  const c = b - 3;
  const d = c * c;
  const e = d + a;
  const f = e - b;
  return f * 10 + a - c + 99;
}
export function gamma(s: string) {
  return s.toUpperCase().split(",").map((w) => w.trim()).join("-");
}
`
	if err := os.WriteFile(filepath.Join(dir, "a.ts"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := graph.Open(filepath.Join(dir, "g.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := Run(store, dir); err != nil {
		t.Fatalf("run: %v", err)
	}

	project := ProjectName(dir)
	// SIMILAR_TO is symmetric and stored as smaller-QN -> larger-QN, so query both
	// directions (Neighbors only applies the type filter for in/out, not both).
	similarTo := func(qn string) map[string]bool {
		out := map[string]bool{}
		for _, dir := range []string{"out", "in"} {
			ns, _ := store.Neighbors(project, project+":"+qn, dir, string(graph.EdgeSimilarTo), 20)
			for _, n := range ns {
				out[n.Name] = true
			}
		}
		return out
	}
	sim := similarTo("a.ts.alpha")
	if !sim["beta"] {
		t.Errorf("alpha and beta are near-clones; expected a SIMILAR_TO edge, got %v", sim)
	}
	if sim["gamma"] {
		t.Errorf("alpha and gamma are unrelated; should not be SIMILAR_TO, got %v", sim)
	}
}
