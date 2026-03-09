# Autosquash Resilience: Cascading Fixups + Rebase Agent

Related issues: streams-it0, streams-rlw

## Problem

When the review step files feedback against an earlier commit, the implement step creates a `fixup!` commit. Two things can go wrong during autosquash:

1. **Silent breakage**: The fixup changes something (e.g., renames an interface) that downstream commits depend on. Autosquash succeeds, but later commits now reference the old name — broken code in the middle of history.

2. **Rebase conflict**: The fixup touches lines also modified by a later commit. Autosquash fails, the stream pauses, and a human must intervene.

Problem 1 is preventable with better prompting. Problem 2 needs a recovery mechanism.

## Design

Two complementary changes, ordered so each is independently useful:

### Layer 1: Teach the implement agent to handle cascading changes (streams-it0)

Update `coding-implement.tmpl` to instruct the agent: when creating a fixup commit, check whether the change affects code that later commits also touch. If so, create additional fixup commits for those downstream commits.

This is pure prompt engineering — no Go changes required. It won't catch every case (the agent might miss a dependency), but it eliminates the most common scenario: renames, interface changes, and signature modifications.

### Layer 2: Rebase agent for conflict resolution (streams-rlw)

When autosquash fails, instead of immediately aborting and pausing, invoke a Claude agent to resolve the conflicts. The key insight is that these conflicts are unusually easy to resolve:

- They're the agent's own recent commits
- The fixup commit message explains the intended change
- Conflicts are small and localized (single-file, few-line hunks)

The rebase agent runs in the worktree (which is already in a "rebasing" state after the failed rebase), resolves conflicts, and continues the rebase. If it can't resolve, we fall back to the current behavior (abort + pause).

**Architecture**: `BeforeReview` already receives `PhaseContext` which includes the `Runtime`. The rebase agent is just another `rt.Run()` call within the autosquash step — no new loop steps or interface changes needed.

## Steps

### 1. Update coding-implement.tmpl with cascading fixup instructions

Add a section after the existing fixup instruction that tells the agent:

> When creating a fixup commit, check the git log for commits after the target SHA. If your fixup changes something those later commits depend on (renamed identifiers, changed signatures, modified interfaces), create additional fixup commits for each affected downstream commit.

Concrete additions to the template:
- Instruct the agent to run `git log --oneline <target-sha>..HEAD` after creating a fixup
- Check if the fixup's changes (renames, signature changes, etc.) appear in later commits
- If so, create `fixup! <later-commit-msg>` commits with the corresponding updates

No Go code changes. Test by reviewing the rendered template output.

### 2. Create coding-rebase.tmpl prompt template

New template at `internal/loop/prompts/coding-rebase.tmpl`. This prompt is rendered with extended `PromptData` and tells the agent:

- You're resolving autosquash rebase conflicts
- The rebase is already in progress (worktree is in rebasing state)
- Check `git status` to find conflicted files
- Read conflicted files, resolve the conflict markers
- The fixup commit messages explain intent — favor the fixup's changes
- Stage resolved files with `git add`
- Run `git rebase --continue`
- Repeat if more conflicts appear
- If you cannot resolve a conflict, explain why and stop

Template variables needed: `{{.RebaseOutput}}` (the stderr/stdout from the failed rebase command).

### 3. Extend PromptData with RebaseOutput field

Add `RebaseOutput string` to the `PromptData` struct in `prompts.go`. This field is only populated for the rebase prompt; it's empty for all other templates.

### 4. Add rebase resolution to CodingPhase.BeforeReview

Modify `BeforeReview` in `coding.go`:

```
Current flow:
  rebase --autosquash → fail → abort → restore stash → return error

New flow:
  rebase --autosquash → fail →
    load coding-rebase.tmpl with RebaseOutput →
    rt.Run() with implement tools →
    check if rebase completed (git status for "rebase in progress") →
      success: restore stash, return nil
      failure: abort → restore stash → return error (include agent output in detail)
```

Key implementation details:

- `BeforeReview` signature stays the same — it already has access to `PhaseContext.Runtime`
- The rebase agent gets the same tools as implement: Bash, Read, Edit, Write, Glob, Grep
- On success, the agent's cost should be tracked. Add a `RebaseCostUSD float64` return or accumulate it on the stream. Simplest approach: return the cost via a new field on `PhaseContext` or accept that it won't be tracked in the snapshot (can add later).
- The rebase agent's output goes through `s.AppendOutput` so it's visible in the TUI tail view.
- Cap the rebase agent's budget (e.g., `maxBudgetUsd: "0.50"`) — conflict resolution should be cheap. If it's burning budget, something is wrong.

### 5. Tests

- **Template test**: Verify `coding-rebase.tmpl` renders correctly with `RebaseOutput` populated
- **Unit test for BeforeReview**: Mock runtime to simulate:
  - Rebase succeeds on first try (no agent needed) — existing behavior preserved
  - Rebase fails, agent resolves, rebase completes — returns nil
  - Rebase fails, agent fails — returns ErrAutosquash with agent detail
- **Integration consideration**: Hard to test the full git rebase flow in unit tests. The existing approach of shelling out to git makes this inherently integration-level. Consider a focused integration test that sets up a repo with a known conflict, runs BeforeReview, and verifies resolution.

## What this does NOT cover

- **Rebase conflicts from non-fixup sources**: This only helps with autosquash during the coding phase. Manual rebases or upstream conflicts are out of scope.
- **Verifying correctness after resolution**: The rebase agent resolves conflicts but doesn't run tests. The review step that follows will catch functional regressions.
- **Multiple resolution attempts**: If the agent fails once, we pause. No retry loop — the conflict may genuinely need human judgment.
