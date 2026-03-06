// Package claude wraps the Claude Code CLI as a runtime.Runtime implementation.
//
// # JSON Output Format (claude -p --output-format json)
//
// The CLI emits a single JSON object on stdout. Key fields:
//
//	type           string  — always "result"
//	subtype        string  — "success", "error_max_turns", "error_max_budget_usd"
//	is_error       bool    — true if the CLI itself errored
//	result         string  — the assistant's text response (absent on budget/turn errors)
//	total_cost_usd float64 — total cost for this invocation
//	session_id     string  — UUID for the session
//	num_turns      int     — number of conversation turns
//	duration_ms    int     — wall-clock duration
//	duration_api_ms int    — API-only duration
//	stop_reason    string  — "end_turn", "tool_use"
//
// # Stream JSON Format (claude -p --output-format stream-json)
//
// The CLI emits NDJSON (one JSON object per line). Key event types:
//
//	type "system"    — subtype "init", contains session_id
//	type "assistant" — contains message with content blocks (text, tool_use)
//	type "result"    — final result, same fields as json format
//
// # Subtypes
//
//   - "success": normal completion. result field contains the response text.
//   - "error_max_turns": hit --max-turns limit. result field may be absent.
//     Exit code 0. Treated as an error by the runtime.
//   - "error_max_budget_usd": hit --max-budget-usd limit. result field absent.
//     Exit code 0. Treated as a budget error by the runtime.
//
// # Error Detection
//
// Budget errors are detected by subtype == "error_max_budget_usd" (exit code 0,
// so exec.ExitError won't catch them). Runtime errors are detected by non-zero
// exit code OR is_error == true.
package claude

import "encoding/json"

// cliResult is the subset of fields we parse from claude -p --output-format json.
type cliResult struct {
	Type         string  `json:"type"`
	Subtype      string  `json:"subtype"`
	IsError      bool    `json:"is_error"`
	Result       string  `json:"result"`
	TotalCostUSD float64 `json:"total_cost_usd"`
	SessionID    string  `json:"session_id"`
	NumTurns     int     `json:"num_turns"`
	DurationMS   int     `json:"duration_ms"`
	StopReason   string  `json:"stop_reason"`
}

// streamEvent is the envelope for NDJSON events from --output-format stream-json.
type streamEvent struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype,omitempty"`

	// Present for type "assistant"
	Message streamMessage `json:"message"`

	// Present for type "result"
	IsError      bool    `json:"is_error,omitempty"`
	Result       string  `json:"result,omitempty"`
	TotalCostUSD float64 `json:"total_cost_usd,omitempty"`
	SessionID    string  `json:"session_id,omitempty"`
	NumTurns     int     `json:"num_turns,omitempty"`
	DurationMS   int     `json:"duration_ms,omitempty"`
}

type streamMessage struct {
	Content []contentBlock `json:"content"`
}

type contentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}
