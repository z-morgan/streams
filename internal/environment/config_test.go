package environment

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigMissing(t *testing.T) {
	cfg, err := LoadConfig("/nonexistent/path")
	if err != nil {
		t.Fatal(err)
	}
	if cfg != nil {
		t.Fatal("expected nil config for missing file")
	}
}

func TestLoadConfigValid(t *testing.T) {
	dir := t.TempDir()
	streamsDir := filepath.Join(dir, ".streams")
	os.MkdirAll(streamsDir, 0o755)

	// Create a dummy compose file.
	composePath := filepath.Join(dir, "docker-compose.yml")
	os.WriteFile(composePath, []byte("services: {}"), 0o644)

	cfg := Config{
		ComposeFile: "docker-compose.yml",
		Service:     "app",
		Port:        3000,
		HealthCheck: "/up",
	}
	data, _ := json.Marshal(cfg)
	os.WriteFile(filepath.Join(streamsDir, configFileName), data, 0o644)

	loaded, err := LoadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded == nil {
		t.Fatal("expected non-nil config")
	}
	if loaded.ComposeFile != "docker-compose.yml" {
		t.Errorf("got compose_file %q, want docker-compose.yml", loaded.ComposeFile)
	}
	if loaded.Service != "app" {
		t.Errorf("got service %q, want app", loaded.Service)
	}
	if loaded.Port != 3000 {
		t.Errorf("got port %d, want 3000", loaded.Port)
	}
}

func TestLoadConfigMissingComposeFile(t *testing.T) {
	dir := t.TempDir()
	streamsDir := filepath.Join(dir, ".streams")
	os.MkdirAll(streamsDir, 0o755)

	cfg := Config{
		ComposeFile: "nonexistent.yml",
		Service:     "app",
		Port:        3000,
	}
	data, _ := json.Marshal(cfg)
	os.WriteFile(filepath.Join(streamsDir, configFileName), data, 0o644)

	_, err := LoadConfig(dir)
	if err == nil {
		t.Fatal("expected error for missing compose file")
	}
}

func TestLoadConfigInvalidPort(t *testing.T) {
	dir := t.TempDir()
	streamsDir := filepath.Join(dir, ".streams")
	os.MkdirAll(streamsDir, 0o755)

	composePath := filepath.Join(dir, "docker-compose.yml")
	os.WriteFile(composePath, []byte("services: {}"), 0o644)

	cfg := Config{
		ComposeFile: "docker-compose.yml",
		Service:     "app",
		Port:        0,
	}
	data, _ := json.Marshal(cfg)
	os.WriteFile(filepath.Join(streamsDir, configFileName), data, 0o644)

	_, err := LoadConfig(dir)
	if err == nil {
		t.Fatal("expected error for invalid port")
	}
}
