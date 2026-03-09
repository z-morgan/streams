# Plan: Delete Stream from TUI

## Context
Completed streams remain in the dashboard with no way to remove them. Users need a way to clean up finished streams from the list.

## Approach
Add a `d` keybinding to the dashboard that deletes the selected stream after a confirmation prompt. Deletion removes the stream data from disk and memory but leaves the git branch and beads issue intact (user is informed of this).

## Changes

### 1. Store: Add `Delete` method
**File:** `internal/store/store.go`
- Add `Delete(id string) error` — removes `<root>/streams/<id>/` directory via `os.RemoveAll`

### 2. Orchestrator: Add `Delete` method
**File:** `internal/orchestrator/orchestrator.go`
- Add `Delete(id string) error`:
  - Return error if stream is currently running (`o.cancels[id]` exists)
  - Remove git worktree: `git worktree remove <worktree-path> --force`
  - Call `store.Delete(id)`
  - Remove from `o.streams`, `o.snaps` maps

### 3. TUI: Add delete keybinding + confirmation overlay
**File:** `internal/ui/app.go`
- Add `showDeleteConfirm bool` and `deleteTargetID string` to `Model`
- Add `streamDeletedMsg` type (like `streamCreatedMsg`)
- In `updateDashboard`: handle `d` key — if stream selected and not running, set `showDeleteConfirm = true`; if running, show status "Stop the stream first"
- In `Update`: handle `showDeleteConfirm` overlay before other overlays
  - `y` → call `orch.Delete` in a command, close overlay
  - `n` / `esc` → close overlay
- Handle `streamDeletedMsg` — clear state, clamp cursor

**File:** `internal/ui/app.go` (rendering)
- In `View()`: render delete confirmation overlay when `showDeleteConfirm` is true
- Add `renderDeleteConfirmOverlay` — shows stream name and message: "The git branch and beads issue will remain intact for manual cleanup."

**File:** `internal/ui/dashboard.go`
- Update `dashboardHelp` to include `d: delete`

## Verification
1. `go build ./...` — compiles
2. `go test ./...` — existing tests pass
3. tmux TUI test: create a stream, stop it, press `d`, confirm with `y`, verify it disappears from the list
4. Verify pressing `d` on a running stream shows the "stop first" message
5. Verify `n`/`esc` cancels the confirmation
