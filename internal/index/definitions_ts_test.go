package index

import (
	"testing"

	"github.com/Lordymine/codegraph/internal/graph"
)

// Contract tests for the extended TS surface added in the M1 gap-fill:
// interface, type alias, enum, abstract class, method decorators, function
// expressions, exported variables. Language-level — covers any TS framework.

func TestDefs_TS_Interface(t *testing.T) {
	nodes, _ := extractDefsFromSource("p", "x.ts", LangTS, []byte("export interface Foo { a: number }\n"))
	n, ok := findDef(nodes, "Foo")
	if !ok {
		t.Fatal("Foo not found")
	}
	if n.Label != graph.LabelInterface {
		t.Errorf("label = %s, want Interface", n.Label)
	}
	if n.Props["is_exported"] != true {
		t.Errorf("is_exported = %v, want true", n.Props["is_exported"])
	}
}

func TestDefs_TS_TypeAlias(t *testing.T) {
	nodes, _ := extractDefsFromSource("p", "x.ts", LangTS, []byte("export type Bar = { x: string }\n"))
	n, ok := findDef(nodes, "Bar")
	if !ok {
		t.Fatal("Bar not found")
	}
	if n.Label != graph.LabelType {
		t.Errorf("label = %s, want Type", n.Label)
	}
}

func TestDefs_TS_Enum(t *testing.T) {
	nodes, _ := extractDefsFromSource("p", "x.ts", LangTS, []byte("export enum Color { Red, Green }\n"))
	n, ok := findDef(nodes, "Color")
	if !ok {
		t.Fatal("Color not found")
	}
	if n.Label != graph.LabelEnum {
		t.Errorf("label = %s, want Enum", n.Label)
	}
}

func TestDefs_TS_AbstractClassAndMethod(t *testing.T) {
	src := "export abstract class Base {\n  abstract go(): void\n}\n"
	nodes, _ := extractDefsFromSource("p", "x.ts", LangTS, []byte(src))
	b, ok := findDef(nodes, "Base")
	if !ok {
		t.Fatal("Base not found")
	}
	if b.Label != graph.LabelClass {
		t.Errorf("label = %s, want Class", b.Label)
	}
	if _, ok := findDef(nodes, "go"); !ok {
		t.Error("abstract method go not found")
	}
}

// The crux for NestJS: route decorators live on methods, as sibling nodes
// preceding the method in the class body.
func TestDefs_TS_MethodDecorators(t *testing.T) {
	src := "export class C {\n  @Get('/list')\n  list() { return 1 }\n  @Post()\n  create() {}\n}\n"
	nodes, _ := extractDefsFromSource("p", "x.ts", LangTS, []byte(src))

	list, ok := findDef(nodes, "list")
	if !ok {
		t.Fatal("list not found")
	}
	if decs, _ := list.Props["decorators"].([]string); len(decs) == 0 || decs[0] != "Get" {
		t.Errorf("list decorators = %v, want [Get]", list.Props["decorators"])
	}
	create, _ := findDef(nodes, "create")
	if decs, _ := create.Props["decorators"].([]string); len(decs) == 0 || decs[0] != "Post" {
		t.Errorf("create decorators = %v, want [Post]", create.Props["decorators"])
	}
}

func TestDefs_TS_FunctionExpression(t *testing.T) {
	nodes, _ := extractDefsFromSource("p", "x.ts", LangTS, []byte("export const fn = function() { return 1 }\n"))
	n, ok := findDef(nodes, "fn")
	if !ok {
		t.Fatal("fn not found")
	}
	if n.Label != graph.LabelFunction {
		t.Errorf("label = %s, want Function", n.Label)
	}
}

func TestDefs_TS_ExportedVariable(t *testing.T) {
	nodes, _ := extractDefsFromSource("p", "x.ts", LangTS, []byte("export const CONFIG = { port: 3000 }\n"))
	n, ok := findDef(nodes, "CONFIG")
	if !ok {
		t.Fatal("CONFIG not found")
	}
	if n.Label != graph.LabelVariable {
		t.Errorf("label = %s, want Variable", n.Label)
	}
	if n.Props["is_exported"] != true {
		t.Errorf("is_exported = %v, want true", n.Props["is_exported"])
	}
}
