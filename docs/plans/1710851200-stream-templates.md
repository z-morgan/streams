# Stream Templates

**Branch:** `stream-templates`

## Context

Phases are currently hardcoded in a single global `phaseTree` in `ui/app.go`. Every stream creation shows the same set of phases. This makes it impossible to experiment with different phase configurations — for example, the step-per-iteration epic (`streams-4ah3`) adds a `refine` phase that changes the pipeline significantly, but there's no way to use the new pipeline without losing the old one.

Templates solve this by introducing named phase configurations. A template defines a name, description, and a phase tree (with nesting). When creating a stream, the user selects a template first, then the phase picker shows only that template's phases. The selected template name is stored on the stream for reference.

## Design

### Template data model

A template is a name, description, and a phase tree:

```go
type Template struct {
    Name        string      `json:"name"`
    Description string      `json:"description"`
    Phases      []PhaseNode `json:"phases"`
}
```

`PhaseNode` already exists in `ui/app.go` — it moves to `internal/stream/template.go` so it can be shared across packages. The tree structure captures nesting (e.g., decompose under plan), which the UI renders as indented checkboxes.

### Built-in templates

Hardcoded in Go. Initially just one:

- **Classic**: `research, plan > decompose, coding, review, polish` — the current phase set.

When `streams-4ah3` lands, a second template gets added (e.g., "Iterative" with `refine` after `coding`). Custom templates can also be defined in config.

### Template selection in creation flow

Current steps: title (0) → task (1) → phases (2) → models (3) → breakpoints (4)

New steps: title (0) → task (1) → **template (2)** → phases (3) → models (4) → breakpoints (5)

The template step shows a vertical list of available templates (name + description). Cursor navigation with j/k, enter to select. No phases are pre-selected — the user checks the ones they want in the next step.

If only one template exists, the step is still shown (establishing the pattern and making the template visible to the user).

### Phase picker changes

The phase picker currently references the global `phaseTree`. After this change, it uses the selected template's `Phases` tree instead. The existing functions (`flattenPhaseTree`, `childPhases`, `selectedPipeline`) already accept `[]PhaseNode` as a parameter, so they work unchanged — they just receive the template's tree instead of the global variable.

### Stream persistence

The `Stream` struct gains a `Template string` field recording which template was used at creation time. This is informational — it doesn't affect runtime behavior. The pipeline (selected phases) is still the authoritative execution state.

### Config support

Templates can be defined in `config.toml`. Built-in templates serve as defaults; config templates are additive (new names) or override built-ins (same name). This follows the existing config precedence model.

```toml
[[template]]
name = "Minimal"
description = "Just coding, no planning phases"
phases = "coding,review"
```

Nesting in config uses a `>` syntax within the comma-separated list:

```toml
phases = "research,plan>decompose,coding,review,polish"
```

Where `>` means "the next phase is a child of the previous one." This is compact and unambiguous since phase names don't contain `>`.

### What templates do NOT control

- Model selection (per-stream concern)
- Breakpoints (per-stream concern)
- Convergence settings (per-stream concern)
- Phase behavior/prompts (phases are shared entities referenced by name)

Copy-on-write phase customization (where a template can fork a shared phase into a template-scoped variant) is a future enhancement. For now, two templates that both include "coding" get the exact same coding phase implementation.

## Steps

### 1. Move PhaseNode to stream package and define Template type

**Files:** `internal/stream/template.go` (new), `internal/ui/app.go`

Move `PhaseNode` from `ui/app.go` to `internal/stream/template.go`. Define the `Template` struct. Add `BuiltinTemplates()` returning the Classic template. Add `FindTemplate(name, templates) *Template` helper.

Update `ui/app.go` to use `stream.PhaseNode` everywhere. Remove the old `PhaseNode` type and `phaseTree` variable. Update `flattenPhaseTree`, `childPhases`, `collectNames`, and `selectedPipeline` to use `stream.PhaseNode`.

### 2. Add Template field to Stream struct

**Files:** `internal/stream/stream.go`

Add `Template string` to the `Stream` struct. This records which template was used at creation. Add a getter `GetTemplate() string` following the existing pattern.

### 3. Thread template name through orchestrator.Create

**Files:** `internal/orchestrator/orchestrator.go`, `internal/ui/app.go`

Add a `template string` parameter to `orchestrator.Create`. Store it on the stream. Update `createStream` in `ui/app.go` to pass the selected template name. Update the pending fields for the beads-init stash flow.

### 4. Insert template picker step in UI creation flow

**Files:** `internal/ui/app.go`

Shift step numbers: phases becomes 3, models becomes 4, breakpoints becomes 5. Add new step 2 for template selection.

New Model fields:
```go
newStreamTemplates    []stream.Template  // available templates (populated on "n" press)
newStreamTemplateCur  int                // cursor into template list
newStreamTemplate     string             // selected template name
```

On pressing "n" to create a stream, populate `newStreamTemplates` from the orchestrator (which merges built-in + config templates).

Step 2 update handler: j/k to navigate, enter to select template and advance to step 3 (phases), esc to go back to step 1.

Step 2 render: show template list with cursor, name in bold, description below each name.

### 5. Wire phase picker to selected template's tree

**Files:** `internal/ui/app.go`

When transitioning from template selection (step 2) to phase picker (step 3):
- Look up the selected template by name
- Reset `newStreamChecked` to empty (no pre-selection)
- Reset `newStreamPhaseCur` to 0

Replace all `phaseTree` references in the creation flow with the selected template's `Phases`:
- `updateNewStreamPipeline` — use template phases for `flattenPhaseTree`, `childPhases`, `selectedPipeline`
- `updateNewStreamModels` — use template phases for `selectedPipeline`
- `updateNewStreamBreakpoints` — use template phases for `selectedPipeline`
- `renderNewStreamOverlay` — pass template phases for rendering step 2 (template list) and step 3+ (phase picker, models, breakpoints)
- `createStream` — use template phases for `selectedPipeline`

The `renderNewStreamOverlay` function signature gains a `templates []stream.Template` and `selectedTemplate string` parameter. The phase tree for steps 3+ is derived from the selected template.

### 6. Thread templates from config through orchestrator to UI

**Files:** `internal/config/config.go`, `internal/orchestrator/orchestrator.go`, `internal/ui/app.go`

Add template parsing to config:
```go
type TemplateConfig struct {
    Name        string
    Description string
    Phases      string // "research,plan>decompose,coding,review,polish"
}
```

Add `Templates []TemplateConfig` to `Config`. Parse `[[template]]` sections in config files.

Add `ParsePhaseTree(spec string) []stream.PhaseNode` that converts the compact string format to a phase tree. The `>` operator nests the next phase under the previous one.

Add `orchestrator.Templates() []stream.Template` that merges built-in templates with config-defined ones (config overrides built-in by name, adds new ones otherwise). Thread this to the UI so the template picker has the full list.

### 7. Display template name in UI

**Files:** `internal/ui/detail.go`, `internal/ui/dashboard.go`

Show the template name in the detail view header (next to stream name/status). This is a small addition — just read `st.GetTemplate()` and render it in a dimmed style.

Optionally show it in the dashboard list view as a column if space permits.

### 8. Tests

**Files:** `internal/stream/template_test.go` (new), `internal/config/config_test.go`, `internal/ui/app_test.go`

- Test `BuiltinTemplates()` returns expected templates with correct phase trees
- Test `FindTemplate` lookup by name
- Test `ParsePhaseTree` with flat lists, nested phases, and multiple nesting levels
- Test config parsing of `[[template]]` sections
- Test that template merge (built-in + config) handles overrides and additions correctly
- Test `selectedPipeline` with different template trees produces correct ordered pipelines

## Notes

- The `defaultPhaseChecks` call at line 590 of `app.go` currently pre-selects phases based on `DefaultPipeline()` from config. With templates, this becomes: no pre-selection (empty checked map). The `DefaultPipeline` config key could be deprecated in favor of templates, but for now both can coexist — `DefaultPipeline` is used as the fallback when `orchestrator.Create` receives an empty pipeline, which is a different code path from the UI creation flow.

- The `renderNewStreamOverlay` function already has a large parameter list (line 2195). Rather than adding more parameters, consider grouping the template-related state into a struct. But this is a refactor that can happen separately if the parameter list gets unwieldy.

- When `streams-4ah3` is implemented, adding the "Iterative" template is a one-line addition to `BuiltinTemplates()` — no structural changes needed.
