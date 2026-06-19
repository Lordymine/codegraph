// Deterministic graph-mode answers for the call questions (0 agents). The graph
// answer to "who calls X" / "what X calls" is exactly `codegraph cli callers/callees`
// — on cobra the 9 graph-responder agents reproduced this verbatim, so we skip the
// responder agents here and spend the budget on the oracle (the part that needs
// independent reasoning). Usage: node graph-answers.js <repo> <outdir>
const { execFileSync } = require('child_process')
const fs = require('fs')
const path = require('path')

const EXE = path.resolve('codegraph.exe')
const REPO = path.resolve(process.argv[2])
const OUT = process.argv[3]

const questions = JSON.parse(fs.readFileSync(path.join(OUT, 'questions.json'), 'utf8'))
const callQs = questions.filter(q => q.type === 'callers' || q.type === 'callees')

function graphNames(qn, type) {
  const tool = type === 'callers' ? 'callers' : 'callees'
  const out = execFileSync(EXE, ['cli', tool, REPO, JSON.stringify({ qualified_name: qn, limit: 500 })], {
    encoding: 'utf8',
    maxBuffer: 64 * 1024 * 1024,
  })
  return out.split('\n').filter(Boolean).map(l => l.split('\t')[1]).filter(Boolean)
}

const answers = callQs.map(q => ({ id: q.id, mode: 'graph', items: graphNames(q.qn, q.type), tokens: 0, calls: 1 }))
fs.writeFileSync(path.join(OUT, 'answers.json'), JSON.stringify(answers, null, 2))
console.log('wrote', path.join(OUT, 'answers.json'), '-', answers.map(a => `${a.id}=${a.items.length}`).join(' '))
