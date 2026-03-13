# Bead Details in Snapshots

**Goal**: Show bead titles alongside IDs in snapshot detail views, and allow
highlighting individual beads to see their full `bd show` output.

## Current State

- `Snapshot.BeadsClosed` and `BeadsOpened` are `[]string` (bare IDs)
- The detail view renders them as `✓ streams-abc` / `+ streams-xyz`
- The loop already fetches full `beadsChild{ID, Title, Status, Notes}` structs
  from the CLI but discards everything except IDs
- The right pane has no interactive mode for completed snapshots — `enter` only
  focuses live output for in-progress rows

## Design

### Step 1: Store titles in snapshots (data model + loop)

Add a `BeadTitles map[string]string` field to `Snapshot`. This maps bead ID →
title for every bead referenced in `BeadsClosed` or `BeadsOpened`.

**Why a map instead of changing `[]string` to `[]struct`**: Existing
`snapshots.jsonl` files have `[]string` arrays. A new optional map field is
fully backward-compatible — old snapshots unmarshal with a nil map, and the
rendering code falls back to ID-only display.

**Files**:
- `internal/stream/snapshot.go` — add `BeadTitles map[string]string`
- `internal/loop/beads.go` — add `FetchChildTitles(parentID) (map[string]string, error)`
  to `BeadsQuerier` interface, implemented by `CLIBeadsQuerier` using
  `fetchChildren` (which already returns titles)
- `internal/loop/loop.go` — after computing `beadsClosed`/`beadsOpened`, call
  `FetchChildTitles` and populate `snap.BeadTitles`. Since closed beads won't
  appear in a post-implement query, call it *before* implement (where we already
  call `FetchOrderedSteps`) and again after review, then merge the maps.

Simpler alternative: `FetchOrderedSteps` already returns `[]Step{ID, Title}`.
Build a title map from the `steps` slice fetched at the top of the loop (covers
beads that exist before implement → captures closed bead titles). After review,
call `FetchOrderedSteps` again for newly opened beads. Merge maps.

Actually, simplest: add `FetchAllChildTitles` that calls `fetchChildren` without
filtering by status, returning titles for all children (open, closed,
in_progress). Call it once when building the snapshot. One extra CLI call per
iteration, but it's fast and keeps the code simple.

### Step 2: Show titles in snapshot detail (rendering)

In `renderSnapshotDetail` (`internal/ui/detail.go`), change the bead list from:

```
✓ streams-abc
+ streams-xyz
```

to:

```
✓ streams-abc — Fix login validation
+ streams-xyz — Add rate limiting tests
```

When `snap.BeadTitles` is nil (old snapshots), fall back to ID-only display.

Also update `reviewFallback()` to include titles when available.

### Step 3: Bead browse mode (interactive detail)

Add a "bead focus" mode to the detail view, activated by pressing `enter` on a
completed snapshot that has beads.

**New fields on `detailView`**:
- `beadFocused bool` — whether bead-browse mode is active
- `beadCursor int` — index into the combined bead list (closed + opened)
- `beadShowOutput string` — cached `bd show` output for the highlighted bead
- `beadShowLoading bool` — true while fetching

**Interaction flow**:
1. User presses `enter` on a completed snapshot with beads
2. Right pane switches to bead-browse mode: shows the bead list with a
   highlight cursor (similar to the iteration list cursor style)
3. `j`/`k` moves the cursor between beads
4. `enter` on a highlighted bead fires a `tea.Cmd` that runs
   `bd show <id>` and returns the output as a `tea.Msg`
5. The bead's full detail replaces the right pane content
6. `esc` returns: from bead detail → bead list → normal snapshot view

**Key handling changes** (`internal/ui/app.go`):
- In `updateDetailView`, when `beadFocused`:
  - `j`/`k` move `beadCursor`
  - `enter` fires `bd show` command
  - `esc` exits bead focus (or bead detail → bead list)
- When not `beadFocused`, `enter` on a completed snapshot enters bead focus

**Rendering** (`internal/ui/detail.go`):
- New `renderBeadBrowse(snap, dv, width)` function:
  - Shows bead list with cursor highlight
  - When `beadShowOutput` is set, shows the full detail instead
- The right pane title changes to "Beads" when in bead-browse mode

**Help text**: Update `detailHelpText` to show bead-related hints when a
snapshot with beads is selected.

## Commit sequence

1. **Data model + loop**: Add `BeadTitles` to Snapshot, `FetchAllChildTitles`
   to BeadsQuerier, populate in loop. No UI changes yet.
2. **Render titles**: Show titles inline in snapshot detail. Backward-compat
   fallback for old snapshots.
3. **Bead browse mode**: Interactive bead selection with `bd show` detail view.
