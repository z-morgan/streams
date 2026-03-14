package mcp

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestLoadConfig_NoFile(t *testing.T) {
	cfg, err := LoadConfig(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg != nil {
		t.Fatalf("expected nil config, got %+v", cfg)
	}
}

func TestLoadConfig_ValidFile(t *testing.T) {
	dir := t.TempDir()
	streamsDir := filepath.Join(dir, ".streams")
	if err := os.MkdirAll(streamsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	content := `{
		"mcpServers": {
			"chrome-devtools": {
				"command": "npx",
				"args": ["@anthropic-ai/chrome-devtools-mcp@latest"]
			},
			"filesystem": {
				"command": "npx",
				"args": ["@anthropic-ai/filesystem-mcp"]
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(streamsDir, "mcp.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected config, got nil")
	}

	if cfg.Path != filepath.Join(streamsDir, "mcp.json") {
		t.Errorf("path = %q, want %q", cfg.Path, filepath.Join(streamsDir, "mcp.json"))
	}

	sort.Strings(cfg.ToolPatterns)
	expected := []string{"mcp__chrome-devtools__*", "mcp__filesystem__*"}
	if len(cfg.ToolPatterns) != len(expected) {
		t.Fatalf("tool patterns = %v, want %v", cfg.ToolPatterns, expected)
	}
	for i, p := range cfg.ToolPatterns {
		if p != expected[i] {
			t.Errorf("tool pattern[%d] = %q, want %q", i, p, expected[i])
		}
	}
}

func TestLoadConfig_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	streamsDir := filepath.Join(dir, ".streams")
	if err := os.MkdirAll(streamsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(streamsDir, "mcp.json"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadConfig(dir)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestLoadConfig_EmptyServers(t *testing.T) {
	dir := t.TempDir()
	streamsDir := filepath.Join(dir, ".streams")
	if err := os.MkdirAll(streamsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(streamsDir, "mcp.json"), []byte(`{"mcpServers": {}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadConfig(dir)
	if err == nil {
		t.Fatal("expected error for empty mcpServers")
	}
}
