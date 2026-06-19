# codegraph — architecture (our Go design)

A small, token-efficient code knowledge graph for AI agents. Inspired by
DeusData/codebase-memory-mcp (see `UPSTREAM.md`), but deliberately scoped down to
**our stack** (TypeScript/JS + Go + NestJS) and a maintainable Go codebase.

## Design principles

1. **The storage is trivial; the value is in the edges.** Two tables (`nodes`,
   `edges`) + FTS5 — same as upstream. We do not over-invest here.
2. **Token-efficiency by construction.** Every query returns a *compact ref*
   (`name + qualified_name + label + file + line`), **never source code**. The
   agent calls `snippet` only when it must read code. That selectivity is the
   entire point — it's where the 10× token saving comes from.
3. **Borrow the type checker, don't rebuild it.** Upstream re-implemented type
   resolution in C ("Hybrid LSP") across 9 languages — months of work. Our key
   bet: **delegate call resolution to the language's own batch indexer** —
   `scip-typescript` for TS/JS, `go/packages` + `go/callgraph` for Go (the libs
   gopls itself calls), read in-process. Same type-checker accuracy as interactive
   LSP but in one pass, with no long-lived server to babysit — and we skip the
   hardest part of the port.
4. **Honest precision.** Unresolved edges are dropped (endpoints must exist in
   the graph), so we never fabricate a call edge. Better a missing edge than a
   wrong one.
5. **Small dependency surface.** Pure-Go SQLite (`modernc.org/sqlite`), stdlib MCP
   server. The heavy deps are tree-sitter (cgo — the binary needs `CGO_ENABLED=1` +
   gcc) for M1 and the SCIP bindings + `go/packages` for M2; scip-typescript is a
   build-time tool (Node), not linked into the binary.

## Storage (internal/graph)

Mirrors upstream exactly so a future `.db` is shape-compatible:

```sql
nodes(id, project, label, name, qualified_name, file_path,
      start_line, end_line, properties JSON, UNIQUE(project, qualified_name))
edges(id, project, source_id, target_id, type, properties JSON,
      UNIQUE(source_id, target_id, type))
nodes_fts  -- FTS5(name, qualified_name, label, file_path) → BM25
-- indexes on edges(source, target, type, +composite) and nodes(label, name, file)
```

`Store` (internal/graph/store.go) is the only thing that touches SQL:
`InsertNodes` (keeps FTS in sync), `InsertEdges` (resolves QN→id, drops
unresolved), `Search` (BM25), `Neighbors` (in/out/both, the basis for
callers/callees), `Snippet` (reads file lines), `Stats`, `ReplaceProject`.

## Indexing pipeline (internal/index)

```
Discover(root)            file walk; hard-ignores + .cbmignore; language detect
  → ExtractDefinitions    per-file, in parallel — tree-sitter AST (treesitter.go)
  → ResolveImports        IMPORTS edges (TS/JS, relative File→File)
  → ResolveCalls          CALLS edges  scip-typescript (TS/JS) + go/packages CHA (Go)
  → Store.InsertNodes/Edges
```

`ExtractDefinitions` (definitions.go + treesitter.go) parses each file with the
official **tree-sitter** (cgo, one parser per goroutine) and emits `File`/`Function`/
`Method`/`Class`/`Interface`/`Type`/`Enum`/`Variable` nodes + `DEFINES` edges — with
real end lines, `is_exported`, and class/method decorators. `ResolveImports`
(imports.go) resolves relative TS/JS imports to File nodes → `IMPORTS` edges (package
and unresolved imports drop). `ResolveCalls` (calls.go) emits `CALLS` edges via the M2
batch indexers — scip-typescript for TS/JS (`internal/scip`) and go/packages + a CHA
call graph for Go (`internal/gocalls`) — dropping callees that aren't known graph symbols.

## Query layer (internal/query)

`Engine` exposes the agent-facing operations, each returning `[]Ref` (compact):
`Search`, `Callers`, `Callees`, `Neighbors`, `Snippet`. This is the contract both
the CLI and the MCP server use, so behavior is identical across entry points.

## MCP server (internal/mcp)

Minimal stdio JSON-RPC 2.0 (newline-delimited — the MCP convention), stdlib only.
Handles `initialize`, `tools/list`, `tools/call`. Tools: `search`, `callers`,
`callees`, `neighbors`, `snippet`. Swap for `github.com/mark3labs/mcp-go` if it
grows.

## CLI (cmd/codegraph)

```
codegraph index <path>                 build the graph
codegraph stats <path>                 node/edge counts
codegraph mcp   <path>                 serve MCP over stdio for a repo
codegraph cli   <tool> <path> <json>   run one query tool (no MCP)
```

Store path: `~/.cache/codegraph/<project>.db`. Project slug derived from the
absolute repo path (matches upstream convention).

## Package layout

```
cmd/codegraph/        CLI entrypoint + subcommands (index/stats/mcp/bench/quality/cli)
internal/graph/       model.go (Node/Edge/labels/edge-types) + store.go (SQLite)
internal/index/       discover.go, definitions.go + treesitter.go, imports.go, calls.go, pipeline.go
internal/scip/        scip-typescript runner + SCIP→CALLS attribution (TS/JS, M2)
internal/gocalls/     go/packages + CHA call graph → CALLS (Go, M2)
internal/query/       query.go (Engine → compact Refs)
internal/mcp/         server.go (stdio JSON-RPC)
internal/bench/       token/tool-call/speed benchmark harness
internal/quality/     answer-quality harness (question gen + scoring)
docs/                 UPSTREAM.md, ARCHITECTURE.md, ROADMAP.md, QUALITY.md, BENCHMARK.md
_upstream/            shallow clone of the original (gitignored, reference only)
```

## What we deliberately are NOT building (yet)

158 languages (→ just our stack), the in-binary embeddings + `semantic_query`
(was the 20-min bottleneck; MinHash/LSH is enough for SIMILAR_TO), the full
Cypher engine (→ fixed query shapes cover ~90% of agent use), C-style arena
allocators (Go GC + goroutines is simpler and fast enough at our repo scale).
