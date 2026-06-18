# Upstream reference — DeusData/codebase-memory-mcp

Everything we learned about the project we are reimplementing. This is our study
sheet. The original is cloned (shallow) under `_upstream/codebase-memory-mcp/`
for source-level reference (gitignored, not part of our repo).

- **Repo:** https://github.com/DeusData/codebase-memory-mcp
- **Docs/site:** https://deusdata.github.io/codebase-memory-mcp/
- **Paper (arXiv):** https://arxiv.org/abs/2603.27277 — *Codebase-Memory: Tree-Sitter-Based Knowledge Graphs for LLM Code Exploration via MCP*
- **Benchmark:** https://github.com/DeusData/codebase-memory-mcp/blob/main/docs/BENCHMARK.md
- **License:** MIT. **Language:** pure C, zero runtime deps, single static binary.
- **Local binary here:** `C:\Users\kocar\AppData\Local\Programs\codebase-memory-mcp\codebase-memory-mcp.exe` (v0.8.1), registered in Claude Code (local scope) for the ajudaqui project.

## What it is

An MCP server that indexes a codebase into a **persistent knowledge graph** of
functions, classes, call chains, HTTP routes and cross-service links, stored in
SQLite. AI agents query the graph instead of reading files one by one. No
embedded LLM — the agent itself is the natural-language → query translator.

## Honest assessment — does it help or hurt?

From their own arXiv paper (31 real-world repos), vs a file-by-file exploration agent:

| Metric | Graph (this tool) | File-by-file baseline |
|---|---|---|
| Answer quality | **83%** | **92%** |
| Tokens | **10× fewer** | 1× |
| Tool calls | **2.1× fewer** | 1× |
| Wins on graph-native ops (callers/hubs) | **19 / 31** langs | — |

**It is a token/quality trade-off, not a strict win.** It saves ~10× tokens but
loses ~9 points of answer quality on average; it wins on *structural* questions
(who-calls, hubs, architecture). The README's headline "120× fewer tokens" is a
best-case (5 pure structural queries: ~3,400 vs ~412,000 tokens) — the rigorous
cross-repo number is 10×.

Their own per-language benchmark (12 questions each): **TypeScript 87%, JS 86%,
Go 87%** (Tier 2); Tier-1 (Lua, Kotlin, C, C++) hits 100%. So for **our** dynamic
TS/JS stack it's a solid B+, not perfect.

**Where it actively helps:** "who calls X", call chains, architecture overview,
disambiguating same-named symbols, and cutting tokens.
**Where it hurts / gets in the way:** stale graph between re-indexes (it's a
snapshot); text/string search (grep wins); imprecise edges in tests + dynamic
code (DI string tokens in NestJS, pg-boss producer/consumer — edges it can't
see). On Windows it took ~20 min to index a 969-file repo (the on-device
embedding pass is the likely bottleneck), far from the "milliseconds" claim.

**Verdict:** use it for *map / understand / who-calls* and token savings; keep
grep as the default for "find this exact thing in the current code". Complement,
not replacement.

## Architecture (from the C source + the SQLite it produced)

### Storage — the whole "graph" is 2 tables

```sql
nodes(id, project, label, name, qualified_name, file_path,
      start_line, end_line, properties JSON, UNIQUE(project, qualified_name))
edges(id, project, source_id, target_id, type, properties JSON,
      UNIQUE(source_id, target_id, type))
node_vectors(node_id, project, vector BLOB)   -- nomic-embed-code, 768-dim
nodes_fts  -- FTS5(name, qualified_name, label, file_path) → BM25
-- indexes: edges(source, target, type, source_type, target_type); nodes(label, name, file)
```

All richness lives in the `properties` JSON (complexity, cognitive, loop_depth,
param_types, signature, is_test, is_exported, decorators, …). **The complexity is
in the extraction pipeline, not the storage.**

Real edge-type distribution on our ajudaqui repo: `DEFINES 6337, CALLS 4819,
USAGE 2532, IMPORTS 2208, DEFINES_METHOD 1053, CONTAINS_FILE 969, SIMILAR_TO 746,
DECORATES 503, THROWS 337, SEMANTICALLY_RELATED 312, HTTP_CALLS 140, INHERITS 23`.

### Indexing pipeline (RAM-first: LZ4 + in-memory SQLite, single dump at end)

`src/pipeline/pass_*.c`, parallel worker pool:

```
discover (gitignore/.cbmignore + language detect)
 → pass_definitions   (tree-sitter AST → nodes)
 → pass_calls         (call resolution — uses Hybrid LSP)  ← the hard/valuable part
 → pass_usages, pass_complexity, pass_enrichment
 → pass_route_nodes (HTTP), pass_k8s / pass_infrascan (IaC), pass_envscan
 → pass_similarity (MinHash + LSH → SIMILAR_TO)
 → pass_semantic_edges (embeddings → SEMANTICALLY_RELATED)
 → graph_buffer.dump → store (SQLite) + persist_hashes (incremental)
```

### The "secret sauce": Hybrid LSP

A lightweight C implementation of per-language type resolution for 9 language
families (Python, TS/JS/JSX/TSX, PHP, C#, Go, C/C++, Java, Kotlin, Rust),
structurally inspired by tsserver, pyright, gopls, Roslyn, JDT, rust-analyzer.
This is what makes `CALLS` edges accurate (param binding, return-type inference,
generic substitution, JSX dispatch). **Most of the effort and most of the
quality live here.**

### Search — four layers

- `search_graph` — regex/label over FTS5 (BM25), `cbm_camel_split` tokenizer.
- `query_graph` — **mini-Cypher** (`MATCH (f:Function)-[:CALLS]->(g) WHERE f.name='x' RETURN g`).
- `trace_path` (alias `trace_call_path`) — BFS, `direction inbound|outbound`.
- `semantic_query` — on-device vector search via bundled `nomic-embed-code`
  (768-dim, compiled into the binary, no API key); 11-signal combined scoring
  (TF-IDF, RRI, API/Type/Decorator signatures, AST profiles, data flow,
  Halstead-lite, MinHash, module proximity, graph diffusion).

### Extras worth stealing later

- **Team-shared artifact:** `.codebase-memory/graph.db.zst` — zstd snapshot
  (index stripped + `VACUUM INTO`), committable, two-tier (zstd-9 best / zstd-3
  fast), `.gitattributes merge=ours`, bootstrap-import + incremental on clone.
- **Cross-repo `CROSS_*` edges** across repos in one store.
- **IaC indexing**: Dockerfiles, K8s, Kustomize as nodes with `IMPORTS` edges.
- **Louvain community detection** for module discovery (`get_architecture`).
- **14 MCP tools**: index_repository, index_status, list_projects, delete_project,
  detect_changes, search_graph, query_graph, trace_path, get_code_snippet,
  get_graph_schema, get_architecture, search_code, manage_adr, ingest_traces.

## The 14 tools — CLI arg quirk (learned the hard way)

`codebase-memory-mcp cli <tool> '<json>'`. **Index tools take `{"repo_path":"…"}`;
query tools take the project NAME `{"project":"<slug>"}`** — passing repo_path to
a query tool returns `{"error":"project not found"}`.

## Source layout (C) — the map for our Go port

```
src/foundation/   arena/slab/vmem allocators, str_intern, hash_table, log, mem
src/discover/     gitignore.c, language.c, userconfig.c  (.cbmignore + .gitignore)
src/pipeline/     pass_*.c (definitions, calls, usages, complexity, semantic,
                  similarity, route_nodes, k8s, infrascan, cross_repo, gitdiff),
                  pass_parallel.c, worker_pool.c, pipeline_incremental.c
src/semantic/     ast_profile.c, semantic.c   (Hybrid LSP)
src/simhash/      minhash.c                    (SIMILAR_TO via LSH)
src/store/        store.c                      (SQLite persistence)
src/graph_buffer/ in-memory graph (gbuf.dump)
src/cypher/       cypher.c                     (query_graph engine)
src/mcp/          mcp.c                        (stdio JSON-RPC)
src/cli/          cli.c, hook_augment.c, progress_sink.c
src/traces/       ingest_traces
src/ui/           http_server.c + embedded React assets (3D graph @ :9749)
```
