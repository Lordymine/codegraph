// Package generics exercises the call graph on generic code. Before the
// RuntimeTypes-free fix (cha.go), x/tools panicked here ("ForEachElement called on
// type containing *types.TypeParam") and gocalls dropped ALL Go CALLS for the repo.
package generics

type Box[T any] struct{ v T }

func (b Box[T]) Get() T { return b.v }

func Map[T, U any](s []T, f func(T) U) []U {
	out := make([]U, 0, len(s))
	for _, x := range s {
		out = append(out, f(x))
	}
	return out
}

func helper() int { return 1 }

func use() int {
	b := Box[int]{v: 7}
	xs := Map([]int{1, 2}, func(i int) int { return i })
	return b.Get() + len(xs) + helper() // helper() is an ordinary (non-generic) call
}
