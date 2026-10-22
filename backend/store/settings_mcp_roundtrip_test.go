package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The frontend sends settings as JSON (Wails binding) and the Go structs are
// what gets written: a key without a field is dropped silently. The MCP
// block is nested two levels deep (mcp.tools.*), so check every leaf
// survives the frontend → Save → file → Load path.
func TestSettingsMCPRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSettingsStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	fromFrontend := `{"theme":"dark","mcp":{"enabled":true,"port":7801,"policy":"confirm_write",
		"tools":{"exec":false,"terminal":true,"files":true}}}`
	var in AppSettings
	if err := json.Unmarshal([]byte(fromFrontend), &in); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(in); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, settingsFileName))
	if err != nil {
		t.Fatal(err)
	}
	var written struct {
		MCP map[string]json.RawMessage `json:"mcp"`
	}
	if err := json.Unmarshal(raw, &written); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"enabled": `true`, "port": `7801`, "policy": `"confirm_write"`,
		"tools": `{"exec":false,"terminal":true,"files":true}`,
	}
	for k, v := range want {
		if got := string(written.MCP[k]); got != v {
			t.Errorf("settings.json mcp.%s = %s, want %s\n%s", k, got, v, raw)
		}
	}

	got, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.MCP == nil || !got.MCP.Enabled || got.MCP.Port != 7801 || got.MCP.Policy != "confirm_write" ||
		got.MCP.Tools != (MCPToolToggles{Exec: false, Terminal: true, Files: true}) {
		t.Fatalf("Load mcp = %+v", got.MCP)
	}
}
