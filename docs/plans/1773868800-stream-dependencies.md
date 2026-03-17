# Stream Dependencies (Blocker Queue)

## Problem

When multiple streams need the same resource (e.g., a shared git worktree base, a test database, a container port), the user must manually pause one and resume it when the other finishes. This is tedious and error-prone.

## Solution

Add the ability to mark streams as "blocked by" other streams. When all blocking streams stop running (paused, stopped, or completed), the dependent stream automatically starts.

## Design

### Data Model

Add `BlockedBy []string` to `Stream` — a list of stream IDs whose completion/pause unblocks this stream.

### Auto-Start Logic

Lives in the orchestrator. After any stream's loop goroutine exits (converge, error, or stop), iterate all streams. For each one with a non-empty `BlockedBy` list that is not already running/completed: if none of its blockers are currently running, auto-start it.

### UI

From the dashboard, press `b` to open a blocker picker overlay for the selected stream. The overlay shows all *other* streams as a multi-select checklist. Currently-set blockers are pre-checked. Space to toggle, enter to confirm, esc to cancel.

The dashboard list shows a "Queued" indicator (with blocker count) when a stream has active blockers preventing it from running.

---

## Steps

### 1. Stream model — add `BlockedBy` field

**`internal/stream/stream.go`**:
- Add `BlockedBy []string` field to `Stream` struct
- Add `SetBlockedBy(ids []string)` and `GetBlockedBy() []string` thread-safe accessors

### 2. Store — persist `BlockedBy`

**`internal/store/store.go`**:
- Add `BlockedBy []string` to `streamData` JSON struct
- Wire `toStreamData` and `fromStreamData`

### 3. Orchestrator — auto-start dependents when blockers resolve

**`internal/orchestrator/orchestrator.go`**:
- Add `SetBlockedBy(id string, blockerIDs []string)` method (validates IDs, delegates to stream, checkpoints)
- Add private `checkDependents()` method: for each stream with `BlockedBy`, if not running/completed and all blockers not running, auto-start it
- Call `checkDependents()` at the end of the loop goroutine (after the stream finishes for any reason), right before cleaning up `cancels`/`done`
- Also expose `ActiveBlockers(id string) []string` — returns the subset of BlockedBy IDs that are currently running (for UI display)

### 4. Keybindings — register `b` on dashboard

**`internal/ui/keybindings.go`**:
- Add `{Key: "b", Action: "blockers", Scope: ScopeDashboard}` and `ScopeDashboardChannels` entries

### 5. UI — blocker picker overlay

**`internal/ui/app.go`**:
- Add overlay state fields: `showBlockers bool`, `blockerChecked map[string]bool`, `blockerCursor int`, `blockerStreamIDs []string`
- `updateBlockers(msg)` handler: j/k navigate, space toggle, enter confirm (calls `orch.SetBlockedBy`), esc cancel
- `renderBlockersOverlay(...)`: centered overlay listing other streams with checkboxes
- Wire into `updateDashboard` on `b` key: populate state from current `BlockedBy`, open overlay
- Wire into `viewString()` and `Update()` dispatch

### 6. Dashboard — show blocked/queued indicator

**`internal/ui/dashboard.go`**:
- Update `statusIndicator()` and `statusLabel()`: when a stream has active blockers (check via orchestrator), show "Queued (N)" instead of the raw status
- Pass orchestrator reference or active-blocker info into the render functions

### 7. Tests

- Stream model: getter/setter round-trip
- Store: serialize/deserialize `BlockedBy`
- Orchestrator: `checkDependents` starts a stream when all blockers resolve
