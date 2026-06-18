// Package gocalls resolves Go CALLS edges in-process via go/packages + a CHA
// call graph — the same machinery gopls uses, no subprocess. CHA is sound on
// libraries (no main needed). Only edges between functions/methods that exist in
// the node set are kept (honest precision); stdlib/dep targets are dropped.
package gocalls

import (
	"go/types"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/callgraph/cha"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"

	"github.com/Lordymine/codegraph/internal/graph"
)

// CallEdges loads the Go packages under root and returns CALLS edges whose caller
// and callee are both known nodes. Best-effort: load errors don't abort (it
// builds the graph from whatever type-checked).
func CallEdges(project, root string, known func(qn string) bool) ([]graph.Edge, error) {
	cfg := &packages.Config{Mode: packages.LoadAllSyntax, Dir: root}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, err
	}
	prog, _ := ssautil.AllPackages(pkgs, ssa.InstantiateGenerics)
	prog.Build()
	cg := cha.CallGraph(prog)

	var edges []graph.Edge
	seen := make(map[string]bool)
	for fn, node := range cg.Nodes {
		callerQN, ok := funcToQN(fn, project, root)
		if !ok || !known(callerQN) {
			continue
		}
		for _, out := range node.Out {
			calleeQN, ok := funcToQN(out.Callee.Func, project, root)
			if !ok || calleeQN == callerQN || !known(calleeQN) {
				continue
			}
			key := callerQN + "\x00" + calleeQN
			if seen[key] {
				continue
			}
			seen[key] = true
			edges = append(edges, graph.Edge{
				Project: project, SourceQN: callerQN, TargetQN: calleeQN,
				Type:  graph.EdgeCalls,
				Props: map[string]any{"resolver": "go/callgraph", "confidence": "high"},
			})
		}
	}
	return edges, nil
}

// funcToQN maps an SSA function to a codegraph qualified name, matching the M1
// Go scheme: "<project>:<relpath>.<RecvType>.<name>" for methods,
// "<project>:<relpath>.<name>" for functions. Returns false for synthetic
// functions, closures, and anything outside the repo (stdlib/deps).
func funcToQN(fn *ssa.Function, project, root string) (string, bool) {
	if fn == nil || fn.Pkg == nil || fn.Synthetic != "" {
		return "", false
	}
	pos := fn.Prog.Fset.Position(fn.Pos())
	if !pos.IsValid() {
		return "", false
	}
	rel, err := filepath.Rel(root, pos.Filename)
	if err != nil {
		return "", false
	}
	rel = filepath.ToSlash(rel)
	if strings.HasPrefix(rel, "../") {
		return "", false // outside the repo
	}
	name := fn.Name()
	if recv := fn.Signature.Recv(); recv != nil {
		rt := recvTypeName(recv.Type())
		if rt == "" {
			return "", false
		}
		return project + ":" + rel + "." + rt + "." + name, true
	}
	return project + ":" + rel + "." + name, true
}

func recvTypeName(t types.Type) string {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	if named, ok := t.(*types.Named); ok {
		return named.Obj().Name()
	}
	return ""
}
