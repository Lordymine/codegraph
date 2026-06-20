// Package recursion is a test fixture for self-call edges: a function that calls
// itself is a real caller of itself (an IDE find-callers and the eval oracle both
// count it), so the resolver must emit the self-edge, not drop it.
package recursion

func fact(n int) int {
	if n <= 1 {
		return 1
	}
	return n * fact(n-1)
}
