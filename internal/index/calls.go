package index

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/Lordymine/codegraph/internal/graph"
	"github.com/Lordymine/codegraph/internal/scip"
)

// ResolveCalls emits CALLS edges. For TS/JS it delegates to scip-typescript
// (batch, type-checker-accurate) per tsconfig subproject and attributes each
// resolved reference to the function/method that encloses it (the caller). It is
// best-effort: a subproject whose scip run fails contributes no edges rather than
// failing the whole index. Go call resolution (go/packages + callgraph) is TODO.
func ResolveCalls(project, root string, files []SourceFile, nodes []graph.Node) []graph.Edge {
	enc := scip.BuildEnclosing(nodes)
	var edges []graph.Edge
	for _, dir := range tsconfigDirs(root) {
		abs := filepath.Join(root, filepath.FromSlash(dir))
		out := filepath.Join(os.TempDir(), "codegraph-"+strings.ReplaceAll(dir, "/", "-")+".scip")
		idx, err := scip.RunAndRead(abs, out)
		if err != nil {
			continue // best-effort per subproject
		}
		edges = append(edges, scip.CallEdges(idx, project, dir, enc)...)
	}
	return edges
}

// tsconfigDirs finds repo-relative directories (other than the root) that contain
// a tsconfig.json — the units scip-typescript indexes. node_modules and hidden
// dirs are skipped.
func tsconfigDirs(root string) []string {
	var dirs []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if hardIgnoreDir[d.Name()] || (strings.HasPrefix(d.Name(), ".") && d.Name() != ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == "tsconfig.json" {
			rel, _ := filepath.Rel(root, filepath.Dir(path))
			if rel = filepath.ToSlash(rel); rel != "." {
				dirs = append(dirs, rel)
			}
		}
		return nil
	})
	return dirs
}
