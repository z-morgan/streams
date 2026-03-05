package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/zmorgan/streams/internal/runtime"
)

// Runtime wraps the Claude Code CLI as a runtime.Runtime.
type Runtime struct {
	// Command is the path to the claude binary. Defaults to "claude".
	Command string
	// WorkDir is the working directory for CLI invocations.
	WorkDir string
}

func (r *Runtime) command() string {
	if r.Command != "" {
		return r.Command
	}
	return "claude"
}

// Run executes a single claude -p invocation and returns the parsed response.
func (r *Runtime) Run(ctx context.Context, req runtime.Request) (*runtime.Response, error) {
	args := []string{"-p", "--output-format", "json"}

	if v, ok := req.Options["allowedTools"]; ok {
		args = append(args, "--allowedTools", v)
	}
	if v, ok := req.Options["appendSystemPrompt"]; ok {
		args = append(args, "--append-system-prompt", v)
	}
	if v, ok := req.Options["permissionMode"]; ok {
		args = append(args, "--permission-mode", v)
	}
	if v, ok := req.Options["maxBudgetUsd"]; ok {
		args = append(args, "--max-budget-usd", v)
	}

	args = append(args, req.Prompt)

	cmd := exec.CommandContext(ctx, r.command(), args...)
	if r.WorkDir != "" {
		cmd.Dir = r.WorkDir
	}
	// Unset CLAUDECODE to allow nested invocation.
	cmd.Env = appendEnvWithout(cmd.Environ(), "CLAUDECODE")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	// Check context cancellation first — it takes priority over exit errors.
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if err != nil {
		return nil, fmt.Errorf("claude CLI failed (exit %v): %s", err, strings.TrimSpace(stderr.String()))
	}

	var result cliResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return nil, fmt.Errorf("failed to parse CLI JSON output: %w", err)
	}

	if result.Subtype == "error_max_budget_usd" {
		return nil, fmt.Errorf("budget exceeded (cost: $%.4f)", result.TotalCostUSD)
	}

	if result.IsError || result.Subtype != "success" {
		return nil, fmt.Errorf("CLI returned error (subtype=%s): %s", result.Subtype, result.Result)
	}

	return &runtime.Response{
		Text:    result.Result,
		CostUSD: result.TotalCostUSD,
	}, nil
}

// appendEnvWithout returns os environ with the named variable removed, plus
// the variable set to empty string to unset it in the child process.
func appendEnvWithout(env []string, name string) []string {
	prefix := name + "="
	filtered := make([]string, 0, len(env))
	for _, e := range env {
		if !strings.HasPrefix(e, prefix) {
			filtered = append(filtered, e)
		}
	}
	return append(filtered, name+"=")
}
