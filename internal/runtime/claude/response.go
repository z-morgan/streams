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
// # Stream JSON Format (claude -p --verbose --output-format stream-json)
//
// The CLI emits NDJSON (one JSON object per line). Requires --verbose flag.
// With --include-partial-messages, key event types:
//
//	type "stream_event" — wraps raw API events (content_block_delta, etc.) in .event field
//	type "assistant"    — complete message with content blocks (emitted after each turn)
//	type "result"       — final result, same fields as json format
//
// Without --include-partial-messages, only "assistant" and "result" events are emitted.
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
//
// With --include-partial-messages, most events have type "stream_event" wrapping
// raw Claude API streaming events in the Event field. Without it, complete
// assistant messages arrive as type "assistant". The final event is type "result".
type streamEvent struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype,omitempty"`

	// Present for type "stream_event" (with --include-partial-messages)
	Event json.RawMessage `json:"event,omitempty"`

	// Present for type "assistant" (complete message, without --include-partial-messages)
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

// apiStreamEvent is a raw Claude API streaming event (nested inside stream_event).
type apiStreamEvent struct {
	Type         string       `json:"type"`
	ContentBlock contentBlock `json:"content_block,omitempty"`
	Delta        apiDelta     `json:"delta,omitempty"`
}

// apiDelta represents an incremental content update from the API.
type apiDelta struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}
