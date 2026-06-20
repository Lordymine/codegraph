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

## M2 — CALLS edges (the big one) ✅ (done)

The hard, valuable part — done via BATCH indexers (NOT interactive LSP), which give
the same type-checker precision in one pass with far less Go code and no long-lived
process to babysit:

- **Go → in-process** (`internal/gocalls`), no gopls subprocess:
  `golang.org/x/tools/go/packages` (LoadAllSyntax) + `go/callgraph` (CHA — sound on
  libraries without a `main`). These are the libs gopls itself calls.
- **TS/JS → `scip-typescript` (batch)** (`internal/scip`) → read `index.scip`
  in-process via `github.com/scip-code/scip/bindings/go/scip`. SCIP emits reference
  occurrences, not call edges → an enclosing-range attribution pass (`BuildEnclosing`
  + `CallEdges`) turns them into caller→callee CALLS. One scip run per tsconfig
  subproject (monorepo), or at the root for a single-package repo.
- `internal/index/calls.go` `ResolveCalls` wires both, best-effort per subproject;
  unresolved callees are dropped (filtered to known Function/Method QNs) and edges
  carry `resolver`/`confidence` tags so compiler-resolved edges supersede heuristics.

**Exit criteria — MET:** `callers`/`callees` correct on the ajuda-aqui
validation-codes module, the 4 same-named `getActiveCode` disambiguated (commits
13ab98f, 8eb84f9).

**Go precision — RESOLVED.** TS/JS scores ~89% (scip is precise). Go started at ~79%
(CHA over-approximates interface/func-value dispatch); fixed by **VTA** refining CHA
plus loading test files (`packages Tests:true`) in `internal/gocalls`. Now ~88% mean /
85% callers / 92% callees, scored **intra-repo** — stdlib/dep callees are excluded from
the truth because the graph drops them by design, exactly as the upstream does and as
its own benchmark grades them (PARTIAL, not FAIL). See `docs/QUALITY.md`.

**NestJS DI blind spot (any resolver):** `@Inject('TOKEN')`, `useClass`/`useFactory`
bindings live in a `providers` array and are invisible to scip and every other
resolver — they'd need a dedicated framework-semantic pass (deferred, M5).

## M3 — Incremental indexing ✅ (done)

Re-indexing is cheap to repeat — the expensive whole-project CALLS pass (scip /
go+VTA) no longer re-runs when it doesn't have to.

- **Per-file content hash** (sha256) on the File node; `DetectChanges` compares the
  files on disk against it → Changed/Added/Deleted.
- **No-op when unchanged** — `Run` skips the whole pipeline if nothing changed
  (cobra: 1.77s full → 0.06s reused, ~29×); the win scales with the CALLS cost.
- **Scope-gated CALLS** — a scope is one tsconfig-project (scip) or the Go module
  (go+VTA). A re-index re-resolves only the scopes whose files changed and reuses the
  stored edges of the rest (read before the wipe via `Store.CallEdges`, kept by
  `scopeOf`/`changedScopes`). Editing one app of a monorepo no longer re-runs scip for
  every other app. The full-index path is unchanged (a never-indexed project marks
  every scope changed → nothing reused).
- **`detect_changes` tool + `codegraph changes <repo>`** — report the change set
  (compact TSV) so an agent can tell whether the graph is stale before trusting it.

Honest limit: true *per-file* CALLS incrementality is impossible (the resolvers are
whole-scope), so the granularity is the scope, not the file — the realistic win.

## M4 — Similarity + light enrichment ✅ (done)

- **`SIMILAR_TO`** via MinHash + LSH over token shingles (`internal/similar`) — near-clone
  detection, no embeddings/model. Wired into the pipeline (threshold 0.7) and surfaced
  by the **`similar`** query/tool. cobra found +231 real near-clones (the cross-shell
  `Gen*CompletionFile` cluster).
- **Cyclomatic complexity** (McCabe) in `properties.complexity` on every Function/Method,
  from the tree-sitter subtree (`internal/index/complexity.go`). Cognitive/loop-depth
  deferred (YAGNI); the hotspots query that reads this is M5. 
- **Dead-code hint** — `dead_code` query: Function/Method with zero inbound `CALLS`,
  minus entry points (exported, decorated, main/init, tests). A candidate list (recall-
  bounded), not a delete list; on cobra it pinpoints `appendIfNotPresent`, which cobra's
  own source marks unused-and-removable.

**Bonus — the dead-code hint exposed a call-graph recall hole, and fixing it lifted Go
quality.** Calls written inside closures (cobra's `Run: func(){...}`) and recursive
self-calls were dropped, so they showed as false dead code. Crediting closure calls to
the enclosing named function (`ssa.Function.Parent()`) + keeping recursive self-edges
(while `dead_code` ignores them) took cobra callers **85→100%** (mean 88→94%), zero
false positives. Also raised the relationship-query default limit 50→500 so hub answers
aren't silently truncated. See `docs/QUALITY.md`.

## M5 — MCP polish + distribution ✅ (done)

The graph becomes a tool you actually use, in any repo, from any agent.

- **Auto-index on serve** — `codegraph mcp` indexes in a background goroutine and
  gates tool calls behind a readiness check (`Server.SetReadiness`), so a freshly-
  registered server "just works" on any repo (no manual `index` step); M3's no-op keeps
  it cheap every launch. Resolves the repo from `$CLAUDE_PROJECT_DIR` or cwd.
- **`codegraph install`** — registers the MCP server into detected agents: Claude Code
  & Codex via their add-CLI (user scope, any repo), opencode via a config-file merge
  that preserves existing config; a manual snippet for the rest (`internal/install`).
- **`get_architecture`** — one-shot repo map from graph aggregates: languages, node/
  edge counts, top packages by symbol, and hotspots (most complex functions — reads
  the M4 `properties.complexity` — + most-called hubs). Community detection deferred
  (YAGNI — dir grouping gives the structure cheaply).
- **HTTP Route nodes** — NestJS `@Controller` + `@Get/@Post/...` → `Route` nodes named
  `<VERB> <path>`, located at the handler (`internal/index/routes.go`). Surfaced via
  `search --label Route` and counted by get_architecture.

Dogfooded: registered into Claude Code/Codex/opencode and used live — which caught two
real bugs (MCP `required:null`, opencode config path). See `docs/QUALITY.md`.

## M6 — deferred from M5 (do when proven)

- ⬜ **`HTTP_CALLS`** (client call-site → `Route`). Deferred deliberately: unlike the
  rest of the graph it is **not** type-checker-delegated — it's heuristic string
  matching (extract a URL from `fetch`/`axios`/`HttpClient` calls, match against route
  patterns). Dynamic URLs (`/users/${id}`) make it low-recall, and a wrong match
  violates honest precision ("a missing edge beats a wrong one"). Revisit only if a
  high-precision, literal-anchored version proves worth it.
- ⬜ Optional: committable `graph.db.zst` team artifact (zstd snapshot + bootstrap).

## Stretch / maybe-never (YAGNI unless proven)

- On-device embeddings + `semantic_query` (heavy; was the upstream bottleneck).
- Full Cypher engine (fixed query shapes cover ~90%).
- 158 languages, IaC/K8s indexing, cross-repo `CROSS_*` edges, 3D graph UI.

## Open design questions

- LSP session management: one long-lived server per language vs per-index spawn?
- TS monorepo: one tsserver per tsconfig, or project-references aware?
- Edge confidence scoring (upstream scores HTTP matches) — worth it for CALLS?
- Store per-project files vs one multi-project DB (upstream supports both).
