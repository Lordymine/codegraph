package scip

import (
	"github.com/Lordymine/codegraph/internal/graph"
)

// BuildEnclosingFromSpans indexes lightweight function spans for caller lookup and
// callee validation without holding full graph.Node values in memory.
func BuildEnclosingFromSpans(spans []graph.FunctionSpan) *EnclosingNodes {
	e := &EnclosingNodes{byFile: make(map[string][]nodeSpan), qns: make(map[string]bool)}
	for _, s := range spans {
		e.byFile[s.FilePath] = append(e.byFile[s.FilePath], nodeSpan{s.StartLine, s.EndLine, s.QualifiedName})
		e.qns[s.QualifiedName] = true
	}
	return e
}
