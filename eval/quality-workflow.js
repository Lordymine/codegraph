// Reusable quality-run workflow for the multi-repo eval (see eval/README.md).
//
// Invoke per repo with:
//   Workflow({ scriptPath: "<this file>", args: { repo, exe, outdir, questions } })
// where `questions` is the array from `<outdir>/questions.json` (produced by
// `codegraph quality gen`). args may arrive as a JSON string (the harness sometimes
// stringifies it) — we parse defensively.
//
// Roles: an independent oracle per question (grep only, NOT the graph), a graph-only
// and a grep-only responder per question, and a judge for the open ones. Writes
// <outdir>/truth.json and <outdir>/answers.json; grade with `codegraph quality score`.

export const meta = {
  name: 'codegraph-quality-run',
  description: 'Quality harness for one repo: independent oracle + graph-vs-baseline responders + judge',
  phases: [
    { title: 'Oracle', detail: 'independent ground truth per question (grep only)' },
    { title: 'Answer', detail: 'graph-only and grep-only responders per question' },
    { title: 'Judge', detail: 'score open answers vs oracle notes' },
    { title: 'Write', detail: 'write truth.json + answers.json' },
  ],
}

const A = typeof args === 'string' ? JSON.parse(args) : args
const REPO = A.repo
const EXE = A.exe
const OUT = A.outdir

const QUESTION = { type: 'object', properties: { id: { type: 'string' }, type: { type: 'string' }, lang: { type: 'string' }, symbol: { type: 'string' }, qn: { type: 'string' }, file: { type: 'string' }, line: { type: 'integer' }, prompt: { type: 'string' } }, required: ['id', 'type', 'prompt'] }
const QUESTIONS = { type: 'object', properties: { questions: { type: 'array', items: QUESTION } }, required: ['questions'] }
const ORACLE_STRUCT = { type: 'object', properties: { items: { type: 'array', items: { type: 'string' } }, notes: { type: 'string' } }, required: ['items'] }
const ORACLE_OPEN = { type: 'object', properties: { notes: { type: 'string' } }, required: ['notes'] }
const RESPONDER = { type: 'object', properties: { items: { type: 'array', items: { type: 'string' } }, text: { type: 'string' }, calls: { type: 'integer' }, tokens_approx: { type: 'integer' } }, required: ['calls'] }
const JUDGE = { type: 'object', properties: { score: { type: 'number' }, reason: { type: 'string' } }, required: ['score'] }

const clamp = (x) => (x < 0 ? 0 : x > 1 ? 1 : x)

function oracleStructPrompt(q) {
  const common = `You are establishing GROUND TRUTH independently of any pre-built index. Repo root: ${REPO}. Use ONLY ripgrep/grep + reading files — do NOT use any 'codegraph' tool. Be exhaustive and precise; a missing or wrong answer corrupts the benchmark.`
  if (q.type === 'callees') {
    return `${common}\nTask: open the body of \`${q.symbol}\` at ${q.file}:${q.line} and list every function/method/hook/component it invokes DIRECTLY **that is DEFINED IN THIS REPO**. EXCLUDE calls into the standard library or third-party dependencies (fmt.*, os.*, builtins like append, methods on external types) — the graph indexes only this repo's symbols, exactly as the upstream does, so external callees are out of scope (intra-repo truth; see docs/QUALITY.md). KEEP func-value / dynamic-dispatch field invocations defined in this repo (e.g. RunE) — those are intra-repo calls the graph may legitimately miss, so they stay as honest misses. Also exclude things merely imported but not called, type annotations, and nested-callback calls belonging to a different function. Return items = de-duplicated intra-repo callee names; notes = how you verified, and what you excluded as external.`
  }
  if (q.type === 'definition') {
    return `${common}\nTask: find where \`${q.symbol}\` is DEFINED (its declaration, not usages). Return items = ["relpath:line"] (one entry, repo-relative, with the line of the declaration); notes = the exact declaration line.`
  }
  return `${common}\nTask: find EVERY function/method/component that DIRECTLY calls/uses \`${q.symbol}\` (defined at ${q.file}:${q.line}). ripgrep the symbol repo-wide, open each hit, keep only real direct call/usage sites (exclude the definition, import statements, type-only refs, comments, strings). For each, record the NAME of the ENCLOSING function/method/component. Return items = de-duplicated caller names; notes = how you verified.`
}

function oracleOpenPrompt(q) {
  return `You are establishing a grading rubric independently. Repo root: ${REPO}. Read \`${q.symbol}\` at ${q.file}:${q.line} and its surroundings (grep/read only, no 'codegraph' tool). In notes, list the KEY FACTS a correct 2-4 sentence explanation MUST contain: the symbol's responsibility, what calls it, and what it depends on. Be concrete (name the real collaborators).`
}

function hint(q) {
  if (q.type === 'callers') return `Run: "${EXE}" cli callers "${REPO}" '{"qualified_name":"${q.qn}","limit":200}'. The output is TSV; items = the 2nd (tab-separated) column = caller names.`
  if (q.type === 'callees') return `Run: "${EXE}" cli callees "${REPO}" '{"qualified_name":"${q.qn}","limit":200}'. items = 2nd column = callee names.`
  if (q.type === 'definition') return `Run: "${EXE}" cli search "${REPO}" '{"query":"${q.symbol}","limit":10}'. Pick the defining node; items = ["relpath:line"] from its 3rd column (file:line).`
  return `Use "${EXE}" cli callers/callees/search on qualified_name ${q.qn} to gather structure, then write a 2-4 sentence explanation as text.`
}

function graphPrompt(q) {
  return `You are a coding agent answering a codebase question using ONLY the codegraph knowledge-graph tool. You may run ONLY this executable, via the Bash tool — NO grep, NO reading source files, NO other tools:\n  "${EXE}" cli <tool> "${REPO}" '<json>'\nwhere <tool> is search | callers | callees | snippet.\nQuestion: ${q.prompt}\n${hint(q)}\nCount every Bash invocation as calls. Estimate tokens_approx = (total characters of tool OUTPUT you read) / 4. Return items (names, or ["relpath:line"] for a definition)${q.type === 'open' ? ' and a 2-4 sentence text' : ''}.`
}

function baselinePrompt(q) {
  return `You are a coding agent answering a codebase question using ONLY ripgrep/grep (via Bash) and reading files (Read tool). You may NOT use any 'codegraph' tool. Repo root: ${REPO}.\nQuestion: ${q.prompt}\nWork as a normal agent would: search, open the files you actually need, reason. Count every tool call (each grep run + each file read) as calls. Estimate tokens_approx = (total characters of grep/file output you read) / 4. Return items (names, or ["relpath:line"] for a definition)${q.type === 'open' ? ' and a 2-4 sentence text' : ''}.`
}

function judgePrompt(q, notes, a) {
  return `Grade an agent's answer against a reference rubric. Question: ${q.prompt}\nReference (the key facts a correct answer must contain):\n${notes}\n\nAgent's answer:\n${a.text || '(no text provided)'}\n\nScore 0..1: 1 = fully correct and complete, 0.5 = partially correct or missing a key fact, 0 = wrong or empty. Be strict but fair. Return score + one-line reason.`
}

// Load the question set from disk (verbatim) so the script needs only repo/exe/
// outdir in args — no large payload, no paraphrase risk (schema-validated).
const loaded = await agent(
  `Read the JSON file at ${OUT}/questions.json and return its contents UNCHANGED as {"questions": <the array>}. Copy every field of every object exactly (id, type, lang, symbol, qn, file, line, prompt) — do not edit, summarize, reorder, or add anything.`,
  { schema: QUESTIONS, label: 'load-questions', phase: 'Oracle' }
)
const Q = (loaded && loaded.questions) || []
if (Q.length === 0) {
  log('no questions loaded — aborting')
  return { repo: REPO, questions: 0, truths: 0, answers: 0 }
}

const results = await pipeline(Q,
  async (q) => {
    if (q.type === 'open') {
      const o = await agent(oracleOpenPrompt(q), { schema: ORACLE_OPEN, label: `oracle:${q.id}`, phase: 'Oracle', effort: 'high' })
      return { q, truth: { id: q.id, notes: (o && o.notes) || '' } }
    }
    const o = await agent(oracleStructPrompt(q), { schema: ORACLE_STRUCT, label: `oracle:${q.id}`, phase: 'Oracle', effort: 'high' })
    return { q, truth: { id: q.id, items: (o && o.items) || [], notes: (o && o.notes) || '' } }
  },
  async (prev) => {
    const q = prev.q
    const [g, b] = await parallel([
      () => agent(graphPrompt(q), { schema: RESPONDER, label: `graph:${q.id}`, phase: 'Answer' }),
      () => agent(baselinePrompt(q), { schema: RESPONDER, label: `baseline:${q.id}`, phase: 'Answer' }),
    ])
    const mk = (mode, r) => ({ id: q.id, mode, items: (r && r.items) || [], text: (r && r.text) || '', tokens: (r && r.tokens_approx) || 0, calls: (r && r.calls) || 0 })
    return { ...prev, answers: [mk('graph', g), mk('baseline', b)] }
  },
  async (prev) => {
    const q = prev.q
    if (q.type !== 'open') return prev
    const notes = prev.truth.notes
    const judged = await parallel(prev.answers.map((a) => async () => {
      const j = await agent(judgePrompt(q, notes, a), { schema: JUDGE, label: `judge:${q.id}:${a.mode}`, phase: 'Judge', effort: 'high' })
      return { ...a, judge: j ? clamp(j.score) : 0 }
    }))
    return { ...prev, answers: judged }
  }
)

const ok = results.filter(Boolean)
const truth = ok.map((r) => r.truth)
const answers = ok.flatMap((r) => r.answers)

await agent(
  `Use the Write tool to create exactly two files with the exact JSON content below — do not modify the JSON, write it verbatim.\n\nFile path: ${OUT}/truth.json\nContent:\n${JSON.stringify(truth, null, 2)}\n\nFile path: ${OUT}/answers.json\nContent:\n${JSON.stringify(answers, null, 2)}\n\nAfter writing both, reply "done".`,
  { label: 'write-results', phase: 'Write' }
)

log(`quality run complete: ${truth.length} truths, ${answers.length} answers -> ${OUT}`)
return { repo: REPO, questions: Q.length, truths: truth.length, answers: answers.length }
