package runtime

import "context"

// Request holds the prompt and runtime-specific options for a single invocation.
type Request struct {
	Prompt    string
	SessionID string            // pre-assigned session ID; passed as --session-id to Claude CLI
	Options   map[string]string // runtime-specific flags (allowedTools, appendSystemPrompt, maxBudgetUsd)
	OnOutput  func(line string) // optional callback for live streaming output lines
}

// Response holds the result of a single runtime invocation.
type Response struct {
	Text      string
	CostUSD   float64
	SessionID string
}

// Runtime is the interface for executing prompts. Each invocation starts a fresh
// session. The returned Response includes the session ID for resume support.
// Context cancellation handles interruption.
type Runtime interface {
	Run(ctx context.Context, req Request) (*Response, error)
}
