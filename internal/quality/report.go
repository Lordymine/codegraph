package quality

import (
	"fmt"
	"sort"
	"strings"
)

// Report renders the full quality comparison as markdown — the answer-quality
// half of the upstream table (quality% graph vs baseline) joined with the token/
// tool-call cost each mode paid to get there.
func Report(qs []Question, truths []Truth, answers []Answer) string {
	scores, aggs := Evaluate(qs, truths, answers)

	var b strings.Builder
	b.WriteString("# codegraph quality harness\n\n")

	// Headline: graph vs baseline.
	b.WriteString("## Answer quality vs cost\n\n")
	b.WriteString("| mode | mean quality | tokens | tool calls |\n")
	b.WriteString("|---|--:|--:|--:|\n")
	for _, mode := range modesOf(aggs) {
		a := aggs[mode]
		b.WriteString(fmt.Sprintf("| **%s** | %.0f%% | %d | %d |\n",
			mode, 100*a.MeanQuality, a.TotalTokens, a.TotalCalls))
	}

	// Per-type quality.
	b.WriteString("\n## Quality by question type\n\n")
	types := []QType{TypeCallers, TypeCallees, TypeDefinition, TypeOpen}
	b.WriteString("| mode |")
	for _, t := range types {
		b.WriteString(" " + string(t) + " |")
	}
	b.WriteString("\n|---|" + strings.Repeat("--:|", len(types)) + "\n")
	for _, mode := range modesOf(aggs) {
		a := aggs[mode]
		b.WriteString("| **" + mode + "** |")
		for _, t := range types {
			if v, ok := a.ByType[t]; ok {
				b.WriteString(fmt.Sprintf(" %.0f%% |", 100*v))
			} else {
				b.WriteString(" – |")
			}
		}
		b.WriteString("\n")
	}

	// Per-question detail.
	b.WriteString("\n## Per question\n\n")
	b.WriteString("| id | type | graph | baseline |\n|---|---|--:|--:|\n")
	byID := map[string]map[string]Score{}
	for _, s := range scores {
		if byID[s.ID] == nil {
			byID[s.ID] = map[string]Score{}
		}
		byID[s.ID][s.Mode] = s
	}
	for _, q := range qs {
		m := byID[q.ID]
		b.WriteString(fmt.Sprintf("| `%s` | %s | %s | %s |\n",
			q.ID, q.Type, pct(m["graph"]), pct(m["baseline"])))
	}

	b.WriteString("\n> Quality = F1 vs the oracle truth for callers/callees, " +
		"file:line match for definition, LLM-judge (0–100%) for open. " +
		"Ground truth is established independently of the graph — see docs/QUALITY.md.\n")
	return b.String()
}

func pct(s Score) string {
	if s.ID == "" {
		return "–"
	}
	return fmt.Sprintf("%.0f%%", 100*s.Quality)
}

func modesOf(aggs map[string]Agg) []string {
	var ms []string
	for m := range aggs {
		ms = append(ms, m)
	}
	// graph first, then baseline, then any others alphabetically.
	sort.Slice(ms, func(i, j int) bool {
		rank := func(s string) int {
			switch s {
			case "graph":
				return 0
			case "baseline":
				return 1
			}
			return 2
		}
		if rank(ms[i]) != rank(ms[j]) {
			return rank(ms[i]) < rank(ms[j])
		}
		return ms[i] < ms[j]
	})
	return ms
}
