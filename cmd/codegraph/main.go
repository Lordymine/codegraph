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

	"github.com/Lordymine/codegraph/internal/graph"
	"github.com/Lordymine/codegraph/internal/index"
	"github.com/Lordymine/codegraph/internal/mcp"
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
	case "mcp":
		err = cmdMCP(arg(2, "."))
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
  codegraph mcp   <path>          Serve the graph over MCP (stdio) for a repo
  codegraph cli   <tool> <path> <json>   Run one query tool (search|callers|callees|neighbors|snippet)

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
	fmt.Printf("indexed %s\n  files=%d nodes=%d edges=%d (dropped %d unresolved)\n",
		res.Project, res.Files, res.Nodes, res.EdgesKept, res.EdgesDropped)
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
		Query, Label, QualifiedName, File string
		StartLine, EndLine, Limit         int
	}
	_ = json.Unmarshal([]byte(raw), &a)

	root, _ = filepath.Abs(root)
	st, project, err := openFor(root)
	if err != nil {
		return err
	}
	defer st.Close()
	eng := query.NewEngine(st, project, root)

	var out any
	switch tool {
	case "search":
		out, err = eng.Search(a.Query, a.Label, a.Limit)
	case "callers":
		out, err = eng.Callers(a.QualifiedName, a.Limit)
	case "callees":
		out, err = eng.Callees(a.QualifiedName, a.Limit)
	case "neighbors":
		out, err = eng.Neighbors(a.QualifiedName, a.Limit)
	case "snippet":
		out, err = eng.Snippet(a.File, a.StartLine, a.EndLine)
	default:
		return fmt.Errorf("unknown tool %q", tool)
	}
	if err != nil {
		return err
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(b))
	return nil
}
