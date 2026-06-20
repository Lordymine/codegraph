// Package mcp implements a minimal Model Context Protocol server over stdio.
//
// Transport: newline-delimited JSON-RPC 2.0 (the MCP stdio convention — one
// JSON message per line). This is deliberately dependency-free; if it grows,
// swap in github.com/mark3labs/mcp-go. It exposes the query tools so a coding
// agent (Claude Code etc.) can drive the graph. See docs/ARCHITECTURE.md.
package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"

	"github.com/Lordymine/codegraph/internal/query"
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any       `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Server serves MCP over the given streams using a query engine.
type Server struct {
	eng *query.Engine
	in  *bufio.Scanner
	out *json.Encoder
}

func NewServer(eng *query.Engine, in io.Reader, out io.Writer) *Server {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	return &Server{eng: eng, in: sc, out: json.NewEncoder(out)}
}

// Serve runs the request loop until stdin closes.
func (s *Server) Serve() error {
	for s.in.Scan() {
		line := s.in.Bytes()
		if len(line) == 0 {
			continue
		}
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			continue
		}
		s.handle(req)
	}
	return s.in.Err()
}

func (s *Server) reply(id json.RawMessage, result any) {
	_ = s.out.Encode(rpcResponse{JSONRPC: "2.0", ID: id, Result: result})
}
func (s *Server) fail(id json.RawMessage, code int, msg string) {
	_ = s.out.Encode(rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}})
}

func (s *Server) handle(req rpcRequest) {
	switch req.Method {
	case "initialize":
		s.reply(req.ID, map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "codegraph", "version": "0.0.1"},
		})
	case "notifications/initialized":
		// no response for notifications
	case "tools/list":
		s.reply(req.ID, map[string]any{"tools": toolSpecs()})
	case "tools/call":
		s.callTool(req)
	default:
		if len(req.ID) > 0 {
			s.fail(req.ID, -32601, "method not found: "+req.Method)
		}
	}
}

type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func (s *Server) callTool(req rpcRequest) {
	var p toolCallParams
	_ = json.Unmarshal(req.Params, &p)
	var args struct {
		Query         string `json:"query"`
		Label         string `json:"label"`
		QualifiedName string `json:"qualified_name"`
		File          string `json:"file"`
		StartLine     int    `json:"start_line"`
		EndLine       int    `json:"end_line"`
		Limit         int    `json:"limit"`
	}
	_ = json.Unmarshal(p.Arguments, &args)

	// Ref-returning tools emit the compact wire format (one TSV line per ref:
	// label<TAB>name<TAB>file:line<TAB>qn); snippet emits raw source. We never
	// JSON-wrap the result — that wrapper is exactly the token overhead the
	// compact format exists to avoid.
	var (
		text string
		err  error
	)
	switch p.Name {
	case "search":
		var refs []query.Ref
		refs, err = s.eng.Search(args.Query, args.Label, args.Limit)
		text = query.CompactRefs(refs)
	case "callers":
		var refs []query.Ref
		refs, err = s.eng.Callers(args.QualifiedName, args.Limit)
		text = query.CompactRefs(refs)
	case "callees":
		var refs []query.Ref
		refs, err = s.eng.Callees(args.QualifiedName, args.Limit)
		text = query.CompactRefs(refs)
	case "neighbors":
		var refs []query.Ref
		refs, err = s.eng.Neighbors(args.QualifiedName, args.Limit)
		text = query.CompactRefs(refs)
	case "similar":
		var refs []query.Ref
		refs, err = s.eng.Similar(args.QualifiedName, args.Limit)
		text = query.CompactRefs(refs)
	case "dead_code":
		var refs []query.Ref
		refs, err = s.eng.DeadCode(args.Limit)
		text = query.CompactRefs(refs)
	case "snippet":
		text, err = s.eng.Snippet(args.File, args.StartLine, args.EndLine)
	case "detect_changes":
		ch, derr := s.eng.DetectChanges()
		if err = derr; err == nil {
			if text = ch.Summary(); text == "" {
				text = "no changes since the last index"
			}
		}
	default:
		s.fail(req.ID, -32602, "unknown tool: "+p.Name)
		return
	}
	if err != nil {
		s.fail(req.ID, -32000, err.Error())
		return
	}
	s.reply(req.ID, map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
	})
}

func toolSpecs() []map[string]any {
	str := map[string]any{"type": "string"}
	num := map[string]any{"type": "integer"}
	spec := func(name, desc string, props map[string]any, required ...string) map[string]any {
		return map[string]any{
			"name": name, "description": desc,
			"inputSchema": map[string]any{"type": "object", "properties": props, "required": required},
		}
	}
	return []map[string]any{
		spec("search", "Ranked BM25 symbol search. Returns compact refs, not code — one TSV line per hit: label<TAB>name<TAB>file:line<TAB>qualified_name. Pass a returned qualified_name straight to callers/callees.",
			map[string]any{"query": str, "label": str, "limit": num}, "query"),
		spec("callers", "Inbound references to a symbol (who uses it). Returns TSV refs (see search). Accepts a qualified_name with or without the project prefix.",
			map[string]any{"qualified_name": str, "limit": num}, "qualified_name"),
		spec("callees", "Outbound references from a symbol (what it uses). Returns TSV refs (see search).",
			map[string]any{"qualified_name": str, "limit": num}, "qualified_name"),
		spec("neighbors", "Both inbound and outbound neighbors of a symbol. Returns TSV refs (see search).",
			map[string]any{"qualified_name": str, "limit": num}, "qualified_name"),
		spec("similar", "Near-clone symbols of this one (SIMILAR_TO edges from MinHash/LSH). Surfaces copy-paste/duplicated logic to refactor. Returns TSV refs (see search).",
			map[string]any{"qualified_name": str, "limit": num}, "qualified_name"),
		spec("dead_code", "CANDIDATES for unused private functions/methods: zero inbound CALLS, excluding entry points (exported, decorated, main/init, tests). NOT a delete list — a caller the resolver missed or an indirect reference (function value, interface, reflection) makes a live function look dead, so confirm each (e.g. grep the name) before acting. Returns TSV refs (see search).",
			map[string]any{"limit": num}),
		spec("snippet", "Read the source lines for a node. Use only when you must see code.",
			map[string]any{"file": str, "start_line": num, "end_line": num}, "file"),
		spec("detect_changes", "List source files changed/added/deleted since the last index (TSV: status<TAB>path, empty = fresh). Check it before trusting the graph for a region; re-index if stale.",
			map[string]any{}),
	}
}

var _ = fmt.Sprintf // reserved for future structured logging
