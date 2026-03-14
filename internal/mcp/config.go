package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Config holds the resolved MCP configuration for a project.
type Config struct {
	Path         string   // absolute path to the mcp.json file
	ToolPatterns []string // e.g. ["mcp__chrome-devtools__*"]
}

const configFileName = "mcp.json"

// mcpFile mirrors the structure of the MCP config JSON file.
type mcpFile struct {
	MCPServers map[string]json.RawMessage `json:"mcpServers"`
}

// LoadConfig reads the MCP config from <projectDir>/.streams/mcp.json.
// Returns nil if the file does not exist (MCP is opt-in).
func LoadConfig(projectDir string) (*Config, error) {
	path := filepath.Join(projectDir, ".streams", configFileName)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read mcp config: %w", err)
	}

	var f mcpFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse mcp config: %w", err)
	}

	if len(f.MCPServers) == 0 {
		return nil, fmt.Errorf("mcp config: mcpServers must contain at least one server")
	}

	patterns := make([]string, 0, len(f.MCPServers))
	for name := range f.MCPServers {
		patterns = append(patterns, "mcp__"+name+"__*")
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve mcp config path: %w", err)
	}

	return &Config{
		Path:         absPath,
		ToolPatterns: patterns,
	}, nil
}
