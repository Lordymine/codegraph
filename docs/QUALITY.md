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
| `callees` | what X calls (intra-repo) | **F1** over intra-repo callee names vs oracle |
| `definition` | where is X defined | **file:line** match (basename + ±3 lines) |
| `open` | explain X's responsibility | **LLM judge**, 0–100% |

Candidates are picked from the graph (call hubs, sampled definitions) — choosing
*what* to ask is not circular; only the *answers* must be independent.

`normName` folds a reference to its last identifier (`Service.getActiveCode`,
`x.getActiveCode()`, `getActiveCode` all compare equal). Same-named methods in
different classes therefore collapse together — an accepted approximation.

## Intra-repo ground truth (callers/callees)

The graph emits CALLS edges **only between symbols defined in this repo** — stdlib,
dependency and builtin targets are dropped by design (honest precision). The upstream
does exactly the same: its `resolve_single_call` emits an edge only when the callee
resolves to an existing node, and its own benchmark grades an external-only callee set
(`SendAsync has 0 outbound — calls external ISender.Send`) as **PARTIAL, not FAIL**.
So the truth **and** the question prompt for callers/callees are **intra-repo**: only
callers/callees themselves defined in the repo count; stdlib/dependency calls
(`fmt.Errorf`, `os.Create`, pflag's `GetBool`, `append`) are excluded from the truth
*and* from what either responder is asked for (so the grep baseline isn't unfairly
penalised for listing external calls it can see and the graph can't).

This is not goalpost-moving — it scores the graph against the contract it actually has
(shared with the upstream). What is **not** excluded: func-value / dynamic dispatch
(`RunE`, callback fields) — intra-repo calls the graph genuinely cannot resolve
statically, so they stay in the truth as honest misses. Measured effect on cobra (Go):
counting stdlib gives callees F1 62%; intra-repo gives **92%** — the gap was the graph
being penalised for stdlib it never indexes.

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

## Results — ajuda-aqui (14 questions, run via the ultracode workflow)

47 agents: an independent oracle per question, a graph-only and a grep-only
responder per question, and a judge for the open ones.

| mode | mean quality | tokens | tool calls |
|---|--:|--:|--:|
| **graph** | **89%** | 14,228 | 22 |
| **baseline** (grep) | **87%** | 115,645 | 166 |

| by type | callers | callees | definition | open |
|---|--:|--:|--:|--:|
| graph | 100% | 65% | 100% | 75% |
| baseline | 99% | 39% | 100% | 100% |

**The honest finding is not "graph is more correct" — it is "graph is as correct,
≈8× cheaper."** A careful grep agent matches the graph on structural questions, but
pays ~8× the tokens and ~7.5× the tool calls to do it (e.g. "who calls Button":
graph = 1 call, the grep agent opened 20+ files). Two genuine quality differences:

- **Callees: graph wins (65% vs 39%).** Deciding whether a call is "direct" or
  nested inside a callback is exactly the type-resolution work humans/agents get
  wrong by hand; the type checker doesn't. (Both scores are depressed because the
  oracle's *strict* "direct in body only" rule excludes `useCallback`/`.map`
  callback calls that both the graph and the agents include — callees F1 is
  sensitive to that definition; the *gap* is the signal, not the absolute.)
- **Open comprehension: baseline wins (100% vs 75%).** Compact refs carry structure,
  not intent — explaining *what a symbol is for* is where reading the code wins.
  This is the upstream's "graph trades quality for tokens", reproduced.

## Results — Go (cobra, gh-cli), and the closure-attribution fix

Measured intra-repo, graph mode, against the independent oracle truth:

| repo | mean | callers | callees | definition | open |
|---|--:|--:|--:|--:|--:|
| **cobra** | 91% | 93% | 92% | 100% | 70% |
| **gh-cli** | 99% | 100% | 97% | – | – |

cobra graph mean 91% sits just under the grep baseline's 93%, at ~4.5× fewer tokens
(4042 vs 18320) and ~3× fewer tool calls (21 vs 60) — the same "as correct, far
cheaper" shape as ajuda-aqui.

**What moved callers from 85% to 93% (cobra): crediting closure calls to the
enclosing named function.** A call written inside a function literal — cobra's
`Run: func(){...}`, flag visitors, locally-assigned closures — has an anonymous SSA
source (`initCompleteCmd$1`) that is not a graph node, so every such call was dropped.
`internal/gocalls` now walks `ssa.Function.Parent()` to the named function/method that
lexically contains the closure and credits the call there (what an IDE "find callers"
does). cobra recovered ~140 real edges; `callers(getCompletions)` went from empty to
`initCompleteCmd`. Zero false positives introduced — it recovers calls that genuinely
happen, just inside a literal. This is a pure recall win, so it raised callers without
touching the interface-dispatch precision VTA already gives.

The same fix surfaced a real dead function via the `dead_code` query:
`appendIfNotPresent`, which cobra's own source comments "is unused by cobra and should
be removed in a version 2" — the lone result after closure recall removed the
false-positive noise.

**On the 50-result cap.** The `callers`/`callees` default limit is 50. On a hub like
gh-cli's `iostreams.Test` (448 real callers) that crushes the *answer's* recall
regardless of resolver quality — a CLI default, not a graph limit. The numbers above
are measured uncapped (limit 1000) so the score reflects the graph, not the cap; the
controlled before/after for the closure fix (85→93) holds the cap fixed on both sides.
Raising the default is a tracked product change.

## A scorer bug we caught (and why the split harness matters)

The first scoring run reported baseline callers at **32%** — four questions at 0%.
That was a *scorer* artifact, not a baseline failure: responders append the location
(`Name (file.tsx:29)`), and `normName` was taking the last `:`-segment — the line
number `29`, not `Name`. Fixed (strip the annotation first; regression-tested), and
re-scored the SAME `truth.json`/`answers.json` **without re-running the 47 agents** —
the payoff of separating data generation from grading. Corrected baseline callers:
**99%**. Lesson worth keeping: a benchmark that flatters your tool by *miscounting
the baseline* is worse than none — verify the surprising number before believing it.

## Reading the result honestly

- We publish whatever the run says. A harness that can only flatter the graph isn't
  a harness — see the bug above.
- Self-reported tokens are agent estimates; the **authoritative** token numbers are
  the deterministic ones in `docs/BENCHMARK.md` (16× window). Tool calls here are
  counted, so trust those.

## Output files (in `outdir`, gitignored)

```
questions.json   the tasks (from `quality gen`)
truth.json       oracle answers          (filled by the workflow)
answers.json     graph + baseline replies (filled by the workflow)
report.md        the graded comparison    (from `quality score`)
```
