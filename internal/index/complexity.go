package index

import tree_sitter "github.com/tree-sitter/go-tree-sitter"

// Cyclomatic complexity = 1 + the number of decision points in a function body
// (McCabe). The decision-node kinds differ per language, and boolean short-circuit
// operators (&&, ||, ??) count too — each is an independent path. tree-sitter
// exposes those operators as anonymous token nodes whose Kind() is the literal, so
// one full-subtree walk catches both control-flow nodes and operators uniformly.
// Only cyclomatic for now; cognitive/loop-depth (ROADMAP M4) stay deferred (YAGNI).

var goDecisionKinds = map[string]bool{
	"if_statement": true, "for_statement": true,
	"expression_case": true, "type_case": true, "communication_case": true,
	"&&": true, "||": true,
}

var tsDecisionKinds = map[string]bool{
	"if_statement": true, "for_statement": true, "for_in_statement": true,
	"while_statement": true, "do_statement": true,
	"switch_case": true, "catch_clause": true, "ternary_expression": true,
	"&&": true, "||": true, "??": true,
}

func goCyclomatic(n *tree_sitter.Node) int { return 1 + countDecisions(n, goDecisionKinds) }
func tsCyclomatic(n *tree_sitter.Node) int { return 1 + countDecisions(n, tsDecisionKinds) }

// countDecisions walks the whole subtree — including anonymous token nodes, so the
// boolean operators count — and tallies the nodes whose kind is a decision point.
func countDecisions(n *tree_sitter.Node, kinds map[string]bool) int {
	count := 0
	for i := uint(0); i < n.ChildCount(); i++ {
		child := n.Child(i)
		if kinds[child.Kind()] {
			count++
		}
		count += countDecisions(child, kinds)
	}
	return count
}
