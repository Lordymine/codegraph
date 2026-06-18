package index

import "github.com/Lordymine/codegraph/internal/graph"

// ResolveCalls is the CALLS-edge pass — and it is the single most important and
// hardest part of the whole project. It is currently a STUB returning no edges.
//
// Why it's hard: turning `foo()` at a call site into an edge to the *correct*
// `foo` definition needs scope + import + type resolution. Same-named symbols
// (our own repo has FOUR `getActiveCode`), methods on inferred receiver types,
// dynamic dispatch, DI-by-string-token (NestJS), and decoupled producers/
// consumers (pg-boss) all defeat naive name matching.
//
// The plan (docs/ROADMAP.md):
//   1. tree-sitter to find call expressions + their enclosing function.
//   2. Delegate type/definition resolution to the language's own LSP server
//      (gopls, tsserver/typescript-go) via textDocument/definition. This buys
//      us the accuracy of a real type checker without re-implementing one per
//      language — the key bet that makes a small Go port viable.
//   3. Emit CALLS edges (caller QN -> callee QN). Unresolved calls are dropped
//      by the store (endpoints must exist), so precision stays honest.
func ResolveCalls(project string, files []SourceFile) []graph.Edge {
	// TODO(milestone-2): tree-sitter + LSP-delegated resolution.
	return nil
}
