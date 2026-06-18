# Benchmark — codegraph

Reproduces the **measurable** part of the upstream headline (token + tool-call
efficiency) and adds our own indexing-speed number. Run it yourself:

```bash
codegraph bench <repo>      # re-indexes, then benchmarks the top call hubs
```

## What this measures — and what it deliberately does not

| Metric | Measured here? | Why |
|---|---|---|
| **Tokens** to answer a structural question | ✅ deterministic | the project's whole bet |
| **Tool calls** to answer it | ✅ deterministic | fewer round-trips = cheaper agent |
| **Indexing speed** | ✅ deterministic | our strongest win vs upstream |
| **Answer quality** (upstream: 83% vs 92%) | ❌ not measured | needs an LLM-as-judge over many repos; reproducing it badly is a *romantic* number, not an engineered one. Left for a separate harness. |

We only report numbers a machine can reproduce exactly. The token figure is a
**ratio** (baseline ÷ graph), so the rough `bytes/4` token estimate cancels out —
the same estimator meters both sides.

## Method

For each question "**who calls `X`?**" we compare three strategies:

| Strategy | Tokens it spends | Tool calls |
|---|---|---|
| **graph** | `callers(X)` → compact refs (name + qn + file + line, no source) | 1 |
| **baseline-window** (efficient agent) | `grep X` output + a **±10-line window** around every match | 1 grep + 1 read/file |
| **baseline-file** (typical agent) | `grep X` output + every **whole matched file** | 1 grep + 1 read/file |

- **Questions** = the top 15 *call hubs* (symbols with the most inbound `CALLS`
  edges). Deterministic, and the hardest case for grep: a real caller set it has
  to reconstruct by hand.
- **±10 lines is generous to the baseline** — more context than strictly needed —
  so the graph's win is a floor, not an inflated ceiling.
- **Why the graph wins, honestly:** it has already resolved the enclosing caller
  of every call site and dropped definitions/imports/comments/homonyms. That is
  exactly the work `grep` leaves for the agent to redo by opening files.
- **Tool-call premise:** the baseline opens one file per file-with-a-match to give
  a *complete, precise* answer (a popular symbol matches in dozens of files). A
  lazy agent that guesses from grep alone would call less — and answer worse.

## Results

### ajuda-aqui — 857 files, real NestJS + React/Next monorepo (TS/JS)

```
indexing: 857 files → 4605 nodes, 8093 edges (6 dropped) in 32.9s  (~26 files/s)

tokens (median per query):  8.7×  vs grep+window   ·  44.9× vs grep+file
tokens (total across set):  8.8×  vs grep+window   ·  40.8× vs grep+file   ← headline
tool calls (total):         graph 15  vs  baseline 511   →  34× fewer
raw tokens:                 graph 31,884 · grep+window 279,920 · grep+file 1,299,580
best case (Button):         26.6× window · 114× file
```

### codegraph self — 25 files (Go)

```
indexing: 25 files → 189 nodes, 291 edges in ~3.9s
tokens (total): 8.0× vs grep+window · 35.3× vs grep+file
tool calls:     graph 15 vs baseline 60 → 4.0× fewer
```

## vs upstream (codebase-memory-mcp)

| | upstream (31-repo paper) | codegraph (ajuda-aqui) |
|---|---|---|
| Tokens | **10×** fewer | **8.8×** (window) / **40.8×** (file) |
| Tool calls | **2.1×** fewer | **34×** fewer¹ |
| Answer quality | 83% vs 92% | *not measured* |
| Index time | ~20 min / 969 files (Windows) | **33s / 857 files** (~36× faster)² |

¹ Our tool-call baseline counts one read per matched file (an upper bound for a
*complete* answer); the upstream's baseline agent likely read more selectively.
The honest, conservative token number is the **window** ratio (8.8×).

² The big gap is because we skip the upstream's on-device embedding pass
(`nomic-embed-code`, 768-dim) — the likely bottleneck. We trade `SEMANTICALLY_RELATED`
edges (a later milestone, M4) for a ~36× faster index. Our `CALLS` accuracy comes
from delegating to type checkers (scip-typescript / go callgraph), not embeddings.

## Honest reading

- The **window ratio (~8–9×)** is the number to quote — it assumes a competent
  agent that reads only the context it needs. We still beat grep there because the
  graph pre-resolves callers and filters noise.
- The **file ratio (~40×)** and **best-case (114×)** describe the common reality of
  agents that slurp whole files; useful, but the optimistic end.
- **Where grep still wins:** finding an exact string/literal, or anything in code
  the type checker can't resolve (dynamic dispatch, DI string tokens). The graph is
  a complement for *map / who-calls / understand*, not a replacement for search.
