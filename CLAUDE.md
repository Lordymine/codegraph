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
3. **Aposta-chave: delegar a LSP, não reimplementar type-checker.** O upstream
   refez resolução de tipos em C ("Hybrid LSP", ~9 langs, meses de trabalho).
   Nós delegamos resolução de chamada/definição ao LSP da linguagem (gopls,
   tsserver/typescript-go) via `textDocument/definition`. Precisão de type-checker
   real de graça, e pulamos a parte mais difícil do port.
4. **Precisão honesta** — aresta não resolvida é descartada (endpoints precisam
   existir no grafo). Melhor aresta faltando que aresta errada.
5. **Poucas dependências** — SQLite puro-Go (`modernc.org/sqlite`, sem cgo), MCP
   server em stdlib. tree-sitter é a única dep pesada que vamos adicionar.

## Estado atual — M0 (scaffold rodando)

- Store SQLite 2 tabelas + FTS5 espelhando o upstream (`internal/graph`).
- Discover (hard-ignores + `.cbmignore`) + detecção de linguagem (Go/TS/JS).
- Pass de definições **por REGEX** (placeholder, `internal/index/definitions.go`)
  → nós File/Function/Method/Class + edges DEFINES. Paralelo (NumCPU goroutines).
- Query engine (`internal/query`): search / callers / callees / neighbors / snippet.
- MCP stdio JSON-RPC mínimo (`internal/mcp`).
- CLI (`cmd/codegraph`): `index | stats | mcp | cli`.
- **Prova:** indexa a si mesmo (9 arq, 75 nós, 66 edges); `cli search` retorna file+line.

**Limitação honesta:** sem CALLS edges ainda (o `ResolveCalls` é stub). O valor
real vem no M2.

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

- **M1** — trocar regex por **tree-sitter** (`github.com/smacker/go-tree-sitter`)
  + grammars Go/TS/TSX; capturar signature/params/decorators; edges IMPORTS.
- **M2 (o grande)** — **CALLS edges via LSP delegation** (gopls/tsserver). Critério
  de saída: callers/callees corretos no módulo validation-codes do ajudaqui, com
  os 4 `getActiveCode` homônimos desambiguados.
- **M3** incremental por hash de arquivo · **M4** SIMILAR_TO (MinHash/LSH) ·
  **M5** get_architecture + registrar no Claude Code.

## Convenções

- Go idiomático; pacotes pequenos; erros explícitos.
- TDD onde fizer sentido (store, query, discover têm contrato testável).
- Conventional Commits (`tipo(escopo): desc em inglês`). Verificar `go build` +
  `go vet` + `go test` antes de commitar. Branch por feature, nunca direto na main.
- Documentação viva: atualizar `docs/ROADMAP.md` ao fechar milestone e
  `docs/ARCHITECTURE.md` ao mudar design.

## Docs

- `docs/UPSTREAM.md` — tudo sobre o original (schema, pipeline, edge types,
  benchmark honesto 83% vs 92% / 10× token, Hybrid LSP, links, source layout).
- `docs/ARCHITECTURE.md` — design Go nosso.
- `docs/ROADMAP.md` — milestones + perguntas em aberto.
