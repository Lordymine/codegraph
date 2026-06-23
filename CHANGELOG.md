# Changelog

All notable changes to codegraph are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project aims to
follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html) (pre-1.0: minor
versions may include breaking changes).

## [Unreleased]

### Planned

- `HTTP_CALLS` (client call-site → route) and a committable `graph.db.zst` team
  artifact are planned for a future release (M6); see `docs/ROADMAP.md`.

## [0.2.0] — 2026-06-23

### Added

- **Memory-budget indexing** (`internal/memory`) — auto-tunes worker count, definition
  batch size, Go heap limit, and scip-typescript Node heap cap from installed RAM (WSL
  and low-RAM profiles). `memory.Gate()` between pipeline phases returns freed pages
  to the OS. Optional `CODEGRAPH_*` env overrides for debugging only.
- **Atomic index builds** — `RunAtomic` writes to `dbPath+.building` and renames on
  success; failed re-indexes leave the previous graph intact. CLI `index` and MCP
  background index use this path.
- **WAL snapshot CALLS reuse** — `Run` opens a second store on the same DB path,
  pins a read snapshot (`BeginReadSnapshot`), and streams unchanged scopes' CALLS via
  `insertReusedCallEdges` after the writer wipes the project.
- **`Store`/`Engine` lifecycle** — `DBPath`, `Reopen`, and `Engine.Close`/`Reopen` for
  MCP and quality tooling after atomic commits.
- **Site release freshness** (from prior unreleased work) — download buttons on the
  GitHub Pages site fetch the latest GitHub Release at runtime; Pages redeploys on
  release publish.

### Fixed

- **MCP index failure recovery** — on background index failure the server reopens the
  previous graph, sets tools ready, and prepends the failure status to every tool
  response so agents see stale-data context with results.
- **MCP Windows file lock** — closes the live DB handle before `RunAtomic` so the
  store file can be replaced on Windows.
- **Scip scope count** — `ScopesRun` increments only after a successful scip scope
  (not on resolver errors).
- **Unified CALLS reuse path** — removed duplicate stash/slice loaders; `Run` and
  `RunAtomic` share `forEachReusableCallEdge` + `insertReusedCallEdges`.

### Changed

- Indexing pipeline uses batched definition extraction and streaming import collection
  to bound peak memory on large repos.
- SIMILAR_TO resolution can be skipped automatically on constrained hosts (`SkipSimilar`).

## [0.1.1] — 2026-06-22

### Fixed

- **MCP server memory** — the long-running `mcp` server now returns freed indexing
  memory to the OS (`debug.FreeOSMemory()`) once the background index finishes. The
  Go call-graph resolver (go/packages + SSA + VTA) spikes the heap to several GB on
  large repos and the runtime kept that arena reserved, so the stdio server sat at
  the indexing peak for its whole life (≈130MB climbing past 10GB and staying there).
  Steady state now drops back to the query baseline (measured: goclaw 3091MB →
  149MB), with no effect on graph precision.

## [0.1.0] — 2026-06-20

First public release. codegraph indexes a Go or TypeScript/JavaScript repository
into a token-efficient knowledge graph and serves it to AI coding agents over MCP.
Validated on real repositories of both stacks.

### Added

- **Graph store** — two-table SQLite (`nodes`, `edges`) + FTS5, pure-Go driver
  (no cgo for storage). Compact TSV wire format for every query (≈16× fewer tokens
  than a grep-driven agent on the conservative baseline).
- **Definitions (M1)** — tree-sitter ASTs → `File`/`Function`/`Method`/`Class`/
  `Interface`/`Type`/`Enum`/`Variable` nodes with real end lines, export flags, and
  decorators. `IMPORTS` edges for TS/JS.
- **Type-checker-accurate calls (M2)** — `CALLS` edges via **scip-typescript**
  (TS/JS) and **go/packages + a VTA call graph** (Go). Honest precision: an edge
  whose endpoints aren't both real nodes is dropped, never guessed.
- **Incremental indexing (M3)** — per-file sha256; a no-op when nothing changed;
  CALLS re-resolution gated to the scopes whose files changed. `detect_changes`.
- **Similarity + enrichment (M4)** — `SIMILAR_TO` near-clones (MinHash + LSH), the
  `similar` and `dead_code` queries, and cyclomatic complexity in node properties.
- **MCP polish + distribution (M5)** — the `mcp` server auto-indexes in the
  background with a readiness gate (works on any repo, no manual step);
  `codegraph install` registers the server into Claude Code, Codex, and opencode;
  `get_architecture` returns a one-shot repo map (languages, packages, hotspots);
  NestJS HTTP `Route` nodes from decorators.
- **Discovery** honors hard-ignores, `.gitignore`, and `.cbmignore`.
- **Tooling** — `index`, `stats`, `changes`, `install`, `mcp`, `bench`, `quality`,
  and `cli` subcommands; an MCP stdio server exposing all query tools.

### Quality

- Answer-quality harness (`docs/QUALITY.md`): graph mode reaches ~89–94% of an
  independent oracle at ~4.5–8× fewer tokens than a grep-driven agent. Go callers
  ~100% intra-repo (cobra, gh-cli); TS ~89%.

[Unreleased]: https://github.com/Lordymine/codegraph/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/Lordymine/codegraph/compare/v0.1.1...v0.2.0
[0.1.1]: https://github.com/Lordymine/codegraph/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/Lordymine/codegraph/releases/tag/v0.1.0
