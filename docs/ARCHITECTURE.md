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
callers/callees), `Snippet` (reads file lines), `Stats`, `FileHashes`,
`ForEachCallEdge` (streaming CALLS for incremental reuse), `ReplaceProject`,
`DBPath`, `Reopen`, and `BeginReadSnapshot`/`EndReadSnapshot` (pins a WAL read
snapshot so a second connection can stream pre-wipe CALLS edges while the writer
connection runs `ReplaceProject`).

## Indexing pipeline (internal/index)

Two entry points share `prepareIndexing` + `runPipeline`:

- **`Run(store, root)`** — tests and direct store use. Opens a **second** `Store` on
  the same DB path, starts a read snapshot (`BeginReadSnapshot`), then wipes and
  rebuilds on the writer connection. Unchanged scopes' CALLS are streamed from the
  snapshot connection via `insertReusedCallEdges`.
- **`RunAtomic(dbPath, root)`** — CLI/MCP production path. Builds into
  `dbPath+.building`, renames on success; a failed re-index leaves the previous graph
  at `dbPath` intact. Reuse reads from the main store file (separate path, never wiped
  mid-pipeline) into the building store.

```
prepareIndexing(store)    DetectChanges → no-op Result if unchanged; else pipelineInput
runPipeline(store, in)
  Discover(root)          file walk; hard-ignores + .gitignore + .cbmignore; language detect
  ReplaceProject          wipe project nodes/edges/FTS on the writer connection
  → indexDefinitionsBatched  tree-sitter defs in bounded batches (memory.MaxWorkers)
  → collectImportsStreaming  IMPORTS edges flushed per file (TS/JS)
  → resolveTSCalls        scip-typescript per changed tsconfig scope; ScopesRun++ only on success
  → resolveGoCalls        go/packages VTA for changed Go scope (best-effort, same contract as TS)
  → insertReusedCallEdges unchanged scopes' CALLS from reuseFrom (snapshot or main file)
  → resolveSimilarFromSpans  SIMILAR_TO from function spans only (skipped on low-RAM hosts)
  memory.Gate()           between every heavy phase — returns freed pages to the OS
```

`ExtractDefinitions` (definitions.go + treesitter.go) parses each file with the
official **tree-sitter** (cgo, one parser per goroutine) and emits `File`/`Function`/
`Method`/`Class`/`Interface`/`Type`/`Enum`/`Variable` nodes + `DEFINES` edges — with
real end lines, `is_exported`, and class/method decorators. `ResolveImports`
(imports.go) resolves relative TS/JS imports to File nodes → `IMPORTS` edges (package
and unresolved imports drop). `ResolveCalls` (calls.go) emits `CALLS` edges via the M2
batch indexers — scip-typescript for TS/JS (`internal/scip`) and go/packages + a VTA
call graph for Go (`internal/gocalls`) — dropping callees that aren't known graph symbols.
Incremental (M3, incremental.go): `DetectChanges` gates a no-op when nothing changed, and
a re-index re-resolves only the changed scopes, reusing the stored CALLS edges of the rest
via `forEachReusableCallEdge` + batched `insertReusedCallEdges`.

### Memory budget (internal/memory)

Indexing auto-tunes at process start from installed RAM (and WSL detection): worker
count, definition batch size, Go `debug.SetMemoryLimit`, scip-typescript
`--max-old-space-size`, and optional `SkipSimilar` on constrained hosts.
`CODEGRAPH_*` env vars override for debugging only — users need not set anything.
`memory.Gate()` runs between pipeline phases (and after each scip scope) to hand freed
heap back to the OS, which matters for the long-running MCP server after a large index.

M4 enrichment: `ResolveSimilar` (similar.go) emits `SIMILAR_TO` near-clone edges from a
MinHash signature + LSH banding over each function's token shingles (`internal/similar`,
no embeddings). The definitions pass also stamps McCabe cyclomatic complexity onto each
Function/Method (`complexity.go`, one tree-sitter subtree walk) into `properties.complexity`.
The Go/TS call resolvers credit calls inside closures to the enclosing named function and
keep recursive self-edges — recall fixes that took intra-repo callers to ~100% (see
`docs/QUALITY.md`).

M5: the definitions pass also emits **`Route` nodes** from NestJS decorators
(`routes.go`) — `@Controller` base + `@Get/@Post/...` path → `<VERB> <path>`, located at
the handler. `get_architecture` (`query/architecture.go`) aggregates the stored graph
into a one-shot repo map (languages, node/edge counts, top packages, complexity/call
hotspots) — the orientation call. `HTTP_CALLS` (client → route) is deferred to M6: it
would be heuristic string matching, not type-checker-delegated.

## Query layer (internal/query)

`Engine` exposes the agent-facing operations: `Search`, `Callers`, `Callees`,
`Neighbors`, `Similar`, `DeadCode` (each returning `[]Ref`), `Architecture` (the repo
map — languages/counts/packages/hotspots, rendered compactly), `Snippet`, and
`DetectChanges`. `Close`/`Reopen` wrap the underlying `Store` — the MCP server closes
before a background `RunAtomic` (Windows file lock), then reopens the committed graph.
This is the contract both the CLI and the MCP server use, so behavior is identical
across entry points. Relationship queries default to limit 500 (a hub can have hundreds
of callers — a low cap would silently truncate the answer).

## MCP server (internal/mcp)

Minimal stdio JSON-RPC 2.0 (newline-delimited — the MCP convention), stdlib only.
Handles `initialize`, `tools/list`, `tools/call`. Tools: `search`, `callers`,
`callees`, `neighbors`, `similar`, `dead_code`, `snippet`, `detect_changes`. Swap for
`github.com/mark3labs/mcp-go` if it grows.

The `mcp` command (M5) auto-indexes in a background goroutine on startup and gates
tool calls behind a readiness check (`Server.SetReadiness`) — the handshake answers
immediately, tools report "indexing" until the graph is built, never a half-written
store. On index failure, `RunAtomic` leaves the previous graph on disk; the server
reopens it and sets `ready=true` so tools keep working, prepending the failure status
to every tool response (stale-data context). Do not run `codegraph index` on the same
repo while MCP is auto-indexing — both contend for the same store file. The repo is
resolved from `$CLAUDE_PROJECT_DIR` (set by Claude Code) or cwd, so one registration
serves any repo. `codegraph install` (`internal/install`) registers the server into
detected agents — Claude Code/Codex via their add-CLI, opencode via a config-file
merge — and prints a manual snippet for the rest.

## CLI (cmd/codegraph)

```
codegraph index   <path>               atomic build (RunAtomic; no-op if unchanged)
codegraph stats   <path>               node/edge counts
codegraph changes <path>               files changed since the last index
codegraph install                      register the MCP server into detected agents
codegraph mcp     <path>               serve MCP over stdio (auto-indexes in background)
codegraph cli     <tool> <path> <json> run one query tool (no MCP)
```

Store path: `~/.cache/codegraph/<project>.db`. Project slug derived from the
absolute repo path (matches upstream convention).

## Package layout

```
cmd/codegraph/        CLI entrypoint + subcommands (index/stats/mcp/bench/quality/cli)
internal/graph/       model.go (Node/Edge/labels/edge-types) + store.go (SQLite)
internal/index/       discover.go, definitions.go + treesitter.go + complexity.go + routes.go, imports.go, calls.go, similar.go, incremental.go, prepare.go, pipeline.go
internal/memory/      auto-tuned indexing RAM budget + Gate() between phases
internal/scip/        scip-typescript runner + SCIP→CALLS attribution (TS/JS, M2)
internal/gocalls/     go/packages + VTA call graph → CALLS (Go, M2; cha.go = generics-safe)
internal/similar/     MinHash signature + LSH banding → SIMILAR_TO near-clone edges (M4)
internal/query/       query.go (Engine → compact Refs)
internal/mcp/         server.go (stdio JSON-RPC + auto-index readiness gate)
internal/install/     register the MCP server into detected agents (M5)
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
