// Package memory provides indexing-time memory budget helpers: auto-tuned concurrency,
// a soft heap limit on constrained hosts, explicit release between pipeline phases,
// and peak-heap sampling for tests. Users do not need to set any environment
// variables — tuning runs once at process start. CODEGRAPH_* env vars exist only as
// optional overrides for debugging.
package memory

import (
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Profile is the auto-detected indexing budget for this host.
type Profile struct {
	MaxWorkers  int
	BatchSize   int
	NodeHeapMB  int // --max-old-space-size for scip-typescript
	SkipSimilar bool
	Host        string // "wsl", "low-ram", or "default" — for tests/diagnostics
}

var (
	tuned   Profile
	tunedOK bool
	tuneMu  sync.Mutex
)

func init() {
	ApplyTuning()
}

// ApplyTuning detects the host (WSL, installed RAM) and configures indexing limits.
// Called automatically at init; safe to call again in tests after changing the env.
func ApplyTuning() {
	tuneMu.Lock()
	defer tuneMu.Unlock()

	tuned = detectProfile()
	tunedOK = true

	if ram := systemRAMBytes(); ram > 0 {
		// Split RAM: ~50% Go heap, ~25% reserved for scip-typescript (NODE_OPTIONS),
		// rest for cgo tree-sitter, SQLite, and the OS. See NodeHeapMB().
		debug.SetMemoryLimit(int64(ram * 50 / 100))
	}
}

// NodeHeapMB returns the --max-old-space-size passed to scip-typescript (MB).
// Auto-derived from the same host profile as the Go heap budget (WSL, low-RAM).
func NodeHeapMB() int {
	if v, ok := envInt("CODEGRAPH_NODE_HEAP_MB"); ok {
		return clamp(v, 256, 16384)
	}
	ensureTuned()
	return tuned.NodeHeapMB
}

// HostRAMBytes reports total installed RAM (Linux /proc/meminfo). Zero when unknown.
func HostRAMBytes() uint64 {
	return systemRAMBytes()
}

// ActiveProfile returns the indexing budget in effect (auto-tune + optional env overrides).
func ActiveProfile() Profile {
	ensureTuned()
	return Profile{
		MaxWorkers:  MaxWorkers(),
		BatchSize:   BatchSize(),
		NodeHeapMB:  NodeHeapMB(),
		SkipSimilar: SkipSimilar(),
		Host:        tuned.Host,
	}
}

// Gate encourages the runtime to return freed pages to the OS between heavy pipeline
// phases. Indexing spikes (VTA, SCIP) allocate large arenas; without an explicit
// gate the Go heap stays at the peak for the rest of the process life.
func Gate() {
	runtime.GC()
	debug.FreeOSMemory()
}

// MaxWorkers caps parallel file extraction during the definitions pass.
func MaxWorkers() int {
	ensureTuned()
	if v, ok := envInt("CODEGRAPH_MAX_WORKERS"); ok {
		return clamp(v, 1, 256)
	}
	return tuned.MaxWorkers
}

// BatchSize is how many files to extract and flush to SQLite per batch.
func BatchSize() int {
	ensureTuned()
	if v, ok := envInt("CODEGRAPH_BATCH_SIZE"); ok {
		return clamp(v, 1, 4096)
	}
	return tuned.BatchSize
}

// SkipSimilar reports whether the SIMILAR_TO pass should be skipped. Auto-skipped
// only on hosts with <4 GiB RAM; otherwise always on unless CODEGRAPH_SKIP_SIMILAR
// is set explicitly.
func SkipSimilar() bool {
	if v := os.Getenv("CODEGRAPH_SKIP_SIMILAR"); v == "1" || v == "true" || v == "yes" {
		return true
	}
	ensureTuned()
	return tuned.SkipSimilar
}

// PeakHeap runs fn and returns the maximum runtime.MemStats.HeapInuse observed
// while fn executes (sampled every 5ms). Used by memory simulation tests.
func PeakHeap(fn func()) uint64 {
	var (
		peak uint64
		mu   sync.Mutex
	)
	done := make(chan struct{})
	go func() {
		tick := time.NewTicker(5 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case <-tick.C:
				var m runtime.MemStats
				runtime.ReadMemStats(&m)
				mu.Lock()
				if m.HeapInuse > peak {
					peak = m.HeapInuse
				}
				mu.Unlock()
			}
		}
	}()
	fn()
	close(done)
	time.Sleep(15 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	return peak
}

func detectProfile() Profile {
	workers := runtime.NumCPU()
	if workers > 4 {
		workers = 4
	}
	if workers < 1 {
		workers = 1
	}
	p := Profile{MaxWorkers: workers, BatchSize: 64, Host: "default"}

	ram := systemRAMBytes()
	wsl := isWSL()

	if wsl {
		p.Host = "wsl"
		p.MaxWorkers = 2
		p.BatchSize = 32
	}

	switch {
	case ram > 0 && ram < 4*1024*1024*1024:
		p.Host = "low-ram"
		p.MaxWorkers = 1
		p.BatchSize = 16
		p.SkipSimilar = true
	case ram > 0 && ram < 8*1024*1024*1024:
		if p.Host == "default" {
			p.Host = "low-ram"
		}
		if p.MaxWorkers > 2 {
			p.MaxWorkers = 2
		}
		if p.BatchSize > 32 {
			p.BatchSize = 32
		}
	}

	p.NodeHeapMB = nodeHeapMBFor(ram, p.Host)
	return p
}

// nodeHeapMBFor sizes the scip-typescript child heap from installed RAM and host
// profile — parallel to the Go SetMemoryLimit split (50% Go / ~20% Node / rest).
func nodeHeapMBFor(ram uint64, host string) int {
	if ram == 0 {
		return 2048
	}
	if ram < 4*1024*1024*1024 {
		return 512
	}
	pct := 25
	switch host {
	case "wsl":
		pct = 18
	case "low-ram":
		pct = 15
	}
	mb := int(ram * uint64(pct) / 100 / 1024 / 1024)
	return clamp(mb, 512, 6144)
}

func isWSL() bool {
	if os.Getenv("WSL_DISTRO_NAME") != "" {
		return true
	}
	if runtime.GOOS != "linux" {
		return false
	}
	data, err := os.ReadFile("/proc/version")
	if err != nil {
		return false
	}
	low := strings.ToLower(string(data))
	return strings.Contains(low, "microsoft") || strings.Contains(low, "wsl")
}

func ensureTuned() {
	tuneMu.Lock()
	ok := tunedOK
	tuneMu.Unlock()
	if !ok {
		ApplyTuning()
	}
}

func envInt(key string) (int, bool) {
	raw := os.Getenv(key)
	if raw == "" {
		return 0, false
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}
	return n, true
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
