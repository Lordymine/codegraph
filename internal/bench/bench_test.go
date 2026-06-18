package bench

import (
	"math"
	"testing"
)

func TestEstimateTokensMonotonic(t *testing.T) {
	if EstimateTokens("") != 1 {
		t.Fatalf("empty should floor to 1, got %d", EstimateTokens(""))
	}
	short := EstimateTokens("func foo()")
	long := EstimateTokens("func foo() { return bar() + baz() + qux() }")
	if long <= short {
		t.Fatalf("longer text must cost more tokens: short=%d long=%d", short, long)
	}
}

func TestContainsWordRespectsBoundaries(t *testing.T) {
	cases := []struct {
		line, word string
		want       bool
	}{
		{"foo(bar)", "foo", true},
		{"x.getActiveCode()", "getActiveCode", true},
		{"getActiveCodeForMobile()", "getActiveCode", false}, // substring, not a whole token
		{"const getActiveCode = 1", "getActiveCode", true},
		{"// getActiveCode in a comment", "getActiveCode", true},
		{"nogetActiveCode", "getActiveCode", false},
		{"plain text", "missing", false},
	}
	for _, c := range cases {
		if got := containsWord(c.line, c.word); got != c.want {
			t.Errorf("containsWord(%q,%q)=%v want %v", c.line, c.word, got, c.want)
		}
	}
}

func TestWindowTextIsSubsetAroundMatch(t *testing.T) {
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = "line"
	}
	lines[50] = "MATCH"
	got := windowText(lines, []int{50}, 10)
	// ±10 around line 50 = 21 lines, far less than the whole 100-line file.
	if want := 21; countLines(got) != want {
		t.Fatalf("window should be %d lines, got %d", want, countLines(got))
	}
	if !contains(got, "MATCH") {
		t.Fatal("window must include the matched line")
	}
}

func TestBaselineWindowNeverCostsMoreThanWholeFile(t *testing.T) {
	c := &Corpus{files: []srcFile{
		{rel: "a.ts", lines: repeat("x", 200)},
		{rel: "b.ts", lines: append(repeat("y", 80), "callMe()", "more")},
	}}
	c.files[1].lines[40] = "callMe()" // a match buried in a big file
	win, file := c.baselineCost("callMe")
	if win.Tokens > file.Tokens {
		t.Fatalf("window baseline (%d) must not exceed whole-file baseline (%d)", win.Tokens, file.Tokens)
	}
	if win.Calls != file.Calls {
		t.Fatalf("both baselines open the same files: win=%d file=%d", win.Calls, file.Calls)
	}
	// b.ts has matches → at least 1 file opened, +1 for the grep call.
	if win.Calls < 2 {
		t.Fatalf("expected grep + >=1 read, got %d calls", win.Calls)
	}
}

func TestSummarizeRatiosAndCalls(t *testing.T) {
	outs := []Outcome{
		{Graph: Cost{Tokens: 100, Calls: 1}, BaselineWin: Cost{Tokens: 1000, Calls: 6}, BaselineFile: Cost{Tokens: 5000, Calls: 6}},
		{Graph: Cost{Tokens: 100, Calls: 1}, BaselineWin: Cost{Tokens: 300, Calls: 3}, BaselineFile: Cost{Tokens: 900, Calls: 3}},
	}
	s := Summarize(outs)
	if s.N != 2 {
		t.Fatalf("N=%d", s.N)
	}
	// total tokens: win 1300/200 = 6.5x, file 5900/200 = 29.5x
	if !approx(s.TotalRatioWin, 6.5) || !approx(s.TotalRatioFile, 29.5) {
		t.Fatalf("total ratios wrong: win=%.2f file=%.2f", s.TotalRatioWin, s.TotalRatioFile)
	}
	// median per-query win ratio: [10, 3] -> 6.5
	if !approx(s.MedianRatioWin, 6.5) {
		t.Fatalf("median win ratio=%.2f want 6.5", s.MedianRatioWin)
	}
	// calls: baseline-win 9, graph 2 -> 4.5x
	if !approx(s.CallRatioWin, 4.5) {
		t.Fatalf("call ratio=%.2f want 4.5", s.CallRatioWin)
	}
}

func TestMedianEvenAndOdd(t *testing.T) {
	if got := median([]float64{3, 1, 2}); !approx(got, 2) {
		t.Fatalf("odd median=%.2f want 2", got)
	}
	if got := median([]float64{4, 1, 3, 2}); !approx(got, 2.5) {
		t.Fatalf("even median=%.2f want 2.5", got)
	}
}

// helpers
func repeat(s string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = s
	}
	return out
}
func countLines(s string) int {
	n := 0
	for _, r := range s {
		if r == '\n' {
			n++
		}
	}
	return n
}
func contains(hay, needle string) bool {
	for i := 0; i+len(needle) <= len(hay); i++ {
		if hay[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
func approx(a, b float64) bool { return math.Abs(a-b) < 0.01 }
