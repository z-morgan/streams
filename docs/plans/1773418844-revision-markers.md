# Revision Markers in UI

## Goal

After a revise operation redirects a stream back to an earlier phase, show visual indicators so the user can see where revisions occurred and what prompted them.

**Three locations**:
1. Dashboard streams view (channel iteration rows) — `[revision]` marker
2. Inspect view left panel (iteration list) — `[revision]` marker
3. Inspect view right panel (snapshot detail) — revision context section

## Current State

When a revise executes (`loop.go:332-353`), it:
- Resets `PipelineIndex` to the target phase
- Resets `Iteration` to 0
- Adds the feedback as guidance (if any)
- Instantiates the new phase and continues the loop

**Problem**: No record of the revision survives into the snapshots. The feedback becomes generic guidance, and there's no way to tell that an iteration was the first one after a revision.

## Plan

### Step 1: Add revision metadata to Snapshot

**`internal/stream/snapshot.go`** — Add two fields to the `Snapshot` struct:

```go
ReviseFrom     string // phase name we revised FROM (empty = not a revision)
ReviseFeedback string // user's feedback prompt when requesting the revision
```

These fields are only populated on the first snapshot after a revision. Empty strings mean "this was a normal iteration."

**`internal/store/store.go`** — No changes needed; the store serializes Snapshot fields via JSON tags, and new fields with `omitempty` will be backward-compatible with existing data.

### Step 2: Record revision metadata in the loop

**`internal/loop/loop.go`** (~line 332-353) — When a pending revise is drained:

1. Capture the current phase name *before* resetting (this is `phase.Name()`, the phase we're revising FROM)
2. Capture `pr.Feedback`
3. Store both on the stream via a new method so the next snapshot can pick them up

**`internal/stream/stream.go`** — Add fields + accessors:

```go
// On Stream struct:
reviseFrom     string
reviseFeedback string

// Methods:
func (s *Stream) SetReviseContext(from, feedback string)
func (s *Stream) DrainReviseContext() (from, feedback string)
```

`DrainReviseContext` returns the values and clears them (same pattern as `DrainPendingRevise`).

**Snapshot capture site** (wherever snapshots are created, likely in the loop after an iteration completes) — Call `DrainReviseContext()` and populate the snapshot's `ReviseFrom` and `ReviseFeedback` fields.

### Step 3: Show `[revision]` marker in iteration list (inspect view left panel)

**`internal/ui/detail.go`**

In `buildIterationList` (~line 65-73), when building rows from snapshots, check if `snap.ReviseFrom != ""`. If so, set a new field on `iterationRow`:

```go
// Add to iterationRow struct:
IsRevision bool
```

In `renderIterationList` (~line 362-384), after composing the label (`"phase N"`), append a gray `[revision]` tag:

```go
if row.IsRevision {
    label += " " + lipgloss.NewStyle().Foreground(colorMuted).Render("[revision]")
}
```

This appears on both selected and unselected rows.

### Step 4: Show `[revision]` marker in dashboard channel view

**`internal/ui/dashboard.go`**

In `renderChannel` (~line 225-236), when iterating snapshots, check `snap.ReviseFrom != ""` and append the marker to the label:

```go
if snap.ReviseFrom != "" {
    label += " [revision]"
}
```

The existing `style.Render()` call will handle the coloring. The marker blends into the muted iteration row text.

### Step 5: Show revision context in snapshot detail (inspect view right panel)

**`internal/ui/detail.go`**

In `renderSnapshotDetail` (~line 466, right after the header/HR), add a new section that appears *before* the Implementation Report when the snapshot has revision metadata:

```go
if snap.ReviseFrom != "" {
    b.WriteString(labelStyle.Render("Revision"))
    b.WriteString("\n")
    b.WriteString(fmt.Sprintf("  Revised from: %s\n", snap.ReviseFrom))
    if snap.ReviseFeedback != "" {
        b.WriteString(fmt.Sprintf("  Feedback: %s\n", wrapText(snap.ReviseFeedback, width-2)))
    }
    b.WriteString("\n" + hr + "\n")
}
```

This gives the user full context on what triggered the revision when they select that iteration.

## Summary of Files Changed

| File | Change |
|------|--------|
| `internal/stream/snapshot.go` | Add `ReviseFrom`, `ReviseFeedback` fields |
| `internal/stream/stream.go` | Add `reviseFrom`/`reviseFeedback` fields + `Set`/`Drain` methods |
| `internal/loop/loop.go` | Record revision context when draining pending revise |
| `internal/ui/detail.go` | `IsRevision` on `iterationRow`, `[revision]` in iteration list, revision section in snapshot detail |
| `internal/ui/dashboard.go` | `[revision]` marker in channel iteration rows |

## Commit Plan

1. **Data model**: Add revision fields to `Snapshot` and `Stream`, with `Set`/`Drain` accessors
2. **Loop**: Record revision context when applying a pending revise, populate snapshot fields
3. **UI: iteration lists**: Show `[revision]` marker in both inspect and dashboard views
4. **UI: snapshot detail**: Show revision context section in the right panel
