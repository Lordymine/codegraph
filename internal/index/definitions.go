package index

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/Lordymine/codegraph/internal/graph"
)

// definitions.go is the definitions pass: it parses a file with tree-sitter and
// emits its File node plus one node (+ DEFINES edge) per top-level symbol. The
// language-specific AST walks live in treesitter.go. This replaced the original
// regex MVP — tree-sitter gives true node boundaries (real end lines), receiver/
// owner-qualified names for homonym disambiguation, export flags, and decorators.

// ExtractDefinitions reads a file from disk and extracts its definitions.
func ExtractDefinitions(project string, f SourceFile) ([]graph.Node, []graph.Edge) {
	data, err := os.ReadFile(f.AbsPath)
	if err != nil {
		return nil, nil
	}
	return extractDefsFromSource(project, f.RelPath, f.Lang, data)
}

// extractDefsFromSource is the testable core: it takes the source bytes directly
// so contract tests can exercise the extractor without touching disk. It always
// returns at least the File node; if the language is unsupported or parsing
// fails, it returns just that (never a wrong symbol).
func extractDefsFromSource(project, relPath string, lang Lang, data []byte) ([]graph.Node, []graph.Edge) {
	fileQN := project + ":" + relPath
	fileNode := graph.Node{
		Project: project, Label: graph.LabelFile,
		Name: baseName(relPath), QualifiedName: fileQN,
		FilePath: relPath, StartLine: 1, EndLine: strings.Count(string(data), "\n") + 1,
		Props: map[string]any{"lang": string(lang), "sha256": hashBytes(data)},
	}
	nodes := []graph.Node{fileNode}
	var edges []graph.Edge

	grammar := langFor(lang)
	if grammar == nil {
		return nodes, edges
	}

	parser := tree_sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(grammar); err != nil {
		return nodes, edges
	}
	tree := parser.Parse(data, nil)
	if tree == nil {
		return nodes, edges
	}
	defer tree.Close()
	root := tree.RootNode()

	// add appends a symbol node and its DEFINES edge. Rows are tree-sitter's
	// 0-based Point.Row; we store 1-based lines.
	add := func(label graph.NodeLabel, name, qnSuffix string, startRow, endRow uint, extra map[string]any) {
		qn := fileQN + "." + qnSuffix
		props := map[string]any{"lang": string(lang), "is_test": IsTestFile(relPath)}
		for k, v := range extra {
			props[k] = v
		}
		nodes = append(nodes, graph.Node{
			Project: project, Label: label, Name: name, QualifiedName: qn,
			FilePath: relPath, StartLine: int(startRow) + 1, EndLine: int(endRow) + 1,
			Props: props,
		})
		edges = append(edges, graph.Edge{Project: project, SourceQN: fileQN, TargetQN: qn, Type: graph.EdgeDefines})
	}

	if lang == LangGo {
		walkGoDefs(root, data, add)
	} else {
		walkTSDefs(root, data, add)
	}
	return nodes, edges
}

// hashBytes is the per-file content hash used for incremental change detection.
func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func baseName(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

// IsTestFile reports whether a repo-relative path is a test file, by the naming
// conventions of the supported stacks (Go `_test.go`, JS/TS `.test.`/`.spec.`,
// and `__tests__` dirs). Exported so the query layer can exclude test functions
// from the dead-code hint — they're invoked by the test runner, not by code.
func IsTestFile(p string) bool {
	return strings.Contains(p, "_test.") || strings.Contains(p, ".test.") ||
		strings.Contains(p, ".spec.") || strings.Contains(p, "/__tests__/")
}
