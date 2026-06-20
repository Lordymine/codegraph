package index

import (
	"testing"

	"github.com/Lordymine/codegraph/internal/graph"
)

// TestRoutes_NestJS pins HTTP Route nodes derived from NestJS decorators: the
// controller's @Controller base path joins each handler's @Get/@Post/... path into
// a Route node named "<VERB> <path>", located at the handler method.
func TestRoutes_NestJS(t *testing.T) {
	src := `import { Controller, Get, Post } from '@nestjs/common';

@Controller('users')
export class UsersController {
  @Get()
  findAll() { return []; }

  @Get(':id')
  findOne() { return {}; }

  @Post('login')
  login() { return {}; }
}`

	nodes, edges := extractDefsFromSource("proj", "users.controller.ts", LangTS, []byte(src))

	routes := map[string]graph.Node{}
	for _, n := range nodes {
		if n.Label == graph.LabelRoute {
			routes[n.Name] = n
		}
	}
	for _, want := range []string{"GET /users", "GET /users/:id", "POST /users/login"} {
		if _, ok := routes[want]; !ok {
			t.Errorf("missing route %q; got routes %v", want, keysOf(routes))
		}
	}
	// A route points at its handler (file:line of the method) so snippet/search land
	// on the handling code.
	if r, ok := routes["GET /users/:id"]; ok {
		if r.FilePath != "users.controller.ts" || r.StartLine == 0 {
			t.Errorf("route location = %s:%d, want the handler method's position", r.FilePath, r.StartLine)
		}
		if r.Props["method"] != "GET" || r.Props["path"] != "/users/:id" {
			t.Errorf("route props = %v, want method=GET path=/users/:id", r.Props)
		}
	}
	// The file DEFINES the route (so it's part of the graph's containment).
	defines := 0
	for _, e := range edges {
		if e.Type == graph.EdgeDefines && e.TargetQN == routes["GET /users"].QualifiedName {
			defines++
		}
	}
	if defines == 0 {
		t.Errorf("expected a DEFINES edge to the route node")
	}
}

func keysOf(m map[string]graph.Node) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
