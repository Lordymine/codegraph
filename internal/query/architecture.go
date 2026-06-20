package query

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Hot is a hotspot: a symbol plus its ranking metric (cyclomatic complexity, or
// inbound caller count).
type Hot struct {
	Ref    Ref
	Metric int
}

// PackageStat is one directory and how many symbols it holds.
type PackageStat struct {
	Dir     string
	Symbols int
}

// Architecture is the compact repo map get_architecture returns.
type Architecture struct {
	Languages          map[string]int
	NodeCounts         map[string]int
	EdgeCounts         map[string]int
	Packages           []PackageStat
	ComplexityHotspots []Hot
	CallHubs           []Hot
}

// Architecture aggregates the graph into a one-shot repo overview: languages, node/
// edge counts, top packages by symbol count, and the complexity/call-hub hotspots.
// All from stored data — no re-scan — so an agent gets direction in a single call
// instead of grepping its way in. Hotspots read the M4 cyclomatic complexity.
func (e *Engine) Architecture(topN int) (Architecture, error) {
	if topN <= 0 {
		topN = 10
	}
	var a Architecture
	var err error
	if a.Languages, err = e.store.LanguageCounts(e.project); err != nil {
		return a, err
	}
	if a.NodeCounts, err = e.store.LabelCounts(e.project); err != nil {
		return a, err
	}
	if a.EdgeCounts, err = e.store.EdgeTypeCounts(e.project); err != nil {
		return a, err
	}

	fileCounts, err := e.store.FileSymbolCounts(e.project)
	if err != nil {
		return a, err
	}
	a.Packages = topPackages(fileCounts, topN)

	complexNodes, err := e.store.TopByComplexity(e.project, topN)
	if err != nil {
		return a, err
	}
	for _, n := range complexNodes {
		a.ComplexityHotspots = append(a.ComplexityHotspots, Hot{Ref: refOf(n), Metric: intProp(n.Props, "complexity")})
	}

	hubs, err := e.store.CallHubs(e.project, topN)
	if err != nil {
		return a, err
	}
	for _, h := range hubs {
		a.CallHubs = append(a.CallHubs, Hot{Ref: refOf(h.Node), Metric: h.Callers})
	}
	return a, nil
}

// topPackages folds per-file symbol counts into per-directory totals and returns the
// top N by symbol count (ties broken by dir name for determinism).
func topPackages(fileCounts map[string]int, n int) []PackageStat {
	byDir := map[string]int{}
	for file, c := range fileCounts {
		byDir[filepath.ToSlash(filepath.Dir(file))] += c
	}
	pkgs := make([]PackageStat, 0, len(byDir))
	for dir, c := range byDir {
		pkgs = append(pkgs, PackageStat{Dir: dir, Symbols: c})
	}
	sort.Slice(pkgs, func(i, j int) bool {
		if pkgs[i].Symbols != pkgs[j].Symbols {
			return pkgs[i].Symbols > pkgs[j].Symbols
		}
		return pkgs[i].Dir < pkgs[j].Dir
	})
	if len(pkgs) > n {
		pkgs = pkgs[:n]
	}
	return pkgs
}

// intProp reads an integer property stored as int (in-memory) or float64 (after a
// JSON round-trip through the store).
func intProp(props map[string]any, key string) int {
	switch v := props[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	}
	return 0
}

// RenderArchitecture renders the map as a compact, token-frugal digest — counts on
// one line each, hotspots as `name<TAB>file:line<TAB>metric`.
func RenderArchitecture(a Architecture) string {
	var b strings.Builder
	b.WriteString("languages: " + renderCounts(a.Languages) + "\n")
	b.WriteString("nodes: " + renderCounts(a.NodeCounts) + "\n")
	b.WriteString("edges: " + renderCounts(a.EdgeCounts) + "\n")
	if len(a.Packages) > 0 {
		b.WriteString("packages (top by symbols):\n")
		for _, p := range a.Packages {
			b.WriteString("  " + p.Dir + "\t" + strconv.Itoa(p.Symbols) + "\n")
		}
	}
	writeHotspots(&b, "hotspots — complexity", a.ComplexityHotspots, "cx")
	writeHotspots(&b, "hotspots — most called", a.CallHubs, "callers")
	return b.String()
}

func writeHotspots(b *strings.Builder, title string, hs []Hot, metric string) {
	if len(hs) == 0 {
		return
	}
	b.WriteString(title + ":\n")
	for _, h := range hs {
		b.WriteString("  " + h.Ref.Name + "\t" + h.Ref.File + ":" + strconv.Itoa(h.Ref.StartLine) + "\t" + metric + "=" + strconv.Itoa(h.Metric) + "\n")
	}
}

func renderCounts(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+strconv.Itoa(m[k]))
	}
	return strings.Join(parts, " ")
}
