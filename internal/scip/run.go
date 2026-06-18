package scip

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"

	scippb "github.com/scip-code/scip/bindings/go/scip"
	"google.golang.org/protobuf/proto"
)

// Read loads a SCIP index from a .scip protobuf file.
func Read(path string) (*scippb.Index, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	idx := &scippb.Index{}
	if err := proto.Unmarshal(data, idx); err != nil {
		return nil, fmt.Errorf("unmarshal scip: %w", err)
	}
	return idx, nil
}

// RunAndRead runs scip-typescript in dir (which must hold a tsconfig.json and
// installed node_modules), writes the index to outPath, and reads it back. It is
// best-effort: callers treat an error as "no TS calls resolved this run".
func RunAndRead(dir, outPath string) (*scippb.Index, error) {
	name, args := npx("@sourcegraph/scip-typescript@latest", "index", "--output", outPath)
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("scip-typescript in %s: %w: %s", dir, err, tail(out, 500))
	}
	return Read(outPath)
}

// npx builds the platform-correct invocation. On Windows npx is a .cmd shim, so
// it must be run through cmd.exe to resolve on PATH.
func npx(pkgAndArgs ...string) (string, []string) {
	args := append([]string{"--yes"}, pkgAndArgs...)
	if runtime.GOOS == "windows" {
		return "cmd", append([]string{"/c", "npx"}, args...)
	}
	return "npx", args
}

func tail(b []byte, n int) string {
	if len(b) > n {
		return string(b[len(b)-n:])
	}
	return string(b)
}
