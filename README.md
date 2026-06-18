# codegraph

A tiny, **token-efficient code knowledge graph** for AI coding agents, in Go.

Index a repo into a SQLite graph of symbols and relationships; an agent queries
the graph (who-calls, callers/callees, ranked search) instead of reading files
one by one — answering structural questions with far fewer tokens.

A scoped-down Go reimplementation of the ideas in
[DeusData/codebase-memory-mcp](https://github.com/DeusData/codebase-memory-mcp)
(pure C). We target **our stack** (TypeScript/JS + Go + NestJS) and bet on
**LSP delegation** (gopls/tsserver) for accurate call edges instead of
re-implementing a type checker. Full background in [`docs/UPSTREAM.md`](docs/UPSTREAM.md).

> Status: **M0 scaffold — runnable.** Indexes, stores, FTS-searches, serves MCP.
> Call-edge resolution and tree-sitter ASTs are the next milestones — see
> [`docs/ROADMAP.md`](docs/ROADMAP.md).

## Quick start

```bash
go build -o codegraph ./cmd/codegraph

./codegraph index /path/to/repo      # build the graph
./codegraph stats /path/to/repo      # node/edge counts
./codegraph cli search /path/to/repo '{"query":"getActiveCode","limit":5}'
./codegraph mcp /path/to/repo        # serve over MCP (stdio) for an agent
```

Store lives in `~/.cache/codegraph/<project>.db`.

## Why

The whole graph is **two tables** (`nodes`, `edges`) + FTS5 — storage is trivial.
The value is in (a) accurate **call edges** and (b) returning **compact refs**
(`name + file + line`), never source, so the agent reads code only when it must.
That selectivity is where the ~10× token saving comes from.

## Docs

- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) — the Go design.
- [`docs/UPSTREAM.md`](docs/UPSTREAM.md) — everything about the original project
  (schema, pipeline, benchmarks, honest assessment, links).
- [`docs/ROADMAP.md`](docs/ROADMAP.md) — milestones.

## Layout

```
cmd/codegraph/    CLI
internal/graph/   model + SQLite store (the 2-table graph + FTS5)
internal/index/   discover, definitions (regex MVP), calls (stub), pipeline
internal/query/   query engine → compact refs
internal/mcp/     stdio JSON-RPC MCP server
_upstream/        shallow clone of the original, for reference (gitignored)
```

## License

MIT.
