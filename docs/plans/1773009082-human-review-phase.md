# Human Review Phase

**Branch:** `human-review-phase`

## Context

After a stream completes its coding phase, it pauses with no further action available beyond re-iterating or deleting. There's no structured handoff to the human operator: no summary of what happened, no way to provide high-level feedback and re-enter the pipeline, and no way to finalize the work into a proper feature branch.

## Design

### The "review" phase

A new `MacroPhase` that runs after coding. Its implement step invokes an agent that:
- Reads the full commit history (BaseSHA → HEAD), diff stat, and all prior snapshot summaries/reviews
- Produces a structured summary: work completed, significant decisions, and testing recommendations

The review phase converges immediately (one iteration) and pauses. From the paused review state, the user has two new actions:

1. **Revise** (`r` key) — Pick a pipeline phase to continue from and optionally provide feedback. The stream re-enters the pipeline at that phase with the feedback injected as guidance, iterating forward from the current code state. No code is reset — commits and history stay intact.
2. **Complete** (`c` key) — Enter a branch name, and the stream converts its worktree branch into a first-class feature branch, removes the worktree, and marks the stream as completed.

### How it fits the existing architecture

The review phase implements `MacroPhase` like every other phase. Key design choices:

- **No review step**: `ReviewPrompt()` returns `""`, signaling the loop to skip the runtime call. This avoids burning API cost on a no-op. A small change to `loop.go` supports this: if the review prompt is empty, skip `rt.Run` but still run the convergence check.
- **Always converges**: `IsConverged()` returns `true` unconditionally.
- **Pauses after convergence**: `TransitionMode()` returns `TransitionPause`.
- **Read-only tools**: The implement step only needs `Bash`, `Read`, `Glob`, `Grep` — no edits.

### Revise mechanism

New orchestrator method `Revise(id, targetPhaseIndex, feedback)`:
1. Sets `PipelineIndex` to the target
2. Resets `Converged` to false
3. Resets `Iteration` to 0
4. Adds feedback as `Guidance` if provided
5. Calls `Start(id)`

The stream continues from the current code state — no commits or code are reset. The target phase picks up where it left off but with the new feedback injected as guidance.

UI: A new overlay triggered by `r` in the detail view (only visible when paused at the review phase). Shows a picker of earlier pipeline phases, plus a textarea for optional feedback.

### Complete/finalize mechanism

New orchestrator method `Complete(id, branchName)`:
1. `git branch -m streams/<id> <branchName>` (rename the worktree branch)
2. `git worktree remove <path> --force` (remove the worktree directory)
3. Set status to `StatusCompleted`
4. Persist

New `StatusCompleted` added to `stream.Status`. Completed streams render distinctly on the dashboard and cannot be started/stopped/guided.

UI: A new overlay triggered by `c` in the detail view (only visible when paused at the review phase). Prompts for a branch name (defaulting to a slug of the stream title), then finalizes.

### PromptData extension

New fields on `PromptData` (populated only by the review phase):
- `CommitLog` — `git log --oneline BaseSHA..HEAD`
- `DiffStat` — `git diff --stat BaseSHA..HEAD`
- `TotalCost` — sum of `CostUSD` across all snapshots
- `SnapshotSummaries` — formatted digest of all prior snapshot summaries and reviews

## Steps

### 1. Skip review step when ReviewPrompt returns ""

**Files:** `internal/loop/loop.go`

Change the review step block (lines 165-184) to check if `reviewPrompt == ""`. If empty, set `reviewResp` to an empty `runtime.Response{}` and skip the `rt.Run` call. The beads query and convergence check still run normally.

This is a prerequisite for the ReviewPhase, which has no meaningful review step.

### 2. ReviewPhase foundation

**Files:**
- `internal/loop/review.go` (new)
- `internal/loop/prompts/review-implement.tmpl` (new)
- `internal/loop/phase.go` — register `"review"` in `NewPhase`
- `internal/ui/app.go` — add `"review"` to `phaseTree`

The ReviewPhase struct:
```go
type ReviewPhase struct{}

func (p *ReviewPhase) Name() string                          { return "review" }
func (p *ReviewPhase) ImplementTools() []string              { return []string{"Bash", "Read", "Glob", "Grep"} }
func (p *ReviewPhase) ReviewTools() []string                 { return nil }
func (p *ReviewPhase) ReviewPrompt(_ PhaseContext) (string, error) { return "", nil }
func (p *ReviewPhase) IsConverged(_ IterationResult) bool    { return true }
func (p *ReviewPhase) BeforeReview(_ PhaseContext) error      { return nil }
func (p *ReviewPhase) TransitionMode() Transition            { return TransitionPause }
func (p *ReviewPhase) ArtifactFile() string                  { return "" }
```

`ImplementPrompt` builds a custom `PromptData` with review-specific fields populated from the stream's snapshots and git state, then renders `review-implement.tmpl`.

The prompt template instructs the agent to:
1. Read the commit log and diff stat (provided in the prompt as context)
2. Explore the code changes
3. Produce a structured response with sections: **Work Completed**, **Key Decisions**, **Testing Recommendations**

Add to `phaseTree` in app.go as a top-level node after `coding`:
```go
{Name: "review"},
```

### 3. Extend PromptData for review context

**Files:**
- `internal/loop/prompts.go` — add fields to `PromptData`
- `internal/loop/review.go` — populate the fields in `ImplementPrompt`

New `PromptData` fields:
```go
CommitLog         string
DiffStat          string
TotalCost         string
SnapshotSummaries string
```

The ReviewPhase's `ImplementPrompt` runs `git log --oneline` and `git diff --stat` via `exec.Command` and formats the snapshot summaries from `ctx.Stream.GetSnapshots()`.

### 4. StatusCompleted and orchestrator.Complete

**Files:**
- `internal/stream/stream.go` — add `StatusCompleted`
- `internal/orchestrator/orchestrator.go` — add `Complete(id, branchName)` method

`StatusCompleted` is a new terminal status. The `Complete` method:
1. Validates the stream is paused and not running
2. Renames the branch: `git branch -m <old> <new>` in `repoDir`
3. Removes the worktree: `git worktree remove <path> --force` in `repoDir`
4. Clears `WorkTree` on the stream (it no longer exists)
5. Sets `Status` to `StatusCompleted`
6. Persists

### 5. Complete UI overlay

**Files:**
- `internal/ui/app.go` — new overlay state, keybinding, render function

New model state:
- `showComplete bool`
- `completeInput textarea.Model` (for branch name)

Trigger: `c` key in detail view, only when the stream is paused at the review phase and converged.

Overlay shows a textarea with a default branch name derived from slugifying the stream title. `ctrl+s` to confirm, `esc` to cancel.

On confirm, sends a `tea.Cmd` that calls `orch.Complete(id, branchName)` and returns a `streamCompletedMsg`.

### 6. Orchestrator.Revise

**Files:**
- `internal/orchestrator/orchestrator.go` — add `Revise(id, targetPhaseIndex, feedback)` method

The method:
1. Validates the stream is paused and the target index is valid (< current PipelineIndex)
2. Sets `PipelineIndex` to `targetPhaseIndex`
3. Resets `Converged` to false
4. Sets `Iteration` to 0
5. Adds `feedback` as `Guidance` if non-empty
6. Calls `Start(id)` to resume

The stream continues from the current code state. No commits are reverted.

### 7. Revise UI overlay

**Files:**
- `internal/ui/app.go` — new overlay state, keybinding, render function

New model state:
- `showRevise bool`
- `revisePhaseCursor int`
- `reviseFeedback textarea.Model`
- `reviseStep int` (0 = phase picker, 1 = feedback input)

Trigger: `r` key in detail view, only when the stream is paused at the review phase and converged.

Phase picker shows all pipeline phases before the review phase. After selecting a phase, optionally enter feedback text. `enter` from the picker moves to feedback, `ctrl+s` from feedback triggers the revise, `esc` goes back.

### 8. Conditional detail view actions

**Files:**
- `internal/ui/detail.go` — update help text and status rendering
- `internal/ui/app.go` — guard `r` and `c` keybindings

When the stream is paused at the review phase (converged, no error, current phase is "review"):
- Show `c: complete  r: revise` in the help bar
- Hide `s: start` (starting review again isn't useful after it converged)

When the stream has `StatusCompleted`:
- Show only `q/esc: back` and `d: delete` in the help bar
- Status marker shows `[Completed]` with a distinct color

### 9. Dashboard rendering for completed streams

**Files:**
- `internal/ui/dashboard.go` — render completed status
- `internal/ui/styles.go` — add completed status color

Completed streams show a "Completed" badge and the renamed branch name. They remain in the list until deleted.

## Notes

- The review phase does not appear in the default pipeline (`"coding"`). Users opt in by selecting it during stream creation or by configuring `pipeline = "plan,decompose,coding,review"`.
- Revising does not reset code or commits. PipelineIndex moves backward so the target phase runs again, but all prior snapshots and commits remain in history.
- A completed stream retains all its snapshots for reference but has no worktree — the branch exists as a normal git branch.
