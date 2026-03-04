package runtime

import "context"

// Request holds the prompt and runtime-specific options for a single invocation.
type Request struct {
	Prompt  string
	Options map[string]string // runtime-specific flags (allowedTools, appendSystemPrompt, maxBudgetUsd)
}

// Response holds the result of a single runtime invocation.
type Response struct {
	Text    string
	CostUSD float64
}

// Runtime is the interface for executing prompts. Each invocation starts a fresh
// session — no session ID tracking. Context cancellation handles interruption.
type Runtime interface {
	Run(ctx context.Context, req Request) (*Response, error)
}
