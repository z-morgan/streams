# Always-Available Revise

## Problem

The `r` (revise) shortcut only appears when a stream is paused at the review phase (`isPausedAtReview`). Users should be able to revise from any state — paused at any phase, or even while an iteration is running.

## Design

### Two modes depending on stream state

**Paused/stopped streams** — revise works immediately, same as today. The only change is removing the review-phase gate so it works at any paused phase.

**Running streams** — revise is *queued*. The loop picks it up between iterations (at the StepGuidance checkpoint), applies the phase reset, and continues from the target phase. A UI indicator shows the pending revise beneath the running iteration row.

### Why a queue instead of stop-then-revise?

Stopping a running agent mid-iteration leaves partial work (uncommitted changes, half-written files). By queuing, the current iteration finishes cleanly — its snapshot is recorded — and *then* the loop rewinds. This is consistent with how `ConvergeASAP` and `DrainGuidance` already work: set a flag, let the loop consume it at the natural boundary.

## Changes

### 1. Add `PendingRevise` to Stream

Add a new field to `stream.Stream` that stores a queued revise request:

```go
// PendingRevise stores a queued revise request for a running stream.
// The loop checks this between iterations and applies it if set.
type PendingRevise struct {
    TargetPhaseIndex int
    Feedback         string
}
```

On `Stream`:
```go
PendingRevise *PendingRevise
```

Add thread-safe accessors: `SetPendingRevise(*PendingRevise)`, `DrainPendingRevise() *PendingRevise`, `GetPendingRevise() *PendingRevise`.

Include `PendingRevise` in JSON serialization so it survives persistence.

### 2. Update the loop to check for pending revise

In `loop.go`, after the StepGuidance drain (line 330), check `DrainPendingRevise()`. If set:

1. Reset `PipelineIndex` to the target phase
2. Set `Converged = false`, `Iteration = 0`
3. If feedback is provided, add it as guidance
4. Instantiate the new phase via `factory` and replace the current `phase`
5. Clear `pendingGuidance` and `continue` the loop

This mirrors what `Orchestrator.Revise` does today, but inline within the running loop.

### 3. Update `Orchestrator.Revise` to handle running streams

Currently `Revise` returns an error if the stream is running. Change it to:

- **If stopped/paused**: apply the revise immediately (existing behavior), then call `o.Start`
- **If running**: set `PendingRevise` on the stream and return. The loop will pick it up.

Remove the "stream is still running" error.

### 4. Remove the `isPausedAtReview` gate from the UI

In `app.go` line 712, change the `r` key handler from:

```go
if st != nil && isPausedAtReview(st)
```

to:

```go
if st != nil && st.GetStatus() != stream.StatusCompleted && st.GetPipelineIndex() > 0
```

The only requirements are: the stream exists, it isn't completed, and there's at least one earlier phase to revise to.

### 5. Update the revise overlay phase list

Currently the phase picker shows `phaseCount = pipelineIdx` (all phases before current). This stays the same — revising to the *current* phase isn't meaningful (just add guidance instead), and revising forward doesn't make sense.

When the stream is running, the overlay should note that the revise will be applied after the current iteration finishes, not immediately. Add a line of help text below the phase list:

```
(will apply after current iteration completes)
```

### 6. Show pending revise indicator in the iteration list

In `renderIterationList`, after the in-progress (spinner) row, append an indicator row when `st.GetPendingRevise() != nil`:

```
  ↩ revise → research
```

Use `colorWarning` for the arrow icon, dimmed style for the text. This row is not selectable — it's purely informational.

To do this, `buildIterationList` needs access to the pending revise state. Add a new `iterationRow` field:

```go
IsPendingRevise    bool
PendingRevisePhase string
```

In `buildIterationList`, if the stream is running and has a pending revise, append this row after the in-progress row.

In `renderIterationList`, render it as a non-selectable, dimmed row with the revise icon.

### 7. Update help text

In `detailHelpText`:

- Add `r: revise` to the running-stream help line (line 237)
- Add `r: revise` to the default paused help line (line 241) and the force-advance help line (line 239)
- Keep it in the `isPausedAtReview` line (line 232) as-is

### 8. Allow cancelling a pending revise

When a revise is already pending and the user presses `r` again, show the overlay but add an option to cancel the pending revise instead. Or simpler: pressing `esc` from the revise overlay while a revise is pending offers to cancel it.

Simplest approach: if `PendingRevise` is set, the `r` key opens a small confirm overlay: "Revise pending (→ research). Press `r` to change, `esc` to cancel." Pressing `r` reopens the phase picker (replacing the pending revise). Pressing `esc` clears `PendingRevise` and closes.

## Commit sequence

1. **Add `PendingRevise` to Stream** — new field, accessors, JSON tag
2. **Loop consumes pending revise** — drain + apply between iterations
3. **Orchestrator queues revise for running streams** — bifurcate paused vs running paths
4. **UI: remove review gate, show `r` everywhere** — keybinding + help text changes
5. **UI: render pending revise indicator** — iteration list row
6. **UI: cancel pending revise** — `r` on pending shows cancel option
