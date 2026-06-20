// Package closures is a test fixture for closure call attribution: a call written
// inside a function literal must be credited to the enclosing named function, not
// lost to the anonymous closure (which is never a graph node). This is the cobra
// pattern — Run: func(){ ... } — that silently zeroed "who calls X" recall.
package closures

// target is the function called from inside a closure.
func target() int { return 42 }

// outer never calls target directly: it installs a closure that does, and returns
// it. The CALLS edge for target must still be attributed to outer (the enclosing
// named function), the way an IDE "find callers" credits the containing function.
func outer() func() int {
	fn := func() int {
		return target()
	}
	return fn
}
