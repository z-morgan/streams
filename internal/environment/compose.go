package environment

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// Up starts a Docker Compose stack for the given project.
func Up(ctx context.Context, workdir string, cfg *Config, projectName string, hostPort int) error {
	args := []string{
		"compose",
		"-p", projectName,
		"-f", cfg.ComposeFile,
		"up", "-d", "--build",
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Dir = workdir
	cmd.Env = appendPortEnv(cmd.Environ(), hostPort)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker compose up: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// Exec runs a command inside a running service container.
func Exec(ctx context.Context, projectName, service, command string) error {
	if command == "" {
		return nil
	}
	args := []string{
		"compose",
		"-p", projectName,
		"exec", service,
		"sh", "-c", command,
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker compose exec: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// Down tears down a Docker Compose stack, removing volumes.
func Down(ctx context.Context, projectName string) error {
	args := []string{
		"compose",
		"-p", projectName,
		"down", "-v",
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker compose down: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// HealthCheck polls a URL until it returns HTTP 200 or the timeout expires.
func HealthCheck(ctx context.Context, hostPort int, path string, timeout time.Duration) error {
	if path == "" {
		return nil
	}

	url := fmt.Sprintf("http://localhost:%d%s", hostPort, path)
	deadline := time.After(timeout)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	client := &http.Client{Timeout: 5 * time.Second}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return fmt.Errorf("health check timed out after %s (GET %s)", timeout, url)
		case <-ticker.C:
			resp, err := client.Get(url)
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					return nil
				}
			}
		}
	}
}

func appendPortEnv(env []string, port int) []string {
	return append(env, fmt.Sprintf("STREAMS_PORT=%d", port))
}
