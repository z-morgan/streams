# Step-per-Iteration Coding with Refinement Phase

**Branch:** `step-per-iteration-coding`

## Context

The current coding phase runs all ordered steps in a single model session. For larger plans with many steps, this means the coding agent works through the entire implementation in one context window, which degrades quality as context fills up. The review agent then evaluates everything at once, and fixup iterations also run in crowded context windows.

The goal is to split coding into two phases:

1. **Coding** — Implements one plan step per model session. After each step, a review agent evaluates the cumulative work against the full plan and can halt forward progress if prior steps need fixing. This mimics how a human developer builds a feature: implementing incrementally, noticing problems as they go, and fixing them before building on a shaky foundation.

2. **Refinement** — After all steps are implemented, does a systematic pass over the full implementation to catch cross-cutting concerns, integration issues, and missed requirements. This is the developer self-reviewing before opening a PR.

The existing **review** phase (human-style PR review) remains unchanged as a separate phase.

## Design

### Coding phase: step-per-iteration with inline review

The coding phase alternates between two modes based on the state of child beads:

**Step mode** — No open review issues exist. The implement agent receives the next unclosed step bead and implements it freely (multiple commits allowed). The step bead is closed when done.

**Fix mode** — Open review issues exist (filed by the previous review). The implement agent addresses those issues with fixup commits. No new steps are started. The agent closes each review issue when resolved.

After each implement step (regardless of mode), the review agent runs. It has access to:
- The full plan artifact (`plan.md`)
- All steps with their status (done/open/current)
- The cumulative diff from base
- Prior snapshot summaries

The review agent evaluates two things, in priority order:
1. **Backward-looking**: Is the implementation so far correct? Are there bugs, missing pieces, or structural problems in what's been built?
2. **Forward-looking**: Is the implementation building the right foundation for upcoming steps? Would the current approach cause problems when later steps are implemented?

If the review files issues, the next iteration enters fix mode. If it files nothing and the current step bead is closed, the next iteration advances to the next step (step mode). The phase converges when all step beads are closed and no review issues remain.

If there are no step beads (e.g., pipeline is just `["coding"]` without decompose), the coding phase falls back to current behavior: implement all work in one shot, review, converge.

### Mode detection

Step beads are created by the decompose phase with the `step` label. Review-filed beads don't have this label. The mode is determined by comparing open children:

```
openAll    = ListOpenChildren(parentID)        // all open child beads
openSteps  = FetchOrderedSteps(parentID)       // open child beads with "step" label
reviewOpen = len(openAll) - len(openSteps)

if reviewOpen > 0  → fix mode
else if len(openSteps) > 0  → step mode (next step = openSteps[0])
else  → converged
```

This requires `FetchOrderedSteps` to filter by the `step` label, which it currently does not — it returns all open children sorted by sequence. Step 1 of the implementation fixes this.

### Refinement phase

Structurally identical to the current implement+review convergence loop. The differences are the prompts:

- **Refinement implement**: "Here's the full implementation across N coding steps. Here are the review assessments from coding. Address any cross-cutting issues, integration problems, or missed requirements." The agent has full editorial freedom — it can refactor across step boundaries, fix interactions between components, add missing error handling, etc.
- **Refinement review**: Evaluates the full codebase holistically. Files beads for remaining issues. Same tier system (T1-T4) and convergence rules as existing review.

The refinement phase converges the same way the current coding phase does: when the review files no new blocking issues.

### Review phase (unchanged)

The existing review phase remains as-is — a human-style PR review that produces a structured summary (work completed, key decisions, testing recommendations) and pauses for human action (revise or complete).

### Pipeline

Default pipeline remains `["coding"]` (backwards compatible — falls back to bulk mode without step beads).

Extended pipeline: `["plan", "decompose", "coding", "refine", "review", "polish"]`

Each phase supports independent model selection via the existing per-phase model config.

### How context flows between phases

```
plan → produces plan.md artifact
decompose → reads plan.md, creates ordered step beads with "step" label
coding → reads plan.md + step beads, implements one step per iteration
  └─ coding review has: plan.md, all steps with status, cumulative diff
refine → reads coding snapshots (review assessments), full diff from base
  └─ refine review has: full codebase, all prior context
review → reads all snapshots, commit log, diff stat → structured summary
polish → commit-scoped cleanup (lint, rubocop, etc.)
```

## Steps

### 1. Filter FetchOrderedSteps by "step" label

**Files:** `internal/loop/beads.go`

Add `Labels` field to `beadsChild`. Update `FetchOrderedSteps` to only return children that have the `step` label. This is the prerequisite for mode detection — without it, review-filed beads would appear as "steps" with sequence 0.

Also add `FetchAllStepsWithStatus(parentID) ([]StepWithStatus, error)` that returns ALL step-labeled children regardless of status (open, closed, in_progress), each annotated with their status. This is needed for the coding review prompt to show which steps are done vs. upcoming.

```go
type StepWithStatus struct {
    Step
    Status string // "open", "closed", "in_progress"
}
```

### 2. Add method to count open non-step children

**Files:** `internal/loop/beads.go`

Add `FetchOpenNonStepChildren(parentID) ([]string, error)` — returns IDs of open children that do NOT have the `step` label. These are review-filed issues that need fixing.

Update `BeadsQuerier` interface with the new methods.

### 3. Extend PhaseContext for mode detection and plan access

**Files:** `internal/loop/phase.go`, `internal/loop/prompts.go`, `internal/loop/loop.go`

Add to `PhaseContext`:
```go
OpenReviewBeads  []string          // IDs of open non-step children
AllSteps         []StepWithStatus  // all step beads with status
PlanContent      string            // contents of plan.md (empty if no plan phase)
```

Add to `PromptData`:
```go
PlanContent       string
AllStepsFormatted string  // all steps with status markers: [done] / [current] / [open]
CurrentStep       string  // title of the step being implemented (step mode only)
CurrentStepID     string  // bead ID of the current step
IsFixMode         bool
ReviewBeadsList   string  // formatted list of open review beads to fix
```

In `Run`, populate the new `PhaseContext` fields before calling `ImplementPrompt`:
- Call `FetchOpenNonStepChildren` and `FetchAllStepsWithStatus`
- Read `plan.md` from the work dir if it exists

### 4. Coding implement prompt: step mode

**Files:** `internal/loop/prompts/coding-implement-step.tmpl` (new)

```
You are implementing one step of a multi-step plan.

Task: {{.Task}}
Parent issue: {{.ParentID}}

## The Plan
{{.PlanContent}}

## Progress
{{.AllStepsFormatted}}

## Your Step
{{.CurrentStepID}} — {{.CurrentStep}}

Implement this step. You may make multiple commits if the work naturally
breaks down that way.

When done:
1. Run relevant tests.
2. Close the step issue: bd close {{.CurrentStepID}}

Rules:
- Only implement this step. Do not work ahead to future steps.
- Do not create new beads issues.
```

### 5. Coding implement prompt: fix mode

**Files:** `internal/loop/prompts/coding-implement-fix.tmpl` (new)

```
You are fixing issues identified during review of previous coding steps.

Task: {{.Task}}
Parent issue: {{.ParentID}}

## The Plan
{{.PlanContent}}

## Progress
{{.AllStepsFormatted}}

## Issues to Fix
{{.ReviewBeadsList}}

For each issue:
1. Read the issue description to understand what needs to change.
2. Make the fix with a fixup commit targeting the relevant SHA:
   git commit --fixup=<sha>
3. Run relevant tests.
4. Close the issue: bd close <issue-id>

After creating a fixup commit, check whether your changes affect code that
later commits also touch. If so, create additional fixup commits for each
affected downstream commit.

Rules:
- Fix all listed issues. Do not implement new steps.
- Do not create new beads issues.
```

### 6. Coding review prompt: plan-aware, forward-looking

**Files:** `internal/loop/prompts/coding-review.tmpl` (replace existing)

```
You are reviewing a coding step in the context of a larger plan.

Task: {{.Task}}
Parent issue: {{.ParentID}}

## The Plan
{{.PlanContent}}

## Steps
{{.AllStepsFormatted}}

## Your responsibilities (in priority order)

1. **Correctness** — Does the implementation work? Are there bugs, edge
   cases, or missing error handling? Do the tests pass?

2. **Foundation** — Is the implementation building the right foundation
   for the upcoming steps? Flag structural decisions now that will cause
   problems later. Consider: will the next steps be able to build cleanly
   on what exists, or will they need to refactor/undo work from this step?

3. **Consistency** — Does this fit with prior steps? Is there drift from
   the plan's design intent?

Steps:
1. Read the relevant code (use Glob/Grep/Read to find what was changed).
2. Review the git log to understand the commit structure.
3. Evaluate against the criteria above.
4. For each improvement, file a child issue referencing the relevant
   commit SHA in the description:
   bd create --parent {{.ParentID}} --title="<specific action>" \
     --type=task --priority=2 \
     --description="Commit <sha>: <what to change and why>"
5. Write a brief summary (2-4 sentences) as your FINAL output.

IMPORTANT: Your summary text MUST be the very last thing you output.

Rules:
- Do NOT edit or write any files.
- Each issue must be a single, actionable change.
- Do not file issues about style/formatting that a linter would catch.
- Maximum 5 issues per review.
- Every issue MUST include a tier tag in the title: [T1], [T2], [T3], [T4].
  [T1] Correctness — runtime error, data loss, or security issue.
  [T2] Completeness — a requirement from the task is not addressed.
  [T3] Design — works but could be structured better for upcoming steps.
  [T4] Polish — style preferences, minor naming.
- Do not file issues labeled "step" — that label is reserved for plan steps.
{{- if ge .Iteration 3}}

NOTE: This is iteration {{.Iteration}}. Only file [T1] or [T2] issues.
{{- end}}
{{- if ge .Iteration 5}}

NOTE: After 5+ iterations, only file [T1] issues.
{{- end}}
```

### 7. Update CodingPhase to support step-per-iteration

**Files:** `internal/loop/coding.go`

Update `ImplementPrompt` to select the template based on mode:

```go
func (p *CodingPhase) ImplementPrompt(ctx PhaseContext) (string, error) {
    data := promptDataFromContext(ctx)
    populateCodingData(&data, ctx)

    if len(ctx.AllSteps) == 0 {
        // No step beads — fall back to bulk mode (current behavior)
        return LoadPrompt("coding", "implement", data)
    }

    if len(ctx.OpenReviewBeads) > 0 {
        return LoadPrompt("coding", "implement-fix", data)
    }
    return LoadPrompt("coding", "implement-step", data)
}
```

Update `ReviewPrompt` to always use the plan-aware review (the new template replaces the old one, so no conditional needed).

Update `IsConverged`: In step-per-iteration mode, convergence means no open children at all (all steps closed + no review issues). In bulk mode (no step beads), use the current logic.

```go
func (p *CodingPhase) IsConverged(result IterationResult) bool {
    if p.hasStepBeads {
        return result.OpenAfterReview == 0
    }
    return result.OpenAfterReview <= result.OpenBeforeReview
}
```

This requires `CodingPhase` to gain a `hasStepBeads` field set during prompt generation, or a cleaner way to thread that state. One option: check `OpenAfterReview == 0` universally — if there are no open children, the phase is done regardless of mode.

### 8. Populate new PhaseContext fields in Run

**Files:** `internal/loop/loop.go`

Before the `ImplementPrompt` call, fetch the additional data:

```go
openNonStep, _ := beads.FetchOpenNonStepChildren(s.BeadsParentID)
allSteps, _ := beads.FetchAllStepsWithStatus(s.BeadsParentID)

var planContent string
planPath := filepath.Join(s.WorkTree, "plan.md")
if data, err := os.ReadFile(planPath); err == nil {
    planContent = string(data)
}

pctx := PhaseContext{
    // ... existing fields ...
    OpenReviewBeads: openNonStep,
    AllSteps:        allSteps,
    PlanContent:     planContent,
}
```

This data is fetched every iteration, which is correct since the bead states change between iterations.

### 9. RefinementPhase implementation

**Files:** `internal/loop/refine.go` (new), `internal/loop/phase.go`

```go
type RefinementPhase struct{}

func (p *RefinementPhase) Name() string { return "refine" }
```

Structurally very similar to the current CodingPhase:
- `ImplementTools`: Bash, Read, Edit, Write, Glob, Grep (same as coding)
- `ReviewTools`: Bash, Read, Glob, Grep (same as coding)
- `IsConverged`: `result.OpenAfterReview <= result.OpenBeforeReview` (standard)
- `BeforeReview`: Autosquash (reuse the same logic from CodingPhase — extract into a shared helper)
- `TransitionMode`: `TransitionAutoAdvance`

Register `"refine"` in `NewPhase` switch.

### 10. Refinement implement prompt

**Files:** `internal/loop/prompts/refine-implement.tmpl` (new)

The refinement implement agent gets:
- The task description
- The plan artifact
- Coding snapshot summaries (including review assessments from each step)
- The current ordered steps (which are now all the review-filed issues)

On iteration 0, this is a proactive cleanup pass. On subsequent iterations, it addresses review-filed issues.

```
You are refining a completed implementation. All plan steps have been
coded. Your job is to address cross-cutting concerns, integration issues,
and problems that span multiple steps.

Task: {{.Task}}
Parent issue: {{.ParentID}}

## Open Issues
{{.OrderedSteps}}

For each issue:
1. Implement the fix.
2. Run relevant tests.
3. Commit with a descriptive message (or fixup commit if targeting a
   specific prior commit).
4. Close the issue: bd close <issue-id>

If there are no open issues, do a proactive pass:
1. Read the full implementation (use git diff against the base).
2. Look for integration issues, inconsistencies between components,
   missed error handling, or dead code left over from iterative development.
3. Fix what you find and commit.

Rules:
- Do not create new beads issues.
- Skip any issues labeled "advisory".
```

### 11. Refinement review prompt

**Files:** `internal/loop/prompts/refine-review.tmpl` (new)

```
You are reviewing a complete implementation for integration quality.
All plan steps have been coded and individually reviewed. Your job is
to evaluate the implementation as a whole.

Task: {{.Task}}
Parent issue: {{.ParentID}}

Steps:
1. Read the full implementation (git diff from base, explore key files).
2. Evaluate against these criteria:
   - Integration: Do the components work together correctly?
   - Missed requirements: Is anything from the task description unaddressed?
   - Dead code: Was anything left behind from iterative development?
   - Consistency: Are naming conventions, patterns, and approaches uniform?
3. For each issue, file a child issue:
   bd create --parent {{.ParentID}} --title="<specific action>" \
     --type=task --priority=2 \
     --description="<what to change and why>"
4. Write a summary as your FINAL output.

Rules:
- Do NOT edit or write any files.
- Maximum 5 issues per review.
- Every issue MUST include a tier tag: [T1], [T2], [T3], or [T4].
- Do not file issues about style/formatting that a linter would catch.
- Do not file issues that are outside the task scope.
{{- if ge .Iteration 3}}

NOTE: Iteration {{.Iteration}}. Only file [T1] or [T2] issues.
{{- end}}
```

### 12. Extract shared autosquash logic

**Files:** `internal/loop/coding.go`, `internal/loop/refine.go`, `internal/loop/autosquash.go`

The `BeforeReview` autosquash logic (including the rebase agent) is currently embedded in `CodingPhase`. Extract it into a shared function in `autosquash.go` so both `CodingPhase` and `RefinementPhase` can call it. (An `autosquash.go` already exists — extend or consolidate there.)

### 13. Register "refine" in UI phase picker

**Files:** `internal/ui/app.go`

Add `"refine"` to the `phaseTree` after `"coding"` so it appears in the pipeline configuration UI during stream creation.

### 14. Tests

**Files:** `internal/loop/coding_test.go`, `internal/loop/refine_test.go` (new), `internal/loop/beads_test.go`

- Test mode detection: step mode vs fix mode vs bulk fallback
- Test convergence: coding converges when all steps + review issues closed
- Test label filtering in `FetchOrderedSteps`
- Test refinement phase convergence (standard behavior)
- Test that coding review prompt includes plan content and all steps with status

## Notes

- The existing `coding-implement.tmpl` is preserved for bulk mode (no step beads). This maintains backwards compatibility for pipelines without a decompose phase.
- The convergence system (tier classification, refinement cap, section tracking) applies to both coding and refinement phases. The coding phase may want a lower refinement cap since it converges per-step.
- The no-progress check in `Run` works unchanged: if an implement iteration closes zero beads (neither step beads nor review beads), it's a genuine stall.
- `ConvergeASAP` works unchanged: it skips the review, so no new issues are filed, and `IsConverged` sees the same or fewer open children.
