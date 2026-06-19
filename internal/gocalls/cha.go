package gocalls

// RuntimeTypes-free reimplementations of three golang.org/x/tools helpers.
//
// Why these exist: ssautil.AllFunctions, cha.CallGraph and (transitively)
// prog.RuntimeTypes() panic on some generic instantiations — x/tools v0.46:
// "ForEachElement called on type containing *types.TypeParam" — which silently
// zeroed Go CALLS for generic-heavy repos (e.g. cli/cli). These copies do exactly
// what the originals do MINUS the prog.RuntimeTypes() call, so the call graph builds
// on generics too. Adapted from golang.org/x/tools (BSD-3-Clause, The Go Authors):
//   - safeAllFunctions  <- go/ssa/ssautil.AllFunctions (sans the RuntimeTypes loop)
//   - chaCallGraph       <- go/callgraph/cha.CallGraph (body, over a given func set)
//   - lazyCallees        <- go/callgraph/internal/chautil.LazyCallees (verbatim; the
//                           package is internal so it cannot be imported)

import (
	"go/types"

	"golang.org/x/tools/go/callgraph"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/types/typeutil"
)

// safeAllFunctions returns the program's functions WITHOUT calling
// prog.RuntimeTypes(). It mirrors ssautil.AllFunctions (package-member functions +
// method sets of named types + every function reachable through their operands) but
// drops the trailing RuntimeTypes() loop — the one unguarded call that panics on
// generic instantiations. We lose only methods reachable purely via reflection,
// which have no static call site and so never become a repo-internal CALLS edge.
func safeAllFunctions(prog *ssa.Program) map[*ssa.Function]bool {
	seen := map[*ssa.Function]bool{}
	var visit func(fn *ssa.Function)
	visit = func(fn *ssa.Function) {
		if fn == nil || seen[fn] {
			return
		}
		seen[fn] = true
		var buf [10]*ssa.Value
		for _, b := range fn.Blocks {
			for _, instr := range b.Instrs {
				for _, op := range instr.Operands(buf[:0]) {
					if f, ok := (*op).(*ssa.Function); ok {
						visit(f)
					}
				}
			}
		}
	}
	methodsOf := func(t types.Type) {
		if types.IsInterface(t) {
			return
		}
		mset := prog.MethodSets.MethodSet(t)
		for sel := range mset.Methods() {
			if sel.Obj().(*types.Func).Signature().TypeParams() == nil {
				visit(prog.MethodValue(sel))
			}
		}
	}
	for _, pkg := range prog.AllPackages() {
		for _, mem := range pkg.Members {
			switch m := mem.(type) {
			case *ssa.Function:
				visit(m)
			case *ssa.Type:
				if named, ok := m.Type().(*types.Named); ok && named.TypeParams() == nil {
					methodsOf(named)
					methodsOf(types.NewPointer(named))
				}
			}
		}
	}
	return seen
}

// chaCallGraph is cha.CallGraph's body over a caller-supplied function set instead of
// ssautil.AllFunctions(prog) — so it never touches prog.RuntimeTypes(). It produces
// the CHA over-approximation VTA then refines.
func chaCallGraph(fns map[*ssa.Function]bool) *callgraph.Graph {
	cg := callgraph.New(nil)
	calleesOf := lazyCallees(fns)
	addEdge := func(fnode *callgraph.Node, site ssa.CallInstruction, g *ssa.Function) {
		callgraph.AddEdge(fnode, site, cg.CreateNode(g))
	}
	for f := range fns {
		fnode := cg.CreateNode(f)
		for _, b := range f.Blocks {
			for _, instr := range b.Instrs {
				site, ok := instr.(ssa.CallInstruction)
				if !ok {
					continue
				}
				if g := site.Common().StaticCallee(); g != nil {
					addEdge(fnode, site, g)
				} else {
					for _, g := range calleesOf(site) {
						addEdge(fnode, site, g)
					}
				}
			}
		}
	}
	return cg
}

// lazyCallees is a verbatim copy of chautil.LazyCallees (internal, can't import). It
// maps a dynamic call site to its CHA callees within fns (the entire implements
// relation between interfaces and concrete types).
func lazyCallees(fns map[*ssa.Function]bool) func(site ssa.CallInstruction) []*ssa.Function {
	var funcsBySig typeutil.Map // value is []*ssa.Function
	methodsByID := make(map[string][]*ssa.Function)
	type imethod struct {
		I  *types.Interface
		id string
	}
	methodsMemo := make(map[imethod][]*ssa.Function)
	lookupMethods := func(I *types.Interface, m *types.Func) []*ssa.Function {
		id := m.Id()
		methods, ok := methodsMemo[imethod{I, id}]
		if !ok {
			for _, f := range methodsByID[id] {
				C := f.Signature.Recv().Type() // named or *named
				if types.Implements(C, I) {
					methods = append(methods, f)
				}
			}
			methodsMemo[imethod{I, id}] = methods
		}
		return methods
	}
	for f := range fns {
		if f.Signature.Recv() == nil {
			if f.Name() == "init" && f.Synthetic == "package initializer" {
				continue
			}
			funcs, _ := funcsBySig.At(f.Signature).([]*ssa.Function)
			funcs = append(funcs, f)
			funcsBySig.Set(f.Signature, funcs)
		} else if obj := f.Object(); obj != nil {
			id := obj.(*types.Func).Id()
			methodsByID[id] = append(methodsByID[id], f)
		}
	}
	return func(site ssa.CallInstruction) []*ssa.Function {
		call := site.Common()
		if call.IsInvoke() {
			tiface := call.Value.Type().Underlying().(*types.Interface)
			return lookupMethods(tiface, call.Method)
		} else if g := call.StaticCallee(); g != nil {
			return []*ssa.Function{g}
		} else if _, ok := call.Value.(*ssa.Builtin); !ok {
			fns, _ := funcsBySig.At(call.Signature()).([]*ssa.Function)
			return fns
		}
		return nil
	}
}
