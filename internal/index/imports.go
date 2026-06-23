package index

import (
	"os"
	"path"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/Lordymine/codegraph/internal/graph"
)

// imports.go is the IMPORTS-edge pass for TS/JS: importer File -> imported File.
//
// Scope (MVP): relative specifiers only (`./x`, `../x`), resolved against the set
// of indexed files. Package imports (`@nestjs/common`, `react`) have no File node
// and are skipped. Go imports target packages, not files, which needs a
// package-node model — deferred. Unresolved edges would be dropped by the store
// anyway; we resolve here in-memory so we never emit a wrong edge.

// fileSrc is a file's bytes + metadata for the testable resolver (no disk).
type fileSrc struct {
	RelPath string
	Lang    Lang
	Data    []byte
}

// ResolveImports reads the files and emits IMPORTS edges. It is the disk-reading
// wrapper around resolveImports. Prefer resolveImportsStreaming during indexing —
// it holds one file at a time instead of the whole codebase.
func ResolveImports(project string, files []SourceFile) []graph.Edge {
	srcs := make([]fileSrc, 0, len(files))
	for _, f := range files {
		data, err := os.ReadFile(f.AbsPath)
		if err != nil {
			continue
		}
		srcs = append(srcs, fileSrc{RelPath: f.RelPath, Lang: f.Lang, Data: data})
	}
	return resolveImports(project, srcs)
}

// collectImportsStreaming resolves IMPORTS one file at a time. Source bytes are not
// retained for the whole repo; only the small edge list grows until a single insert.
func collectImportsStreaming(project string, files []SourceFile) ([]graph.Edge, error) {
	exists := make(map[string]bool, len(files))
	for _, f := range files {
		exists[f.RelPath] = true
	}
	var edges []graph.Edge
	for _, f := range files {
		if langFor(f.Lang) == nil || f.Lang == LangGo {
			continue
		}
		data, err := os.ReadFile(f.AbsPath)
		if err != nil {
			continue
		}
		edges = append(edges, importEdgesForFile(project, f.RelPath, f.Lang, data, exists)...)
		data = nil
	}
	return edges, nil
}

func importEdgesForFile(project, relPath string, lang Lang, data []byte, exists map[string]bool) []graph.Edge {
	fileQN := project + ":" + relPath
	var edges []graph.Edge
	for _, spec := range extractImportSpecifiers(langFor(lang), data) {
		target, ok := resolveTSImport(relPath, spec, exists)
		if !ok || target == relPath {
			continue
		}
		edges = append(edges, graph.Edge{
			Project: project, SourceQN: fileQN,
			TargetQN: project + ":" + target, Type: graph.EdgeImports,
			Props: map[string]any{"specifier": spec},
		})
	}
	return edges
}

// resolveImports is the testable core: it resolves each TS/JS file's relative
// imports against the known file set and returns IMPORTS edges.
func resolveImports(project string, files []fileSrc) []graph.Edge {
	exists := make(map[string]bool, len(files))
	for _, f := range files {
		exists[f.RelPath] = true
	}

	var edges []graph.Edge
	for _, f := range files {
		grammar := langFor(f.Lang)
		if grammar == nil || f.Lang == LangGo {
			continue // TS/JS only for now
		}
		fileQN := project + ":" + f.RelPath
		for _, spec := range extractImportSpecifiers(grammar, f.Data) {
			target, ok := resolveTSImport(f.RelPath, spec, exists)
			if !ok || target == f.RelPath {
				continue
			}
			edges = append(edges, graph.Edge{
				Project: project, SourceQN: fileQN,
				TargetQN: project + ":" + target, Type: graph.EdgeImports,
				Props: map[string]any{"specifier": spec},
			})
		}
	}
	return edges
}

// extractImportSpecifiers returns the raw `from` strings of import/export
// statements (e.g. "./bar", "@nestjs/common"), quotes stripped.
func extractImportSpecifiers(grammar *tree_sitter.Language, data []byte) []string {
	parser := tree_sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(grammar); err != nil {
		return nil
	}
	tree := parser.Parse(data, nil)
	if tree == nil {
		return nil
	}
	defer tree.Close()

	root := tree.RootNode()
	var specs []string
	for i := uint(0); i < root.NamedChildCount(); i++ {
		n := root.NamedChild(i)
		if k := n.Kind(); k != "import_statement" && k != "export_statement" {
			continue
		}
		src := n.ChildByFieldName("source") // nil for `export { x }` with no `from`
		if src == nil {
			continue
		}
		specs = append(specs, unquote(src.Utf8Text(data)))
	}
	return specs
}

// resolveTSImport resolves a relative specifier to an existing repo-relative file
// path, trying common extensions then a directory index file. Returns
// ("", false) for non-relative (package) specifiers or when nothing matches.
func resolveTSImport(importerRel, specifier string, exists map[string]bool) (string, bool) {
	if !strings.HasPrefix(specifier, ".") {
		return "", false
	}
	base := path.Join(path.Dir(importerRel), specifier)
	exts := []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"}

	if exists[base] { // specifier already carried an extension
		return base, true
	}
	for _, e := range exts {
		if exists[base+e] {
			return base + e, true
		}
	}
	for _, e := range exts {
		if cand := base + "/index" + e; exists[cand] {
			return cand, true
		}
	}
	return "", false
}

func unquote(s string) string {
	if len(s) >= 2 {
		q := s[0]
		if (q == '\'' || q == '"' || q == '`') && s[len(s)-1] == q {
			return s[1 : len(s)-1]
		}
	}
	return s
}
