# Per-Stream Pipeline Selection

## Problem

The pipeline (which macro-phases a stream runs through) is set globally at
launch via `--pipeline` flag, defaulting to `"coding"`. All streams inherit this
same pipeline. A research-oriented task like "Understand the authentication
architecture" gets the `coding` phase, when it should start with `plan`.

## Goal

Let users pick a pipeline preset when creating a stream in the TUI. The global
`--pipeline` flag becomes the default, but each stream can override it.

## Design

Add a second step to the "New Stream" overlay. After entering a task description
and pressing enter, the user sees a pipeline picker with presets:

```
Pipeline:
> plan + code        (plan → coding)
  full               (plan → decompose → coding)
  code only          (coding)
```

Navigate with j/k, confirm with enter, esc to go back to task input.

The presets are defined as a Go constant slice — no config file. The default
cursor position is determined by the global `--pipeline` flag (whichever preset
matches it, or "code only" if no match).

## Steps

### 1. Add pipeline presets constant

**File:** `internal/ui/app.go`

Define the presets as a package-level var:

```go
var pipelinePresets = []struct {
    Label    string
    Pipeline []string
}{
    {"plan + code", []string{"plan", "coding"}},
    {"full", []string{"plan", "decompose", "coding"}},
    {"code only", []string{"coding"}},
}
```

### 2. Add per-stream pipeline to orchestrator.Create

**File:** `internal/orchestrator/orchestrator.go`

Change `Create(task string)` to `Create(task string, pipeline []string)`. If the
`pipeline` argument is nil/empty, fall back to `o.config.Pipeline` (preserving
current default behavior for headless mode).

Update `createBeadsParent` — no changes needed, it doesn't use pipeline.

Update the headless `runHeadless` caller in `main.go` to pass `nil` (uses global
default).

### 3. Add pipeline picker state to Model

**File:** `internal/ui/app.go`

Add to `Model`:

```go
newStreamStep     int  // 0 = task input, 1 = pipeline picker
newStreamPipeline int  // cursor index into pipelinePresets
```

### 4. Update the "New Stream" overlay flow

**File:** `internal/ui/app.go`

Modify `updateNewStream`:

- **Step 0 (task input):** On enter, if task is non-empty, advance to step 1.
  Set `newStreamPipeline` to the preset index matching `o.config.Pipeline`
  (default to last preset if no match).
- **Step 1 (pipeline picker):** j/k to move cursor, enter to confirm and
  create the stream, esc to go back to step 0.

Modify `renderNewStreamOverlay` to render the appropriate step:

- Step 0: current task input (unchanged)
- Step 1: show the task as a header, then the preset list with cursor indicator

### 5. Pass selected pipeline through to Create

**File:** `internal/ui/app.go`

In the enter handler for step 1, call `orch.Create(task, selectedPipeline)`
instead of `orch.Create(task)`. The beads init flow (`pendingTask`) also needs
to carry the selected pipeline — add `pendingPipeline []string` to Model.

### 6. Update headless mode caller

**File:** `cmd/streams/main.go`

Change `orch.Create(task)` to `orch.Create(task, nil)` so it uses the global
default.

### 7. Tests

- Update `orchestrator_test.go` to pass pipeline to `Create`.
- Test that `Create(task, nil)` uses config default.
- Test that `Create(task, []string{"plan","coding"})` sets the stream's pipeline.
