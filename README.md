<div align="center">

# codegraph

**A token-efficient code knowledge graph for AI coding agents.**

Stop letting your agent read the repo file-by-file. Index it once into a graph of
symbols and *type-checker-accurate* call edges, then answer structural questions —
*who calls this? what does it call? where is it?* — in **one query, compact refs,
a fraction of the tokens.**

`Go 1.26` · `SQLite (pure-Go, no cgo for storage)` · `MCP-native` · `MIT`
· status: **M0–M2 done — indexes, resolves calls, serves agents**

</div>

---

## TL;DR — the numbers

Measured on **ajuda-aqui** (857-file real NestJS + React/Next monorepo), fully
reproducible with `codegraph bench <repo>`:

| | codegraph | grep-driven agent | upstream (paper) |
|---|--:|--:|--:|
| **Tokens** to answer "who calls X" | **1×** | 16×–74× | 10× |
| **Tool calls** to answer it | **1** | up to 73 (avg 34×) | 2.1× |
| **Index time** | **30 s** | — | ~20 min¹ |
| **Call-edge accuracy** | type-checker-grade | n/a | "Hybrid LSP" (re-implemented) |

> **16× fewer tokens** against a *conservative* baseline (agent reads only a
> ±10-line window around each grep hit); **74×** against the common one (agent
> reads whole files); **206×** best-case. The conservative number is the one to
> trust — see [Methodology](docs/BENCHMARK.md).

¹ Upstream's ~20 min / 969 files on Windows; the gap is the on-device embedding
pass we skip (we trade semantic edges for a ~36× faster index — see below).

---

## The problem

An LLM agent dropped into an unfamiliar codebase explores the way a person can't
afford to: it `grep`s a symbol, gets dozens of hits, then **opens file after file**
to figure out which hits are real calls, which are imports, which are the
definition, which are a same-named decoy. Every opened file is tokens spent, and
the agent still ends up *guessing direction* — chasing the wrong call path because
flat text search has no idea what calls what.

That is the failure codegraph removes: **give the agent the call graph, so it
moves with direction instead of searching blind.**

## The idea

Index the repo into a tiny graph — **two tables** (`nodes`, `edges`) + an FTS5
index — and make every query return a **compact reference**, never source. One
tab-separated line per result, no JSON overhead, project prefix stripped:

```
label   name           file:line                    qualified_name
Method  getActiveCode  …/validation-codes.service.ts:64   …service.ts.ValidationCodesService.getActiveCode
```

The agent reads actual code only when it deliberately asks for a `snippet`, and a
returned `qualified_name` feeds straight back into `callers`/`callees`. That
selectivity *is* the token saving — halving this representation doubled our token
win (see [BENCHMARK](docs/BENCHMARK.md)). The graph's job is to answer
"who/what/where" structurally and hand back the smallest pointer that resolves it.

## The bet that makes it cheap and accurate

The upstream project ([DeusData/codebase-memory-mcp](https://github.com/DeusData/codebase-memory-mcp),
pure C) earned its accurate call edges the hard way: it **re-implemented per-language
type resolution** ("Hybrid LSP", ~9 language families, structurally inspired by
tsserver/gopls/Roslyn). Most of the effort and most of the quality live there.

codegraph makes the opposite bet:

> **Delegate call resolution to the type checkers that already exist.**
> TypeScript/JS via **[scip-typescript](https://github.com/sourcegraph/scip-typescript)**
> (a batch indexer, *not* an interactive LSP); Go via **`go/packages` + a CHA call
> graph** (the machinery gopls itself uses). We get type-checker-grade precision —
> param binding, overload/return-type inference, same-name disambiguation — for
> free, and skip the single hardest part of the port.

This is the project's central, falsifiable claim: **you don't need to re-implement
a type checker to build an accurate code knowledge graph for LLMs.** Delegation buys
the same call-edge quality at a fraction of the engineering cost *and* a ~36× faster
index (no embedding pass).

Two design rules keep it honest:

- **Honest precision.** An edge whose endpoints aren't both real nodes in the graph
  is **dropped**, not guessed. A missing edge beats a wrong one — an agent that
  trusts the graph must never be sent the wrong way. (ajuda-aqui: 6 edges dropped of
  ~8,100 — the type checker resolves almost everything.)
- **Storage is trivial on purpose.** The whole "graph" is an adjacency list with
  indexes; graph queries are just indexed SQL. The value is in the *edges* and the
  *compact-ref protocol*, not the database.

## How it works

```
discover            gitignore/.cbmignore + language detect (Go/TS/TSX/JS)
  → definitions     tree-sitter ASTs → File/Function/Method/Class/Interface/
                    Type/Enum nodes, signatures, decorators   (parallel, NumCPU)
  → imports         IMPORTS edges (relative module resolution)
  → calls           CALLS edges:
                      · TS/JS  → scip-typescript per tsconfig subproject
                      · Go     → go/packages + CHA call graph (in-process)
  → store           SQLite: nodes + edges + FTS5(BM25), QN→id resolved in memory
```

Query engine returns compact refs for: **search** (ranked BM25), **callers**,
**callees**, **neighbors**, and **snippet** (the one tool that returns source).
All exposed over **MCP (stdio JSON-RPC)** so any MCP-capable agent can use it.

## Quick start

```bash
go build -o codegraph ./cmd/codegraph        # needs cgo (tree-sitter) + Node (scip)

./codegraph index  /path/to/repo             # build the graph
./codegraph stats  /path/to/repo             # node/edge counts
./codegraph bench  /path/to/repo             # reproduce the token/speed numbers

# one-shot queries (also available over MCP)
./codegraph cli search   /path/to/repo '{"query":"getActiveCode","limit":5}'
./codegraph cli callers  /path/to/repo '{"qualified_name":"proj:…Service.getActiveCode"}'
./codegraph cli callees  /path/to/repo '{"qualified_name":"proj:…Controller.getActiveCode"}'

./codegraph mcp /path/to/repo                # serve over MCP (stdio) to an agent
```

Store lives in `~/.cache/codegraph/<project>.db`.

## What it does *not* do (read this)

Credibility is part of the pitch. codegraph is a **complement to grep, not a
replacement**:

- **Find an exact string/literal?** grep wins. The graph indexes symbols, not text.
- **Dynamic dispatch / DI string tokens / reflection?** The type checker can't
  resolve them, so the edge is honestly dropped — the graph won't see those calls.
- **Stale between re-indexes.** The graph is a snapshot; incremental re-index by
  file hash is the next milestone (M3).
- **Answer *quality* is not yet self-measured.** The upstream paper reports 83% vs a
  92% file-by-file baseline — a real token/quality trade-off. We do **not** publish a
  quality number we can't yet measure rigorously (it needs an LLM-as-judge harness).
  We publish only the deterministic numbers above.

Use it for **map / understand / who-calls / disambiguate / cut tokens.**

## Roadmap to a paper

This repo is an in-progress systems contribution. The intended paper —
*"Type-checker delegation for token-efficient code knowledge graphs"* — ships only
after the engineering clears a hard bar:

- [x] **M0–M2** — graph, tree-sitter definitions, type-checker-delegated CALLS,
      benchmark harness.
- [x] **≥15× token efficiency on the conservative (window) baseline** — the paper's
      gate. **Cleared: 16.0× total / 15.3× median** on ajuda-aqui, via a compact TSV
      wire format (keys once, project prefix stripped) that ~halved graph-side tokens.
- [ ] **M3** — incremental re-index by file hash (kills the 30 s on unchanged repos).
- [ ] **M4** — `SIMILAR_TO` via MinHash/LSH; **M5** — `get_architecture`, MCP polish.
- [ ] **Quality harness** — LLM-as-judge over N repos, to report our own answer-quality
      number honestly.

See [`docs/ROADMAP.md`](docs/ROADMAP.md) for the full plan and open questions.

## Docs

- [`docs/BENCHMARK.md`](docs/BENCHMARK.md) — methodology + full results (what we
  measure, what we don't, and why the ratios are honest).
- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) — the Go design.
- [`docs/UPSTREAM.md`](docs/UPSTREAM.md) — everything about the original C project
  (schema, pipeline, edge types, the "Hybrid LSP", its honest assessment, links).
- [`docs/ROADMAP.md`](docs/ROADMAP.md) — milestones.

## Layout

```
cmd/codegraph/    CLI (index | stats | bench | mcp | cli)
internal/graph/   model + SQLite store (the 2-table graph + FTS5)
internal/index/   discover, tree-sitter definitions, imports, calls pipeline
internal/scip/    SCIP reader + scip-typescript driver → TS/JS CALLS edges
internal/gocalls/ go/packages + CHA call graph → Go CALLS edges
internal/query/   query engine → compact refs
internal/bench/   token / tool-call efficiency harness
internal/mcp/     stdio JSON-RPC MCP server
_upstream/        shallow clone of the original, for reference (gitignored)
```

## Credits

A scoped-down Go reimagining of the ideas in
[DeusData/codebase-memory-mcp](https://github.com/DeusData/codebase-memory-mcp)
(MIT). We target one stack well (TypeScript/JS + Go + NestJS) and swap their
re-implemented type resolution for type-checker delegation.

## License

MIT.
