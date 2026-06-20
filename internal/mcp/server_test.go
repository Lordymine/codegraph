package mcp

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lordymine/codegraph/internal/graph"
	"github.com/Lordymine/codegraph/internal/query"
)

// driveToolCall runs the server over a single `search` tool call and returns the
// text content of its reply, with the given readiness gate installed (nil = none).
func driveToolCall(t *testing.T, ready func() (bool, string)) string {
	t.Helper()
	store, err := graph.Open(filepath.Join(t.TempDir(), "g.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	eng := query.NewEngine(store, "proj", t.TempDir())

	req := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search","arguments":{"query":"foo"}}}` + "\n"
	var out bytes.Buffer
	srv := NewServer(eng, strings.NewReader(req), &out)
	if ready != nil {
		srv.SetReadiness(ready)
	}
	if err := srv.Serve(); err != nil {
		t.Fatalf("serve: %v", err)
	}
	var resp struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &resp); err != nil {
		t.Fatalf("bad response %q: %v", out.String(), err)
	}
	if len(resp.Result.Content) == 0 {
		return ""
	}
	return resp.Result.Content[0].Text
}

// TestServer_GatesToolCallsUntilIndexed pins the auto-index gate: while the
// background index is still building, a tool call returns a human "indexing" status
// instead of querying a half-built store; once ready, the call serves normally
// (here: an empty search result, i.e. not the status message).
func TestServer_GatesToolCallsUntilIndexed(t *testing.T) {
	const msg = "codegraph is indexing, retry shortly"

	if got := driveToolCall(t, func() (bool, string) { return false, msg }); got != msg {
		t.Errorf("not-ready tool call = %q, want the indexing status %q", got, msg)
	}
	if got := driveToolCall(t, func() (bool, string) { return true, msg }); got == msg {
		t.Errorf("ready tool call must serve the query, not the indexing status; got %q", got)
	}
}
