package quality

import (
	"fmt"

	"github.com/Lordymine/codegraph/internal/graph"
	"github.com/Lordymine/codegraph/internal/query"
)

// Generate builds a deterministic question set from an indexed project, mirroring
// the upstream's per-language structure (~14 questions): a mix of structural
// queries (callers/callees/definition, objectively scorable) and open
// comprehension queries (judge-scored). Candidates are picked from the graph —
// picking WHAT to ask is not circular; the ground-truth answers come from the
// oracle, not the graph.
//
// Symbols are sampled in STRATA across the call-degree distribution (hub →
// typical → leaf), not just the top hubs. Cherry-picking only the most-called
// symbols would flatter the graph (where grep struggles most); a representative
// set must include the easy leaf cases too. The open questions still target hubs
// (worth explaining); the structural ones span the distribution.
func Generate(st *graph.Store, project, lang string) ([]Question, error) {
	var qs []Question
	add := func(q Question) {
		q.Lang = lang
		qs = append(qs, q)
	}

	// Pull the full ranked list (cap high), then sample evenly across it.
	inRanked, err := st.TopByInboundCalls(project, 1000)
	if err != nil {
		return nil, err
	}
	inHubs := stratifiedSample(inRanked, 6)
	for i, n := range inHubs {
		add(Question{
			ID: fmt.Sprintf("callers-%02d", i+1), Type: TypeCallers,
			Symbol: n.Name, QN: query.StripProjectPrefix(n.QualifiedName),
			File: n.FilePath, Line: n.StartLine,
			Prompt: fmt.Sprintf("List EVERY function or method in this repository that directly calls `%s` "+
				"(the one defined in %s:%d). Answer as a list of the caller symbol names.",
				n.Name, n.FilePath, n.StartLine),
		})
	}

	outRanked, err := st.TopByOutboundCalls(project, 1000)
	if err != nil {
		return nil, err
	}
	outHubs := stratifiedSample(outRanked, 3)
	for i, n := range outHubs {
		add(Question{
			ID: fmt.Sprintf("callees-%02d", i+1), Type: TypeCallees,
			Symbol: n.Name, QN: query.StripProjectPrefix(n.QualifiedName),
			File: n.FilePath, Line: n.StartLine,
			Prompt: fmt.Sprintf("List every function or method DEFINED IN THIS REPOSITORY that `%s` (defined "+
				"in %s:%d) calls directly. Exclude calls into the standard library or third-party "+
				"dependencies (e.g. fmt.*, os.*, builtins) — only intra-repo callees count. Keep "+
				"func-value/field invocations defined here. Answer as a list of the callee symbol names.",
				n.Name, n.FilePath, n.StartLine),
		})
	}

	defs := sampleDefinitions(st, project, 3)
	for i, n := range defs {
		add(Question{
			ID: fmt.Sprintf("definition-%02d", i+1), Type: TypeDefinition,
			Symbol: n.Name, QN: query.StripProjectPrefix(n.QualifiedName),
			Prompt: fmt.Sprintf("Where is `%s` defined? Answer with a single `relpath:line`.", n.Name),
		})
	}

	// Open comprehension questions — where a structural graph is weakest and the
	// honest token/quality trade-off shows. These target real hubs (the top of the
	// ranking) since a widely-used symbol is the one worth explaining. Judge-scored.
	for i, n := range firstN(inRanked, 2) {
		add(Question{
			ID: fmt.Sprintf("open-%02d", i+1), Type: TypeOpen,
			Symbol: n.Name, QN: query.StripProjectPrefix(n.QualifiedName), File: n.FilePath,
			Prompt: fmt.Sprintf("In 2-4 sentences, explain the responsibility of `%s` (%s) and how it fits "+
				"into the surrounding module — what calls it and what it depends on.", n.Name, n.FilePath),
		})
	}

	return qs, nil
}

// sampleDefinitions picks a few defined symbols (functions first, then methods)
// for "where is X defined" questions.
func sampleDefinitions(st *graph.Store, project string, n int) []graph.Node {
	var out []graph.Node
	if fns, err := st.SampleByLabel(project, string(graph.LabelFunction), n); err == nil {
		out = append(out, fns...)
	}
	if len(out) < n {
		if ms, err := st.SampleByLabel(project, string(graph.LabelMethod), n-len(out)); err == nil {
			out = append(out, ms...)
		}
	}
	return firstN(out, n)
}

func firstN(ns []graph.Node, n int) []graph.Node {
	if len(ns) < n {
		return ns
	}
	return ns[:n]
}

// stratifiedSample picks n items spread evenly across a slice already sorted by
// call degree (descending). It returns the top item, the bottom item, and evenly
// spaced points between — so a question set spans hubs, typical symbols and leaf
// symbols instead of only the most-called ones. Deterministic.
func stratifiedSample(sorted []graph.Node, n int) []graph.Node {
	if n <= 0 || len(sorted) == 0 {
		return nil
	}
	if len(sorted) <= n {
		return sorted
	}
	out := make([]graph.Node, 0, n)
	seen := map[int]bool{}
	for i := range n {
		// even positions across [0, len-1]; i=0 -> top, i=n-1 -> last.
		idx := i * (len(sorted) - 1) / (n - 1)
		if seen[idx] {
			idx++ // avoid a duplicate when slots collide on a short slice
		}
		if idx >= len(sorted) {
			break
		}
		seen[idx] = true
		out = append(out, sorted[idx])
	}
	return out
}
