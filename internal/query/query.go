// Package query turns store calls into compact, agent-friendly results.
//
// Token-efficiency principle: every result is a small struct (name + file +
// line + label), NEVER source code. The agent asks for Snippet only when it
// actually needs to read code. That selectivity is where the 10x token saving
// comes from — see docs/ARCHITECTURE.md.
package query

import (
	"strconv"
	"strings"

	"github.com/Lordymine/codegraph/internal/graph"
	"github.com/Lordymine/codegraph/internal/index"
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

// CompactRefs renders refs as the token-efficient wire format: one tab-separated
// line per ref — `label<TAB>name<TAB>file:line<TAB>qn`. No repeated JSON keys, and
// the project prefix is stripped from the qualified name (the engine re-adds it on
// input, so a returned qn can be passed straight back to callers/callees). This is
// the format the MCP/CLI tools return AND the format the benchmark meters, so the
// reported token win reflects the real product, not a measurement trick.
func CompactRefs(refs []Ref) string {
	var b strings.Builder
	for _, r := range refs {
		b.WriteString(r.Label)
		b.WriteByte('\t')
		b.WriteString(r.Name)
		b.WriteByte('\t')
		b.WriteString(r.File)
		b.WriteByte(':')
		b.WriteString(strconv.Itoa(r.StartLine))
		b.WriteByte('\t')
		b.WriteString(StripProjectPrefix(r.QualifiedName))
		b.WriteByte('\n')
	}
	return b.String()
}

// StripProjectPrefix drops the `project:` prefix from a qualified name. The rest
// is still globally unambiguous within a project, so it round-trips through
// normalizeQN on the next query.
func StripProjectPrefix(qn string) string {
	if _, rest, found := strings.Cut(qn, ":"); found {
		return rest
	}
	return qn
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

// Similar: near-clone symbols (SIMILAR_TO edges) of this one. The edge is stored
// once as smaller-QN -> larger-QN, so a clone may sit on either side — hence both
// directions, filtered to SIMILAR_TO so call/define neighbors don't leak in.
func (e *Engine) Similar(qualifiedName string, limit int) ([]Ref, error) {
	return e.neighbors(qualifiedName, "both", "SIMILAR_TO", limit)
}

// normalizeQN lets callers pass a qualified name with or without the project
// prefix — the compact wire format strips it, so a returned qn comes back short.
func (e *Engine) normalizeQN(qn string) string {
	prefix := e.project + ":"
	if strings.HasPrefix(qn, prefix) {
		return qn
	}
	return prefix + qn
}

func (e *Engine) neighbors(qn, dir, edgeType string, limit int) ([]Ref, error) {
	ns, err := e.store.Neighbors(e.project, e.normalizeQN(qn), dir, edgeType, limit)
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

// DetectChanges reports which source files changed since the last index — the
// staleness check behind the detect_changes tool. The agent can tell whether the
// graph is fresh for a region, and re-index if not (cheap now: scope-gated).
func (e *Engine) DetectChanges() (index.Changes, error) {
	return index.DetectChanges(e.store, e.project, e.repoRoot)
}
