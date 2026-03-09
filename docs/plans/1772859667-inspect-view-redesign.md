# Inspect View Redesign: Iteration-Based Two-Pane Layout

## Goal

Replace the current snapshot-centric detail view with an iteration-centric two-pane layout. The left pane lists iterations (completed + in-progress); the right pane shows snapshot details for completed iterations or a live tail view for the in-progress iteration. This subsumes the standalone tail view (`viewTail`), which gets removed.

## Data Model

An "iteration" is either:
- **Completed**: backed by a `Snapshot` in `st.GetSnapshots()` (has `Phase`, `Iteration`, `Summary`, etc.)
- **In-progress**: a virtual row derived from `st.GetIteration()` + `currentPhase(st)` when stream status is `Running` or `Paused` (mid-iteration, no snapshot yet)

The iteration list is: all snapshots (in order) + optionally one in-progress row at the bottom.

## Steps

### Step 1: Restructure `detailView` and build iteration list model

Replace `snapCursor` with an iteration-aware cursor. Add a helper that builds a unified iteration list from snapshots + in-progress state.

**Files**: `internal/ui/detail.go`

- Define an `iterationRow` struct: `{Phase string, Iteration int, IsInProgress bool, IsPaused bool, SnapshotIndex int}` — `SnapshotIndex` is -1 for in-progress rows.
- Add `buildIterationList(st *stream.Stream) []iterationRow` that:
  - Appends one row per snapshot from `st.GetSnapshots()`
  - If the stream is `Running`, appends an in-progress row for the current iteration
  - If the stream is `Paused` and current iteration has no matching snapshot (paused mid-iteration), appends a paused in-progress row
- Update `detailView` to hold `iterCursor int` (replacing `snapCursor`), plus `tailScroll int` for future use.

### Step 2: Render the left pane as iteration rows

Replace `renderSnapshotList` with `renderIterationList`.

**Files**: `internal/ui/detail.go`

- Each row shows: `plan 1`, `coding 2`, etc. Completed rows show phase + iteration number (1-indexed for display). Error rows get a `!` marker.
- In-progress row shows: `> coding 3...` (running) or `coding 3 (paused)` (paused mid-iteration).
- Highlighted row uses `snapshotSelectedStyle`, others use `snapshotNormalStyle`.

### Step 3: Render the right pane conditionally

Update the right pane to show either snapshot details or tail output based on the selected iteration.

**Files**: `internal/ui/detail.go`, `internal/ui/tail.go`

- If `iterationRow.IsInProgress == false`: render `renderSnapshotDetail` as today (using `SnapshotIndex`).
- If `iterationRow.IsInProgress == true`: render the tail view inline (auto-follow, no scroll controls yet). Extract the tail rendering logic from `renderTail` into a reusable `renderTailContent(st, width, height) string` that shows the last N lines of output.
- For paused mid-iteration: show the output buffer (frozen) with a "(paused)" indicator.

### Step 4: Wire up `renderDetail` with the new model

Replace the current `renderDetail` function body.

**Files**: `internal/ui/detail.go`

- Call `buildIterationList` to get iteration rows.
- If no rows exist, show "Waiting for output..." (stream just started, no output yet).
- Otherwise render the two-pane layout with `renderIterationList` (left) and the conditional right pane.
- Update help text to remove `t: tail` since tailing is integrated.

### Step 5: Update cursor logic in `updateDetail`

**Files**: `internal/ui/app.go`

- j/k navigates the iteration list (clamp to iteration count, not snapshot count).
- Remove the `t` keybinding (no standalone tail view).
- When entering the detail view from the dashboard, default cursor to the last iteration (which may be in-progress).
- Cursor stays on the same iteration index when new iterations appear — the list grows below the cursor naturally when the cursor is on a completed iteration, or the in-progress row moves down while the cursor stays put.

### Step 6: Remove standalone tail view

**Files**: `internal/ui/app.go`, `internal/ui/tail.go`, `internal/ui/styles.go`

- Remove `viewTail` from the `view` enum.
- Remove `tailView` struct and `tail` field from `Model`.
- Remove `updateTail` method and its case in `Update`.
- Remove the `viewTail` case from `View()`.
- Keep `tail.go` but repurpose it: it now only exports `renderTailContent` (the reusable function from Step 3). Or inline it into `detail.go` and delete `tail.go`.
- `toolLineStyle` in styles.go is still used by the tail content renderer, so keep it.

### Step 7: Add focus switching (Enter key)

**Files**: `internal/ui/detail.go`, `internal/ui/app.go`

- Add `focusRight bool` to `detailView`.
- When `focusRight` is false (default): j/k navigate iterations, Enter sets `focusRight = true` (only when in-progress iteration is selected — for completed iterations, Enter could be a no-op or toggle expanded view later).
- When `focusRight` is true: j/k scroll the tail output (reintroduce `scrollOffset`), Esc sets `focusRight = false`.
- Visual indicator: dim the left pane or highlight the right pane border when focused right.

## Commit Plan

1. Steps 1-4: "Redesign inspect view with iteration-based two-pane layout"
2. Steps 5-6: "Remove standalone tail view, wire iteration navigation"
3. Step 7: "Add focus switching between iteration list and detail pane"
