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

## M1 — Real ASTs via tree-sitter

Replace the regex extractor with `github.com/smacker/go-tree-sitter` + Go/TS/TSX
grammars.

- Accurate node boundaries (true `end_line`, nested methods, decorators).
- Capture `properties`: signature, param names/types, return type, `is_exported`,
  `is_test`, decorator tags (so NestJS `@Injectable`/`@Controller` are visible).
- `IMPORTS` edges from import statements (cheap, accurate).
- **Exit criteria:** node counts within ~10% of upstream on the ajudaqui repo.

## M2 — CALLS edges via LSP delegation (the big one)

The hard, valuable part. Instead of re-implementing type resolution:

1. tree-sitter finds call expressions + their enclosing function (the caller QN).
2. For each call site, ask the language's LSP server for the definition:
   - Go → `gopls`
   - TS/JS → `typescript-language-server` / `tsserver` / typescript-go
   via `textDocument/definition` (start one LSP per language, reuse the session).
3. Map the returned definition location back to a node QN → emit `CALLS` edge.
4. Drop unresolved calls (honest precision).

- **Why this bet:** real type-checker accuracy, no per-language reimplementation.
- **Risks:** LSP startup latency (amortize by batching), TS project config
  discovery (tsconfig), mapping a definition *location* to our node (line-range
  containment). Cache resolutions by (file, callee-text, scope).
- **Exit criteria:** `callers`/`callees` correct on the ajudaqui validation-codes
  module (the 4 same-named `getActiveCode` disambiguated correctly).

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
