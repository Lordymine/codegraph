# CLAUDE.md — codegraph

> Trabalhe SEMPRE a partir desta pasta (`d:/projetos/codegraph`), abrindo a
> sessão do Claude Code aqui dentro — não pela pasta do ajudaqui. Este projeto é
> independente, com git próprio.

## O que é

`codegraph` — grafo de conhecimento de código **token-efficient** para agentes de
IA, em Go. Reimplementação enxuta das ideias do
[DeusData/codebase-memory-mcp](https://github.com/DeusData/codebase-memory-mcp)
(que é C puro). Foco no **stack do Rafael** (TypeScript/JS + Go + NestJS), não 158
linguagens. Module: `github.com/Lordymine/codegraph`. Go 1.26.

Indexa um repo → grafo SQLite de símbolos + relações; o agente consulta o grafo
(quem-chama, callers/callees, busca rankeada) em vez de ler arquivo por arquivo.

## Princípios de design (não violar sem motivo)

1. **Storage é trivial** — 2 tabelas (`nodes`, `edges`) + FTS5, igual ao upstream.
   O valor está nas **arestas CALLS**, não no banco.
2. **Token-efficient por construção** — toda query retorna **ref compacta**
   (`name + qualified_name + label + file + line`), NUNCA código. Código só via a
   tool `snippet`. É daí que vem o ~10× de economia de token.
3. **Aposta-chave: delegar ao type-checker da linguagem, não reimplementar.** O
   upstream refez resolução de tipos em C ("Hybrid LSP", ~9 langs, meses de
   trabalho). Nós delegamos a resolução de chamadas aos **indexadores batch** da
   linguagem — `scip-typescript` (TS/JS) e `go/packages` + `go/callgraph` (Go, as
   libs que o próprio gopls usa) — lidos in-process. Mesma precisão de type-checker
   real, num passe só, sem servidor LSP vivo pra babá. Pulamos a parte mais difícil
   do port.
4. **Precisão honesta** — aresta não resolvida é descartada (endpoints precisam
   existir no grafo). Melhor aresta faltando que aresta errada.
5. **Poucas dependências** — SQLite puro-Go (`modernc.org/sqlite`), MCP server em
   stdlib. tree-sitter (cgo) é a dep pesada do M1 — por isso o build exige
   `CGO_ENABLED=1` + gcc. scip-typescript entra como ferramenta de build (Node), não
   liga no binário.

## Estado atual — M0 + M1 + M2 + M3 fechados

- Store SQLite 2 tabelas + FTS5 espelhando o upstream (`internal/graph`).
- Discover (hard-ignores + `.cbmignore`) + detecção de linguagem (Go/TS/JS).
- **M1** — definições via **tree-sitter** (`internal/index/treesitter.go`): nós
  File/Function/Method/Class/Interface/Type/Enum/Variable + edges DEFINES, com
  `end_line` real, `is_exported` e decorators (NestJS etc.). Edges IMPORTS (TS/JS).
  Paralelo (NumCPU goroutines).
- **M2** — edges **CALLS** com precisão de type-checker: `scip-typescript` (TS/JS,
  `internal/scip`) + `go/packages` + CHA (Go, `internal/gocalls`), costurados em
  `internal/index/calls.go`. Tags `resolver`/`confidence` nas arestas.
- **M3** — indexação incremental (`internal/index/incremental.go`): sha256 por arquivo,
  `DetectChanges`, **no-op quando intacto** (cobra 1.77s→0.06s) e **CALLS gated por
  escopo** (re-roda scip/Go só dos escopos com arquivo mudado, reusa o resto).
- Query engine (`internal/query`): search / callers / callees / neighbors / snippet /
  detect_changes.
- MCP stdio JSON-RPC (`internal/mcp`); CLI (`cmd/codegraph`): `index | stats | changes |
  mcp | bench | quality | cli`.
- **Prova M2:** `callees(ResolveCalls)` → as 6 funções que ela chama;
  `callers(Store.InsertEdges)` → `pipeline.Run`; os 4 `getActiveCode` homônimos do
  ajuda-aqui desambiguados.

**Qualidade (medida):** harness de answer-quality — TS/JS ~89%; Go ~88% mean / 85%
callers / 92% callees, medido **intra-repo** (callees de stdlib/lib fora do oráculo,
porque o grafo os dropa por design — igual ao upstream, que grada isso como PARTIAL).
Go chegou lá com **VTA** (`internal/gocalls`, substituiu o CHA impreciso) + carregar
arquivos de teste (`packages Tests:true`). Ver `docs/QUALITY.md`.

## Build & uso

```bash
go build -o codegraph ./cmd/codegraph     # ou: make build
./codegraph index <repo>                  # constrói o grafo
./codegraph stats <repo>
./codegraph cli search <repo> '{"query":"getActiveCode","limit":5}'
./codegraph mcp <repo>                     # serve MCP (stdio)
make test                                  # go test ./...
```

Store: `~/.cache/codegraph/<project>.db`. Original clonado (shallow) em
`_upstream/codebase-memory-mcp/` para referência (gitignored).

## Próximos passos (ordem) — ver `docs/ROADMAP.md`

- **M1** ✅ tree-sitter + superfície TS completa + IMPORTS.
- **M2** ✅ CALLS edges via indexadores batch (scip-typescript + go/packages CHA).
- **M3** ✅ incremental: hash por arquivo + no-op + CALLS gated por escopo + `detect_changes`.
- **M4 (próximo)** SIMILAR_TO (MinHash/LSH) · **M5** get_architecture + registrar no Claude Code.
- **Qualidade Go ≥85%** ✅ — VTA (substituiu CHA) + carregar arquivos de teste,
  medição intra-repo. Track de paper/eval na memória.

## Convenções

- Go idiomático; pacotes pequenos; erros explícitos.
- TDD onde fizer sentido (store, query, discover têm contrato testável).
- Conventional Commits (`tipo(escopo): desc em inglês`). Verificar `go build` +
  `go vet` + `go test` antes de commitar.
- **Autoria dos commits (regra rígida):** NUNCA adicionar trailer `Co-Authored-By`
  (nem de IA/Claude). Todo commit parte exclusivamente do usuário
  `Rafael Oliveira <rafaelkefren@gmail.com>` (travado no git config local). Um hook
  `commit-msg` remove o trailer automaticamente como reforço.
- Documentação viva: atualizar `docs/ROADMAP.md` ao fechar milestone e
  `docs/ARCHITECTURE.md` ao mudar design.

## Docs

- `docs/UPSTREAM.md` — tudo sobre o original (schema, pipeline, edge types,
  benchmark honesto 83% vs 92% / 10× token, Hybrid LSP, links, source layout).
- `docs/ARCHITECTURE.md` — design Go nosso.
- `docs/ROADMAP.md` — milestones + perguntas em aberto.
