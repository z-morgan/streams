# Streams — Architecture

## Tech Stack

| Layer | Choice | Why |
|-------|--------|-----|
| Language | Go | Strong concurrency (goroutines/channels), single binary, fast compilation |
| TUI framework | [Bubble Tea](https://github.com/charmbracelet/bubbletea) | Elm architecture (model/update/view), mature ecosystem, active community |
| TUI styling | [Lip Gloss](https://github.com/charmbracelet/lipgloss) | Composable styling, pairs with Bubble Tea |
| TUI components | [Bubbles](https://github.com/charmbracelet/bubbles) | Ready-made textarea, viewport, list, spinner |
| Subprocess mgmt | `os/exec` | Standard library, sufficient for CLI wrapping |
| Persistence | JSON / JSONL files | No database dependency, human-readable, simple append for snapshots |

### Rationale

Go was chosen over Ruby, Rust, Zig, TypeScript, and Python. Key factors:

- **Concurrency model** — goroutines and channels map directly to "one loop per stream" with cheap, safe parallelism.
- **Mature TUI ecosystem** — Bubble Tea is the most capable terminal UI framework available. Direct prior art exists: `agent-deck` (Go/Bubble Tea AI agent session manager) and `ccboard` (Rust/Ratatui Claude Code monitor).
- **Single binary** — no runtime dependencies for distribution.
- **`os/exec`** — straightforward subprocess management for wrapping Claude Code CLI.

---

## Project Layout

```
cmd/streams/main.go                    — Entry point
internal/
  runtime/runtime.go                   — Runtime interface
  runtime/claude/claude.go             — Claude Code CLI implementation
  stream/stream.go                     — Stream state model + enums
  stream/snapshot.go                   — Snapshot and Guidance types
  loop/loop.go                         — Refinement loop goroutine
  loop/gates.go                        — Quality gate interface + defaults
  orchestrator/orchestrator.go         — Multi-stream manager
  ui/app.go                            — Root Bubble Tea model
  ui/dashboard.go                      — Dashboard view
  ui/detail.go                         — Stream detail/inspect view
  ui/styles.go                         — Lip Gloss styles
  store/store.go                       — Disk persistence
```

---

## Runtime Interface

```go
// internal/runtime/runtime.go

type Request struct {
    Prompt    string
    SessionID string            // empty for new session
    Options   map[string]string // runtime-specific flags
}

type Response struct {
    Text      string
    SessionID string
    Cost      Cost
}

type Cost struct {
    InputTokens  int
    OutputTokens int
}

type Event struct {
    Type string // "text", "tool_use", "done", "error"
    Data string
}

type Runtime interface {
    // Run sends a prompt and blocks for the complete response (autonomous mode).
    Run(ctx context.Context, req Request) (*Response, error)

    // Stream sends a prompt and returns a channel of incremental events (pairing mode).
    Stream(ctx context.Context, req Request) (<-chan Event, error)
}
```

- Context cancellation handles interruption for guidance injection.
- The Claude CLI implementation wraps `claude --print --output-format json` via `os/exec`.
- The interface is intentionally minimal — different runtimes (SDK, other agents) can implement it without inheriting CLI-specific concerns.

---

## Stream State Model

```go
// internal/stream/stream.go

type Phase int
const (
    PhaseIdle Phase = iota
    PhaseImplement
    PhaseReview
    PhaseCheckpoint
    PhaseGuidance
)

type Mode int
const (
    ModeAutonomous Mode = iota
    ModePairing
)

type Status int
const (
    StatusCreated Status = iota
    StatusRunning
    StatusPaused
    StatusStopped
)

type Stream struct {
    mu          sync.RWMutex
    ID          string
    Name        string
    Task        string
    Mode        Mode
    Status      Status
    Phase       Phase
    Iteration   int
    Converged   bool
    SessionID   string
    Snapshots   []Snapshot  // append-only
    Guidance    []Guidance  // queued by TUI, drained by loop
    CreatedAt   time.Time
    UpdatedAt   time.Time
}
```

- Thread-safe via `sync.RWMutex` — the TUI reads state while the loop goroutine writes.
- Snapshots are append-only; each iteration produces exactly one.
- Guidance is a FIFO queue: the TUI pushes, the loop drains at the guidance phase.

### Snapshot

```go
// internal/stream/snapshot.go

type Snapshot struct {
    Iteration   int
    Summary     string       // what the agent did
    Review      string       // self-review output
    GateResults []GateResult // per-gate pass/fail + detail
    Timestamp   time.Time
}

type Guidance struct {
    Text      string
    Timestamp time.Time
}
```

---

## Loop

```go
// internal/loop/loop.go — one goroutine per stream

// Phases per iteration:
//   1. Implement/Refine — send task + prior review to runtime
//   2. Review — send implementation for self-review against quality gates
//   3. Checkpoint — produce snapshot, persist state
//   4. Guidance — drain guidance queue, incorporate into next iteration
```

### Quality Gates

```go
// internal/loop/gates.go

type GateResult struct {
    Gate   string
    Passed bool
    Detail string
}

type Gate interface {
    Name() string
    Evaluate(reviewText string) GateResult
}
```

Default gates (parsed from review output):
- **Pattern conformance** — does this match how the codebase already does things?
- **Simplicity** — can anything be removed or consolidated?
- **Readability** — would a new developer understand this without comments?
- **Hindsight check** — knowing what I know now, would I approach this differently?

Convergence is detected by keywords in the review output (e.g., "no further improvements"). When detected, the stream's `Converged` flag is set and surfaced in the TUI. The loop does not auto-stop.

---

## Orchestrator

```go
// internal/orchestrator/orchestrator.go

type Orchestrator struct {
    streams   map[string]*stream.Stream
    loops     map[string]context.CancelFunc
    store     *store.Store
    program   *tea.Program // for sending events to TUI
}
```

Responsibilities:
- Manages the stream collection (create, list, get).
- Starts and stops loop goroutines per stream.
- Routes guidance from the TUI to the correct stream.
- Sends `LoopEvent` messages to the TUI via `tea.Program.Send()` so the UI updates in real time.
- Persists state on every checkpoint.

---

## TUI

### Views

| View | Purpose |
|------|---------|
| **Dashboard** | Stream list with cursor. Shows name, phase indicator, iteration count, gate summary, convergence flag. |
| **Detail** | Snapshot inspector for a single stream. Navigate snapshots with left/right. Shows summary, review, gate results. |
| **Guidance overlay** | Textarea overlay triggered by `g`. Submit with `ctrl+s`, cancel with `esc`. |

### Key Bindings

| Key | Dashboard | Detail |
|-----|-----------|--------|
| `j` / `k` | Move cursor up/down | — |
| `enter` | Inspect selected stream | — |
| `n` | New stream | — |
| `s` | Start/resume selected stream | — |
| `x` | Stop selected stream | — |
| `g` | Open guidance input | Open guidance input |
| `left` / `right` | — | Navigate snapshots |
| `esc` | Quit | Back to dashboard |
| `q` | Quit | Back to dashboard |

### Event Flow

The TUI receives `LoopEvent` messages via `tea.Program.Send()`. These trigger `Update()` calls that refresh stream state in the model. The `View()` function renders from the model — no direct state sharing between the TUI and loop goroutines beyond the thread-safe `Stream` struct.

---

## Persistence

```
~/.streams/streams/<stream-id>/
  stream.json      — metadata (config, status, session ID, mode)
  snapshots.jsonl   — append-only snapshot log (one JSON object per line)
```

- JSON format, no database dependency.
- `stream.json` is rewritten on each checkpoint.
- `snapshots.jsonl` is append-only — new snapshots are appended, never rewritten.
- On startup, the orchestrator loads all stream directories and reconstructs state.
- Loops do not auto-resume on restart — the user explicitly starts them.
