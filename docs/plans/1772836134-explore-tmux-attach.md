# Explore: tmux-based Attach

Branch: `explore/tmux-attach`

## Problem

The current attach flow requires: (1) manually stopping the stream, (2) waiting for `session_id` to become available. The user wants to drop from the inspect view directly into the Claude Code TUI while the stream is running.

## Why "pure tmux" doesn't work

Running Claude interactively in tmux and controlling it via `tmux send-keys` is impractical:
- Implement prompts are thousands of characters (task context, beads steps, system rules) — can't reliably pipe through `send-keys`
- Detecting "Claude is idle" by parsing tmux pane output is fragile
- Lose structured JSON output needed for convergence checks, cost tracking, gate evaluation

## Viable hybrid: pre-assigned sessions + tmux attach

Key CLI insight: `--session-id <uuid>` pre-assigns a session ID, and `--resume <uuid>` works with both `-p` (programmatic) and interactive mode. You can alternate between modes on the same session.

### Architecture

```
Normal loop (unchanged):
  claude -p --session-id <uuid> --output-format stream-json ...
  → structured JSON, loop makes decisions

Attach flow (new):
  1. Auto-pause loop (cancel context, wait for claude -p to exit)
  2. tmux new-session -d -s streams-<id> claude --resume <uuid>
  3. tea.ExecProcess → tmux attach-session -t streams-<id>
  4. User interacts with Claude TUI (sees full history from automated runs)
  5. User detaches (ctrl+b d) or exits Claude → returns to streams TUI
  6. Prompt: restart loop? (y/n)
```

### Benefits of tmux over raw tea.ExecProcess

- **Detach/reattach**: `ctrl+b d` returns to streams without killing Claude; can re-attach later
- **Persistence**: tmux session survives even if streams TUI crashes
- **Multi-stream**: could have interactive Claude sessions running across multiple streams simultaneously
- **Session history**: interactive Claude shows all history from automated `-p` runs

### Changes required

#### Step 1: Pre-assign session IDs
- Generate UUID when creating a stream
- Add `SessionID` field to `runtime.Request`
- Pass `--session-id` on first call, `--resume` on subsequent calls
- Session ID available instantly (no waiting)

#### Step 2: Add orchestrator interrupt
- `Interrupt(streamID) (sessionID string, err error)` — cancels loop context, waits for goroutine, returns session ID
- Needs a `sync.WaitGroup` or done channel per running stream

#### Step 3: tmux attach in UI
- Press `a` in detail view:
  - If running: call `orch.Interrupt()`, then launch tmux session
  - If stopped/paused with session_id: launch tmux session directly
- `tmux new-session -d -s streams-<id> claude --resume <uuid>`
- `tea.ExecProcess(tmux attach-session -t streams-<id>)`
- On return: show restart prompt

#### Step 4: Cleanup
- Kill tmux session when stream is deleted
- Kill tmux session when stream loop restarts (can't have both)

### Open questions

1. Should we reuse the same session across implement + review, or keep them separate? Currently each `rt.Run()` is a fresh session. Reusing would give richer context but risks prompt confusion.
2. Should tmux be a hard dependency or gracefully degrade to `tea.ExecProcess` when tmux is unavailable?
3. When the user interacts during attach and makes changes, how does the loop handle the modified state on resume?
