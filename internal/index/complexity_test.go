package index

import "testing"

// TestComplexity_Cyclomatic pins the cyclomatic complexity stored in node props:
// 1 + one per branch point (if / for / while / case / catch / ternary / &&|| / ??).
// Checked across every emit path — Go function & method, TS function, arrow-const,
// and class method — so a missed call site shows up.
func TestComplexity_Cyclomatic(t *testing.T) {
	goSrc := `package p

func simple() int { return 1 }

func branchy(n int) int {
	if n > 0 && n < 10 {
		for i := 0; i < n; i++ {
			_ = i
		}
	}
	return n
}

type T struct{}

func (t T) method(x int) int {
	switch x {
	case 1:
		return 1
	case 2:
		return 2
	}
	return 0
}`

	tsSrc := `export function simpleTs(x: number): number { return x; }

export const arrowed = (x: number) => (x > 0 ? 1 : 0);

export class C {
	method(x: number): number {
		if (x > 0 || x < -5) {
			for (const i of [1, 2]) { x += i; }
		}
		return x;
	}
}`

	cases := []struct {
		name string
		lang Lang
		src  string
		want map[string]int
	}{
		{"go", LangGo, goSrc, map[string]int{
			"simple":  1, // no branches
			"branchy": 4, // if + && + for
			"method":  3, // switch with two cases
		}},
		{"ts", LangTS, tsSrc, map[string]int{
			"simpleTs": 1, // no branches
			"arrowed":  2, // ternary
			"method":   4, // if + || + for-of
		}},
	}

	for _, tc := range cases {
		nodes, _ := extractDefsFromSource("proj", "f."+tc.name, tc.lang, []byte(tc.src))
		got := map[string]int{}
		for _, n := range nodes {
			if c, ok := n.Props["complexity"].(int); ok {
				got[n.Name] = c
			}
		}
		for name, want := range tc.want {
			if got[name] != want {
				t.Errorf("%s: complexity(%s) = %d, want %d", tc.name, name, got[name], want)
			}
		}
	}
}
