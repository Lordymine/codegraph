package scip

import (
	"strings"

	scippb "github.com/scip-code/scip/bindings/go/scip"

	"github.com/Lordymine/codegraph/internal/graph"
)

// Enclosing maps a call-site (repo-relative file + 1-based line) to the qualified
// name of the function/method that contains it — i.e. the caller — and reports
// whether a qualified name is a known call target.
type Enclosing interface {
	At(relpath string, line int) (qn string, ok bool)
	Has(qn string) bool
}

// CallEdges turns SCIP reference occurrences into CALLS edges: callee = the
// type-checker-resolved referenced symbol, caller = the function/method enclosing
// the reference. Only references (not definitions) to invocable symbols count.
// Edges whose endpoints don't map are skipped; the store drops any that don't
// resolve to real nodes (honest precision).
func CallEdges(index *scippb.Index, project, pathPrefix string, enc Enclosing) []graph.Edge {
	var edges []graph.Edge
	seen := map[string]bool{}
	for _, doc := range index.GetDocuments() {
		rel := joinPath(pathPrefix, doc.GetRelativePath())
		for _, occ := range doc.GetOccurrences() {
			if occ.GetSymbolRoles()&int32(scippb.SymbolRole_Definition) != 0 {
				continue // a definition site is not a call
			}
			if !invocable(occ.GetSymbol()) {
				continue
			}
			calleeQN, ok := symbolToQN(occ.GetSymbol(), project, pathPrefix)
			if !ok || !enc.Has(calleeQN) {
				continue // external/unknown target — drop the candidate up front
			}
			r := occ.GetRange()
			if len(r) == 0 {
				continue
			}
			callerQN, ok := enc.At(rel, int(r[0])+1) // SCIP lines are 0-based
			// A self-edge (recursion) is kept: a function referencing itself is a real
			// caller of itself, counted by the eval oracle (parity with the Go resolver).
			if !ok {
				continue
			}
			key := callerQN + "\x00" + calleeQN
			if seen[key] {
				continue
			}
			seen[key] = true
			edges = append(edges, graph.Edge{
				Project: project, SourceQN: callerQN, TargetQN: calleeQN,
				Type:  graph.EdgeCalls,
				Props: map[string]any{"resolver": "scip", "confidence": "high"},
			})
		}
	}
	return edges
}

// invocable reports whether a symbol is a function/method (callable), filtering
// out types, parameters, and locals by the last descriptor's suffix.
func invocable(symbol string) bool {
	p, err := scippb.ParseSymbol(symbol)
	if err != nil || len(p.Descriptors) == 0 {
		return false
	}
	last := p.Descriptors[len(p.Descriptors)-1]
	return last.Suffix == scippb.Descriptor_Method || last.Suffix == scippb.Descriptor_Term
}

func joinPath(prefix, rel string) string {
	rel = strings.ReplaceAll(rel, "\\", "/")
	if prefix == "" {
		return rel
	}
	return strings.TrimSuffix(prefix, "/") + "/" + rel
}

// EnclosingNodes is an Enclosing built from graph nodes: for a (file, line) it
// returns the innermost Function/Method node whose line span contains it, and
// tracks all Function/Method QNs as the set of valid call targets.
type EnclosingNodes struct {
	byFile map[string][]nodeSpan
	qns    map[string]bool
}

type nodeSpan struct {
	start, end int
	qn         string
}

// BuildEnclosing indexes Function/Method nodes by file for caller lookup and by
// QN for callee validation.
func BuildEnclosing(nodes []graph.Node) *EnclosingNodes {
	e := &EnclosingNodes{byFile: make(map[string][]nodeSpan), qns: make(map[string]bool)}
	for _, n := range nodes {
		if n.Label != graph.LabelFunction && n.Label != graph.LabelMethod {
			continue
		}
		e.byFile[n.FilePath] = append(e.byFile[n.FilePath], nodeSpan{n.StartLine, n.EndLine, n.QualifiedName})
		e.qns[n.QualifiedName] = true
	}
	return e
}

// Has reports whether qn is a known function/method node (a valid call target).
func (e *EnclosingNodes) Has(qn string) bool { return e.qns[qn] }

func (e *EnclosingNodes) At(relpath string, line int) (string, bool) {
	best, bestSize := "", 1<<30
	for _, s := range e.byFile[relpath] {
		if line >= s.start && line <= s.end {
			if sz := s.end - s.start; sz < bestSize {
				bestSize, best = sz, s.qn
			}
		}
	}
	return best, best != ""
}
