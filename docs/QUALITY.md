# Quality harness — codegraph

The answer-quality half of the upstream benchmark, the part `docs/BENCHMARK.md`
deliberately left out. The token harness proves the graph is *cheap*; this one
asks whether an agent using it is *right* — and at what cost — versus a grep-driven
agent.

```bash
codegraph quality gen   <repo> [outdir] [lang]   # build the question set
#   ... run the ultracode workflow to fill truth.json + answers.json ...
codegraph quality score <outdir>                 # grade -> report.md
```

## Why a separate harness (and why it's hard to do honestly)

Answer quality can't be metered like tokens — it needs a *correct answer* to grade
against, and a model to produce the agent's answer. Two honesty traps:

1. **Circular ground truth.** If the "correct answer" is read from our own graph,
   the graph-driven agent is perfect by construction and the number is a lie. So
   **ground truth is established independently by an exhaustive oracle** (run in the
   workflow, no token budget, all tools) — never from the graph.
2. **Romantic judging.** A vague "is this answer good?" LLM score is noise. So
   structural questions are graded **objectively** (set F1 / file:line match); only
   open comprehension questions fall back to an LLM judge, and we label them as such.

## Question set (≈14, mirroring the upstream's ~12/language)

| type | question | scoring |
|---|---|---|
| `callers` | who calls X | **F1** over caller names vs oracle |
| `callees` | what X calls | **F1** over callee names vs oracle |
| `definition` | where is X defined | **file:line** match (basename + ±3 lines) |
| `open` | explain X's responsibility | **LLM judge**, 0–100% |

Candidates are picked from the graph (call hubs, sampled definitions) — choosing
*what* to ask is not circular; only the *answers* must be independent.

`normName` folds a reference to its last identifier (`Service.getActiveCode`,
`x.getActiveCode()`, `getActiveCode` all compare equal). Same-named methods in
different classes therefore collapse together — an accepted approximation.

## The three roles (run by the ultracode workflow)

1. **Oracle** — for each structural question, finds the *true* answer exhaustively
   (any tool, no budget). Writes `truth.json`. This is the independent authority.
2. **Responders** — two agents answer every question under realistic constraint and
   self-report tokens + tool calls:
   - `graph` — may use **only** the codegraph tools (`cli search|callers|callees|snippet`).
   - `baseline` — may use **only** `grep` + file reads.
3. **Judge** — scores the `open` answers 0–1 against the oracle's notes.

`codegraph quality score` then computes F1 for structural answers, ingests the
judge scores for open ones, and emits the comparison table.

## Reading the result honestly

- **Structural questions favor the graph** — it encodes exactly that call structure
  (type-checker-derived). The honest finding there is the *cost gap*: the baseline
  may reach similar quality but pays the token/tool-call bill `docs/BENCHMARK.md`
  quantifies.
- **Open questions are where the graph is weakest** — compact refs don't explain
  intent. This is where the upstream's "graph trades ~9 quality points for ~10×
  tokens" shows up, and where we expect the baseline to hold its own.
- We publish whatever the run says. A harness that can only flatter the graph isn't
  a harness.

## Output files (in `outdir`, gitignored)

```
questions.json   the tasks (from `quality gen`)
truth.json       oracle answers          (filled by the workflow)
answers.json     graph + baseline replies (filled by the workflow)
report.md        the graded comparison    (from `quality score`)
```
