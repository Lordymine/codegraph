package install

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestOpencodeConfigPath_PrefersExistingJsonc pins that the installer merges into
// the config file opencode actually reads: if the user has an opencode.jsonc, target
// THAT (don't strand the registration in a second opencode.json the agent ignores);
// otherwise default to opencode.json.
func TestOpencodeConfigPath_PrefersExistingJsonc(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	// No file yet → default to .json.
	if got := opencodeConfigPath(); filepath.Base(got) != "opencode.json" {
		t.Errorf("with no existing config, path = %q, want opencode.json", got)
	}

	// A pre-existing .jsonc must win.
	ocDir := filepath.Join(dir, "opencode")
	if err := os.MkdirAll(ocDir, 0o755); err != nil {
		t.Fatal(err)
	}
	jsonc := filepath.Join(ocDir, "opencode.jsonc")
	if err := os.WriteFile(jsonc, []byte(`{"instructions":["x"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := opencodeConfigPath(); got != jsonc {
		t.Errorf("with an existing opencode.jsonc, path = %q, want %q", got, jsonc)
	}
}

// TestRun_InstallsDetectedAndCollectsManual pins the flow: a detected agent with an
// Install runs it (Installed); a detected agent without Install yields Manual text;
// an undetected agent is skipped entirely.
func TestRun_InstallsDetectedAndCollectsManual(t *testing.T) {
	var did []string
	agents := []Agent{
		{
			Name:    "auto",
			Detect:  func() bool { return true },
			Install: func(bin string) error { did = append(did, "auto:"+bin); return nil },
			Manual:  func(bin string) string { return "manual auto" },
		},
		{
			Name:   "manualOnly",
			Detect: func() bool { return true },
			Manual: func(bin string) string { return "paste for " + bin },
		},
		{
			Name:    "absent",
			Detect:  func() bool { return false },
			Install: func(bin string) error { t.Fatal("undetected agent must not install"); return nil },
		},
	}

	outs := Run(agents, "/usr/bin/codegraph")

	if !slices.Equal(did, []string{"auto:/usr/bin/codegraph"}) {
		t.Fatalf("auto agent should have installed once, got %v", did)
	}
	by := map[string]Outcome{}
	for _, o := range outs {
		by[o.Agent] = o
	}
	if o := by["auto"]; !o.Installed {
		t.Errorf("auto outcome = %+v, want Installed", o)
	}
	if o := by["manualOnly"]; o.Installed || o.Manual == "" {
		t.Errorf("manualOnly outcome = %+v, want manual text and not installed", o)
	}
	if _, ok := by["absent"]; ok {
		t.Errorf("undetected agent must be skipped")
	}
}

// TestClaudeCommand / TestCodexCommand pin the CLI registrations. Both register a
// stdio server with no repo-path arg — the server reads $CLAUDE_PROJECT_DIR or its
// cwd at runtime, so one registration serves any repo. Claude uses user scope.
func TestClaudeCommand(t *testing.T) {
	got := ClaudeCommand("/opt/codegraph")
	want := []string{"claude", "mcp", "add", "--scope", "user", "--transport", "stdio", "codegraph", "--", "/opt/codegraph", "mcp"}
	if !slices.Equal(got, want) {
		t.Errorf("ClaudeCommand =\n %v\nwant\n %v", got, want)
	}
}

func TestCodexCommand(t *testing.T) {
	got := CodexCommand("/opt/codegraph")
	want := []string{"codex", "mcp", "add", "codegraph", "--", "/opt/codegraph", "mcp"}
	if !slices.Equal(got, want) {
		t.Errorf("CodexCommand =\n %v\nwant\n %v", got, want)
	}
}

// TestMergeOpencodeConfig pins the opencode JSON merge: it adds the codegraph local
// server WITHOUT clobbering the user's other config (other top-level keys and other
// MCP servers survive). opencode has no add-CLI, so this file merge is the auto path.
func TestMergeOpencodeConfig(t *testing.T) {
	// From empty: creates the mcp.codegraph local entry.
	out, err := mergeOpencodeConfig(nil, "/opt/codegraph")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	cg := got["mcp"].(map[string]any)["codegraph"].(map[string]any)
	if cg["type"] != "local" || cg["enabled"] != true {
		t.Errorf("codegraph entry = %v, want type=local enabled=true", cg)
	}
	cmd := cg["command"].([]any)
	if len(cmd) != 2 || cmd[0] != "/opt/codegraph" || cmd[1] != "mcp" {
		t.Errorf("command = %v, want [/opt/codegraph mcp]", cmd)
	}

	// Into an existing config: preserve the user's model setting and other server.
	existing := []byte(`{"model":"anthropic/claude","mcp":{"other":{"type":"local","command":["other"],"enabled":true}}}`)
	out2, err := mergeOpencodeConfig(existing, "/opt/codegraph")
	if err != nil {
		t.Fatal(err)
	}
	var got2 map[string]any
	if err := json.Unmarshal(out2, &got2); err != nil {
		t.Fatal(err)
	}
	if got2["model"] != "anthropic/claude" {
		t.Errorf("merge clobbered top-level keys: %v", got2)
	}
	mcp := got2["mcp"].(map[string]any)
	if _, ok := mcp["other"]; !ok {
		t.Errorf("merge dropped the user's existing 'other' server: %v", mcp)
	}
	if _, ok := mcp["codegraph"]; !ok {
		t.Errorf("merge did not add codegraph: %v", mcp)
	}
}
