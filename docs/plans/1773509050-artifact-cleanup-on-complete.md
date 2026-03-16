# Plan: Clean up artifact files on stream completion

**Branch:** `zm/artifact-cleanup-on-complete`

## Problem

When a user presses `c` to complete a stream, `orchestrator.Complete()` rejects the
operation if the worktree has uncommitted changes. But the pipeline itself creates
artifact files (`research.md`, `plan.md`) that are never committed — the research and
plan prompts explicitly say "Do not commit." These files are ephemeral working copies;
their content is already captured in `snapshots.jsonl` at each checkpoint (loop.go:328-333).

This means **every stream with a research or plan phase will fail to complete** unless
the artifact files happen to get committed during a later phase (which is incidental,
not guaranteed).

### How it was discovered

Stream `plentish-1a9` in the `plentish` project. The research phase wrote `research.md`
(modifying a file that already existed in the base commit from a prior task). The plan
phase wrote `plan.md`, which was incidentally committed during the coding phase (review
feedback asked for plan updates). At completion time, `research.md` was still dirty →
`Complete()` rejected with "worktree has uncommitted changes."

## Fix

Add artifact file cleanup to `Complete()` before the `git status --porcelain` check.
Use the existing `loop.NewPhase()` / `ArtifactFile()` interface to discover which files
each phase produces, then reset or remove them.

### Step 1: Add artifact cleanup in `Complete()`

**File:** `internal/orchestrator/orchestrator.go`

Insert the following block after line 448 (`o.mu.Unlock()`) and before the porcelain
check (line 450, comment "Refuse to complete if there are uncommitted changes"):

```go
// Clean up artifact files left by pipeline phases (research.md, plan.md, etc.).
// Their content is already captured in snapshots — the on-disk copies are ephemeral.
if worktree != "" {
    for _, phaseName := range st.Pipeline {
        ph, err := loop.NewPhase(phaseName)
        if err != nil {
            continue
        }
        af := ph.ArtifactFile()
        if af == "" {
            continue
        }
        co := exec.Command("git", "checkout", "--", af)
        co.Dir = worktree
        if coErr := co.Run(); coErr != nil {
            os.Remove(filepath.Join(worktree, af))
        }
    }
}
```

**Why `git checkout` with `os.Remove` fallback:**
- If the artifact file existed in the base commit (tracked), `git checkout` resets it
  to the committed version, making the worktree clean for that file.
- If the artifact file was created by the stream (untracked), `git checkout` fails and
  `os.Remove` deletes it instead.

**No new imports needed.** The orchestrator already imports `loop`, `os`, `filepath`,
and `os/exec`.

### Step 2: Add a test

**File:** `internal/orchestrator/orchestrator_test.go` (or a new file if the existing
test file doesn't cover `Complete()`)

Test case: "Complete succeeds when artifact files are dirty"

1. Set up a stream with a `["research", "plan", "coding"]` pipeline.
2. Create the worktree and make a commit beyond the base SHA.
3. Write `research.md` and `plan.md` to the worktree (leaving them as uncommitted
   modifications or untracked files).
4. Call `Complete()` → assert it succeeds (no error).
5. Verify the worktree was removed and the stream status is `StatusCompleted`.

Also test the inverse: a non-artifact dirty file should still cause `Complete()` to
reject. Write an arbitrary file (e.g., `app.rb`) to the worktree without committing,
call `Complete()` → assert it returns an error containing "uncommitted changes."

### Step 3: Verify with `go test`

Run `go test ./internal/orchestrator/...` and confirm the new test passes alongside
existing tests.

## Summary

| Step | What | Files |
|------|------|-------|
| 1 | Add artifact cleanup before porcelain check | `internal/orchestrator/orchestrator.go` |
| 2 | Add test for dirty-artifact completion | `internal/orchestrator/orchestrator_test.go` |
| 3 | Run tests | — |
