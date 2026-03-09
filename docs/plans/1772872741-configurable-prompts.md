# Configurable Phase Prompts

## Problem

Phase prompts are hardcoded in Go structs (`PlanPhase`, `DecomposePhase`, etc.). When streams invokes `claude -p` in a user's project directory, Claude Code loads the user's CLAUDE.md files. If those contain planning/coding guidelines, they can conflict with the hardcoded phase prompts. Users also can't customize how streams orchestrates their work.

## Design

Replace hardcoded `fmt.Sprintf` prompts with Go `text/template` templates. Templates are loaded from a user-level config directory with embedded defaults as fallback.

### Resolution order

1. `~/.config/streams/prompts/<phase>-<step>.tmpl` (user override)
2. Embedded default (what's hardcoded today)

### Template variables

All templates receive a `PromptData` struct:

```go
type PromptData struct {
    Task         string // stream task description
    ParentID     string // beads parent issue ID
    Iteration    int    // current iteration number
    OrderedSteps string // formatted step list (coding phase)
}
```

### Template file naming

```
research-implement.tmpl
research-review.tmpl
plan-implement.tmpl
plan-review.tmpl
decompose-implement.tmpl
decompose-review.tmpl
coding-implement.tmpl
coding-review.tmpl
```

### What stays non-configurable

`orchestratorRules` in `loop.go` — these are safety rails (no push, no commit unless told, tool restrictions) appended via `--append-system-prompt`. Users can customize *what* the agent does, not bypass operational guardrails.

## Steps

### 1. Add prompt loader with embedded defaults

Create `internal/loop/prompts.go`:
- Embed default `.tmpl` files from `internal/loop/prompts/` using `//go:embed`
- `PromptData` struct with the template variables
- `LoadPrompt(phase, step string, data PromptData) (string, error)` function that:
  1. Checks `~/.config/streams/prompts/<phase>-<step>.tmpl`
  2. Falls back to embedded default
  3. Executes the template with `PromptData`
- Unit test: verify embedded defaults load and render correctly
- Unit test: verify user override takes precedence (use `t.TempDir()` as config dir)

### 2. Extract current prompts into template files

Create `internal/loop/prompts/` directory with 8 `.tmpl` files. These are 1:1 translations of the current `fmt.Sprintf` strings, using `{{.Task}}`, `{{.ParentID}}`, etc.

One wrinkle: several phases have different prompts for iteration 0 vs subsequent iterations. Handle this with `{{if eq .Iteration 0}}` conditionals inside the template, keeping it to one template file per phase-step pair.

### 3. Refactor phases to use the loader

Update `ImplementPrompt` and `ReviewPrompt` on all 4 phase structs to call `LoadPrompt()` instead of `fmt.Sprintf`. The phase structs become thin wrappers — they still own `Name()`, tool lists, `IsConverged()`, `BeforeReview()`, and `TransitionMode()`, but delegate prompt generation to the template system.

Since `LoadPrompt` can return an error but the `MacroPhase` interface methods return `string`, add a second return value to `ImplementPrompt` and `ReviewPrompt` in the interface, and update the call sites in `loop.go`.

### 4. Update tests

- Update `plan_test.go`, `decompose_test.go`, and any other phase tests to assert against rendered template output
- Test that the iteration-0 vs subsequent-iteration branching works in templates
- Test error case: malformed user template returns a clear error

### 5. Add `streams prompts` CLI subcommand

Add a way for users to bootstrap their prompt overrides:
- `streams prompts --list` — show all prompt template names
- `streams prompts --export <name>` — print the default template to stdout so users can pipe it to their config dir and edit

This avoids users needing to guess the template format or variables.
