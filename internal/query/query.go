// Package query turns store calls into compact, agent-friendly results.
//
// Token-efficiency principle: every result is a small struct (name + file +
// line + label), NEVER source code. The agent asks for Snippet only when it
// actually needs to read code. That selectivity is where the 10x token saving
// comes from — see docs/ARCHITECTURE.md.
package query

import (
	"github.com/Lordymine/codegraph/internal/graph"
)

// Ref is the compact reference returned for every symbol.
type Ref struct {
	Name          string `json:"name"`
	QualifiedName string `json:"qualified_name"`
	Label         string `json:"label"`
	File          string `json:"file"`
	StartLine     int    `json:"start_line"`
	EndLine       int    `json:"end_line"`
}

func refOf(n graph.Node) Ref {
	return Ref{
		Name: n.Name, QualifiedName: n.QualifiedName, Label: string(n.Label),
		File: n.FilePath, StartLine: n.StartLine, EndLine: n.EndLine,
	}
}

// Engine wraps a store + repo root for a single project.
type Engine struct {
	store    *graph.Store
	project  string
	repoRoot string
}

func NewEngine(store *graph.Store, project, repoRoot string) *Engine {
	return &Engine{store: store, project: project, repoRoot: repoRoot}
}

// Search: ranked symbol search (BM25). label optional ("Function", "Class"...).
func (e *Engine) Search(q, label string, limit int) ([]Ref, error) {
	hits, err := e.store.Search(e.project, q, label, limit)
	if err != nil {
		return nil, err
	}
	refs := make([]Ref, 0, len(hits))
	for _, h := range hits {
		refs = append(refs, refOf(h.Node))
	}
	return refs, nil
}

// Callers: who calls this symbol (inbound CALLS edges only).
func (e *Engine) Callers(qualifiedName string, limit int) ([]Ref, error) {
	return e.neighbors(qualifiedName, "in", "CALLS", limit)
}

// Callees: what this symbol calls (outbound CALLS edges only).
func (e *Engine) Callees(qualifiedName string, limit int) ([]Ref, error) {
	return e.neighbors(qualifiedName, "out", "CALLS", limit)
}

// Neighbors: all related nodes, any edge type, both directions.
func (e *Engine) Neighbors(qualifiedName string, limit int) ([]Ref, error) {
	return e.neighbors(qualifiedName, "both", "", limit)
}

func (e *Engine) neighbors(qn, dir, edgeType string, limit int) ([]Ref, error) {
	ns, err := e.store.Neighbors(e.project, qn, dir, edgeType, limit)
	if err != nil {
		return nil, err
	}
	refs := make([]Ref, 0, len(ns))
	for _, n := range ns {
		refs = append(refs, refOf(n))
	}
	return refs, nil
}

// Snippet: the actual source for a node (only when the agent needs to read).
func (e *Engine) Snippet(filePath string, start, end int) (string, error) {
	return graph.Snippet(e.repoRoot, filePath, start, end)
}
