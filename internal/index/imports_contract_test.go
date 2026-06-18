package index

import (
	"testing"

	"github.com/Lordymine/codegraph/internal/graph"
)

// Contract tests for the IMPORTS pass. They pin the resolution behavior: relative
// specifiers resolve to the right repo file (extensions, parent dirs, index
// files, re-exports); package and unresolved imports produce no edge (honest
// precision).

func hasImport(edges []graph.Edge, src, tgt string) bool {
	for _, e := range edges {
		if e.Type == graph.EdgeImports && e.SourceQN == src && e.TargetQN == tgt {
			return true
		}
	}
	return false
}

func TestImports_TS_RelativeSameDir(t *testing.T) {
	edges := resolveImports("p", []fileSrc{
		{RelPath: "src/foo.ts", Lang: LangTS, Data: []byte("import { B } from './bar'\n")},
		{RelPath: "src/bar.ts", Lang: LangTS, Data: []byte("export class B {}\n")},
	})
	if !hasImport(edges, "p:src/foo.ts", "p:src/bar.ts") {
		t.Fatalf("missing IMPORTS foo.ts->bar.ts; got %+v", edges)
	}
}

func TestImports_TS_ParentDir(t *testing.T) {
	edges := resolveImports("p", []fileSrc{
		{RelPath: "src/a/b.ts", Lang: LangTS, Data: []byte("import { U } from '../lib/util'\n")},
		{RelPath: "src/lib/util.ts", Lang: LangTS, Data: []byte("export const U = 1\n")},
	})
	if !hasImport(edges, "p:src/a/b.ts", "p:src/lib/util.ts") {
		t.Fatalf("missing IMPORTS b.ts->util.ts; got %+v", edges)
	}
}

func TestImports_TS_IndexFile(t *testing.T) {
	edges := resolveImports("p", []fileSrc{
		{RelPath: "src/foo.ts", Lang: LangTS, Data: []byte("import { B } from './bar'\n")},
		{RelPath: "src/bar/index.ts", Lang: LangTS, Data: []byte("export class B {}\n")},
	})
	if !hasImport(edges, "p:src/foo.ts", "p:src/bar/index.ts") {
		t.Fatalf("missing IMPORTS foo.ts->bar/index.ts; got %+v", edges)
	}
}

func TestImports_TS_TsxExtension(t *testing.T) {
	edges := resolveImports("p", []fileSrc{
		{RelPath: "src/app.ts", Lang: LangTS, Data: []byte("import { C } from './Comp'\n")},
		{RelPath: "src/Comp.tsx", Lang: LangTSX, Data: []byte("export const C = 1\n")},
	})
	if !hasImport(edges, "p:src/app.ts", "p:src/Comp.tsx") {
		t.Fatalf("missing IMPORTS app.ts->Comp.tsx; got %+v", edges)
	}
}

func TestImports_TS_ReExportResolved(t *testing.T) {
	edges := resolveImports("p", []fileSrc{
		{RelPath: "src/index.ts", Lang: LangTS, Data: []byte("export { B } from './bar'\n")},
		{RelPath: "src/bar.ts", Lang: LangTS, Data: []byte("export class B {}\n")},
	})
	if !hasImport(edges, "p:src/index.ts", "p:src/bar.ts") {
		t.Fatalf("missing re-export IMPORTS index.ts->bar.ts; got %+v", edges)
	}
}

func TestImports_TS_ExternalPackageDropped(t *testing.T) {
	edges := resolveImports("p", []fileSrc{
		{RelPath: "src/foo.ts", Lang: LangTS, Data: []byte("import { Injectable } from '@nestjs/common'\n")},
	})
	if len(edges) != 0 {
		t.Fatalf("external import must not produce an edge; got %+v", edges)
	}
}

func TestImports_TS_UnresolvedRelativeDropped(t *testing.T) {
	edges := resolveImports("p", []fileSrc{
		{RelPath: "src/foo.ts", Lang: LangTS, Data: []byte("import { B } from './missing'\n")},
	})
	if len(edges) != 0 {
		t.Fatalf("unresolved relative import must drop; got %+v", edges)
	}
}
