// Package graph defines the knowledge-graph data model and its SQLite store.
//
// The model is deliberately tiny — two entities, Node and Edge — mirroring the
// upstream codebase-memory-mcp schema. All the richness lives in Node.Props /
// Edge.Props (JSON blobs) so we never have to migrate columns as the analysis
// passes get smarter. See docs/ARCHITECTURE.md.
package graph

// NodeLabel is the kind of a graph node (its "type" in graph terms).
type NodeLabel string

const (
	LabelFile      NodeLabel = "File"
	LabelModule    NodeLabel = "Module"
	LabelFunction  NodeLabel = "Function"
	LabelMethod    NodeLabel = "Method"
	LabelClass     NodeLabel = "Class"
	LabelInterface NodeLabel = "Interface" // TS interface
	LabelType      NodeLabel = "Type"      // TS type alias
	LabelEnum      NodeLabel = "Enum"      // TS enum
	LabelVariable  NodeLabel = "Variable"  // exported/top-level binding
	LabelRoute     NodeLabel = "Route"     // HTTP endpoint (later pass)
)

// EdgeType is the kind of a relationship between two nodes. The MVP only emits
// a subset; the rest are reserved so query code can be written against the full
// vocabulary from day one. Mirrors the upstream edge types.
type EdgeType string

const (
	EdgeDefines       EdgeType = "DEFINES"        // container -> member (file defines func, class defines method)
	EdgeContainsFile  EdgeType = "CONTAINS_FILE"  // folder/module -> file
	EdgeCalls         EdgeType = "CALLS"          // caller -> callee (needs real resolution / LSP)
	EdgeImports       EdgeType = "IMPORTS"        // module -> imported module
	EdgeInherits      EdgeType = "INHERITS"       // class -> base class
	EdgeImplements    EdgeType = "IMPLEMENTS"     // class -> interface
	EdgeDecorates     EdgeType = "DECORATES"      // decorator -> target (Nest @Injectable etc.)
	EdgeHTTPCalls     EdgeType = "HTTP_CALLS"     // call-site -> route (cross-service)
	EdgeSimilarTo     EdgeType = "SIMILAR_TO"     // near-clone (MinHash + LSH)
	EdgeSemanticalRel EdgeType = "SEMANTICALLY_RELATED"
)

// Node is a symbol or container in the codebase. qualified_name is the unique
// key within a project (e.g. "proj.apps.api.src.foo.Bar.baz").
type Node struct {
	ID            int64                  // assigned by the store on insert
	Project       string                 // project name (one store can hold many)
	Label         NodeLabel              //
	Name          string                 // short name, e.g. "getActiveCode"
	QualifiedName string                 // unique within project
	FilePath      string                 // repo-relative path
	StartLine     int                    //
	EndLine       int                    //
	Props         map[string]any         // signature, params, complexity, is_test, ...
}

// Edge is a directed relationship between two nodes (by qualified name at build
// time; resolved to node IDs by the store on flush).
type Edge struct {
	Project    string
	SourceQN   string // qualified_name of the source node
	TargetQN   string // qualified_name of the target node
	Type       EdgeType
	Props      map[string]any
}

// SearchHit is a ranked result from the FTS index.
type SearchHit struct {
	Node Node
	Rank float64
}
