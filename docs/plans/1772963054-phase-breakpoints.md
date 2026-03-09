# Phase Breakpoints

## Problem

When a multi-phase stream runs (e.g., research → plan → coding), phases with `TransitionAutoAdvance` silently flow into the next phase. The user has no opportunity to review intermediate artifacts (the research doc, the plan) before the stream plows ahead. Currently, only `CodingPhase` pauses on convergence via `TransitionPause`; everything else auto-advances.

The user wants to place **breakpoints between phases** during stream creation — points where the stream pauses automatically so they can review output, then either continue to the next phase or provide feedback to re-iterate the current phase.

## Design

### Core concept: `Breakpoints` on the Stream

Add a `Breakpoints []int` field to the `Stream` struct. Each entry is a **pipeline index** where the stream should pause *after* that phase converges. This is checked in the loop's convergence/transition logic, overriding `TransitionAutoAdvance`.

Example: pipeline `["research", "plan", "coding"]` with breakpoints `[0, 1]` means:
- After research converges → pause (breakpoint at index 0)
- After plan converges → pause (breakpoint at index 1)
- Coding already pauses via `TransitionPause`

### Resume behavior

When resuming from a breakpoint pause, two options:

1. **Continue** (press `s`): `orch.Start()` picks up at the next pipeline index as usual.
2. **Re-iterate** (press `r` or use guidance `g` then `s`): User provides feedback via guidance, then starts. The loop detects `Converged == true` with pending guidance and resets convergence, re-running the current phase with that feedback injected.

The re-iterate flow reuses the existing guidance mechanism — no new concept needed. The loop just needs to check for pending guidance when resuming a converged phase and reset `Converged` + `Iteration` if guidance is present.

## Implementation Steps

### Step 1: Add `Breakpoints` to Stream model + persistence

**Files:** `internal/stream/stream.go`, `internal/store/store.go`

- Add `Breakpoints []int` field to `Stream` struct (after `PipelineIndex`)
- Add `GetBreakpoints() []int` accessor method
- Add `Breakpoints []int` to `streamData` JSON struct (`json:"breakpoints,omitempty"`)
- Wire through `toStreamData` / `fromStreamData`

### Step 2: Check breakpoints in the loop's convergence transition

**Files:** `internal/loop/loop.go`

In the convergence block (line ~209-232), change the auto-advance condition:

```go
if converged {
    s.SetConverged(true)

    pipeline := s.GetPipeline()
    nextIdx := s.GetPipelineIndex() + 1

    // Check breakpoints before auto-advancing
    hasBreakpoint := false
    for _, bp := range s.GetBreakpoints() {
        if bp == s.GetPipelineIndex() {
            hasBreakpoint = true
            break
        }
    }

    if !hasBreakpoint && phase.TransitionMode() == TransitionAutoAdvance && nextIdx < len(pipeline) {
        // existing auto-advance logic
    }

    s.SetStatus(stream.StatusPaused)
    return
}
```

### Step 3: Handle re-iterate on resume with guidance

**Files:** `internal/orchestrator/orchestrator.go`

In `Start()`, before launching the loop goroutine, check if the stream is converged and has pending guidance. If so, reset convergence state to re-run the current phase:

```go
if st.Converged && st.GetGuidanceCount() > 0 {
    st.SetConverged(false)
    // Don't reset iteration — keep the count going so the loop
    // knows this isn't the first iteration (for no-progress checks)
}
```

The loop's existing guidance drain at `StepGuidance` will pick up the feedback and inject it into the next implement prompt.

If resumed *without* guidance (just pressing `s`), the stream is converged, so the loop needs to advance to the next phase before entering the iteration loop. Add a check at the top of `Run()`:

```go
if s.Converged {
    // Converged + no guidance = advance to next phase
    pipeline := s.GetPipeline()
    nextIdx := s.GetPipelineIndex() + 1
    if nextIdx < len(pipeline) {
        nextPhase, err := factory(pipeline[nextIdx])
        // ...
        s.SetPipelineIndex(nextIdx)
        s.SetConverged(false)
        s.SetIteration(0)
        phase = nextPhase
    } else {
        s.SetStatus(stream.StatusPaused)
        return
    }
}
```

### Step 4: Add breakpoint picker to creation flow (UI)

**Files:** `internal/ui/app.go`

Add a new creation step (step 3) between phase selection and stream creation:

1. Add `newStreamBreakpoints map[int]bool` to the `Model` struct
2. Add `newStreamBPCursor int` for cursor position
3. In `updateNewStreamPipeline`, change `enter` to advance to step 3 instead of creating
4. Add `updateNewStreamBreakpoints(msg)` handler for step 3
5. In step 3's `enter`, create the stream (passing breakpoints to `orch.Create`)

The breakpoint picker shows the selected pipeline with toggle-able breakpoints *between* phases:

```
New Stream

Title: Implement auth
Task: Add OAuth flow...

Set breakpoints (pause between phases):

  research
  ──── [x] pause after research ────
  plan
  ──── [ ] pause after plan ────
  coding

j/k: navigate  space: toggle  enter: create  esc: back
```

The cursor moves between the N-1 gaps (not the phase names). Space toggles the breakpoint. If only one phase is selected, skip this step entirely.

### Step 5: Thread breakpoints through `orch.Create`

**Files:** `internal/orchestrator/orchestrator.go`, `internal/ui/app.go`

- Change `Create(title, task string, pipeline []string)` signature to `Create(title, task string, pipeline []string, breakpoints []int)`
- Set `st.Breakpoints = breakpoints` in the stream constructor
- Update the call site in `updateNewStreamPipeline` (now in step 3's handler)
- Update `pendingBreakpoints []int` stash field for beads-init flow
- Update the headless `runHeadless()` path in `main.go` (pass `nil` for breakpoints)

### Step 6: Render breakpoint status in dashboard/detail views

**Files:** `internal/ui/dashboard.go`, `internal/ui/detail.go`

- In the detail view's phase/pipeline display, show a `⏸` or `●` marker next to phases that have breakpoints
- When a stream is paused at a breakpoint, show "Paused (breakpoint)" instead of just "Paused" in the status

### Step 7: Tests

- Test breakpoint persistence round-trip (store save/load)
- Test loop pauses at breakpoint despite `TransitionAutoAdvance`
- Test re-iterate: guidance + resume resets convergence and re-runs phase
- Test continue: resume without guidance advances to next phase
- Test UI: breakpoint picker renders correctly, toggles work

## Key decisions

- **Breakpoints are indices, not phase names**: Simpler, avoids ambiguity with duplicate phase names (not currently possible but defensive).
- **Re-iterate uses existing guidance mechanism**: No new concept needed. Guidance + converged state = re-iterate.
- **Skip breakpoint picker for single-phase pipelines**: No gaps to place breakpoints in.
- **Breakpoints stored on Stream, not on Phase**: This is a per-stream user choice, not a phase-level property. `TransitionMode()` on the phase is the *default* behavior; breakpoints are user overrides.
