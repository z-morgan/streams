# Per-Phase Model Configuration

## Goal

Make the Claude model configurable per-phase on each stream. Users should be able
to pick a single model for all phases or assign different models to individual
phases. The available model list should stay current automatically. An existing
stream's model config should be editable from the detail view via a new
"stream config" menu that also absorbs the current breakpoints overlay.

## Design Decisions

### Model discovery

The Claude CLI `--model` flag accepts **aliases** (`sonnet`, `opus`, `haiku`)
that auto-resolve to the latest version, plus full model IDs like
`claude-sonnet-4-6`. Aliases are the primary mechanism for staying current on
new model drops — when Anthropic ships Sonnet 5.0, `--model sonnet` picks it up
automatically.

For users who want to pin a specific version or see what's available, we add
dynamic discovery via the Anthropic Models API (`GET /v1/models`). This requires
an API key (`ANTHROPIC_API_KEY` env var). When available, the TUI fetches the
model catalog once at startup and merges it with the aliases. When unavailable,
the alias list is sufficient.

Model list priority:
1. `default` — no `--model` flag, uses whatever the CLI is configured with
2. Aliases — `sonnet`, `opus`, `haiku` (always present, auto-track latest)
3. API-discovered models — full IDs grouped by family (only when API key set)
4. Custom input — user can type an arbitrary model ID

### Data model

```go
// ModelConfig holds per-phase model selections.
type ModelConfig struct {
    Default  string            `json:"default,omitempty"`   // model for all phases unless overridden
    PerPhase map[string]string `json:"per_phase,omitempty"` // phase name → model override
}

// ModelForPhase returns the model to use for a given phase.
// Resolution: per-phase override → default → "" (CLI default).
func (mc ModelConfig) ModelForPhase(phase string) string {
    if m, ok := mc.PerPhase[phase]; ok && m != "" && m != "default" {
        return m
    }
    if mc.Default != "" && mc.Default != "default" {
        return mc.Default
    }
    return "" // empty = don't pass --model, use CLI default
}
```

Stored in `stream.json` as a `models` field alongside `breakpoints`, `notify`, etc.

### Threading through the runtime

The model flows: `Stream.Models` → `loop.Run()` → `buildRequest()` →
`Request.Options["model"]` → `appendCommonArgs()` → `--model <value>`.

`buildRequest` gets a new `model string` parameter. The loop looks up
`s.Models.ModelForPhase(phase.Name())` before each implement and review call.
Same for `runSlots` (polish phase uses its phase-level model for all slots).

### Wizard flow

Current wizard steps: Title(0) → Task(1) → Phases(2) → Breakpoints(3)

New: Title(0) → Task(1) → Phases(2) → **Models(3)** → Breakpoints(4)

The model step has two modes:

**All-phases mode** (default):
```
Select Model

  ▌● default (CLI default)
    sonnet
    opus
    haiku
    ────────────
    claude-sonnet-4-6        ← only if API models fetched
    claude-opus-4-6
    ...
    ────────────
    [enter custom model...]

  ☐ Configure per phase

  j/k: navigate  space: select  enter: confirm  esc: back
```

Selecting "Configure per phase" switches to **per-phase mode**:
```
Select Models per Phase

    research    default ▸
  ▌ plan        sonnet  ▸
    coding      default ▸
    review      haiku   ▸

  ☐ Configure per phase

  j/k: navigate phase  ←/→ or h/l: cycle model  enter: confirm  esc: back
```

In per-phase mode, each phase row shows its current model. `h/l` (or `←/→`)
cycles through the model list for the focused phase. The available models are the
same list used in all-phases mode.

### Stream config menu

The `b` shortcut in the detail view currently opens an "Edit Breakpoints" overlay.
This becomes a tabbed "Stream Config" overlay with two sections:

```
┌─ Stream Config ──────────────────────────────────┐
│                                                   │
│  ▌Breakpoints    Models                           │
│                                                   │
│  research                                         │
│  ── [x] pause after research ──                   │
│  plan                                             │
│  ── [ ] pause after plan ──                       │
│  coding                                           │
│                                                   │
│  1 ● bell   2 ○ flash   3 ○ system                │
│                                                   │
│  tab: switch section  j/k: navigate  space: toggle│
│  enter: save  esc: cancel                         │
└───────────────────────────────────────────────────┘
```

Pressing `tab` switches to the Models tab:

```
┌─ Stream Config ──────────────────────────────────┐
│                                                   │
│   Breakpoints   ▌Models                           │
│                                                   │
│  All phases:  default ▸                           │
│  ── or ──                                         │
│  ☐ Configure per phase                            │
│                                                   │
│                                                   │
│  tab: switch section  j/k: navigate  h/l: cycle   │
│  space: toggle per-phase  enter: save  esc: cancel│
└───────────────────────────────────────────────────┘
```

The Models tab reuses the same two-mode UX from the wizard (all-phases vs
per-phase), but pre-populated with the stream's current config.

---

## Implementation Plan

### Step 1: ModelConfig type + persistence + CLI flag

Add the data model, wire `--model` through the runtime, and persist to disk.
No UI changes yet — streams default to empty ModelConfig (CLI default model).

**Files changed:**
- `internal/stream/stream.go` — Add `ModelConfig` type with `ModelForPhase()`,
  add `Models ModelConfig` field to `Stream`, add `GetModels()`/`SetModels()`.
- `internal/store/store.go` — Add `Models *modelConfigData` to `streamData`,
  update `toStreamData()`/`fromStreamData()`.
- `internal/runtime/claude/claude.go` — In `appendCommonArgs()`, add case for
  `req.Options["model"]` → `--model <value>`.
- `internal/loop/loop.go` — Change `buildRequest()` signature to accept `model string`.
  In `Run()`, look up `s.Models.ModelForPhase(phase.Name())` before calling
  `buildRequest()` for both implement and review requests. Same in `runSlots()`.

**Test:** `go build`, existing tests pass. Create a stream, verify stream.json
has `"models": null` or omitted. Manually set a model in stream.json, start the
stream, verify `--model` appears in the CLI invocation (visible in debug logs).

### Step 2: Model discovery

Fetch available models from the Anthropic API at startup, fall back to aliases.

**Files changed:**
- `internal/models/models.go` (new) — `ModelList` struct (aliases + discovered).
  `Fetcher` struct with `FetchAsync()` (background goroutine), `Models()` (returns
  cached result). Uses `ANTHROPIC_API_KEY` env var. Calls
  `GET https://api.anthropic.com/v1/models` with `x-api-key` header, parses
  response to extract model IDs. Filters to Claude models, groups by family.
  Always prepends `["default", "sonnet", "opus", "haiku"]` as aliases.
- `internal/ui/app.go` — `Model` struct gets `modelFetcher *models.Fetcher`.
  Initialize in `New()`, call `FetchAsync()` in `Init()`.

**Test:** `go build`. With `ANTHROPIC_API_KEY` set, verify models are fetched
(add a debug log). Without it, verify aliases are returned.

### Step 3: Wizard model selection step (all-phases mode)

Add the model step to the new-stream wizard. Initially just the all-phases
single-model selection.

**Files changed:**
- `internal/ui/app.go`:
  - Add wizard state: `newStreamModelCursor int`, `newStreamModels stream.ModelConfig`,
    `newStreamPerPhase bool`.
  - Shift breakpoints from step 3 to step 4.
  - Add `updateNewStreamModels()` handler for step 3.
  - Render model list with cursor, "configure per phase" checkbox.
  - On enter at step 3 → advance to step 4 (breakpoints).
  - Update `createStream()` to pass `ModelConfig` to orchestrator.
- `internal/orchestrator/orchestrator.go`:
  - Add `models stream.ModelConfig` parameter to `Create()`.
  - Set `st.Models = models` in the created stream.
- `cmd/streams/main.go` — Update `Create()` call if needed (likely no change
  since TUI drives creation).

**Test:** Build, run TUI, create a stream. Verify the model step appears between
phases and breakpoints. Select "opus", confirm, verify stream.json contains
`"models": {"default": "opus"}`.

### Step 4: Per-phase model selection in wizard

Extend the wizard model step with per-phase mode.

**Files changed:**
- `internal/ui/app.go`:
  - Add state: `newStreamPhaseModelCursor int` (which phase row is focused),
    `newStreamPhaseModels map[string]string` (per-phase selections).
  - When "Configure per phase" is toggled, switch rendering to show each
    selected pipeline phase with its model.
  - `h/l` or `←/→` cycles the model for the focused phase.
  - On enter, build `ModelConfig{PerPhase: ...}` and advance to breakpoints.

**Test:** Build, create a stream with research+plan+coding pipeline. Toggle
"Configure per phase", set research=haiku, plan=sonnet, coding=opus. Verify
stream.json has per-phase entries. Start stream, verify each phase invocation
uses the correct `--model` flag.

### Step 5: Stream config menu (replaces breakpoints overlay)

Replace the `b` breakpoints overlay with a tabbed config menu containing
Breakpoints and Models tabs.

**Files changed:**
- `internal/ui/app.go`:
  - Replace `showEditBreakpoints bool` with `showStreamConfig bool`.
  - Add `streamConfigTab int` (0=breakpoints, 1=models).
  - Add model-editing state: `editModelConfig stream.ModelConfig`,
    `editModelCursor int`, `editPerPhase bool`,
    `editPhaseModelCursor int`.
  - `updateStreamConfig()` handler: `tab` switches tabs, delegates to
    `updateStreamConfigBreakpoints()` or `updateStreamConfigModels()`.
  - Breakpoints tab: same logic as current `updateEditBreakpoints()`, just
    nested under the tab.
  - Models tab: same UX as wizard model step (all-phases + per-phase toggle).
  - On `enter`: save both breakpoints AND model config to stream.
  - Rename `renderEditBreakpointsOverlay()` → `renderStreamConfigOverlay()`.
    Render tab header with active indicator, then delegate to tab-specific
    content renderer.
  - Update `b` key handler in `updateDetail()` to set `showStreamConfig = true`
    and initialize both breakpoint and model editing state.
  - Update overlay priority in `Update()`.
- `internal/stream/stream.go` — Ensure `SetModels()` is thread-safe (already
  mutex-protected via the `mu` field pattern).

**Test:** Build, run TUI, open a stream's detail view, press `b`. Verify the
tabbed config menu appears with Breakpoints tab active. Press `tab` to switch
to Models. Change model selection, press `enter`. Verify stream.json updated.
Restart stream, verify new model is used.

---

## Non-goals

- **Per-step model selection** (implement vs review within a phase): Out of scope.
  Each phase gets one model that's used for both implement and review steps.
- **Polish slot-level models**: Polish phase gets one model for all its slots.
- **Model validation**: We pass the model string to the CLI as-is. The CLI
  handles validation and error reporting.
- **Effort/thinking budget per phase**: Potentially a future addition to the
  same config menu, but not part of this work.
