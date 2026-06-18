# eval — multi-repo quality evaluation (for the paper)

The single-repo quality run (ajuda-aqui, `docs/QUALITY.md`) showed graph ≈ baseline
quality at ~8× lower cost. One private repo is not a paper. This folder is the
harness for scaling that to **N public repos** so the number is reproducible.

## How many repos

- **Pilot: 5** — validate the pipeline on public repos and *measure variance* before
  committing budget. Don't pick the final N by guess; pick it by the pilot's variance.
- **Paper: 12–15** public repos, ~12–14 questions each (~150–180 paired questions).
  The **cost** effect (≈8×, low variance) is significant by ~8 repos; the **quality**
  equivalence (small gap) needs the larger N. More than ~20 is diminishing returns for
  our focused TS/JS + Go scope (upstream's 31 covered ~15 languages — we don't).

`repos.json` is the candidate list (mixed stacks + sizes). `status` stays `candidate`
until a repo's build is verified locally.

## What makes the number credible (matters more than N)

1. **Public, well-known repos.** A reviewer must be able to reproduce it; ajuda-aqui
   (private) is for development only, never the paper.
2. **Stratified questions — done.** `quality gen` now samples symbols across the
   call-degree distribution (hub → typical → leaf), not just the top hubs, so the set
   isn't cherry-picked to where the graph wins. See `internal/quality/questions.go`.
3. **Stochasticity.** LLM answers vary; plan k=3 runs per question (or report variance).
   Not yet automated — tracked below.

## Per-repo pipeline

```bash
# 1. clone + build so the type checkers can resolve (REQUIRED)
git clone <url> eval/checkouts/<id>
cd eval/checkouts/<id> && <build>     # e.g. pnpm install  /  go mod download

# 2. index + generate the stratified question set
codegraph index eval/checkouts/<id>
codegraph quality gen eval/checkouts/<id> eval/runs/<id> <lang>

# 3. fill truth.json + answers.json via the ultracode workflow
#    (oracle + graph-only/grep-only responders + judge; see docs/QUALITY.md)

# 4. grade
codegraph quality score eval/runs/<id>     # -> eval/runs/<id>/report.md
```

`eval/checkouts/` and `eval/runs/` are gitignored (clones + per-repo artifacts).
`repos.json` and this protocol are versioned.

## TODO before the paper run

- [ ] Verify each candidate's build in `repos.json` (flip `status` to `verified`).
- [ ] Aggregator: combine per-repo `report.md`/scores into one table
      (mean quality graph vs baseline, token/call ratios, by-type, **by-language**)
      with variance / CIs. (`codegraph quality aggregate eval/runs/` — not built yet.)
- [ ] Automate k=3 runs per question and report variance.
- [ ] Run the pilot 5, do a real power analysis, then fix the final N.
