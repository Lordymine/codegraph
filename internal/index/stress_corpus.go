package index

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSyntheticGoModule creates a Go module with nFiles spread across packages to
// stress go/packages + VTA without external dependencies.
func writeSyntheticGoModule(t *testing.T, nFiles int) (dir string, sourceBytes int64) {
	t.Helper()
	dir = t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module stress.test\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stressDir := filepath.Join(dir, "stress")
	if err := os.MkdirAll(stressDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < nFiles; i++ {
		path := filepath.Join(stressDir, fmt.Sprintf("f%d.go", i))
		body := syntheticGoFile(i, nFiles)
		sourceBytes += int64(len(body))
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir, sourceBytes
}

// syntheticGoFile emits one file in package stress (no cross-package imports — avoids
// import cycles while still giving VTA hundreds of mutual calls in one module).
func syntheticGoFile(i, nFiles int) string {
	callee := (i + 1) % nFiles
	var b strings.Builder
	b.WriteString("package stress\n\n")
	fmt.Fprintf(&b, "func Fn%d(x int) int {\n", i)
	fmt.Fprintf(&b, "  if x <= 0 { return %d }\n", i)
	fmt.Fprintf(&b, "  return Fn%d(x-1) + local%d(x)\n", callee, i)
	fmt.Fprintf(&b, "}\n\nfunc local%d(v int) int {\n", i)
	for k := 0; k < 8; k++ {
		fmt.Fprintf(&b, "  v += %d\n", k+i)
	}
	fmt.Fprintf(&b, "  return v\n}\n")
	return b.String()
}

// writeSyntheticTSCorpus creates many .ts files (no tsconfig — defs/imports/similar only;
// scip-typescript is exercised separately when tsconfig exists).
func writeSyntheticTSCorpus(t *testing.T, nFiles int) (dir string, sourceBytes int64) {
	t.Helper()
	dir = t.TempDir()
	for i := 0; i < nFiles; i++ {
		body := syntheticTSFile(i)
		sourceBytes += int64(len(body))
		path := filepath.Join(dir, fmt.Sprintf("src/pkg/f%d.ts", i))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir, sourceBytes
}

func syntheticTSFile(i int) string {
	var b strings.Builder
	b.WriteString("import { helper")
	b.WriteString(fmt.Sprint(i % 7))
	b.WriteString(" } from './h")
	b.WriteString(fmt.Sprint(i % 7))
	b.WriteString("';\n")
	for j := 0; j < 3; j++ {
		fmt.Fprintf(&b, "export function fn%d_%d(x: number, y: string) {\n", i, j)
		for k := 0; k < 40; k++ {
			fmt.Fprintf(&b, "  const v%d = x + %d + y.length + '%s'.codePointAt(0);\n", k, k*3+i+j, strings.Repeat("a", 8+(k%5)))
		}
		fmt.Fprintf(&b, "  return helper%d(v0, v1) + %d;\n", i%7, j)
		b.WriteString("}\n")
	}
	return b.String()
}

func stressEnabled() bool {
	return os.Getenv("CODEGRAPH_STRESS") == "1" || os.Getenv("CODEGRAPH_STRESS") == "true"
}

// stressHeapCeiling returns the max acceptable HeapInuse for a stress run. When host
// RAM is known (Linux/WSL), peak must stay under 60% of installed RAM; otherwise a
// conservative fixed ceiling per stack.
func stressHeapCeiling(ram uint64, stack string) uint64 {
	if ram > 0 {
		return ram * 60 / 100
	}
	switch stack {
	case "go":
		return 3 * 1024 * 1024 * 1024
	default:
		return 512 * 1024 * 1024
	}
}
