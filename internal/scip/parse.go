// Package scip maps a SCIP index (produced by scip-typescript) into codegraph
// CALLS edges. SCIP gives type-checker-resolved reference occurrences; we turn
// each into a caller->callee edge by mapping symbols to our node qualified names
// and attributing each call site to the function/method that encloses it.
package scip

import (
	"strings"

	scippb "github.com/scip-code/scip/bindings/go/scip"
)

// symbolToQN converts a SCIP symbol (moniker) into a codegraph qualified name,
// matching the M1 scheme: "<project>:<relpath>.<Owner>.<name>" for methods and
// "<project>:<relpath>.<name>" for top-level functions. pathPrefix is the
// repo-relative dir where scip-typescript ran (e.g. "apps/api"), since SCIP file
// paths are relative to that root. Returns ("", false) for symbols we don't map
// (locals, parameters, externals).
func symbolToQN(symbol, project, pathPrefix string) (string, bool) {
	parsed, err := scippb.ParseSymbol(symbol)
	if err != nil || parsed == nil {
		return "", false
	}
	var pathParts, nameParts []string
	for _, d := range parsed.Descriptors {
		switch d.Suffix {
		case scippb.Descriptor_Namespace:
			pathParts = append(pathParts, d.Name)
		case scippb.Descriptor_Type, scippb.Descriptor_Method, scippb.Descriptor_Term:
			nameParts = append(nameParts, d.Name)
		}
	}
	if len(pathParts) == 0 || len(nameParts) == 0 {
		return "", false
	}
	relpath := strings.Join(pathParts, "/")
	if pathPrefix != "" {
		relpath = strings.TrimSuffix(pathPrefix, "/") + "/" + relpath
	}
	return project + ":" + relpath + "." + strings.Join(nameParts, "."), true
}
