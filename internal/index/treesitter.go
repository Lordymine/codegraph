package index

import (
	"strings"
	"unicode"
	"unicode/utf8"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
	tree_sitter_ts "github.com/tree-sitter/tree-sitter-typescript/bindings/go"

	"github.com/Lordymine/codegraph/internal/graph"
)

// Language objects are immutable and safe to share across parsers/goroutines, so
// we build them once. Parsers are NOT goroutine-safe and are created per call
// (the pipeline already runs one goroutine per file); a parser pool is a later
// optimization if profiling shows churn.
var (
	goLang  = tree_sitter.NewLanguage(tree_sitter_go.Language())
	tsLang  = tree_sitter.NewLanguage(tree_sitter_ts.LanguageTypescript())
	tsxLang = tree_sitter.NewLanguage(tree_sitter_ts.LanguageTSX())
)

// langFor maps a detected language to its tree-sitter grammar. JS/JSX use the TSX
// grammar (a syntactic superset that parses both) until a dedicated JS grammar is
// worth adding.
func langFor(lang Lang) *tree_sitter.Language {
	switch lang {
	case LangGo:
		return goLang
	case LangTS:
		return tsLang
	case LangTSX, LangJS:
		return tsxLang
	default:
		return nil
	}
}

// addFn appends one definition node (+ its DEFINES edge) to the result. qnSuffix
// is appended to the file's qualified name; it must be unique within the file so
// homonyms (e.g. same-named methods on different receivers) get distinct QNs.
type addFn func(label graph.NodeLabel, name, qnSuffix string, startRow, endRow uint, extra map[string]any)

// walkGoDefs emits top-level Go definitions: functions, methods (qualified by
// receiver type so homonyms disambiguate), and type declarations.
func walkGoDefs(root *tree_sitter.Node, src []byte, add addFn) {
	for i := uint(0); i < root.NamedChildCount(); i++ {
		n := root.NamedChild(i)
		switch n.Kind() {
		case "function_declaration":
			name := n.ChildByFieldName("name")
			if name == nil {
				continue
			}
			nm := name.Utf8Text(src)
			add(graph.LabelFunction, nm, nm, n.StartPosition().Row, n.EndPosition().Row,
				map[string]any{"is_exported": goExported(nm)})

		case "method_declaration":
			name := n.ChildByFieldName("name")
			if name == nil {
				continue
			}
			nm := name.Utf8Text(src)
			recv := goReceiver(n, src)
			qn := nm
			if recv != "" {
				qn = recv + "." + nm // Store.Close vs Other.Close — distinct QNs
			}
			add(graph.LabelMethod, nm, qn, n.StartPosition().Row, n.EndPosition().Row,
				map[string]any{"is_exported": goExported(nm), "receiver": recv})

		case "type_declaration":
			for j := uint(0); j < n.NamedChildCount(); j++ {
				ts := n.NamedChild(j)
				if ts.Kind() != "type_spec" {
					continue
				}
				name := ts.ChildByFieldName("name")
				if name == nil {
					continue
				}
				nm := name.Utf8Text(src)
				add(graph.LabelClass, nm, nm, n.StartPosition().Row, n.EndPosition().Row,
					map[string]any{"is_exported": goExported(nm)})
			}
		}
	}
}

// walkTSDefs emits top-level TS/JS definitions. An `export_statement` wraps the
// real declaration and carries the decorator as a sibling field, so we unwrap it
// once, record `is_exported`/decorators, then dispatch on the inner declaration.
func walkTSDefs(root *tree_sitter.Node, src []byte, add addFn) {
	for i := uint(0); i < root.NamedChildCount(); i++ {
		n := root.NamedChild(i)
		exported := false
		var decorators []string
		decl := n

		if n.Kind() == "export_statement" {
			exported = true
			if d := n.ChildByFieldName("decorator"); d != nil {
				decorators = append(decorators, decoratorName(d, src))
			}
			if inner := n.ChildByFieldName("declaration"); inner != nil {
				decl = inner
			}
		}

		switch decl.Kind() {
		case "function_declaration", "generator_function_declaration":
			name := decl.ChildByFieldName("name")
			if name == nil {
				continue
			}
			nm := name.Utf8Text(src)
			add(graph.LabelFunction, nm, nm, decl.StartPosition().Row, decl.EndPosition().Row,
				tsExtra(exported, nil))

		case "class_declaration", "abstract_class_declaration":
			name := decl.ChildByFieldName("name")
			if name == nil {
				continue
			}
			nm := name.Utf8Text(src)
			add(graph.LabelClass, nm, nm, decl.StartPosition().Row, decl.EndPosition().Row,
				tsExtra(exported, decorators))
			walkTSClassMethods(decl, nm, src, add)

		case "lexical_declaration", "variable_declaration":
			for j := uint(0); j < decl.NamedChildCount(); j++ {
				vd := decl.NamedChild(j)
				if vd.Kind() != "variable_declarator" {
					continue
				}
				val := vd.ChildByFieldName("value")
				if val == nil {
					continue
				}
				if k := val.Kind(); k != "arrow_function" && k != "function_expression" {
					continue
				}
				name := vd.ChildByFieldName("name")
				if name == nil {
					continue
				}
				nm := name.Utf8Text(src)
				add(graph.LabelFunction, nm, nm, decl.StartPosition().Row, decl.EndPosition().Row,
					tsExtra(exported, nil))
			}
		}
	}
}

// walkTSClassMethods emits Method nodes for a class body, qualified by the class
// name so methods of different classes don't collide.
func walkTSClassMethods(class *tree_sitter.Node, className string, src []byte, add addFn) {
	body := class.ChildByFieldName("body")
	if body == nil {
		return
	}
	for j := uint(0); j < body.NamedChildCount(); j++ {
		m := body.NamedChild(j)
		if m.Kind() != "method_definition" {
			continue
		}
		name := m.ChildByFieldName("name")
		if name == nil {
			continue
		}
		mn := name.Utf8Text(src)
		add(graph.LabelMethod, mn, className+"."+mn, m.StartPosition().Row, m.EndPosition().Row, nil)
	}
}

// goReceiver returns the receiver type name of a Go method ("Store" for both
// `(s Store)` and `(s *Store)`), or "" if none.
func goReceiver(method *tree_sitter.Node, src []byte) string {
	rl := method.ChildByFieldName("receiver")
	if rl == nil {
		return ""
	}
	for j := uint(0); j < rl.NamedChildCount(); j++ {
		pd := rl.NamedChild(j)
		if pd.Kind() != "parameter_declaration" {
			continue
		}
		t := pd.ChildByFieldName("type")
		if t == nil {
			continue
		}
		if t.Kind() == "pointer_type" { // unwrap *T
			t = t.NamedChild(0)
		}
		if t != nil {
			return t.Utf8Text(src)
		}
	}
	return ""
}

// goExported reports whether a Go identifier is exported (starts uppercase).
func goExported(name string) bool {
	if name == "" {
		return false
	}
	r, _ := utf8.DecodeRuneInString(name)
	return unicode.IsUpper(r)
}

// decoratorName extracts the decorator's bare name: "@Injectable()" -> "Injectable",
// "@Controller('users')" -> "Controller".
func decoratorName(d *tree_sitter.Node, src []byte) string {
	t := strings.TrimPrefix(d.Utf8Text(src), "@")
	for i, r := range t {
		if r == '(' || r == '.' || r == '<' || r == ' ' || r == '\t' || r == '\n' {
			return t[:i]
		}
	}
	return t
}

// tsExtra builds the per-node properties for TS/JS symbols.
func tsExtra(exported bool, decorators []string) map[string]any {
	m := map[string]any{"is_exported": exported}
	if len(decorators) > 0 {
		m["decorators"] = decorators
	}
	return m
}
