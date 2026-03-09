# Context-Aware Agent Handoff (Implement Continuations)

## Context

When a stream's implement step runs, the Claude agent may fill its context window before completing all assigned work. Claude Code handles this internally with auto-compaction (~83.5% threshold), but quality degrades post-compaction. The streams orchestrator has no visibility into this — it treats every implement completion identically and proceeds to review regardless.

This feature adds token-level awareness to the loop. When the implement step ends near context capacity with remaining work, it automatically re-runs implement in a fresh session with a handoff summary, rather than proceeding to review of incomplete work. This is a "continuation" — same logical iteration, fresh context window.

## Research Findings

The Claude CLI JSON result already includes a `usage` object (currently ignored by streams) with per-model token breakdowns: `inputTokens`, `outputTokens`, `cacheCreationTokens`, `cacheReadTokens`, `totalTokens`, `contextWindow`, `maxOutputTokens`. The ratio `totalTokens / contextWindow` gives context utilization.

Other frameworks' approaches:
- **OpenHands**: Client-side condenser with LLM summarization (head/tail preservation)
- **Claude Code**: Server-side auto-compaction at ~83.5%, configurable via env var
- **CrewAI**: Boolean `respect_context_window` with automatic summarization
- **LangGraph**: Manual `trim_messages` — no built-in compaction

The streams approach is different: rather than compacting within a session, we detect high utilization and start a fresh session with an explicit handoff. This preserves full context quality in the new session.

## Design

### Detection

After each implement invocation, check `totalTokens / contextWindow`. If the ratio exceeds a configurable threshold (default 0.45), AND there are still open beads remaining, trigger a continuation.

45% is well below Claude Code's compaction threshold (~83.5%). Triggering early gives the fresh session maximum room to work, and avoids the quality degradation that comes with compaction.

### Continuation Flow

```
implement step
  └─ context capacity reached? (usage ratio > threshold AND open beads remain)
      YES → fresh implement session with handoff context (loop, max 3)
      NO  → proceed to autosquash → review → checkpoint
```

A continuation does NOT increment the iteration counter. It's the same logical iteration continuing in a fresh session. The snapshot aggregates costs, commits, and beads across all continuation passes.

### Handoff Prompt

The continuation gets the same implement prompt, plus an appended "Continuation Context" section (same pattern as guidance injection). The handoff includes:
- List of beads already closed
- Commits already made (SHAs)
- Previous agent's summary text

### ConvergeASAP Interaction

If ConvergeASAP is set, skip continuations — the user wants to wrap up fast.

## Implementation Steps

### Step 1: Parse token usage from CLI response

**Files:** `internal/runtime/runtime.go`, `internal/runtime/claude/response.go`, `internal/runtime/claude/claude.go`

Add `Usage` struct to runtime package:

```go
type Usage struct {
    InputTokens   int
    OutputTokens  int
    TotalTokens   int
    ContextWindow int
}
```

Add `NumTurns int` and `Usage Usage` to `runtime.Response`.

Add usage fields to `cliResult` and `streamEvent` in response.go. Propagate in both `runJSON` and `runStreaming` paths in claude.go.

**Note:** Validate the exact `usage` field structure by running `claude -p --output-format json` once during implementation. The research indicates it's a flat object on the result, but this needs confirmation.

### Step 2: Add token usage and continuation tracking to Snapshot

**Files:** `internal/stream/snapshot.go`

```go
type TokenUsage struct {
    TotalTokens   int `json:"total_tokens"`
    ContextWindow int `json:"context_window"`
}
```

Add to `Snapshot`: `Continuations int` and `TokenUsage *TokenUsage` (pointer for backward-compatible JSON serialization).

### Step 3: Add config keys

**Files:** `internal/config/config.go`

New fields on `Config`:
- `ContextHandoffThreshold *float64` — ratio threshold (default 0.45, 0 = disabled)
- `MaxContinuations *int` — max per iteration (default 3)

Add parsing for `context-handoff-threshold` and `max-continuations` keys. Update `Defaults()`, `merge()`, `WriteFile()`, `Format()`, `ValidKeys()`.

### Step 4: Implement continuation loop in loop.go

**Files:** `internal/loop/loop.go`

Replace the `maxIterations int` parameter in `Run()` with a config struct:

```go
type LoopConfig struct {
    MaxIterations           int
    ContextHandoffThreshold float64
    MaxContinuations        int
}
```

Wrap the implement step in a continuation loop. Pseudocode:

```
continuations := 0
var accCost float64
var accClosed, accCommits []string
var lastSummary string
var lastUsage runtime.Usage

for {
    prompt := phase.ImplementPrompt(pctx)
    if continuations > 0 {
        prompt = appendHandoffSection(prompt, lastSummary, accClosed, accCommits)
    } else if len(pendingGuidance) > 0 {
        prompt = appendGuidanceSection(prompt, pendingGuidance)
    }

    resp := rt.Run(ctx, buildRequest(prompt, phase.ImplementTools()))
    accCost += resp.CostUSD
    lastSummary = resp.Text
    lastUsage = resp.Usage

    // track beads closed and commits this pass
    newClosed := setDiff(idsBefore, idsAfterImpl)
    accClosed = append(accClosed, newClosed...)
    newCommits := git.CommitsBetween(headBefore, headAfterImpl)
    accCommits = append(accCommits, newCommits...)

    // update baselines for next pass
    idsBefore = idsAfterImpl
    headBefore = headAfterImpl

    if !shouldContinue(lastUsage, threshold, continuations, maxContinuations, s.GetConvergeASAP()) {
        break
    }
    continuations++
    s.AppendOutput("[streams] Context capacity reached, continuing in fresh session...")
}
```

`shouldContinue` returns true when: threshold > 0, count < max, usage ratio >= threshold, and ConvergeASAP not set.

The existing no-progress check (`iteration > 0 && len(beadsClosed) == 0`) uses `accClosed` (aggregated across continuations).

Add `appendHandoffSection()` function (same pattern as `appendGuidanceSection`).

Update snapshot construction to use accumulated values and set `Continuations` and `TokenUsage`.

### Step 5: Thread config through orchestrator

**Files:** `internal/orchestrator/orchestrator.go`, `cmd/streams/main.go`

Add fields to orchestrator config, pass to `loop.Run` via `LoopConfig`. Wire from `config.Config` in main.go.

### Step 6: Display in TUI

**Files:** `internal/ui/detail.go` or `internal/ui/app.go` (wherever snapshot rendering lives)

In iteration/snapshot display, show context utilization as a percentage when `TokenUsage` is present. If `Continuations > 0`, show indicator like `(x3)` meaning 3 total runs.

## Verification

1. `go build ./...` — compiles
2. `go test ./...` — existing tests pass
3. Unit test: mock runtime returns high usage → continuation triggered
4. Unit test: max continuations respected
5. Unit test: threshold=0 disables feature
6. Unit test: ConvergeASAP skips continuations
7. tmux TUI test: run a stream, verify token usage appears in detail view
8. Manual: run a real stream with a large task, observe continuation behavior in logs
