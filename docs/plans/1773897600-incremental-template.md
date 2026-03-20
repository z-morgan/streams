# Incremental Template: Step-per-Iteration Coding with Refinement

**Branch:** `incremental-template`

## Context

The original plan (`1773897600-step-per-iteration-coding.md`) proposed modifying the existing coding phase to conditionally support step-per-iteration behavior. This adapted plan implements the same functionality as a **new template** — the first template beyond the built-in "Classic" — so existing stream behavior is completely untouched.

The Classic template's `coding` phase implements all work items in a single model session and uses a general-purpose review prompt. This works well for medium tasks but degrades for larger implementations where the coding agent's context fills up.

The new "Incremental" template splits coding into two distinct phases:

1. **step-coding** — Implements one plan step per model session. After each step, a plan-aware review evaluates cumulative work and can file issues that must be fixed before the next step. This is the "build incrementally, fix as you go" pattern.

2. **refine** — After all steps are implemented, a holistic pass catches cross-cutting concerns, integration issues, and missed requirements. This is the "self-review before opening a PR" pattern.

The existing `coding` phase, Classic template, and `review` phase are untouched.

## Design

### New template

```
Name:        "Incremental"
Description: "Step-by-step coding with inline review and refinement"
Pipeline:    research, plan > decompose, step-coding, refine, review, polish
```

The `decompose` phase already creates step beads with the `step` label and `sequence:N` notes. The `step-coding` phase relies on these to determine what to implement and in what order.

### step-coding phase: mode detection

Each iteration, the phase determines its mode by examining child beads:

```
allOpen       = ListOpenChildren(parentID)
stepBeads     = FetchStepBeads(parentID)        // open children with "step" label
openSteps     = [s for s in stepBeads if s.Status != "closed"]
reviewOpen    = len(allOpen) - len(openSteps)

if reviewOpen > 0  → fix mode   (address review-filed issues first)
if len(openSteps) > 0  → step mode (implement next step)
else  → converged
```

**Step mode**: The implement agent receives the next unclosed step bead and implements it. The step bead is closed when done.

**Fix mode**: Open review issues exist (filed by the previous review). The implement agent addresses those with fixup commits. No new steps are started. Each issue is closed when resolved.

After each implement iteration (regardless of mode), the review runs. It has the full plan, all steps with status markers, and evaluates both backward (is what we built correct?) and forward (is this the right foundation for upcoming steps?).

### step-coding phase: convergence

Convergence means no open children at all — all step beads closed AND no review issues remain. This is checked as `result.OpenAfterReview == 0`.

### refine phase

Structurally identical to the current coding phase's implement+review cycle. Differences are only in the prompts:

- **Implement**: "All plan steps are coded. Address cross-cutting issues, integration problems, or missed requirements." The agent has full editorial freedom across step boundaries.
- **Review**: Evaluates the full codebase holistically. Files beads for remaining issues. Same tier system and convergence rules as existing review.

Convergence follows the standard rule: `result.OpenAfterReview <= result.OpenBeforeReview`.

### BeadsQuerier extensions

Two new methods on the `BeadsQuerier` interface:

- `FetchStepBeads(parentID)` — returns ALL children with the `step` label, regardless of status, each annotated with status. Needed for progress display in prompts.
- `FetchOpenNonStepChildren(parentID)` — returns IDs of open children WITHOUT the `step` label. These are review-filed issues that need fixing.

The existing `FetchOrderedSteps` stays unchanged — Classic's `coding` phase depends on it.

### PhaseContext extensions

New fields populated by `Run` every iteration:

```go
PlanContent      string            // contents of plan.md (empty if no plan phase)
StepBeads        []StepWithStatus  // all step-labeled children with status
OpenReviewBeads  []string          // IDs of open non-step children
```

These fields are populated for all phases (the data is harmless to phases that ignore it). `Run` already calls `beads.FetchOrderedSteps` each iteration — the new queries are analogous.

### How context flows

```
plan → produces plan.md artifact
decompose → reads plan.md, creates ordered step beads with "step" label
step-coding → reads plan.md + step beads, implements one step per iteration
  └─ review has: plan.md, all steps with status, cumulative diff
refine → reads coding snapshots, full diff from base
  └─ review evaluates full codebase holistically
review → reads all snapshots, structured summary (unchanged)
polish → commit-scoped cleanup (unchanged)
```

## Steps

### 1. Add `Labels` field to beadsChild and new BeadsQuerier methods

**Files:** `internal/loop/beads.go`

Add `Labels` field to the `beadsChild` struct (the JSON from `bd show --children --json` already includes labels).

Add `StepWithStatus` type:
```go
type StepWithStatus struct {
    Step
    Status string // "open", "closed", "in_progress"
}
```

Add `FetchStepBeads(parentID) ([]StepWithStatus, error)` — fetches all children, filters to those with the `step` label, returns them with status and sequence. Sorted by sequence.

Add `FetchOpenNonStepChildren(parentID) ([]string, error)` — fetches all children, returns IDs of open/in_progress children that do NOT have the `step` label.

Update the `BeadsQuerier` interface with both new methods.

### 2. Extend PhaseContext with new fields and populate in Run

**Files:** `internal/loop/phase.go`, `internal/loop/loop.go`

Add to `PhaseContext`:
```go
PlanContent     string
StepBeads       []StepWithStatus
OpenReviewBeads []string
```

In `Run`, before the implement prompt call, populate the new fields:
- Call `beads.FetchStepBeads(s.BeadsParentID)` and `beads.FetchOpenNonStepChildren(s.BeadsParentID)`
- Read `plan.md` from work dir if it exists

These are fetched every iteration since bead states change between iterations.

### 3. Add PromptData fields and formatting helpers

**Files:** `internal/loop/prompts.go`, `internal/loop/phase.go`

Add to `PromptData`:
```go
PlanContent       string
AllStepsFormatted string  // "[done] Step 1 — Title\n[current] Step 2 — Title\n..."
CurrentStep       string  // title of current step (step mode)
CurrentStepID     string  // bead ID of current step
IsFixMode         bool
ReviewBeadsList   string  // formatted list of open review beads
```

Add `promptDataFromContext` population for the new PhaseContext fields (or let each phase populate the prompt-specific fields in its own `ImplementPrompt`).

Add a formatting function that takes `[]StepWithStatus` and the current step index, and returns the formatted progress string with `[done]` / `[current]` / `[open]` markers.

### 4. StepCodingPhase struct and registration

**Files:** `internal/loop/step_coding.go` (new), `internal/loop/phase.go`

```go
type StepCodingPhase struct{}

func (p *StepCodingPhase) Name() string              { return "step-coding" }
func (p *StepCodingPhase) ImplementTools() []string   { /* same as CodingPhase */ }
func (p *StepCodingPhase) ReviewTools() []string      { /* same as CodingPhase */ }
func (p *StepCodingPhase) TransitionMode() Transition { return TransitionAutoAdvance }
func (p *StepCodingPhase) ArtifactFile() string       { return "" }
```

`ImplementPrompt`: Reads PhaseContext, determines mode (fix vs step), selects the appropriate template, populates PromptData with mode-specific fields.

`ReviewPrompt`: Always uses the plan-aware coding review template (injects plan content, all steps with status).

`IsConverged`: `result.OpenAfterReview == 0` — all steps closed and no review issues remain.

`BeforeReview`: Same autosquash logic as CodingPhase — call the shared `autosquash()` function from `autosquash.go`. Also needs agent-based rebase resolution, so extract the `runRebaseAgent` logic from CodingPhase into a shared function that both can call.

Register `"step-coding"` in the `NewPhase` switch.

### 5. Step-coding prompt templates

**Files:**
- `internal/loop/prompts/step-coding-implement-step.tmpl` (new)
- `internal/loop/prompts/step-coding-implement-fix.tmpl` (new)
- `internal/loop/prompts/step-coding-review.tmpl` (new)

**step-coding-implement-step.tmpl** (step mode): Receives the full plan, progress with status markers, and the specific step to implement. Instructs the agent to implement only this step, run tests, commit, and close the step bead.

**step-coding-implement-fix.tmpl** (fix mode): Receives the full plan, progress, and the list of open review issues. Instructs the agent to address each issue with fixup commits and close each when resolved.

**step-coding-review.tmpl**: Receives the plan, all steps with status, and evaluates both correctness (backward-looking) and foundation quality (forward-looking). Files issues with tier tags. Maximum 5 issues. Same iteration-based tier gating as existing review (>=3 iterations → T1/T2 only, >=5 → T1 only).

See the original plan doc for the full prompt text — adapt those templates, replacing `coding` with `step-coding` in the LoadPrompt calls.

### 6. RefinementPhase struct and registration

**Files:** `internal/loop/refine.go` (new), `internal/loop/phase.go`

```go
type RefinementPhase struct{}

func (p *RefinementPhase) Name() string              { return "refine" }
func (p *RefinementPhase) ImplementTools() []string   { /* same as CodingPhase */ }
func (p *RefinementPhase) ReviewTools() []string      { /* same as CodingPhase */ }
func (p *RefinementPhase) TransitionMode() Transition { return TransitionAutoAdvance }
func (p *RefinementPhase) ArtifactFile() string       { return "" }

func (p *RefinementPhase) IsConverged(result IterationResult) bool {
    return result.OpenAfterReview <= result.OpenBeforeReview  // standard
}
```

`BeforeReview`: Call shared `autosquash()` from `autosquash.go`. No agent-based rebase resolution needed (refinement changes are smaller, failures can just be logged).

Register `"refine"` in the `NewPhase` switch.

### 7. Refinement prompt templates

**Files:**
- `internal/loop/prompts/refine-implement.tmpl` (new)
- `internal/loop/prompts/refine-review.tmpl` (new)

**refine-implement.tmpl**: On iteration 0 (no open issues), does a proactive pass over the full implementation: reads the diff, looks for integration issues, inconsistencies, dead code. On subsequent iterations, addresses review-filed issues.

**refine-review.tmpl**: Evaluates the full codebase holistically against integration quality, missed requirements, dead code, consistency. Files issues with tier tags. Same tier gating rules.

See the original plan doc for the full prompt text.

### 8. Extract shared rebase-agent logic from CodingPhase

**Files:** `internal/loop/coding.go`, `internal/loop/step_coding.go`

The `runRebaseAgent` method on CodingPhase uses `context.Background()`, the prompt loader, and a budget-limited runtime. Extract this into a package-level function `runRebaseAgent(ctx, pctx, rebaseOutput)` so both CodingPhase and StepCodingPhase can call it from their `BeforeReview` methods.

Update CodingPhase.BeforeReview to call the extracted function instead of `p.runRebaseAgent`.

### 9. Register "Incremental" as a builtin template

**Files:** `internal/stream/template.go`

Add to `BuiltinTemplates()`:
```go
{
    Name:        "Incremental",
    Description: "Step-by-step coding with inline review and refinement",
    Phases: []PhaseNode{
        {Name: "research"},
        {Name: "plan", Children: []PhaseNode{
            {Name: "decompose"},
        }},
        {Name: "step-coding"},
        {Name: "refine"},
        {Name: "review"},
        {Name: "polish"},
    },
},
```

### 10. Tests

**Files:** `internal/loop/beads_test.go`, `internal/loop/step_coding_test.go` (new), `internal/loop/refine_test.go` (new), `internal/stream/template_test.go`

- Test `FetchStepBeads` filters by `step` label and includes status
- Test `FetchOpenNonStepChildren` returns only non-step open children
- Test mode detection in StepCodingPhase: step mode vs fix mode
- Test step-coding convergence: converges when `OpenAfterReview == 0`
- Test refinement phase convergence (standard behavior)
- Test that the Incremental template is in `BuiltinTemplates()` and selectable
- Test that step-coding review prompt includes plan content and step status markers

## Notes

- The existing `coding-implement.tmpl` and `coding-review.tmpl` are untouched. Classic streams use them as before.
- The `autosquash.go` shared function already exists for polish slots. CodingPhase.BeforeReview duplicates some of that logic (with agent-based resolution on top). Step 8 consolidates this.
- `ConvergeASAP` works for step-coding: it skips review, so no new issues are filed, and `IsConverged` sees `OpenAfterReview == 0` if all steps were already closed.
- The no-progress check in Run works unchanged: if an implement iteration closes zero beads, it's a stall.
- Section tracking doesn't apply to step-coding or refine (no `ArtifactFile`), same as the current coding phase.
- Per-phase model selection works automatically via the existing `ModelForPhase` lookup — users can set different models for `step-coding` and `refine` independently.
