# Resilient Stream Creation

**Problem**: When stream creation fails (e.g., git config issue, bad branch name), all wizard inputs are lost. The user must re-enter 6 steps of configuration from scratch.

**Solution**: Two complementary changes:
1. Keep the wizard open on failure so the user can retry after fixing the problem
2. Pre-validate git state before attempting creation to catch common issues early

## Step 1: Keep wizard open on creation failure

Currently `createStream()` unconditionally closes the wizard before launching the async create command:

```go
// app.go:1678-1679
m.showNewStream = false
m.newStreamStep = 0
```

Then when `streamCreatedMsg` arrives with an error, the wizard is already gone and only an error bar is shown.

**Changes:**

### `internal/ui/app.go` — `createStream()`

Instead of closing the wizard immediately, stash the wizard inputs and set a "creating" flag. The wizard overlay stays visible but shows a "Creating..." state (similar to how `m.creating` already works — just don't hide the overlay).

- Remove `m.showNewStream = false` and `m.newStreamStep = 0` from `createStream()`
- The overlay render function will detect `m.creating == true` and show a spinner/status

### `internal/ui/app.go` — `streamCreatedMsg` handler

On success: close the wizard as before (`m.showNewStream = false`, `m.newStreamStep = 0`).

On error: keep `m.showNewStream = true`, keep `m.newStreamStep` at the final step (where the user submitted), and show the error message inline in the wizard overlay instead of just the status bar. Add a new field `newStreamError string` to hold the error message for display inside the overlay.

```go
case streamCreatedMsg:
    m.creating = false
    if msg.err != nil {
        m.newStreamError = msg.err.Error()
        // wizard stays open — user can press Enter to retry
        return m, nil
    }
    // success: close wizard and start stream
    m.showNewStream = false
    m.newStreamStep = 0
    if err := m.orch.Start(msg.stream.ID); err != nil {
        m = m.withError("Stream created but failed to start: " + err.Error())
    }
    return m, nil
```

### `internal/ui/app.go` — wizard Enter handler (retry)

When the user presses Enter on the final step and `newStreamError` is non-empty, clear the error and call `createStream()` again. The wizard inputs are still in the textarea models — nothing was reset.

### `internal/ui/dashboard.go` — render the error in the overlay

When rendering the new stream overlay on the final step, if `newStreamError != ""`, show it as a styled error line above the "Press Enter to create" prompt. Clear the error when the user makes any change (edits a field, changes step).

### `internal/ui/app.go` — `beadsInitDoneMsg` handler

The beads init flow also needs updating. Currently the pending-state stash + `showNewStream = false` pattern is used for beads init. After beads init succeeds, the `beadsInitDoneMsg` handler calls `orch.Create()` directly using the pending fields. If *that* create fails, the same problem occurs — inputs are lost.

Update the `beadsInitDoneMsg` error path to reopen the wizard at the final step with the error displayed, rather than just showing a status bar error. Restore the wizard state from the pending fields.

## Step 2: Pre-validate before creating

Add an `Orchestrator.PreflightCheck()` method that validates prerequisites without side effects. Call it synchronously before launching the async `Create()` command. If it fails, show the error inline in the wizard without ever closing it or creating partial state.

### `internal/orchestrator/orchestrator.go` — new `PreflightCheck()` method

```go
func (o *Orchestrator) PreflightCheck() error {
    repoDir := o.config.RepoDir

    // Verify git HEAD is resolvable (catches empty repos, detached HEAD edge cases).
    if _, err := gitHead(repoDir); err != nil {
        return fmt.Errorf("git HEAD: %w", err)
    }

    // Verify the default branch exists and we can create worktrees from it.
    // This catches the "master vs main" misconfiguration.
    cmd := exec.Command("git", "rev-parse", "--verify", "HEAD")
    cmd.Dir = repoDir
    if err := cmd.Run(); err != nil {
        return fmt.Errorf("cannot resolve HEAD — is the repository initialized with commits?")
    }

    // Verify bd is available (catches missing beads CLI).
    if _, err := exec.LookPath("bd"); err != nil {
        return fmt.Errorf("beads CLI (bd) not found in PATH")
    }

    return nil
}
```

### `internal/ui/app.go` — call preflight in `createStream()`

Before setting `m.creating = true` and launching the async command, call `PreflightCheck()` synchronously. It's fast (just a couple of git commands) and avoids creating orphaned beads issues or partial state:

```go
func (m Model) createStream(...) (tea.Model, tea.Cmd) {
    // ... capture inputs ...

    if err := m.orch.PreflightCheck(); err != nil {
        m.newStreamError = err.Error()
        return m, nil  // wizard stays open, error displayed
    }

    // ... proceed with beads init check / async create ...
}
```

## Step 3: Clean up orphaned beads on worktree failure

Currently, if `createBeadsParent()` succeeds but `createWorktree()` fails, an orphaned beads issue is left behind. Add cleanup in `Create()`:

```go
parentID, err := createBeadsParent(title, repoDir)
if err != nil {
    return nil, fmt.Errorf("create beads parent: %w", err)
}

baseSHA, err := gitHead(repoDir)
if err != nil {
    cleanupBeadsParent(parentID, repoDir)
    return nil, fmt.Errorf("git HEAD: %w", err)
}

if err := createWorktree(repoDir, worktreePath, branch); err != nil {
    cleanupBeadsParent(parentID, repoDir)
    return nil, fmt.Errorf("create worktree: %w", err)
}
```

Where `cleanupBeadsParent` is a small helper that runs `bd close` + `bd delete --force`, logging but not propagating errors (best-effort cleanup).

## New fields on Model

```go
newStreamError string // error from last creation attempt, shown inline in wizard
```

## Summary of file changes

| File | What changes |
|------|-------------|
| `internal/ui/app.go` | Add `newStreamError` field. Update `createStream()` to not close wizard. Update `streamCreatedMsg` handler to keep wizard open on error, close on success. Add preflight call. Update beads init error path. Clear error on step changes. |
| `internal/orchestrator/orchestrator.go` | Add `PreflightCheck()` method. Add `cleanupBeadsParent()` helper. Add cleanup calls in `Create()` on partial failure. |
| `internal/ui/dashboard.go` | Update `renderNewStreamOverlay` to display `newStreamError` on the final step. |
