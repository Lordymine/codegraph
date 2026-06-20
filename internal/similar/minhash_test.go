package similar

import (
	"strconv"
	"strings"
	"testing"
)

func toks(s string) []string { return strings.Fields(s) }

// TestSignatureJaccard pins the MinHash core: a fixed-size signature whose fraction
// of matching positions estimates the Jaccard similarity of the two token streams'
// shingle sets. Identical streams estimate ~1, disjoint ~0, a near-clone in between.
func TestSignatureJaccard(t *testing.T) {
	const k, n = 3, 128

	a := Signature(toks("a b c d e f g h i j"), k, n)
	if j := EstJaccard(a, Signature(toks("a b c d e f g h i j"), k, n)); j < 0.99 {
		t.Errorf("identical token streams should estimate ~1.0, got %.2f", j)
	}
	if j := EstJaccard(a, Signature(toks("p q r s t u v w"), k, n)); j > 0.05 {
		t.Errorf("disjoint streams should estimate ~0, got %.2f", j)
	}

	// 20 tokens, one changed -> ~3 of 18 shingles differ -> Jaccard ~0.71.
	base := make([]string, 20)
	for i := range base {
		base[i] = "t" + strconv.Itoa(i)
	}
	near := append([]string(nil), base...)
	near[10] = "CHANGED"
	if j := EstJaccard(Signature(base, k, n), Signature(near, k, n)); j < 0.5 || j > 0.92 {
		t.Errorf("near-clone (1/20 token changed) should estimate high but <1, got %.2f", j)
	}
}
