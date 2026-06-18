package index

import (
	"os"
	"regexp"
	"strings"

	"github.com/Lordymine/codegraph/internal/graph"
)

// definitions.go is the MVP "definitions pass": a REGEX-based symbol extractor.
//
// This is intentionally a placeholder. It is fast and zero-dependency, but
// imprecise — it cannot see scope, generics, or call sites. The real pass will
// use tree-sitter (github.com/smacker/go-tree-sitter) for accurate ASTs and an
// LSP-delegation step (gopls / tsserver) for type-resolved CALLS edges.
// See docs/ROADMAP.md. Until then this gets the graph populated so the store,
// FTS search and MCP server can be exercised end to end.

var (
	reGoFunc   = regexp.MustCompile(`(?m)^func\s+(?:\([^)]*\)\s+)?([A-Za-z_]\w*)\s*[\(\[]`)
	reGoType   = regexp.MustCompile(`(?m)^type\s+([A-Za-z_]\w*)\s+`)
	reTSFunc   = regexp.MustCompile(`(?m)^\s*(?:export\s+)?(?:async\s+)?function\s+([A-Za-z_$]\w*)`)
	reTSClass  = regexp.MustCompile(`(?m)^\s*(?:export\s+)?(?:abstract\s+)?class\s+([A-Za-z_$]\w*)`)
	reTSArrow  = regexp.MustCompile(`(?m)^\s*(?:export\s+)?const\s+([A-Za-z_$]\w*)\s*=\s*(?:async\s*)?\([^)]*\)\s*(?::[^=]+)?=>`)
	reTSMethod = regexp.MustCompile(`(?m)^\s{2,}(?:public |private |protected |async |static )*([A-Za-z_$]\w*)\s*\([^)]*\)\s*[:{]`)
)

// ExtractDefinitions parses one file and returns its nodes + DEFINES edges.
func ExtractDefinitions(project string, f SourceFile) ([]graph.Node, []graph.Edge) {
	data, err := os.ReadFile(f.AbsPath)
	if err != nil {
		return nil, nil
	}
	src := string(data)
	lineOf := lineIndexer(src)

	fileQN := project + ":" + f.RelPath
	fileNode := graph.Node{
		Project: project, Label: graph.LabelFile,
		Name: baseName(f.RelPath), QualifiedName: fileQN,
		FilePath: f.RelPath, StartLine: 1, EndLine: strings.Count(src, "\n") + 1,
		Props: map[string]any{"lang": string(f.Lang)},
	}
	nodes := []graph.Node{fileNode}
	var edges []graph.Edge

	add := func(label graph.NodeLabel, name string, off int) {
		ln := lineOf(off)
		qn := fileQN + "." + name
		nodes = append(nodes, graph.Node{
			Project: project, Label: label, Name: name, QualifiedName: qn,
			FilePath: f.RelPath, StartLine: ln, EndLine: ln,
			Props: map[string]any{"lang": string(f.Lang), "is_test": isTest(f.RelPath)},
		})
		edges = append(edges, graph.Edge{Project: project, SourceQN: fileQN, TargetQN: qn, Type: graph.EdgeDefines})
	}

	switch f.Lang {
	case LangGo:
		collect(src, reGoFunc, func(name string, off int) { add(graph.LabelFunction, name, off) })
		collect(src, reGoType, func(name string, off int) { add(graph.LabelClass, name, off) })
	default: // ts / tsx / js
		collect(src, reTSFunc, func(name string, off int) { add(graph.LabelFunction, name, off) })
		collect(src, reTSArrow, func(name string, off int) { add(graph.LabelFunction, name, off) })
		collect(src, reTSClass, func(name string, off int) { add(graph.LabelClass, name, off) })
		collect(src, reTSMethod, func(name string, off int) {
			if !tsKeyword[name] {
				add(graph.LabelMethod, name, off)
			}
		})
	}
	return nodes, edges
}

// tsKeyword filters control-flow words the loose method regex would catch.
var tsKeyword = map[string]bool{
	"if": true, "for": true, "while": true, "switch": true, "catch": true,
	"return": true, "constructor": false, // keep constructors
}

func collect(src string, re *regexp.Regexp, fn func(name string, off int)) {
	for _, m := range re.FindAllSubmatchIndex([]byte(src), -1) {
		if len(m) >= 4 {
			fn(src[m[2]:m[3]], m[0])
		}
	}
}

// lineIndexer returns a function mapping a byte offset to a 1-based line number.
func lineIndexer(src string) func(off int) int {
	var starts []int
	starts = append(starts, 0)
	for i := 0; i < len(src); i++ {
		if src[i] == '\n' {
			starts = append(starts, i+1)
		}
	}
	return func(off int) int {
		lo, hi := 0, len(starts)-1
		for lo < hi {
			mid := (lo + hi + 1) / 2
			if starts[mid] <= off {
				lo = mid
			} else {
				hi = mid - 1
			}
		}
		return lo + 1
	}
}

func baseName(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

func isTest(p string) bool {
	return strings.Contains(p, "_test.") || strings.Contains(p, ".test.") ||
		strings.Contains(p, ".spec.") || strings.Contains(p, "/__tests__/")
}
