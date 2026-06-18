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

	var (
		result any
		err    error
	)
	switch p.Name {
	case "search":
		result, err = s.eng.Search(args.Query, args.Label, args.Limit)
	case "callers":
		result, err = s.eng.Callers(args.QualifiedName, args.Limit)
	case "callees":
		result, err = s.eng.Callees(args.QualifiedName, args.Limit)
	case "neighbors":
		result, err = s.eng.Neighbors(args.QualifiedName, args.Limit)
	case "snippet":
		result, err = s.eng.Snippet(args.File, args.StartLine, args.EndLine)
	default:
		s.fail(req.ID, -32602, "unknown tool: "+p.Name)
		return
	}
	if err != nil {
		s.fail(req.ID, -32000, err.Error())
		return
	}
	// MCP tool results are content arrays; JSON-encode the structured payload.
	payload, _ := json.Marshal(result)
	s.reply(req.ID, map[string]any{
		"content": []map[string]any{{"type": "text", "text": string(payload)}},
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
		spec("search", "Ranked BM25 symbol search. Returns compact refs (name+file+line), not code.",
			map[string]any{"query": str, "label": str, "limit": num}, "query"),
		spec("callers", "Inbound references to a symbol (who uses it).",
			map[string]any{"qualified_name": str, "limit": num}, "qualified_name"),
		spec("callees", "Outbound references from a symbol (what it uses).",
			map[string]any{"qualified_name": str, "limit": num}, "qualified_name"),
		spec("neighbors", "Both inbound and outbound neighbors of a symbol.",
			map[string]any{"qualified_name": str, "limit": num}, "qualified_name"),
		spec("snippet", "Read the source lines for a node. Use only when you must see code.",
			map[string]any{"file": str, "start_line": num, "end_line": num}, "file"),
	}
}

var _ = fmt.Sprintf // reserved for future structured logging
