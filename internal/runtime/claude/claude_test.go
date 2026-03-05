package claude

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zmorgan/streams/internal/runtime"
)

// writeFakeClaude creates a shell script that prints the given JSON to stdout
// and exits with the given code.
func writeFakeClaude(t *testing.T, dir string, jsonOutput string, exitCode int) string {
	t.Helper()
	path := filepath.Join(dir, "fake-claude")
	script := "#!/bin/sh\n"
	script += "cat <<'FAKEJSON'\n" + jsonOutput + "\nFAKEJSON\n"
	if exitCode != 0 {
		script += "exit " + itoa(exitCode) + "\n"
	}
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	return path
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}

func TestRunSuccess(t *testing.T) {
	dir := t.TempDir()
	fakeClaude := writeFakeClaude(t, dir, `{
		"type": "result",
		"subtype": "success",
		"is_error": false,
		"result": "hello world",
		"total_cost_usd": 0.05,
		"session_id": "abc-123",
		"num_turns": 1,
		"duration_ms": 1000,
		"stop_reason": "end_turn"
	}`, 0)

	rt := &Runtime{Command: fakeClaude}
	resp, err := rt.Run(context.Background(), runtime.Request{
		Prompt: "say hello",
		Options: map[string]string{
			"allowedTools":       "Bash,Read",
			"appendSystemPrompt": "be nice",
			"permissionMode":     "dontAsk",
			"maxBudgetUsd":       "1.00",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Text != "hello world" {
		t.Errorf("got text %q, want %q", resp.Text, "hello world")
	}
	if resp.CostUSD != 0.05 {
		t.Errorf("got cost %v, want 0.05", resp.CostUSD)
	}
}

func TestRunBudgetExceeded(t *testing.T) {
	dir := t.TempDir()
	fakeClaude := writeFakeClaude(t, dir, `{
		"type": "result",
		"subtype": "error_max_budget_usd",
		"is_error": false,
		"total_cost_usd": 2.50,
		"session_id": "abc-456",
		"num_turns": 3,
		"duration_ms": 5000,
		"stop_reason": "end_turn"
	}`, 0)

	rt := &Runtime{Command: fakeClaude}
	_, err := rt.Run(context.Background(), runtime.Request{Prompt: "do stuff"})
	if err == nil {
		t.Fatal("expected error for budget exceeded")
	}
	if !strings.Contains(err.Error(), "budget exceeded") {
		t.Errorf("error should mention budget exceeded, got: %v", err)
	}
}

func TestRunCLIError(t *testing.T) {
	dir := t.TempDir()
	fakeClaude := writeFakeClaude(t, dir, `{
		"type": "result",
		"subtype": "error",
		"is_error": true,
		"result": "something went wrong",
		"total_cost_usd": 0.01,
		"session_id": "abc-789",
		"num_turns": 1,
		"duration_ms": 100,
		"stop_reason": "end_turn"
	}`, 0)

	rt := &Runtime{Command: fakeClaude}
	_, err := rt.Run(context.Background(), runtime.Request{Prompt: "fail"})
	if err == nil {
		t.Fatal("expected error for is_error=true")
	}
	if !strings.Contains(err.Error(), "CLI returned error") {
		t.Errorf("error should mention CLI returned error, got: %v", err)
	}
}

func TestRunNonZeroExit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fake-claude")
	script := "#!/bin/sh\necho 'some error' >&2\nexit 1\n"
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}

	rt := &Runtime{Command: path}
	_, err := rt.Run(context.Background(), runtime.Request{Prompt: "crash"})
	if err == nil {
		t.Fatal("expected error for non-zero exit")
	}
	if !strings.Contains(err.Error(), "claude CLI failed") {
		t.Errorf("error should mention CLI failed, got: %v", err)
	}
}

func TestRunContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	dir := t.TempDir()
	fakeClaude := writeFakeClaude(t, dir, `{"type":"result","subtype":"success","is_error":false,"result":"ok","total_cost_usd":0.01}`, 0)

	rt := &Runtime{Command: fakeClaude}
	_, err := rt.Run(ctx, runtime.Request{Prompt: "cancelled"})
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}

func TestRunMalformedJSON(t *testing.T) {
	dir := t.TempDir()
	fakeClaude := writeFakeClaude(t, dir, `not json at all`, 0)

	rt := &Runtime{Command: fakeClaude}
	_, err := rt.Run(context.Background(), runtime.Request{Prompt: "bad json"})
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if !strings.Contains(err.Error(), "failed to parse") {
		t.Errorf("error should mention parse failure, got: %v", err)
	}
}
