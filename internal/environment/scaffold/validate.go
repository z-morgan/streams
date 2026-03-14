package scaffold

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// ValidateCompose runs "docker compose config --quiet" against the generated
// compose file to catch YAML or syntax errors. Returns nil if valid.
func ValidateCompose(ctx context.Context, projectDir string, composeFile string) error {
	args := []string{
		"compose",
		"-f", composeFile,
		"config", "--quiet",
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Dir = projectDir
	// Set STREAMS_PORT so variable substitution doesn't fail validation
	cmd.Env = append(cmd.Environ(), "STREAMS_PORT=3000")

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("compose validation failed: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// CheckDockerAvailable verifies that "docker compose version" succeeds.
// Returns nil if Docker Compose v2 is available.
func CheckDockerAvailable(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "docker", "compose", "version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker compose not available: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}
