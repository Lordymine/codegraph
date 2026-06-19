// Package withtest is a fixture proving test files contribute CALLS edges: lib_test.go
// calls Target, so the graph must hold a TestTarget->Target edge (packages Tests:true).
package withtest

func Target() int { return 42 }
