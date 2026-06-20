// codegraph — a tiny, token-efficient code knowledge graph for AI agents.
//
// A Go reimplementation (MVP) of the ideas in DeusData/codebase-memory-mcp.
// See docs/ for the full design, the upstream reference, and the roadmap.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/Lordymine/codegraph/internal/bench"
	"github.com/Lordymine/codegraph/internal/graph"
	"github.com/Lordymine/codegraph/internal/index"
	"github.com/Lordymine/codegraph/internal/mcp"
	"github.com/Lordymine/codegraph/internal/quality"
	"github.com/Lordymine/codegraph/internal/query"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "index":
		err = cmdIndex(arg(2, "."))
	case "stats":
		err = cmdStats(arg(2, "."))
	case "changes":
		err = cmdChanges(arg(2, "."))
	case "mcp":
		err = cmdMCP(arg(2, "."))
	case "bench":
		err = cmdBench(arg(2, "."))
	case "quality":
		err = cmdQuality(os.Args[2:])
	case "cli":
		err = cmdCLI(os.Args[2:])
	case "-h", "--help", "help":
		usage()
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func arg(i int, def string) string {
	if len(os.Args) > i {
		return os.Args[i]
	}
	return def
}

func usage() {
	fmt.Fprint(os.Stderr, `codegraph — code knowledge graph for AI agents

Usage:
  codegraph index <path>          Index a repo into the local graph store
  codegraph stats <path>          Show node/edge counts for a repo
  codegraph changes <path>        List source files changed since the last index
  codegraph mcp   <path>          Serve the graph over MCP (stdio) for a repo
  codegraph bench <path>          Re-index + measure token/tool-call/speed efficiency
  codegraph quality gen <repo> [outdir] [lang]   Generate the answer-quality question set
  codegraph quality score <dir>                  Grade filled truth+answers -> report.md
  codegraph cli   <tool> <path> <json>   Run one query tool (search|callers|callees|neighbors|similar|dead_code|snippet)

Store lives in ~/.cache/codegraph/<project>.db
`)
}

func storePath(project string) (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cache, "codegraph")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, project+".db"), nil
}

func openFor(root string) (*graph.Store, string, error) {
	root, _ = filepath.Abs(root)
	project := index.ProjectName(root)
	sp, err := storePath(project)
	if err != nil {
		return nil, "", err
	}
	st, err := graph.Open(sp)
	if err != nil {
		return nil, "", err
	}
	return st, project, nil
}

func cmdIndex(root string) error {
	st, _, err := openFor(root)
	if err != nil {
		return err
	}
	defer st.Close()
	res, err := index.Run(st, root)
	if err != nil {
		return err
	}
	if res.Reused {
		fmt.Printf("unchanged %s — reused index (files=%d nodes=%d edges=%d)\n",
			res.Project, res.Files, res.Nodes, res.EdgesKept)
		return nil
	}
	fmt.Printf("indexed %s\n  files=%d nodes=%d edges=%d (dropped %d unresolved)\n",
		res.Project, res.Files, res.Nodes, res.EdgesKept, res.EdgesDropped)
	return nil
}

func cmdChanges(root string) error {
	st, project, err := openFor(root)
	if err != nil {
		return err
	}
	defer st.Close()
	ch, err := index.DetectChanges(st, project, root)
	if err != nil {
		return err
	}
	if !ch.Any() {
		fmt.Println("no changes since last index")
		return nil
	}
	fmt.Print(ch.Summary())
	return nil
}

func cmdStats(root string) error {
	st, project, err := openFor(root)
	if err != nil {
		return err
	}
	defer st.Close()
	n, e, err := st.Stats(project)
	if err != nil {
		return err
	}
	fmt.Printf("project=%s nodes=%d edges=%d\n", project, n, e)
	return nil
}

func cmdMCP(root string) error {
	root, _ = filepath.Abs(root)
	st, project, err := openFor(root)
	if err != nil {
		return err
	}
	defer st.Close()
	eng := query.NewEngine(st, project, root)
	return mcp.NewServer(eng, os.Stdin, os.Stdout).Serve()
}

// cmdBench reproduces the upstream's measurable headline (token + tool-call
// efficiency) plus our own indexing-speed number. It re-indexes the repo (timing
// it), then asks "who calls X" for the top call hubs and compares the graph
// against two grep-based baselines. Answer-quality (83% vs 92%) is NOT measured —
// that needs an LLM judge; this reports only deterministic numbers.
func cmdBench(root string) error {
	root, _ = filepath.Abs(root)
	st, project, err := openFor(root)
	if err != nil {
		return err
	}
	defer st.Close()

	// 1) Indexing speed (our win vs upstream's ~20 min on Windows). Time is
	// measured clean (no MemStats sampling in the loop, which would STW and skew
	// it); memory is read once after, as a footprint — not a sampled peak.
	t0 := time.Now()
	res, err := index.Run(st, root)
	if err != nil {
		return err
	}
	elapsed := time.Since(t0)
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)

	// 2) Token / tool-call efficiency over the top call hubs.
	hubs, err := st.TopByInboundCalls(project, 15)
	if err != nil {
		return err
	}
	corpus, err := bench.LoadCorpus(root)
	if err != nil {
		return err
	}
	eng := query.NewEngine(st, project, root)

	var outs []bench.Outcome
	for _, q := range bench.QuestionsFromHubs(hubs) {
		o, err := bench.RunOne(eng, corpus, q)
		if err != nil {
			return err
		}
		outs = append(outs, o)
	}
	sum := bench.Summarize(outs)

	printBench(res, elapsed, m1.HeapInuse, outs, sum)
	return nil
}

func printBench(res index.Result, elapsed time.Duration, heapBytes uint64, outs []bench.Outcome, s bench.Summary) {
	fmt.Printf("# codegraph benchmark — %s\n\n", res.Project)

	fmt.Printf("## Indexing speed\n\n")
	fmt.Printf("files=%d nodes=%d edges=%d (dropped %d) · time=%s · %.0f files/s · heap=%dMB (footprint, not peak)\n\n",
		res.Files, res.Nodes, res.EdgesKept, res.EdgesDropped, elapsed.Round(time.Millisecond),
		float64(res.Files)/elapsed.Seconds(), heapBytes/(1024*1024))

	fmt.Printf("## Token efficiency — \"who calls X\" over %d call hubs\n\n", s.N)
	fmt.Printf("| symbol | callers | grep files | graph tok | win tok (×) | file tok (×) |\n")
	fmt.Printf("|---|--:|--:|--:|--:|--:|\n")
	for _, o := range outs {
		fmt.Printf("| `%s` | %d | %d | %d | %d (%.1f×) | %d (%.1f×) |\n",
			o.Question.Name, o.GraphResults, o.MatchFiles, o.Graph.Tokens,
			o.BaselineWin.Tokens, ratioOf(o.BaselineWin.Tokens, o.Graph.Tokens),
			o.BaselineFile.Tokens, ratioOf(o.BaselineFile.Tokens, o.Graph.Tokens))
	}

	fmt.Printf("\n## Summary\n\n")
	fmt.Printf("- **Tokens (median per query):** %.1f× vs grep+window · %.1f× vs grep+file\n",
		s.MedianRatioWin, s.MedianRatioFile)
	fmt.Printf("- **Tokens (total across set):** %.1f× vs grep+window · %.1f× vs grep+file  ← the \"10×\" headline\n",
		s.TotalRatioWin, s.TotalRatioFile)
	fmt.Printf("- **Tool calls (total):** graph %d vs baseline %d → %.1f× fewer\n",
		s.GraphCalls, s.BaselineWinCalls, s.CallRatioWin)
	fmt.Printf("- **Raw tokens:** graph=%d · grep+window=%d · grep+file=%d\n",
		s.GraphTokens, s.BaselineWinTokens, s.BaselineFileTokens)
}

func ratioOf(a, b int) float64 {
	if b == 0 {
		b = 1
	}
	return float64(a) / float64(b)
}

// cmdQuality drives the answer-quality harness:
//
//	codegraph quality gen   <repo> [outdir] [lang]   generate the question set
//	codegraph quality score <dir>                    grade filled truth+answers
//
// `gen` writes questions.json (+ truth/answers scaffolds) for the ultracode
// workflow to fill; `score` reads them back and writes report.md.
func cmdQuality(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: codegraph quality <gen|score> ...")
	}
	switch args[0] {
	case "gen":
		if len(args) < 2 {
			return fmt.Errorf("usage: codegraph quality gen <repo> [outdir] [lang]")
		}
		repo := args[1]
		outdir := "quality-run"
		if len(args) > 2 {
			outdir = args[2]
		}
		lang := "ts"
		if len(args) > 3 {
			lang = args[3]
		}
		return cmdQualityGen(repo, outdir, lang)
	case "score":
		if len(args) < 2 {
			return fmt.Errorf("usage: codegraph quality score <dir>")
		}
		return cmdQualityScore(args[1])
	default:
		return fmt.Errorf("unknown quality subcommand %q", args[0])
	}
}

func cmdQualityGen(repo, outdir, lang string) error {
	st, project, err := openFor(repo)
	if err != nil {
		return err
	}
	defer st.Close()

	// Index on demand so `gen` is self-contained.
	if n, _, _ := st.Stats(project); n == 0 {
		if _, err := index.Run(st, repo); err != nil {
			return err
		}
	}

	qs, err := quality.Generate(st, project, lang)
	if err != nil {
		return err
	}
	if len(qs) == 0 {
		return fmt.Errorf("no questions generated (is the repo indexed with CALLS edges?)")
	}

	if err := os.MkdirAll(outdir, 0o755); err != nil {
		return err
	}
	// truth scaffold: one entry per structural question for the oracle to fill.
	var truth []quality.Truth
	for _, q := range qs {
		if q.Type != quality.TypeOpen {
			truth = append(truth, quality.Truth{ID: q.ID, Notes: "oracle: fill Items independently of the graph"})
		}
	}
	if err := writeJSON(filepath.Join(outdir, "questions.json"), qs); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(outdir, "truth.json"), truth); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(outdir, "answers.json"), []quality.Answer{}); err != nil {
		return err
	}
	abs, _ := filepath.Abs(repo)
	meta := map[string]any{"repo": abs, "project": project, "lang": lang, "questions": len(qs)}
	if err := writeJSON(filepath.Join(outdir, "meta.json"), meta); err != nil {
		return err
	}

	fmt.Printf("generated %d questions for %s -> %s/\n", len(qs), project, outdir)
	fmt.Printf("  questions.json  the tasks (run the ultracode workflow to fill truth.json + answers.json)\n")
	fmt.Printf("  then: codegraph quality score %s\n", outdir)
	return nil
}

func cmdQualityScore(dir string) error {
	var qs []quality.Question
	var truth []quality.Truth
	var answers []quality.Answer
	if err := readJSON(filepath.Join(dir, "questions.json"), &qs); err != nil {
		return err
	}
	if err := readJSON(filepath.Join(dir, "truth.json"), &truth); err != nil {
		return err
	}
	if err := readJSON(filepath.Join(dir, "answers.json"), &answers); err != nil {
		return err
	}
	report := quality.Report(qs, truth, answers)
	if err := os.WriteFile(filepath.Join(dir, "report.md"), []byte(report), 0o644); err != nil {
		return err
	}
	fmt.Print(report)
	return nil
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// cmdCLI: codegraph cli <tool> <path> <json-args>
func cmdCLI(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: codegraph cli <tool> <path> [json]")
	}
	tool, root := args[0], args[1]
	raw := "{}"
	if len(args) > 2 {
		raw = args[2]
	}
	var a struct {
		Query         string `json:"query"`
		Label         string `json:"label"`
		QualifiedName string `json:"qualified_name"`
		File          string `json:"file"`
		StartLine     int    `json:"start_line"`
		EndLine       int    `json:"end_line"`
		Limit         int    `json:"limit"`
	}
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return fmt.Errorf("bad json args: %w", err)
	}

	root, _ = filepath.Abs(root)
	st, project, err := openFor(root)
	if err != nil {
		return err
	}
	defer st.Close()
	eng := query.NewEngine(st, project, root)

	// Ref-returning tools print the compact wire format (one TSV line per ref);
	// snippet prints raw source. Both are already token-minimal — no JSON wrapper.
	var out string
	switch tool {
	case "search":
		var refs []query.Ref
		refs, err = eng.Search(a.Query, a.Label, a.Limit)
		out = query.CompactRefs(refs)
	case "callers":
		var refs []query.Ref
		refs, err = eng.Callers(a.QualifiedName, a.Limit)
		out = query.CompactRefs(refs)
	case "callees":
		var refs []query.Ref
		refs, err = eng.Callees(a.QualifiedName, a.Limit)
		out = query.CompactRefs(refs)
	case "neighbors":
		var refs []query.Ref
		refs, err = eng.Neighbors(a.QualifiedName, a.Limit)
		out = query.CompactRefs(refs)
	case "similar":
		var refs []query.Ref
		refs, err = eng.Similar(a.QualifiedName, a.Limit)
		out = query.CompactRefs(refs)
	case "dead_code":
		var refs []query.Ref
		refs, err = eng.DeadCode(a.Limit)
		out = query.CompactRefs(refs)
	case "snippet":
		out, err = eng.Snippet(a.File, a.StartLine, a.EndLine)
	default:
		return fmt.Errorf("unknown tool %q", tool)
	}
	if err != nil {
		return err
	}
	fmt.Print(out)
	return nil
}
