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
	if resp.SessionID != "abc-123" {
		t.Errorf("got session_id %q, want %q", resp.SessionID, "abc-123")
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

func TestRunStreamingTextDeltas(t *testing.T) {
	dir := t.TempDir()
	// Simulate stream-json output with stream_event lines followed by a result.
	ndjson := `{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"Hello "}}}
{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"world\n"}}}
{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"Second line"}}}
{"type":"stream_event","event":{"type":"content_block_stop"}}
{"type":"result","subtype":"success","is_error":false,"result":"Hello world\nSecond line","total_cost_usd":0.03,"session_id":"s-1","num_turns":1,"duration_ms":500}`

	fakeClaude := writeFakeClaude(t, dir, ndjson, 0)

	var lines []string
	rt := &Runtime{Command: fakeClaude}
	resp, err := rt.Run(context.Background(), runtime.Request{
		Prompt:   "say hello",
		OnOutput: func(line string) { lines = append(lines, line) },
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Text != "Hello world\nSecond line" {
		t.Errorf("got text %q, want %q", resp.Text, "Hello world\nSecond line")
	}
	if resp.SessionID != "s-1" {
		t.Errorf("got session_id %q, want %q", resp.SessionID, "s-1")
	}
	if len(lines) != 2 {
		t.Fatalf("got %d output lines, want 2: %v", len(lines), lines)
	}
	if lines[0] != "Hello world" {
		t.Errorf("line[0] = %q, want %q", lines[0], "Hello world")
	}
	if lines[1] != "Second line" {
		t.Errorf("line[1] = %q, want %q", lines[1], "Second line")
	}
}

func TestRunStreamingToolUse(t *testing.T) {
	dir := t.TempDir()
	ndjson := `{"type":"stream_event","event":{"type":"content_block_start","content_block":{"type":"tool_use","name":"Read","input":{"file_path":"/tmp/test.go"}}}}
{"type":"stream_event","event":{"type":"content_block_stop"}}
{"type":"result","subtype":"success","is_error":false,"result":"done","total_cost_usd":0.02,"session_id":"s-2","num_turns":1,"duration_ms":300}`

	fakeClaude := writeFakeClaude(t, dir, ndjson, 0)

	var lines []string
	rt := &Runtime{Command: fakeClaude}
	resp, err := rt.Run(context.Background(), runtime.Request{
		Prompt:   "read file",
		OnOutput: func(line string) { lines = append(lines, line) },
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Text != "done" {
		t.Errorf("got text %q, want %q", resp.Text, "done")
	}
	if resp.SessionID != "s-2" {
		t.Errorf("got session_id %q, want %q", resp.SessionID, "s-2")
	}
	if len(lines) != 1 {
		t.Fatalf("got %d output lines, want 1: %v", len(lines), lines)
	}
	if lines[0] != "> Read /tmp/test.go" {
		t.Errorf("line[0] = %q, want %q", lines[0], "> Read /tmp/test.go")
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

func TestRunStreamingScannerError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fake-claude")
	// Output a single line > 1MB to exceed the scanner's buffer limit,
	// triggering bufio.ErrTooLong.
	script := "#!/bin/sh\ndd if=/dev/zero bs=1100000 count=1 2>/dev/null | tr '\\0' 'a'\n"
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}

	rt := &Runtime{Command: path}
	_, err := rt.Run(context.Background(), runtime.Request{
		Prompt:   "overflow",
		OnOutput: func(line string) {},
	})
	if err == nil {
		t.Fatal("expected error for scanner buffer overflow")
	}
	if !strings.Contains(err.Error(), "scanner") {
		t.Errorf("error should mention scanner, got: %v", err)
	}
}

func TestRunStreamingMissingResultFallback(t *testing.T) {
	dir := t.TempDir()
	// Stream events without a final result event, exit 0.
	ndjson := `{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"work done\n"}}}
{"type":"stream_event","event":{"type":"content_block_stop"}}`

	fakeClaude := writeFakeClaude(t, dir, ndjson, 0)

	var lines []string
	rt := &Runtime{Command: fakeClaude}
	resp, err := rt.Run(context.Background(), runtime.Request{
		Prompt:    "do work",
		SessionID: "existing-session-123",
		OnOutput:  func(line string) { lines = append(lines, line) },
	})
	if err != nil {
		t.Fatalf("expected degraded response, got error: %v", err)
	}
	if resp.SessionID != "existing-session-123" {
		t.Errorf("got session_id %q, want %q", resp.SessionID, "existing-session-123")
	}
	// The streamed text should still have been delivered via the callback.
	if len(lines) == 0 {
		t.Error("expected at least one output line from streaming")
	}
}
