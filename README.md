<div align="center">

# codegraph

**Give your AI coding agent the call graph — so it moves with direction instead of grepping blind.**

A token-efficient code knowledge graph for AI agents. Index a repo once into a graph
of symbols and *type-checker-accurate* call edges, then answer the structural
questions an agent actually asks — *who calls this? what does it call? where is it?
what's the shape of this codebase?* — in **one query, compact refs, a fraction of the tokens.**

[![CI](https://github.com/Lordymine/codegraph/actions/workflows/ci.yml/badge.svg)](https://github.com/Lordymine/codegraph/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/Lordymine/codegraph?sort=semver)](https://github.com/Lordymine/codegraph/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go)](go.mod)

`Go` · `SQLite (pure-Go)` · `MCP-native` · **Go + TypeScript/JavaScript** · status: **M0–M5 done — dogfooded on real repos**

</div>

---

## The problem it solves

An LLM agent dropped into an unfamiliar codebase explores the way a human can't
afford to: it `grep`s a symbol, gets dozens of hits, then **opens file after file**
to work out which hits are real calls, which are imports, which is the definition,
which is a same-named decoy. Every opened file is tokens spent — and the agent still
ends up *guessing direction*, chasing the wrong call path because flat text search has
no idea what calls what.

That is the failure codegraph removes. The graph knows the structure, so the agent
asks one question and gets the precise answer — at a fraction of the token cost of
reading the repo by hand.

## The numbers

Measured with `codegraph bench <repo>` (fully reproducible) and the answer-quality
harness in [`docs/QUALITY.md`](docs/QUALITY.md):

| | codegraph | grep-driven agent |
|---|--:|--:|
| **Tokens** to answer "who calls X" | **1×** | 16×–74× |
| **Tool calls** to answer it | **1** | up to ~34× more |
| **Answer quality** vs an independent oracle | **89–94%** | matched, at ~4.5–8× the cost |
| **Index time** (e.g. a 1.9k-file TS repo) | **~1 min** | — |

> **16× fewer tokens** against a *conservative* baseline (the agent reads only a
> ±10-line window around each grep hit); 74× against the common "read whole files"
> one. The conservative number is the one to trust — see [methodology](docs/BENCHMARK.md).

## The idea

Index the repo into a tiny graph — **two tables** (`nodes`, `edges`) + an FTS5 index —
and make every query return a **compact reference**, never source. One tab-separated
line per result, no JSON overhead, project prefix stripped:

```
label   name           file:line                          qualified_name
Method  getActiveCode  src/validation-codes.service.ts:64  …service.ts.ValidationCodesService.getActiveCode
```

The agent reads actual code only when it deliberately asks for a `snippet`, and a
returned `qualified_name` feeds straight back into `callers`/`callees`. That
selectivity *is* the token saving.

## The bet (the scientific claim)

The upstream project ([DeusData/codebase-memory-mcp](https://github.com/DeusData/codebase-memory-mcp),
pure C) earned its accurate call edges the hard way: it **re-implemented per-language
type resolution** ("Hybrid LSP", ~9 language families, months of work).

codegraph makes the opposite bet:

> **Delegate call resolution to the type checkers that already exist.**
> TypeScript/JS via **[scip-typescript](https://github.com/sourcegraph/scip-typescript)**
> (a batch indexer, not a live LSP); Go via **`go/packages` + a VTA call graph** (the
> machinery gopls itself uses). Type-checker-grade precision — parameter binding,
> overload/return-type inference, interface-dispatch narrowing, same-name
> disambiguation — for free, and we skip the single hardest part of the port.

This is the project's central, **falsifiable** claim: *you don't need to re-implement
a type checker to build an accurate code knowledge graph for LLMs.* Delegation buys the
same call-edge quality at a fraction of the engineering cost and a ~36× faster index
(no embedding pass).

Two rules keep it honest:

- **Honest precision.** An edge whose endpoints aren't both real nodes is **dropped**,
  not guessed. A missing edge beats a wrong one — an agent that trusts the graph must
  never be sent the wrong way.
- **Storage is trivial on purpose.** The whole "graph" is an adjacency list with
  indexes; queries are just indexed SQL. The value is in the *edges* and the
  *compact-ref protocol*.

## Languages supported (today)

Scoped deliberately to one stack done well, not 158 done shallowly.

| Language | Definitions | `CALLS` resolution | Extras |
|---|---|---|---|
| **Go** | tree-sitter | `go/packages` + **VTA** call graph (in-process) | closure & recursive-call recall; ~100% intra-repo callers |
| **TypeScript / TSX** | tree-sitter | **scip-typescript** per `tsconfig` (monorepo-aware) | `IMPORTS`; NestJS `@Controller/@Get` → HTTP `Route` nodes |
| **JavaScript / JSX** | tree-sitter | scip-typescript | parsed via the TSX grammar |

Generated/built code committed to a repo (vendored module caches, minified bundles,
Prisma clients) is kept out via `.gitignore` (honored automatically) and `.cbmignore`.

## What the agent gets (MCP tools)

`get_architecture` (one-shot repo map), `search` (ranked BM25), `callers`, `callees`,
`neighbors`, `similar` (near-clones), `dead_code` (uncalled private functions),
`detect_changes` (staleness), and `snippet` (the one tool that returns source). All
over **MCP (stdio JSON-RPC)** so any MCP-capable agent can use it.

## Use it with your agent

One command registers codegraph as an MCP server in every supported agent on your
`PATH`, and prints a paste-ready snippet for the rest:

```bash
codegraph install
```

- **Claude Code** and **Codex** — via their own CLI (`claude mcp add --scope user` /
  `codex mcp add`), so it works in **every** repo you open.
- **opencode** — merged into your `opencode.jsonc`/`.json` (existing config preserved).
- **Any other MCP agent** — the command prints the stdio server line to add by hand.

No per-repo step: the server auto-indexes whatever repo the agent opens (reading
`$CLAUDE_PROJECT_DIR`, else its working directory). The first index runs in the
background while the agent stays responsive; re-launches are an incremental no-op. Index
builds are atomic — a failed re-index leaves the previous graph queryable, and the
server surfaces a failure notice alongside tool results when that happens. Do not run
`codegraph index` on the same repo while MCP is active (both contend for the store file).

### Prerequisites

codegraph delegates to real type checkers, so each language needs its toolchain.
Without it, definitions/search still work and call edges degrade gracefully (dropped,
never wrong):

- **Build:** cgo + a C compiler (tree-sitter). Prefer a prebuilt
  [release binary](https://github.com/Lordymine/codegraph/releases); else `CGO_ENABLED=1 go build`.
- **TypeScript/JS:** Node.js on `PATH` (scip-typescript runs via `npx`), the repo's
  `node_modules` installed, and a `tsconfig.json`.
- **Go:** the Go toolchain on `PATH` and a buildable module (`go mod download`).

## Quick start (CLI)

```bash
go build -o codegraph ./cmd/codegraph          # needs cgo (tree-sitter)

./codegraph index  /path/to/repo               # build the graph
./codegraph get_architecture /path/to/repo     # (via cli) orient: languages, packages, hotspots
./codegraph cli callers /path/to/repo '{"qualified_name":"…Service.getActiveCode"}'
./codegraph mcp /path/to/repo                  # serve over MCP (stdio), auto-indexing
```

Store lives in `~/.cache/codegraph/<project>.db`.

## Validated on real repositories

Dogfooded — registered into Claude Code/Codex/opencode and run live on real repos of
both stacks. The resolvers scale and the maps are immediately useful:

| repo | stack | files | `CALLS` | dropped | index |
|---|---|--:|--:|--:|--:|
| goclaw (AI gateway) | Go | 1,107 | 12,758 | 3 | 48 s |
| openclaude (TUI agent) | TS | 1,917 | 22,160 | 33 | 59 s |

On both, `get_architecture` names the system at a glance (multi-tenant gateway vs TUI
coding agent) from real hotspots and call hubs. Live use also caught and fixed real
bugs (e.g. discovery now honors `.gitignore`).

## What it does *not* do (read this)

Credibility is part of the pitch. codegraph **complements grep, it doesn't replace it**:

- **Find an exact string/literal?** grep wins — the graph indexes symbols, not text.
- **Dynamic dispatch / DI string tokens / reflection?** The type checker can't resolve
  them, so the edge is honestly dropped.
- **Stale between re-indexes** — but re-index is incremental (a no-op when unchanged),
  and the MCP server auto-re-indexes on launch. If a re-index fails, the previous graph
  stays available with a visible failure status.

Use it for **map / understand / who-calls / disambiguate / cut tokens.**

## Status & roadmap

- [x] **M0–M2** — graph, tree-sitter definitions, type-checker-delegated `CALLS`, benchmark harness.
- [x] **M3** — incremental re-index (no-op when unchanged; scope-gated `CALLS`).
- [x] **M4** — `SIMILAR_TO` (MinHash/LSH), `similar`/`dead_code`, cyclomatic complexity.
- [x] **M5** — auto-index-on-serve, `codegraph install`, `get_architecture`, NestJS `Route` nodes.
- [ ] **M6** — `HTTP_CALLS` (client ↔ route), committable `graph.db.zst` artifact.

This is an in-progress systems contribution; the intended paper is *"Type-checker
delegation for token-efficient code knowledge graphs."* See [`docs/ROADMAP.md`](docs/ROADMAP.md).

## Docs

- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) — the Go design.
- [`docs/BENCHMARK.md`](docs/BENCHMARK.md) — token methodology + full results.
- [`docs/QUALITY.md`](docs/QUALITY.md) — the answer-quality harness (graph vs grep, honestly graded).
- [`docs/UPSTREAM.md`](docs/UPSTREAM.md) — everything about the original C project.
- [`docs/ROADMAP.md`](docs/ROADMAP.md) — milestones and open questions.
- [`CONTRIBUTING.md`](CONTRIBUTING.md) · [`CHANGELOG.md`](CHANGELOG.md)

## Credits

A scoped-down Go reimagining of the ideas in
[DeusData/codebase-memory-mcp](https://github.com/DeusData/codebase-memory-mcp) (MIT).
We target one stack well (TypeScript/JS + Go + NestJS) and swap their re-implemented
type resolution for type-checker delegation.

## License

[MIT](LICENSE).
