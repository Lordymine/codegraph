# Contributing to codegraph

Thanks for helping. codegraph is a small, opinionated Go project — a token-efficient
code knowledge graph for AI agents. This guide is short on purpose.

## Prerequisites

- **Go 1.26+**.
- **A C compiler** (gcc/clang) — the build links tree-sitter via cgo, so
  `CGO_ENABLED=1` is required. On Windows, MinGW-w64 (e.g. WinLibs) on `PATH`.
- **Node.js** — only at *index time*, for TypeScript/JS repos (scip-typescript runs
  via `npx`). Not needed to build or to test.

```bash
go build -o codegraph ./cmd/codegraph    # or: make build
make test                                 # or: go test ./...
```

## The loop (small, reviewable increments)

1. **One change at a time.** Keep PRs focused and small enough to review in minutes.
2. **Test-first where it earns its keep** — the store, query, discover, and the call
   resolvers all have testable contracts. A behavior change should come with a test
   that describes it.
3. **Green gate before every commit:** `go build ./...`, `go vet ./...`,
   `go test ./...`, and `gofmt`. CI runs the same on every PR.
4. **Conventional Commits** — `type(scope): description` in English
   (`feat(query): ...`, `fix(gocalls): ...`, `docs: ...`).

## Design principles (don't violate without a reason)

- **Honest precision.** An edge whose endpoints aren't both real nodes is *dropped*,
  never guessed. A missing edge beats a wrong one — an agent that trusts the graph
  must never be sent the wrong way.
- **Delegate to the type checker.** Call resolution is delegated to scip-typescript
  (TS/JS) and go/packages (Go), not re-implemented. New language support should follow
  the same bet.
- **Storage is trivial on purpose.** The value is in the edges and the compact-ref
  protocol, not the database.
- **Token-efficient by construction.** Queries return compact refs, never source;
  code comes only via `snippet`.

See `docs/ARCHITECTURE.md` for the design and `docs/ROADMAP.md` for what's planned.

## Pull requests

`main` is protected: it does not accept direct pushes (except by admins). Open a PR
from a branch; CI must pass before merge.

- Branch, commit, push, open a PR against `main`.
- Make sure CI is green (build, vet, test, gofmt).
- Describe what changed and why; link an issue if there is one.

## Commit authorship

Commits are authored solely by the repository owner identity configured in this repo.
**Do not add `Co-Authored-By` trailers** (including AI/assistant trailers) — a
`commit-msg` hook strips them as a safeguard.

## Reporting bugs / ideas

Open an issue with: what you ran, the repo/stack, what you expected, and what you got.
For indexing issues, `codegraph stats <repo>` and the `dropped` count from
`codegraph index <repo>` are useful to include.
