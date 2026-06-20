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

// DeadCode lists private Function/Method nodes the graph sees no caller for: zero
// inbound CALLS, minus the entry points whose callers can't be in-graph by design
// — exported symbols (public API), decorated members (framework-invoked),
// main/init, and test functions.
//
// It is a candidate list to investigate, NOT a delete list. Precision is bounded
// by CALLS recall: a real caller the resolver missed, or an indirect reference
// (function value, interface dispatch, reflection), makes a live function look
// dead. On cobra, the top results were mostly such false positives — but it did
// surface `appendIfNotPresent`, which cobra's own source marks unused. The agent
// must confirm each (e.g. grep the name) before acting.
func (e *Engine) DeadCode(limit int) ([]Ref, error) {
	if limit <= 0 {
		limit = 100
	}
	cands, err := e.store.FunctionsWithoutInboundCalls(e.project)
	if err != nil {
		return nil, err
	}
	refs := make([]Ref, 0, limit)
	for _, n := range cands {
		if isEntryPoint(n) {
			continue
		}
		refs = append(refs, refOf(n))
		if len(refs) >= limit {
			break
		}
	}
	return refs, nil
}

// isEntryPoint reports whether an uncalled symbol legitimately has no in-graph
// caller — so the absence of callers is not evidence that it's dead.
func isEntryPoint(n graph.Node) bool {
	if n.Props["is_exported"] == true {
		return true
	}
	if n.Name == "main" || n.Name == "init" {
		return true
	}
	if index.IsTestFile(n.FilePath) {
		return true
	}
	return hasDecorators(n.Props)
}

// hasDecorators reports whether a node carries any decorator. The value round-trips
// through JSON as []any (string slices come back as []interface{}), so accept both.
func hasDecorators(props map[string]any) bool {
	switch d := props["decorators"].(type) {
	case []any:
		return len(d) > 0
	case []string:
		return len(d) > 0
	}
	return false
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
