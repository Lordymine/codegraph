package quality

import (
	"math"
	"testing"
)

func TestF1(t *testing.T) {
	// perfect
	if _, _, f := f1([]string{"a", "b"}, []string{"a", "b"}); !approx(f, 1) {
		t.Fatalf("perfect F1=%v", f)
	}
	// half recall (found 1 of 2), perfect precision -> F1 = 2*1*0.5/1.5 = 0.667
	if p, r, f := f1([]string{"a"}, []string{"a", "b"}); !approx(p, 1) || !approx(r, 0.5) || !approx(f, 2.0/3) {
		t.Fatalf("partial p=%v r=%v f=%v", p, r, f)
	}
	// empty truth, empty answer -> correctly said nothing
	if _, _, f := f1(nil, nil); !approx(f, 1) {
		t.Fatalf("empty/empty F1=%v want 1", f)
	}
	// empty truth, non-empty answer -> hallucinated
	if _, _, f := f1([]string{"a"}, nil); !approx(f, 0) {
		t.Fatalf("halluc F1=%v want 0", f)
	}
}

func TestNormNameFoldsQualifiers(t *testing.T) {
	for _, s := range []string{"getActiveCode", "Service.getActiveCode", "x.getActiveCode()", "src/a.ts:getActiveCode"} {
		if got := normName(s); got != "getactivecode" {
			t.Fatalf("normName(%q)=%q", s, got)
		}
	}
}

// Regression: responders append a (file:line) or @ location annotation after the
// name. The line number must NOT be mistaken for the symbol (that scored real
// answers as 0%). The name is what counts.
func TestNormNameStripsLocationAnnotations(t *testing.T) {
	cases := map[string]string{
		"ForgotPasswordScreen (apps/mobile/app/(auth)/forgot-password.tsx:29)": "forgotpasswordscreen",
		"RegisterPage @ apps/admin/src/app/(auth)/register/page.tsx:23":        "registerpage",
		"apps/admin/src/components/ui/button.tsx:Button":                       "button",
		"CatalogPage":                                                          "catalogpage",
		"MoneyField (extra-section.tsx:152)":                                   "moneyfield",
	}
	for in, want := range cases {
		if got := normName(in); got != want {
			t.Fatalf("normName(%q)=%q want %q", in, got, want)
		}
	}
}

func TestMatchDefinition(t *testing.T) {
	truth := []string{"src/foo/bar.ts:42"}
	if !matchDefinition([]string{"src/foo/bar.ts:43"}, truth) { // within ±3
		t.Fatal("should match within tolerance")
	}
	if !matchDefinition([]string{"bar.ts:42"}, truth) { // basename + exact line
		t.Fatal("should match by basename")
	}
	if matchDefinition([]string{"src/foo/bar.ts:99"}, truth) { // line too far
		t.Fatal("should not match far line")
	}
	if matchDefinition([]string{"other.ts:42"}, truth) { // wrong file
		t.Fatal("should not match wrong file")
	}
}

func TestEvaluateAggregates(t *testing.T) {
	qs := []Question{
		{ID: "callers-01", Type: TypeCallers},
		{ID: "open-01", Type: TypeOpen},
	}
	truths := []Truth{{ID: "callers-01", Items: []string{"a", "b"}}}
	j := 0.8
	answers := []Answer{
		{ID: "callers-01", Mode: "graph", Items: []string{"a", "b"}, Tokens: 100, Calls: 1},
		{ID: "open-01", Mode: "graph", Text: "...", Judge: &j, Tokens: 50, Calls: 1},
		{ID: "callers-01", Mode: "baseline", Items: []string{"a"}, Tokens: 2000, Calls: 6},
		{ID: "open-01", Mode: "baseline", Text: "...", Judge: &j, Tokens: 1500, Calls: 5},
	}
	_, aggs := Evaluate(qs, truths, answers)
	g, base := aggs["graph"], aggs["baseline"]
	// graph: callers F1=1, open=0.8 -> mean 0.9
	if !approx(g.MeanQuality, 0.9) {
		t.Fatalf("graph mean=%v want 0.9", g.MeanQuality)
	}
	// baseline: callers F1 = 2*1*0.5/1.5 = .667, open .8 -> mean ~.733
	if !approx(base.MeanQuality, (2.0/3+0.8)/2) {
		t.Fatalf("baseline mean=%v", base.MeanQuality)
	}
	if g.TotalTokens != 150 || base.TotalTokens != 3500 {
		t.Fatalf("tokens g=%d base=%d", g.TotalTokens, base.TotalTokens)
	}
	if g.TotalCalls != 2 || base.TotalCalls != 11 {
		t.Fatalf("calls g=%d base=%d", g.TotalCalls, base.TotalCalls)
	}
}

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }
