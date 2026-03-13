package environment

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"encoding/json"
)

// Config describes how to provision a containerized environment for a stream.
// Loaded from .streams/environment.yml in the target project directory.
type Config struct {
	ComposeFile   string        `json:"compose_file"`
	Service       string        `json:"service"`
	Setup         string        `json:"setup,omitempty"`
	Port          int           `json:"port"`
	HealthCheck   string        `json:"health_check,omitempty"`
	HealthTimeout time.Duration `json:"health_timeout,omitempty"`
}

const configFileName = "environment.json"

// LoadConfig reads the environment config from <projectDir>/.streams/environment.json.
// Returns nil if the file does not exist (the feature is opt-in).
func LoadConfig(projectDir string) (*Config, error) {
	path := filepath.Join(projectDir, ".streams", configFileName)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read environment config: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse environment config: %w", err)
	}

	if err := cfg.validate(projectDir); err != nil {
		return nil, err
	}

	if cfg.HealthTimeout == 0 {
		cfg.HealthTimeout = 60 * time.Second
	}

	return &cfg, nil
}

func (c *Config) validate(projectDir string) error {
	if c.ComposeFile == "" {
		return fmt.Errorf("environment config: compose_file is required")
	}
	if c.Service == "" {
		return fmt.Errorf("environment config: service is required")
	}
	if c.Port <= 0 || c.Port > 65535 {
		return fmt.Errorf("environment config: port must be between 1 and 65535")
	}

	composePath := filepath.Join(projectDir, c.ComposeFile)
	if _, err := os.Stat(composePath); os.IsNotExist(err) {
		return fmt.Errorf("environment config: compose file %q not found", composePath)
	}

	return nil
}
