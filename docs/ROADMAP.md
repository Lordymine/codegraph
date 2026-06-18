# codegraph — roadmap

Milestones, smallest-useful-first. Each one ships something runnable.

## M0 — Scaffold ✅ (done)

- Two-table SQLite store + FTS5, mirroring upstream schema.
- File discovery (hard-ignores + `.cbmignore`), language detection (Go/TS/JS).
- Regex definitions pass → `File`/`Function`/`Method`/`Class` nodes + `DEFINES` edges.
- Parallel per-file extraction (NumCPU goroutines).
- Query engine (search / callers / callees / neighbors / snippet) → compact Refs.
- Minimal MCP stdio server + CLI (`index`, `stats`, `mcp`, `cli`).
- **Proof:** `codegraph index .` on itself → 9 files, 75 nodes, 66 edges; `search`
  returns correct file+line refs.

## M1 — Real ASTs via tree-sitter ✅ (done)

Replaced the regex extractor with the **official** `github.com/tree-sitter/go-tree-sitter`
(cgo) + Go/TS/TSX grammars (not smacker — it had drifted since ~2023).

- Accurate node boundaries (true `end_line`), receiver/owner-qualified names so
  homonyms disambiguate, `is_exported`, `is_test`.
- Full TS surface: Function/Method/Class + Interface/Type/Enum/Variable, abstract
  classes, function expressions. Decorators on classes AND methods (NestJS
  `@Injectable`/`@Controller`/`@Get`/`@Post`) — captured generically as strings,
  so any decorator framework (Angular, TypeORM) comes for free.
- `IMPORTS` edges (TS/JS): relative specifiers resolved File→File; package/unresolved
  imports dropped (honest precision). Go imports deferred (package-level model).
- Build needs cgo: gcc on PATH + `CGO_ENABLED=1` (WinLibs mingw-w64 on this machine).
- **Result vs upstream on ajuda-aqui (857 files, 3.1s):** Interface 393=393, Type
  141=141, decorators 363=363, Method 1052≈1053, Class 343≈344. Divergences
  (Function, Variable, File) are deliberate scope (top-level only, ignored dirs).
- **Scope left out (not bugs):** nested functions/callbacks, property decorators
  (`@Column`), Go interface-vs-struct (both → Class).

Design note: **codegraph indexes the LANGUAGE (TS/JS/Go), not frameworks.** All of
NestJS/Next/Electron/RN/Expo/Fastify/Tauri are TS/JS — symbols/imports/calls work
for all of them with zero per-framework code. Framework semantics (HTTP routes,
IPC, DI) are an optional, pluggable pass added only on real need.

## M2 — CALLS edges (the big one) — IN PROGRESS

The hard, valuable part. Delegate to the real compiler, but via BATCH indexers —
NOT interactive LSP (callHierarchy/references is O(symbols) round-trips for a
whole-repo graph, plus cold start). The batch tools have the SAME type-checker
precision in one pass:

- **Go → in-process**, no gopls subprocess: `golang.org/x/tools/go/packages`
  (LoadAllSyntax) + `go/callgraph` (CHA — sound on libraries without a `main`).
  These are the libs gopls itself calls.
- **TS/JS → `scip-typescript` (batch)** → read `index.scip` in-process via
  `github.com/sourcegraph/scip/bindings/go/scip`. SCIP emits reference occurrences,
  not call edges → a ~100-line enclosing-range attribution pass turns them into
  caller→callee CALLS (port `Beneficial-AI-Foundation/scip-callgraph`).
- Keep M1 tree-sitter as the cheap structural layer; tag edge confidence so
  compiler-resolved edges supersede heuristic ones.
- **Why batch not LSP:** same precision, one pass, far less Go code, no long-lived
  process to babysit. Cost: build-time Node + tsconfig dep (binary stays single).
- **Risks:** monorepo tsconfig (scip has `--pnpm-workspaces`/`--infer-tsconfig`);
  NestJS DI (`@Inject('TOKEN')`, `useClass`/`useFactory`) is invisible to ALL
  resolvers (binding lives in a `providers` array) — a separate framework pass.
- **Spike first:** run scip-typescript on the ajudaqui validation-codes module and
  confirm the 4 same-named `getActiveCode` disambiguate before committing the design.
- **Exit criteria:** `callers`/`callees` correct on validation-codes (4 `getActiveCode`).

## M3 — Incremental indexing

- Persist a per-file content hash (sha256 + mtime).
- `detect_changes`: re-index only changed files; diff nodes/edges; map to affected
  symbols. Avoids the full re-index cost on every edit.

## M4 — Similarity + light enrichment

- `SIMILAR_TO` via MinHash + LSH over token shingles (near-clone detection) — easy
  and high-signal; no embeddings/model needed.
- Complexity metrics in `properties` (cyclomatic/cognitive, loop depth).
- Dead-code hint: functions with zero inbound `CALLS` (excluding entry points).

## M5 — MCP polish + distribution

- `get_architecture` (languages, packages, entry points, routes, hotspots) from
  graph aggregates + a community-detection pass.
- HTTP route nodes + `HTTP_CALLS` matching (NestJS controllers ↔ client calls).
- `install`-style registration helper for Claude Code (`claude mcp add`).
- Optional: committable `graph.db.zst` team artifact (zstd snapshot + bootstrap).

## Stretch / maybe-never (YAGNI unless proven)

- On-device embeddings + `semantic_query` (heavy; was the upstream bottleneck).
- Full Cypher engine (fixed query shapes cover ~90%).
- 158 languages, IaC/K8s indexing, cross-repo `CROSS_*` edges, 3D graph UI.

## Open design questions

- LSP session management: one long-lived server per language vs per-index spawn?
- TS monorepo: one tsserver per tsconfig, or project-references aware?
- Edge confidence scoring (upstream scores HTTP matches) — worth it for CALLS?
- Store per-project files vs one multi-project DB (upstream supports both).
