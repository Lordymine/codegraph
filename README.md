<div align="center">

# codegraph

**A token-efficient code knowledge graph for AI coding agents.**

Stop letting your agent read the repo file-by-file. Index it once into a graph of
symbols and *type-checker-accurate* call edges, then answer structural questions —
*who calls this? what does it call? where is it?* — in **one query, compact refs,
a fraction of the tokens.**

`Go 1.26` · `SQLite (pure-Go, no cgo for storage)` · `MCP-native` · `MIT`
· status: **M0–M5 done — type-checker-accurate calls, incremental, near-clones, repo map, one-command agent setup; dogfooded on real Go + TS repos**

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
> (a batch indexer, *not* an interactive LSP); Go via **`go/packages` + a VTA call
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
                      · Go     → go/packages + VTA call graph (in-process)
  → store           SQLite: nodes + edges + FTS5(BM25), QN→id resolved in memory
```

Query engine returns compact refs for: **get_architecture** (one-shot repo map —
languages, packages, hotspots), **search** (ranked BM25), **callers**, **callees**,
**neighbors**, **similar** (near-clones), **dead_code** (uncalled private functions),
**detect_changes** (staleness), and **snippet** (the one tool that returns source). All
exposed over **MCP (stdio JSON-RPC)** so any MCP-capable agent can use it.

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

## Use it with your agent

One command registers codegraph as an MCP server in every supported agent on your
`PATH`, and prints a paste-ready snippet for the rest:

```bash
codegraph install
```

- **Claude Code** and **Codex** — registered via their own CLI (`claude mcp add
  --scope user` / `codex mcp add`), so it works in **every** repo you open.
- **opencode** — merged into your `opencode.jsonc`/`.json` (your existing config is
  preserved, not clobbered).
- **Any other MCP agent** — the command prints the stdio server line to add by hand.

There's **no per-repo step**: the server auto-indexes whatever repo the agent opens
(it reads `$CLAUDE_PROJECT_DIR`, else its working directory). The first index of a
repo runs in the background while the agent stays responsive — tools answer
"indexing, retry shortly" until it's ready; re-launches are an incremental no-op, so
the agent always queries a fresh graph.

### Prerequisites

codegraph delegates call resolution to real type checkers, so each language needs its
toolchain present. Without it, definitions/search still work and call edges degrade
gracefully (dropped, never wrong):

- **Build:** the binary needs cgo + a C compiler (tree-sitter). Prefer a prebuilt
  release; otherwise `CGO_ENABLED=1 go build`.
- **TypeScript/JS:** **Node.js** on PATH (scip-typescript runs via `npx`), plus the
  repo's `node_modules` installed and a `tsconfig.json` for full call resolution.
- **Go:** the **Go toolchain** on PATH and a buildable module (`go mod download`).

## What it does *not* do (read this)

Credibility is part of the pitch. codegraph is a **complement to grep, not a
replacement**:

- **Find an exact string/literal?** grep wins. The graph indexes symbols, not text.
- **Dynamic dispatch / DI string tokens / reflection?** The type checker can't
  resolve them, so the edge is honestly dropped — the graph won't see those calls.
- **Stale between re-indexes.** The graph is a snapshot — but re-index is incremental
  (M3: a no-op when nothing changed), and the MCP server auto-re-indexes on launch, so
  an agent always opens a fresh graph.
- **Answer quality is now self-measured — honestly.** A separate harness
  (`docs/QUALITY.md`) pits a graph-only agent against a grep-only one, graded against
  an independent oracle. On ajuda-aqui: **89% vs 87% quality at ~8× fewer tokens and
  ~7.5× fewer tool calls** — the graph matches a careful grep agent's correctness far
  more cheaply, wins on call resolution, and *loses* on open comprehension (refs
  carry structure, not intent). One repo so far; the paper needs N.

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
- [x] **M3** — incremental re-index by file hash (no-op on unchanged repos; CALLS
      gated by changed scope).
- [x] **M4** — `SIMILAR_TO` via MinHash/LSH + `similar`/`dead_code`/cyclomatic
      complexity; the recall fixes that came with it took Go callers to ~100%.
- [x] **M5** — MCP polish + distribution: auto-index-on-serve, `codegraph install`
      (one-command agent setup), `get_architecture` (repo map + hotspots), and NestJS
      `Route` nodes. `HTTP_CALLS` deferred to M6 (heuristic, not type-checker-delegated).
- [x] **Quality harness** — graph 89–94% vs baseline at ~4.5–8× less cost on
      ajuda-aqui/cobra/gh-cli (`docs/QUALITY.md`). Remaining for the paper: scale to N repos.

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
cmd/codegraph/    CLI (index | stats | changes | install | mcp | bench | quality | cli)
internal/graph/   model + SQLite store (the 2-table graph + FTS5)
internal/index/   discover, tree-sitter definitions, imports, calls pipeline
internal/scip/    SCIP reader + scip-typescript driver → TS/JS CALLS edges
internal/gocalls/ go/packages + VTA call graph → Go CALLS edges
internal/similar/ MinHash + LSH → SIMILAR_TO near-clone edges
internal/install/ register the MCP server into detected agents
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
