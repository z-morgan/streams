package claude

import (
	"bufio"
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
// When req.OnOutput is set, it uses --output-format stream-json and streams
// parsed events through the callback. Otherwise it falls back to --output-format json.
func (r *Runtime) Run(ctx context.Context, req runtime.Request) (*runtime.Response, error) {
	if req.OnOutput != nil {
		return r.runStreaming(ctx, req)
	}
	return r.runJSON(ctx, req)
}

// runJSON is the original non-streaming path using --output-format json.
func (r *Runtime) runJSON(ctx context.Context, req runtime.Request) (*runtime.Response, error) {
	args := []string{"-p", "--output-format", "json"}
	args = appendCommonArgs(args, req)
	args = append(args, req.Prompt)

	cmd := exec.CommandContext(ctx, r.command(), args...)
	if r.WorkDir != "" {
		cmd.Dir = r.WorkDir
	}
	cmd.Env = appendEnvWithout(cmd.Environ(), "CLAUDECODE")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

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

// runStreaming uses --output-format stream-json and parses NDJSON events.
func (r *Runtime) runStreaming(ctx context.Context, req runtime.Request) (*runtime.Response, error) {
	args := []string{"-p", "--output-format", "stream-json"}
	args = appendCommonArgs(args, req)
	args = append(args, req.Prompt)

	cmd := exec.CommandContext(ctx, r.command(), args...)
	if r.WorkDir != "" {
		cmd.Dir = r.WorkDir
	}
	cmd.Env = appendEnvWithout(cmd.Environ(), "CLAUDECODE")

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start claude CLI: %w", err)
	}

	var finalResult *cliResult
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var event streamEvent
		if err := json.Unmarshal(line, &event); err != nil {
			continue // skip unparseable lines
		}

		switch event.Type {
		case "assistant":
			for _, block := range event.Message.Content {
				switch block.Type {
				case "text":
					if text := strings.TrimSpace(block.Text); text != "" {
						req.OnOutput(text)
					}
				case "tool_use":
					req.OnOutput(formatToolUse(block.Name, block.Input))
				}
			}

		case "result":
			finalResult = &cliResult{
				Type:         event.Type,
				Subtype:      event.Subtype,
				IsError:      event.IsError,
				Result:       event.Result,
				TotalCostUSD: event.TotalCostUSD,
				SessionID:    event.SessionID,
				NumTurns:     event.NumTurns,
				DurationMS:   event.DurationMS,
			}
		}
	}

	waitErr := cmd.Wait()

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if waitErr != nil && finalResult == nil {
		return nil, fmt.Errorf("claude CLI failed (exit %v): %s", waitErr, strings.TrimSpace(stderr.String()))
	}

	if finalResult == nil {
		return nil, fmt.Errorf("no result event in stream output")
	}

	if finalResult.Subtype == "error_max_budget_usd" {
		return nil, fmt.Errorf("budget exceeded (cost: $%.4f)", finalResult.TotalCostUSD)
	}

	if finalResult.IsError || finalResult.Subtype != "success" {
		return nil, fmt.Errorf("CLI returned error (subtype=%s): %s", finalResult.Subtype, finalResult.Result)
	}

	return &runtime.Response{
		Text:    finalResult.Result,
		CostUSD: finalResult.TotalCostUSD,
	}, nil
}

func appendCommonArgs(args []string, req runtime.Request) []string {
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
	return args
}

// formatToolUse creates a human-readable line for a tool invocation.
func formatToolUse(name string, input json.RawMessage) string {
	var params map[string]interface{}
	if json.Unmarshal(input, &params) != nil {
		return "> " + name
	}

	switch name {
	case "Read":
		if fp, ok := params["file_path"].(string); ok {
			return "> Read " + fp
		}
	case "Edit":
		if fp, ok := params["file_path"].(string); ok {
			return "> Edit " + fp
		}
	case "Write":
		if fp, ok := params["file_path"].(string); ok {
			return "> Write " + fp
		}
	case "Glob":
		if p, ok := params["pattern"].(string); ok {
			return "> Glob " + p
		}
	case "Grep":
		if p, ok := params["pattern"].(string); ok {
			return "> Grep " + p
		}
	case "Bash":
		if c, ok := params["command"].(string); ok {
			summary := c
			if len(summary) > 80 {
				summary = summary[:77] + "..."
			}
			return "> Bash " + summary
		}
	}

	return "> " + name
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
