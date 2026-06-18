package quality

import (
	"path"
	"strconv"
	"strings"
)

// Evaluate scores every answer against the oracle truth and aggregates per mode.
// Structural answers score by F1 (definition by file:line match); open answers
// take the judge's 0..1 score (0 if the judge left none).
func Evaluate(qs []Question, truths []Truth, answers []Answer) ([]Score, map[string]Agg) {
	qByID := map[string]Question{}
	for _, q := range qs {
		qByID[q.ID] = q
	}
	tByID := map[string]Truth{}
	for _, t := range truths {
		tByID[t.ID] = t
	}

	var scores []Score
	aggs := map[string]*Agg{}
	for _, a := range answers {
		q, ok := qByID[a.ID]
		if !ok {
			continue
		}
		var sc Score
		switch q.Type {
		case TypeOpen:
			j := 0.0
			if a.Judge != nil {
				j = clamp01(*a.Judge)
			}
			sc = Score{ID: a.ID, Mode: a.Mode, Type: q.Type, Quality: j, Precision: j, Recall: j}
		case TypeDefinition:
			ok := matchDefinition(a.Items, tByID[a.ID].Items)
			q01 := b2f(ok)
			sc = Score{ID: a.ID, Mode: a.Mode, Type: q.Type, Quality: q01, Precision: q01, Recall: q01}
		default: // callers / callees
			p, r, f := f1(a.Items, tByID[a.ID].Items)
			sc = Score{ID: a.ID, Mode: a.Mode, Type: q.Type, Quality: f, Precision: p, Recall: r}
		}
		scores = append(scores, sc)

		ag := aggs[a.Mode]
		if ag == nil {
			ag = &Agg{Mode: a.Mode, ByType: map[QType]float64{}}
			aggs[a.Mode] = ag
		}
		ag.N++
		ag.MeanQuality += sc.Quality
		ag.ByType[q.Type] += sc.Quality
		ag.TotalTokens += a.Tokens
		ag.TotalCalls += a.Calls
	}

	// finalize means
	out := map[string]Agg{}
	typeCounts := map[QType]int{}
	for _, q := range qs {
		typeCounts[q.Type]++
	}
	for mode, ag := range aggs {
		if ag.N > 0 {
			ag.MeanQuality /= float64(ag.N)
		}
		for typ, sum := range ag.ByType {
			if c := typeCounts[typ]; c > 0 {
				ag.ByType[typ] = sum / float64(c)
			}
		}
		out[mode] = *ag
	}
	return scores, out
}

// f1 computes set precision/recall/F1 over normalized symbol names.
func f1(answer, truth []string) (p, r, f float64) {
	A := toSet(answer)
	T := toSet(truth)
	if len(T) == 0 {
		if len(A) == 0 {
			return 1, 1, 1 // correctly said "nothing"
		}
		return 0, 1, 0 // claimed callers where there are none
	}
	tp := 0
	for x := range A {
		if T[x] {
			tp++
		}
	}
	if len(A) > 0 {
		p = float64(tp) / float64(len(A))
	}
	r = float64(tp) / float64(len(T))
	if p+r > 0 {
		f = 2 * p * r / (p + r)
	}
	return p, r, f
}

func toSet(xs []string) map[string]bool {
	s := map[string]bool{}
	for _, x := range xs {
		if n := normName(x); n != "" {
			s[n] = true
		}
	}
	return s
}

// normName reduces a symbol reference to its last identifier, lowercased, so
// "ValidationCodesService.getActiveCode", "service.getActiveCode()" and
// "getActiveCode" all compare equal. (Collisions between same-named methods in
// different classes are folded together — an accepted approximation, noted in
// docs/QUALITY.md.)
func normName(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "()")
	if i := strings.LastIndexAny(s, "./:#\\"); i >= 0 {
		s = s[i+1:]
	}
	s = strings.TrimSuffix(s, "()")
	return strings.ToLower(strings.TrimSpace(s))
}

// matchDefinition is true if the answer points at the same file (by basename)
// and a line within ±3 of any truth entry. A missing line matches on file alone.
func matchDefinition(answer, truth []string) bool {
	for _, a := range answer {
		af, al := splitFileLine(a)
		if af == "" {
			continue
		}
		for _, t := range truth {
			tf, tl := splitFileLine(t)
			if tf == "" || path.Base(af) != path.Base(tf) {
				continue
			}
			if al == 0 || tl == 0 || abs(al-tl) <= 3 {
				return true
			}
		}
	}
	return false
}

func splitFileLine(s string) (file string, line int) {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\\", "/"))
	i := strings.LastIndex(s, ":")
	if i < 0 {
		return s, 0
	}
	if n, err := strconv.Atoi(strings.TrimSpace(s[i+1:])); err == nil {
		return strings.TrimSpace(s[:i]), n
	}
	return s, 0
}

func clamp01(f float64) float64 {
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}
func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
func abs(i int) int {
	if i < 0 {
		return -i
	}
	return i
}
