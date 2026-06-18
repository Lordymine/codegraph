package query

import (
	"strings"
	"testing"
)

func TestCompactRefsFormat(t *testing.T) {
	refs := []Ref{
		{Name: "bar", QualifiedName: "proj:src/a.ts.Foo.bar", Label: "Method", File: "src/a.ts", StartLine: 10, EndLine: 20},
		{Name: "baz", QualifiedName: "proj:src/b.ts.baz", Label: "Function", File: "src/b.ts", StartLine: 3, EndLine: 5},
	}
	out := CompactRefs(refs)
	want := "Method\tbar\tsrc/a.ts:10\tsrc/a.ts.Foo.bar\n" +
		"Function\tbaz\tsrc/b.ts:3\tsrc/b.ts.baz\n"
	if out != want {
		t.Fatalf("compact format mismatch:\n got %q\nwant %q", out, want)
	}
	if strings.Contains(out, "proj:") {
		t.Fatal("project prefix leaked into the wire format")
	}
}

func TestStripAndNormalizeRoundTrip(t *testing.T) {
	e := &Engine{project: "proj"}
	full := "proj:src/a.ts.Foo.bar"

	short := StripProjectPrefix(full)
	if short != "src/a.ts.Foo.bar" {
		t.Fatalf("strip = %q", short)
	}
	// A returned (stripped) qn must round-trip back to the stored full qn.
	if got := e.normalizeQN(short); got != full {
		t.Fatalf("normalize(short) = %q, want %q", got, full)
	}
	// A full qn passed back in is left untouched (idempotent).
	if got := e.normalizeQN(full); got != full {
		t.Fatalf("normalize(full) = %q, want %q", got, full)
	}
}

func TestStripProjectPrefixNoColon(t *testing.T) {
	if got := StripProjectPrefix("noprefix"); got != "noprefix" {
		t.Fatalf("got %q", got)
	}
}
