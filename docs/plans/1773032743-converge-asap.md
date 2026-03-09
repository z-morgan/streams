# Converge ASAP: Race-to-Convergence Shortcut

## Problem

When a stream is running, the user has no way to tell it "finish up cleanly." The only options are:
- **Stop** (`x`): Hard cancel — loop exits immediately, mid-iteration state
- **Guidance** (`g`): Nudge the agent, but doesn't control convergence
- Wait for natural convergence

Sometimes you can see the phase is "good enough" and you want it to wrap up without another review filing more work.

## Design

### Mechanism: Skip the Review Step

The loop iterates: `implement → autosquash → review → convergence check`. The review step is what files new beads, keeping the phase alive. If we skip the review:

- `OpenAfterReview == OpenBeforeReview` (no change)
- `IsConverged()` returns `true` naturally
- Normal transition logic applies (auto-advance or pause)

This is clean because:
- The current implement step finishes (no mid-thought abort)
- Autosquash still runs (git history stays clean)
- Phase transition works normally (breakpoints respected)
- The flag is a one-shot signal, self-clearing

### Key: `w` (wrap up)

Available in detail view when a stream is running. Shows a confirmation dialog before setting the flag.

## Implementation Steps

### Step 1: Stream State — Add `ConvergeASAP` Flag

**Files:** `internal/stream/stream.go`

Add a `ConvergeASAP` bool field to the Stream struct. Add thread-safe getter/setter/drain methods following existing patterns:
- `SetConvergeASAP(v bool)` — setter
- `GetConvergeASAP() bool` — getter
- `DrainConvergeASAP() bool` — atomically read and clear (like `DrainGuidance`)

### Step 2: Loop — Check Flag and Skip Review

**Files:** `internal/loop/loop.go`

After the autosquash step and before the review step, check `s.DrainConvergeASAP()`. If true:
- Skip the review agent call entirely
- Construct an `IterationResult` with `OpenAfterReview = openBefore` (the count before review)
- This makes `IsConverged()` return true for all standard phases
- Log a message to stream output indicating convergence was forced

The rest of the convergence/transition logic runs unchanged.

### Step 3: Orchestrator — Add `Converge` Method

**Files:** `internal/orchestrator/orchestrator.go`

Add `Converge(id string) error`:
- Validate stream exists and is running
- Call `st.SetConvergeASAP(true)`
- Return nil

Simple — the loop picks up the flag on its next review checkpoint.

### Step 4: UI — Confirmation Dialog and Shortcut

**Files:** `internal/ui/app.go`

**Model state:** Add `showConvergeConfirm bool` flag.

**Shortcut:** In `updateDetail()`, bind `w` when stream is running (`StatusRunning`). Sets `showConvergeConfirm = true`.

**Dialog:** Simple confirmation like delete — no text input needed:
```
Wrap Up Phase

Skip remaining review iterations and converge
the current phase as quickly as possible.

The current implement step will finish, but no
further review work will be filed.

w: confirm  esc: cancel
```

**Handler:** On `w` confirm, call `m.orch.Converge(m.selectedID)`, set status message "Wrapping up phase...". On `esc`, dismiss.

**Overlay priority:** Insert in the overlay hierarchy (in `Update()`) alongside the other overlays.

## Commit Plan

1. Stream state: add `ConvergeASAP` field + accessors
2. Loop: check flag, skip review when set
3. Orchestrator: add `Converge()` method
4. UI: add `w` shortcut + confirmation dialog
