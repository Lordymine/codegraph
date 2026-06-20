package index

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/Lordymine/codegraph/internal/graph"
)

// routes.go derives HTTP Route nodes from NestJS decorators (@Controller + @Get/
// @Post/...). The decorator capture in treesitter.go records (name, arg) pairs; here
// we join the controller base path with each handler's path into a Route node, placed
// at the handler method so snippet/search land on the handling code.

// decor is a captured decorator: its bare name and first string-literal argument
// (the path for @Controller('users') / @Get(':id'); "" when there's no string arg).
type decor struct {
	name string
	arg  string
}

// httpVerbs maps NestJS method decorators to their HTTP verb.
var httpVerbs = map[string]string{
	"Get": "GET", "Post": "POST", "Put": "PUT", "Delete": "DELETE",
	"Patch": "PATCH", "Options": "OPTIONS", "Head": "HEAD", "All": "ALL",
}

// emitRoutes emits one Route node per HTTP-verb decorator on a handler method, but
// only inside a class that is itself a @Controller. The route's location is the
// handler method's, and add() wires the file→route DEFINES edge.
func emitRoutes(pending []decor, isController bool, base, methodQN string, m *tree_sitter.Node, add addFn) {
	if !isController {
		return
	}
	for _, d := range pending {
		verb, ok := httpVerbs[d.name]
		if !ok {
			continue
		}
		path := joinRoute(base, d.arg)
		add(graph.LabelRoute, verb+" "+path, methodQN+"#"+verb,
			m.StartPosition().Row, m.EndPosition().Row,
			map[string]any{"method": verb, "path": path, "handler": methodQN})
	}
}

// joinRoute joins a controller base path and a handler sub-path into a normalized
// "/a/b" route, dropping empty segments and stray slashes.
func joinRoute(base, sub string) string {
	var parts []string
	for _, p := range []string{base, sub} {
		if p = strings.Trim(p, "/"); p != "" {
			parts = append(parts, p)
		}
	}
	return "/" + strings.Join(parts, "/")
}

// decorNames extracts the bare decorator names (the method node's `decorators` prop
// keeps the existing shape — names only).
func decorNames(ds []decor) []string {
	out := make([]string, 0, len(ds))
	for _, d := range ds {
		out = append(out, d.name)
	}
	return out
}

// controllerArg returns (isController, basePath) for a class's decorators.
func controllerArg(ds []decor) (bool, string) {
	for _, d := range ds {
		if d.name == "Controller" {
			return true, d.arg
		}
	}
	return false, ""
}

// decoratorArg returns a decorator's first string-literal argument, or "".
// (@Controller('users') -> "users", @Get(':id') -> ":id", @Get() -> "").
func decoratorArg(d *tree_sitter.Node, src []byte) string {
	var call *tree_sitter.Node
	for i := uint(0); i < d.NamedChildCount(); i++ {
		if c := d.NamedChild(i); c.Kind() == "call_expression" {
			call = c
			break
		}
	}
	if call == nil {
		return ""
	}
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return ""
	}
	for i := uint(0); i < args.NamedChildCount(); i++ {
		if a := args.NamedChild(i); a.Kind() == "string" {
			return stringFragment(a, src)
		}
	}
	return ""
}

// stringFragment returns the content of a tree-sitter `string` node (without quotes);
// an empty literal ('') has no fragment child, so it returns "".
func stringFragment(n *tree_sitter.Node, src []byte) string {
	for i := uint(0); i < n.NamedChildCount(); i++ {
		if c := n.NamedChild(i); c.Kind() == "string_fragment" {
			return c.Utf8Text(src)
		}
	}
	return ""
}
