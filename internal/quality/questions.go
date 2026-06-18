package quality

import (
	"fmt"

	"github.com/Lordymine/codegraph/internal/graph"
	"github.com/Lordymine/codegraph/internal/query"
)

// Generate builds a deterministic question set from an indexed project, mirroring
// the upstream's per-language structure (~12 questions): a mix of structural
// queries (callers/callees/definition, objectively scorable) and open
// comprehension queries (judge-scored). Candidates are picked from the graph
// (hubs) — picking WHAT to ask is not circular; the ground-truth answers come
// from the oracle, not the graph.
func Generate(st *graph.Store, project, lang string) ([]Question, error) {
	var qs []Question
	add := func(q Question) {
		q.Lang = lang
		qs = append(qs, q)
	}

	inHubs, err := st.TopByInboundCalls(project, 6)
	if err != nil {
		return nil, err
	}
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

	outHubs, err := st.TopByOutboundCalls(project, 3)
	if err != nil {
		return nil, err
	}
	for i, n := range outHubs {
		add(Question{
			ID: fmt.Sprintf("callees-%02d", i+1), Type: TypeCallees,
			Symbol: n.Name, QN: query.StripProjectPrefix(n.QualifiedName),
			File: n.FilePath, Line: n.StartLine,
			Prompt: fmt.Sprintf("List EVERY function or method that `%s` (defined in %s:%d) calls directly. "+
				"Answer as a list of the callee symbol names.",
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
	// honest token/quality trade-off shows. Judge-scored.
	for i, n := range firstN(inHubs, 2) {
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
