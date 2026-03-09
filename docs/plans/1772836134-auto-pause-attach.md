# Auto-pause Attach

Branch: `auto-pause-attach` (off main)

## Problem

Attaching to a stream's Claude session has two friction points:
1. Must manually stop the stream before attaching
2. Must wait for `session_id` to become available (first `claude -p` call must complete)

## Solution

When the user presses `a`, automatically pause the loop, wait for the goroutine to finish, and immediately launch `claude --resume <session_id>` via `tea.ExecProcess`. On return, prompt to restart.

## Steps

### Step 1: Add done channel + Interrupt method to orchestrator

The orchestrator needs a way to cancel a loop AND wait for it to finish. Currently `Stop()` fires-and-forgets the cancel.

- Add `done map[string]chan struct{}` to Orchestrator
- In `Start()`, create a done channel and `close(done)` when the goroutine exits
- Add `Interrupt(id string) error` that cancels the context and blocks on `<-done`
- `Stop()` stays as-is (fire-and-forget cancel for the "x" key)

### Step 2: Pre-assign session IDs

Eliminate the "no session_id yet" delay by generating a UUID at stream creation and passing it to the first `claude -p` call.

- `uuid.New()` in `orchestrator.Create()`, stored as `stream.SessionID`
- Add `SessionID` field to `runtime.Request`
- In `claude.Runtime.Run()`, pass `--session-id <uuid>` when `req.SessionID` is set
- In `loop.Run()`, set `req.SessionID = s.GetSessionID()` on the implement request
- After the first call, subsequent calls can use `--resume` instead (or keep using `--session-id` — same effect)

Dependency: `github.com/google/uuid` (or use `crypto/rand` to avoid the dep)

### Step 3: Change attach handler in UI

Replace the current `a` key handler in `updateDetail`:

```
Current:
  if running → "Stop the stream first"
  if no session_id → "No session ID yet"
  else → tea.ExecProcess(claude --resume)

New:
  if running → show "Pausing..." status, fire Interrupt as tea.Cmd
  on interruptDoneMsg → launch tea.ExecProcess(claude --resume)
  if not running + has session_id → launch tea.ExecProcess directly
  if not running + no session_id → "No session ID yet" (shouldn't happen with Step 2)
```

New message types:
- `interruptDoneMsg{sessionID string, err error}`
- `claudeExitMsg` already exists

### Step 4: Restart prompt after attach exit

When `claudeExitMsg` is received:
- If the stream was auto-paused (not manually stopped before attach), show an overlay: "Restart stream? (y/n)"
- `y` → `orch.Start(id)`
- `n` → back to detail view, stream stays paused

Track this with a `attachedFromRunning bool` field on the Model.

### Step 5: Handle edge cases

- **Interrupt timeout**: If the loop goroutine doesn't finish within ~10s (e.g., Claude call hangs), show an error and don't attach. Consider sending SIGKILL to the claude process via `cmd.Cancel` in the context.
- **Attach while creating**: If the stream is in "Created" status with no session, disable `a`.
- **Multiple rapid attach presses**: Ignore `a` if already interrupting.

## Changes by file

| File | Change |
|---|---|
| `internal/orchestrator/orchestrator.go` | Add `done` map, `Interrupt()` method, wire done channel in `Start()` |
| `internal/runtime/runtime.go` | Add `SessionID` to `Request` |
| `internal/runtime/claude/claude.go` | Pass `--session-id` when `req.SessionID` is set |
| `internal/loop/loop.go` | Set `req.SessionID` from stream on implement requests |
| `internal/stream/stream.go` | No changes (SessionID field already exists) |
| `internal/ui/app.go` | New message types, updated `a` handler, restart prompt overlay |
| `internal/orchestrator/orchestrator.go` | Generate UUID in `Create()` |
| `go.mod` | Maybe add `github.com/google/uuid` |
