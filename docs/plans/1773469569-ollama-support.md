# Ollama Model Support + Rate Limit Fallback

## Context

Streams currently only supports Anthropic models via the Claude Code CLI. The user wants:

1. **Ollama model selection** — Pick a local Ollama model when creating/configuring a stream
2. **Rate limit fallback** — When the primary (Anthropic) model hits a rate/usage limit, automatically continue with a local Ollama model

### Current Architecture

- **Model discovery** (`internal/models/models.go`): `Fetcher` queries `GET https://api.anthropic.com/v1/models` and exposes aliases + API models via `AllOptions()`
- **Model config** (`internal/stream/stream.go`): `ModelConfig` holds `Default` + `PerPhase` map. `ModelForPhase()` resolves which model string to use
- **Runtime** (`internal/runtime/claude/claude.go`): Passes `--model <value>` to `claude -p`. The claude CLI subprocess gets its environment from `cmd.Environ()`
- **Error handling** (`internal/loop/loop.go`): `classifyError()` distinguishes `ErrBudget` from `ErrRuntime`. No rate limit detection exists. All errors pause the stream for manual intervention
- **Model picker UI** (`internal/ui/app.go`): Step 3 of the new-stream wizard. Shows a flat list from `modelFetcher.AllOptions()`. Supports all-phases and per-phase selection modes
- **Store** (`internal/store/store.go`): Persists `ModelConfig` as `modelConfigData` in `stream.json`

### Key Uncertainty

How does Claude Code CLI connect to Ollama? Possible mechanisms:
- Environment variables (`ANTHROPIC_BASE_URL`, `OPENAI_BASE_URL`, etc.)
- A `--provider` flag
- A custom model prefix convention

**This must be researched first.** The runtime adaptation layer depends on the answer. The user says they run `ollama serve` then `ollama launch claude` — the exact flags/env vars that make this work need to be identified.

---

## Plan

### Step 1: Ollama Client Package

Create `internal/ollama/ollama.go` with functions to talk to the Ollama HTTP API:

- `IsRunning() bool` — HTTP GET `http://localhost:11434` (Ollama's default health endpoint). Returns true if the server responds.
- `ListModels() ([]Model, error)` — HTTP GET `http://localhost:11434/api/tags`. Returns the list of locally available models (name, size, etc.).
- `Model` struct — `Name string`, `Size int64`, `ModifiedAt time.Time`

Keep this minimal. No background goroutines — call these on-demand from the UI.

**Files**: `internal/ollama/ollama.go`

### Step 2: Integrate Ollama Models into Model Discovery

Extend `models.Fetcher` to also discover Ollama models:

- Add an `ollamaModels []OllamaEntry` field and an `ollamaFetched bool`
- On `FetchAsync()`, also launch an Ollama discovery goroutine (calls `ollama.ListModels()`)
- Add `OllamaOptions() []string` that returns model names with an `ollama:` prefix (e.g., `ollama:llama3.2`)
- Update `AllOptions()` to return: aliases → Anthropic API models → Ollama models
- Add `IsOllamaModel(name string) bool` helper that checks for the `ollama:` prefix
- Add `OllamaRunning() bool` that delegates to `ollama.IsRunning()`

**Files**: `internal/models/models.go`, `internal/ollama/ollama.go`

### Step 3: Model Picker UI — Show Ollama Models

Update the model selection step (step 3) in the new-stream wizard:

- Render the model list in grouped sections:
  - **Aliases** — `default`, `sonnet`, `opus`, `haiku`
  - **Anthropic API** — discovered API models
  - **Local (Ollama)** — discovered Ollama models, each with `ollama:` prefix
- Show an Ollama status indicator in the section header:
  - `Local (Ollama) ● running` (green) or `Local (Ollama) ○ not running` (dim)
- If Ollama is not running and has no models, show a hint: `Start with: ollama serve`
- The cursor skips section headers; only model entries are selectable

This also applies to the per-phase model dropdown (h/l cycling).

**Files**: `internal/ui/app.go` (new-stream wizard rendering + key handling)

### Step 4: Runtime — Provider-Aware Model Passing

Modify the claude runtime to handle `ollama:`-prefixed model names:

- In `appendCommonArgs()`, detect the `ollama:` prefix on the model value
- Strip the prefix and pass the bare model name to `--model`
- Set provider-specific environment variables on `cmd.Env` (the exact vars depend on Step 0 research — likely `ANTHROPIC_BASE_URL=http://localhost:11434/v1` or a `--provider` flag)
- The BudgetRuntime wrapper passes through unchanged — budget may not apply to Ollama, but the claude CLI will handle it gracefully

This is the step that depends most on the research into how Claude Code talks to Ollama. The plan assumes we need to set env vars on the subprocess; adjust if a CLI flag works instead.

**Files**: `internal/runtime/claude/claude.go`

### Step 5: Rate Limit Error Detection

Add rate limit awareness to the error classification system:

- Add `ErrRateLimit ErrorKind` to `internal/stream/errors.go`
- Update `classifyError()` in `internal/loop/loop.go` to detect rate limit errors. Claude CLI error messages typically contain strings like:
  - `"rate limit"`, `"rate_limit"`, `"429"`, `"overloaded"`, `"too many requests"`, `"usage limit"`
  - Check both the error string and the CLI stderr output
- Update the error kind display name and the store's `parseErrorKind()` for the new variant
- Update `internal/diagnosis/tab.go` and `internal/diagnosis/prompt.go` if they reference error kinds

**Files**: `internal/stream/errors.go`, `internal/loop/loop.go`, `internal/store/store.go`, `internal/diagnosis/`

### Step 6: Fallback Configuration — Data Model + Persistence

Add a fallback model configuration to the stream:

- Add `FallbackConfig` struct to `internal/stream/stream.go`:
  ```go
  type FallbackConfig struct {
      Enabled bool   `json:"enabled"`
      Model   string `json:"model"` // e.g. "ollama:llama3.2"
  }
  ```
- Add `Fallback FallbackConfig` field to `Stream`
- Add thread-safe `SetFallback(FallbackConfig)` / `GetFallback() FallbackConfig` methods
- Add `fallbackData` to `internal/store/store.go`, wire into `toStreamData` / `fromStreamData`

**Files**: `internal/stream/stream.go`, `internal/store/store.go`

### Step 7: Fallback Configuration — New Stream Wizard

Add a fallback configuration step to the new-stream wizard:

- Add as step 5 (after breakpoints, before confirmation) — or integrate into the model selection step (step 3) as a sub-section
- **Decision**: integrate into step 3 (model selection) to keep related config together
- Below the model picker, add a toggle: `[space] Enable rate limit fallback`
- When enabled, show a second model picker for the fallback model (filtered to Ollama models only, since the point is to fall back to a local model)
- Show Ollama status: `● Ollama running (3 models available)` or `○ Ollama not running`
- Store the result in `newStreamFallback FallbackConfig` on the Model struct

**Files**: `internal/ui/app.go`

### Step 8: Fallback Configuration — Settings Overlay

Add a way to configure fallback on a running/paused stream:

- Add a new overlay accessible from the detail view (e.g., `f` key for "fallback")
- Shows:
  - Toggle: enabled/disabled
  - Fallback model selector (Ollama models)
  - Ollama server status
- Applies changes via `s.SetFallback()`

**Files**: `internal/ui/app.go` or `internal/ui/detail.go`

### Step 9: Auto-Retry with Fallback Model

Modify the loop to automatically retry on rate limit errors when fallback is configured:

- In `loop.go`, after `classifyError()` returns `ErrRateLimit`:
  1. Check `s.GetFallback().Enabled`
  2. If not enabled, fall through to normal error recording (pause)
  3. If enabled:
     - Log a message to output: `"Rate limit hit — falling back to <fallback model>"`
     - Rebuild the request with the fallback model substituted
     - Retry `rt.Run(ctx, fallbackReq)`
     - If the fallback also fails, record the error and pause
     - If it succeeds, continue the loop normally with the fallback response
- The fallback applies per-invocation. On the next iteration, the loop uses the primary model again (giving the rate limit time to clear). If it hits again, it falls back again.
- Track whether the current invocation used a fallback model in the snapshot for visibility

**Files**: `internal/loop/loop.go`, `internal/stream/snapshot.go` (add `UsedFallback bool` field)

### Step 10: Snapshot + UI — Fallback Visibility

Make it visible in the UI when a fallback was used:

- Add `UsedFallback bool` and `FallbackModel string` to `Snapshot`
- In the detail view snapshot rendering, show an indicator when fallback was used (e.g., `⚡ Used fallback: ollama:llama3.2`)
- In the dashboard, optionally show a status indicator on the stream channel

**Files**: `internal/stream/snapshot.go`, `internal/ui/detail.go`, `internal/ui/dashboard.go`

---

## Commit Strategy

| Commit | Steps | Description |
|--------|-------|-------------|
| 1 | 1 | Add Ollama client package |
| 2 | 2 | Integrate Ollama models into model discovery |
| 3 | 3 | Show Ollama models in model picker UI |
| 4 | 4 | Runtime: provider-aware model passing |
| 5 | 5 | Add ErrRateLimit error kind |
| 6 | 6 | Add FallbackConfig to stream data model |
| 7 | 7-8 | Fallback config UI (wizard + settings overlay) |
| 8 | 9 | Auto-retry loop logic on rate limit |
| 9 | 10 | Fallback visibility in snapshots + UI |

## Open Questions

1. **Claude Code + Ollama mechanism**: What exact `--model` format, env vars, or CLI flags does Claude Code need to route requests to Ollama? This must be tested before implementing Step 4.
2. **Session continuity**: When falling back to Ollama mid-stream, should we start a new session or try to resume? New session is safer since the Ollama model won't share context.
3. **Ollama base URL configurability**: Should we hardcode `localhost:11434` or make it configurable? Start hardcoded, add config later if needed.
