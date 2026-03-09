# Polish Phase

## Problem

After the coding phase converges, the stream's work is functionally complete but may have rough edges: inconsistent commit messages, linter violations, style issues. Currently there's no automated way to clean these up — the user must do it manually or hope the coding agent caught everything.

## Design

A new `polish` pipeline phase that runs N configurable **slots** serially over the completed work. Each slot is a single agent invocation with its own prompt template, tool set, and scope. The phase runs once (single iteration, always converges) and auto-advances.

### Slot Definition

```go
type SlotScope string

const (
    ScopeDiff   SlotScope = "diff"   // agent sees whole-stream commit log and diff stat
    ScopeCommit SlotScope = "commit" // prompt includes per-commit SHAs, messages, and diffs
)

type Slot struct {
    Name   string
    Scope  SlotScope
    Tools  []string
    Budget string // optional per-slot budget cap (e.g. "1.00"); empty = inherit stream budget
}
```

The prompt template for each slot follows the existing pattern: `polish-<name>.tmpl` loaded via `LoadPrompt("polish", name, data)`. User overrides at `~/.config/streams/prompts/polish-<name>.tmpl`.

### Default Slots

Three defaults ship embedded, in this order:

1. **lint** (`polish-lint.tmpl`, commit-scoped) — Detect the project's linter from config files and run it per commit. Fix violations with fixup commits.
2. **rubocop** (`polish-rubocop.tmpl`, commit-scoped) — Run rubocop per commit. Pre-flight check for Gemfile/.rubocop.yml; graceful early-exit if not a Ruby project.
3. **commit-messages** (`polish-commits.tmpl`, diff-scoped) — Rewrite commit messages to follow project conventions using `git rebase -i`.

### Prompt Data

Extend `PromptData` with two new fields:

- `BaseSHA string` — the stream's rebase target, needed by all polish templates for git commands.
- `Commits string` — pre-formatted per-commit sections (SHA, message, diff) for commit-scoped slots. Empty for diff-scoped slots.

Diff-scoped slots reuse the existing `CommitLog` and `DiffStat` fields (already on PromptData, currently used by ReviewPhase).

### Loop Integration

New interface:

```go
type SlottedPhase interface {
    MacroPhase
    Slots() []Slot
}
```

At the top of `Run()`, type-assert the phase. If it's a `SlottedPhase`, call `runSlots()` instead of entering the implement→review cycle:

```go
if slotted, ok := phase.(SlottedPhase); ok {
    runSlots(ctx, s, slotted, rt, git, onCheckpoint)
    return
}
```

`runSlots()` iterates over `phase.Slots()`:

1. Build prompt data (commits for commit-scoped, commit log + diff stat for diff-scoped).
2. Load and render the slot's prompt template.
3. Invoke the agent (with optional `BudgetRuntime` wrapper if slot has a budget cap).
4. For commit-scoped slots, run autosquash to collapse fixup commits before the next slot sees history.
5. Record a `Snapshot` with `SlotName` set so the UI can distinguish slot outputs.
6. Fire `onCheckpoint`.

After all slots complete, set `Converged = true` and `Status = Paused`.

### Snapshot Extension

Add `SlotName string` to the `Snapshot` struct. Empty for non-polish phases. The detail view can use this to show "polish · lint" instead of "polish iter 0".

### Configuration

Add `polish-slots` as a new config key, following the existing `pipeline` pattern (comma-separated names):

```
polish-slots = "lint,rubocop,commit-messages"
```

If set, it **fully replaces** the defaults (consistent with how `pipeline` works — explicit config means you know what you want). If unset, all three defaults run.

The `PolishPhase` needs access to the resolved config to know which slots to run. Rather than changing the `PhaseFactory` signature, the orchestrator builds a closure that captures config:

```go
factory := func(name string) (loop.MacroPhase, error) {
    if name == "polish" {
        return loop.NewPolishPhase(resolvedSlotNames), nil
    }
    return loop.NewPhase(name)
}
```

### Phase Registration

- Add `"polish"` case to `NewPhase()` (uses defaults when called without config).
- Add `{Name: "polish"}` to the `phaseTree` in `app.go` (as a top-level node after "coding", before "review").
- `ValidatePipeline` accepts "polish" as a valid phase name.

### Failure Handling

When a slot's agent invocation fails:
- Record the error in the slot's snapshot.
- **Continue to the next slot** — polish work is best-effort. A linter failure shouldn't block commit message cleanup.
- After all slots run, if any slot errored, set `LastError` to the first failure so the user sees it in the dashboard.

## Implementation Steps

### Step 1: One slot end-to-end (diff-scoped)

Wire the simplest slot through every layer.

**Files:** `internal/loop/slot.go`, `internal/loop/prompts/polish-commits.tmpl`, `internal/loop/polish.go`, `internal/loop/loop.go`, `internal/loop/phase.go`, `internal/loop/prompts.go`, `internal/stream/snapshot.go`

- Define `Slot`, `SlotScope`, `SlottedPhase` interface, and `DefaultSlots()` in `slot.go`.
- Add `BaseSHA` field to `PromptData`.
- Add `SlotName` field to `Snapshot`.
- Write `polish-commits.tmpl` — instructs agent to review and rewrite commit messages using `git rebase -i {{.BaseSHA}}`.
- Implement `PolishPhase` in `polish.go`: `Name()` returns "polish", `Slots()` returns configured slots, MacroPhase methods are stubs (never called by `runSlots`). Constructor `NewPolishPhase(slotNames []string)` filters `DefaultSlots()` by the given names.
- Add `runSlots()` to `loop.go` with the type-assertion branch in `Run()`.
- Register `"polish"` in `NewPhase()`.
- Test: mock runtime, one diff-scoped slot, verify snapshot recorded with slot name and converged state.

### Step 2: Commit-scoped slots + autosquash

Add per-commit data gathering and the two commit-scoped templates.

**Files:** `internal/loop/prompts/polish-lint.tmpl`, `internal/loop/prompts/polish-rubocop.tmpl`, `internal/loop/slot.go`, `internal/loop/polish.go`

- Add `Commits` field to `PromptData`.
- Add `gatherCommitData(workDir, baseSHA string) (string, error)` to `polish.go` — runs `git log --reverse --format=... -p` between BaseSHA and HEAD, formats each commit as a section with SHA, message, and diff.
- In `runSlots()`, populate `Commits` for commit-scoped slots and `CommitLog`/`DiffStat` for diff-scoped slots.
- After each commit-scoped slot, run autosquash (reuse `CodingPhase.BeforeReview` logic — extract the autosquash/stash/restore sequence into a shared helper).
- Write `polish-lint.tmpl` — detect project linter from config files, run per commit, fixup violations.
- Write `polish-rubocop.tmpl` — pre-flight check for Ruby project, run rubocop per commit, fixup violations.
- Tests: commit-scoped slot gets commit data in prompt, autosquash runs after commit-scoped slot.

### Step 3: Config + orchestrator wiring

**Files:** `internal/config/config.go`, `internal/orchestrator/orchestrator.go`, `internal/ui/app.go`

- Add `PolishSlots *string` to `Config` struct.
- Handle `"polish-slots"` in `parse()`, `SetInFile()`, `WriteFile()`, `Format()`, `ValidKeys()`.
- Default: `nil` (meaning "use built-in defaults").
- In orchestrator, build a factory closure that passes resolved slot names to `NewPolishPhase()` when phase is "polish".
- Add `{Name: "polish"}` to `phaseTree` in `app.go`.
- Tests: config parsing, merge, slot name validation.

### Step 4: Integration test

**Files:** `internal/loop/polish_test.go`

- Full pipeline test: coding → polish with mock runtime.
- Verify: slots run in order, each gets its own snapshot with `SlotName`, phase converges, autosquash runs after commit-scoped slots.
- Verify: rubocop slot prompt includes pre-flight skip logic.
- Verify: slot failure doesn't halt remaining slots.
- Verify: config override replaces default slot list.
