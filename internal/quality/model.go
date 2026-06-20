// Package quality is the answer-quality harness — the half of the upstream
// benchmark our token harness deliberately left out. It measures whether an LLM
// agent reaches the CORRECT answer to a codebase question, and at what token/
// tool-call cost, comparing a graph-driven agent against a grep-driven one.
//
// Honesty by construction: ground truth is NOT read from our own graph (that
// would make the graph agent trivially perfect). It is established independently
// by an exhaustive oracle (run by the ultracode workflow), then both agents are
// scored against it. Structural questions score by F1 (objective); open
// questions score by an LLM judge (0..1). See docs/QUALITY.md.
package quality

// QType is the kind of question.
type QType string

const (
	TypeCallers    QType = "callers"    // who calls X — score: F1 over caller names
	TypeCallees    QType = "callees"    // what X calls — score: F1 over callee names
	TypeDefinition QType = "definition" // where is X defined — score: file:line match
	TypeOpen       QType = "open"       // free-form comprehension — score: LLM judge
)

// Question is one item the agents must answer.
type Question struct {
	ID     string `json:"id"`
	Type   QType  `json:"type"`
	Lang   string `json:"lang"`
	Symbol string `json:"symbol,omitempty"`
	QN     string `json:"qn,omitempty"`   // stripped qualified name (graph mode can use directly)
	File   string `json:"file,omitempty"` // where the symbol is defined
	Line   int    `json:"line,omitempty"`
	Prompt string `json:"prompt"` // the natural-language task given to both agents
}

// Truth is the oracle-established correct answer for a structural question. For
// callers/callees, Items is the set of expected symbol names; for definition,
// Items is ["relpath:line"]. Open questions carry no precomputed truth.
type Truth struct {
	ID    string   `json:"id"`
	Items []string `json:"items,omitempty"`
	Notes string   `json:"notes,omitempty"`
}

// Answer is one mode's response to one question.
type Answer struct {
	ID     string   `json:"id"`
	Mode   string   `json:"mode"`            // "graph" | "baseline"
	Items  []string `json:"items,omitempty"` // structural answer set
	Text   string   `json:"text,omitempty"`  // open-question answer
	Tokens int      `json:"tokens"`          // tokens the agent spent reaching the answer
	Calls  int      `json:"calls"`           // tool calls the agent made
	Judge  *float64 `json:"judge,omitempty"` // 0..1 quality for open questions (judge-filled)
}

// Score is the graded result for one (question, mode).
type Score struct {
	ID        string  `json:"id"`
	Mode      string  `json:"mode"`
	Type      QType   `json:"type"`
	Quality   float64 `json:"quality"` // F1 for structural, judge for open
	Precision float64 `json:"precision"`
	Recall    float64 `json:"recall"`
}

// Agg aggregates per-mode quality and cost.
type Agg struct {
	Mode        string            `json:"mode"`
	N           int               `json:"n"`
	MeanQuality float64           `json:"mean_quality"`
	ByType      map[QType]float64 `json:"by_type"`
	TotalTokens int               `json:"total_tokens"`
	TotalCalls  int               `json:"total_calls"`
}
