package scip

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	scippb "github.com/scip-code/scip/bindings/go/scip"
	"google.golang.org/protobuf/proto"

	"github.com/Lordymine/codegraph/internal/memory"
)

// scipTypescriptVersion is pinned so index output stays reproducible across npx runs.
const scipTypescriptVersion = "0.4.0"

// RunStats reports resource use of one scip-typescript invocation. PeakRSSBytes is
// sampled from the child process on Linux/WSL (/proc); zero on platforms where RSS
// is not tracked (Windows native).
type RunStats struct {
	PeakRSSBytes uint64
	NodeHeapMB   int
	Elapsed      time.Duration
}

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
	data = nil
	return idx, nil
}

// RunAndRead runs scip-typescript in dir (which must hold a tsconfig.json and
// installed node_modules), writes the index to outPath, and reads it back. It is
// best-effort: callers treat an error as "no TS calls resolved this run".
func RunAndRead(dir, outPath string) (*scippb.Index, RunStats, error) {
	st, err := runScip(dir, outPath)
	if err != nil {
		return nil, st, err
	}
	idx, err := Read(outPath)
	if err != nil {
		return nil, st, err
	}
	memory.Gate() // drop ReadFile+unmarshal buffers before CallEdges walks the index
	return idx, st, nil
}

func runScip(dir, outPath string) (RunStats, error) {
	st := RunStats{NodeHeapMB: memory.NodeHeapMB()}
	name, args := npx("@sourcegraph/scip-typescript@"+scipTypescriptVersion, "index", "--output", outPath)
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = nodeEnv(st.NodeHeapMB)

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	t0 := time.Now()
	if err := cmd.Start(); err != nil {
		return st, fmt.Errorf("start scip-typescript: %w", err)
	}
	done := make(chan struct{})
	var peak atomic.Uint64
	go func() {
		peak.Store(peakChildRSS(cmd.Process.Pid, done))
	}()
	waitErr := cmd.Wait()
	close(done)
	st.PeakRSSBytes = peak.Load()
	st.Elapsed = time.Since(t0)

	if waitErr != nil {
		return st, fmt.Errorf("scip-typescript in %s: %w: %s", dir, waitErr, tail(out.Bytes(), 500))
	}
	return st, nil
}

// nodeEnv returns os.Environ with NODE_OPTIONS augmented by --max-old-space-size so
// the scip-typescript child cannot grow past the auto-tuned budget.
func nodeEnv(heapMB int) []string {
	limit := fmt.Sprintf("--max-old-space-size=%d", heapMB)
	env := os.Environ()
	for i, e := range env {
		if strings.HasPrefix(e, "NODE_OPTIONS=") {
			env[i] = e + " " + limit
			return env
		}
	}
	return append(env, "NODE_OPTIONS="+limit)
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
