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
  loop/loop.go                         — Iteration loop goroutine
  loop/phase.go                        — MacroPhase interface
  loop/plan.go                         — Plan macro-phase implementation
  loop/decompose.go                    — Decompose macro-phase implementation
  loop/coding.go                       — Coding macro-phase implementation
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
    Options   map[string]string // runtime-specific flags (allowedTools, appendSystemPrompt, maxBudgetUsd)
}

type Response struct {
    Text    string
    CostUSD float64
}

type Runtime interface {
    // Run sends a prompt and blocks for the complete response (autonomous mode).
    Run(ctx context.Context, req Request) (*Response, error)
}
```

- Each invocation starts a fresh session — no session ID tracking. Context passes through beads, not conversation history.
- Context cancellation handles interruption for guidance injection.
- The Claude CLI implementation wraps `claude -p --output-format json` via `os/exec`.
- The interface is intentionally minimal — different runtimes (SDK, other agents) can implement it without inheriting CLI-specific concerns.
- `Stream()` for pairing mode will be added later when pairing mode is built.

---

## Stream State Model

```go
// internal/stream/stream.go

// IterStep tracks where we are within a single iteration.
type IterStep int
const (
    StepImplement IterStep = iota
    StepAutosquash  // coding phase only
    StepReview
    StepCheckpoint
    StepGuidance
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
    mu            sync.RWMutex
    ID            string
    Name          string
    Task          string
    Mode          Mode
    Status        Status
    Pipeline      []string    // ordered macro-phase names, e.g. ["plan","decompose","coding"]
    PipelineIndex int         // which macro-phase is active
    IterStep      IterStep    // where within the current iteration
    Iteration     int         // iteration count within current macro-phase
    Converged     bool
    BeadsParentID string
    BaseSHA       string      // commit the stream branched from; rebase target
    Branch        string      // e.g. "streams/<stream-id>"
    WorkTree      string      // absolute path to git worktree
    Snapshots     []Snapshot  // append-only
    Guidance      []Guidance  // queued by TUI, drained by loop
    CreatedAt     time.Time
    UpdatedAt     time.Time
}
```

- Thread-safe via `sync.RWMutex` — the TUI reads state while the loop goroutine writes.
- `Pipeline` + `PipelineIndex` track the macro-phase. `IterStep` tracks position within an iteration. `Iteration` resets to 0 on each macro-phase transition.
- Snapshots are append-only; each iteration produces exactly one.
- Guidance is a FIFO queue: the TUI pushes, the loop drains at the guidance step.

### Git Branch & Working Directory

Each stream gets its own git branch and worktree for isolation:

**On stream creation:**
1. Record `BaseSHA = HEAD` — this is the rebase target for autosquash.
2. Create a worktree: `git worktree add .streams/worktrees/<stream-id> -b streams/<stream-id>`
3. The CLI runtime's `WorkDir` points to the worktree path.

**During the coding phase:**
- All commits land on the stream's branch inside its worktree.
- Autosquash rebases onto BaseSHA: `git rebase --autosquash <BaseSHA>` (run inside the worktree).
- The user's main working directory is untouched.

**On stream completion/cleanup:**
- `git worktree remove .streams/worktrees/<stream-id>`
- Branch can be merged, rebased onto main, or deleted — user's choice.

**Why worktrees over in-place:**
- User can keep using the repo while a stream runs.
- Multiple streams get natural isolation (each has its own worktree + branch).
- `git worktree add` is a single command — minimal implementation cost.

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

### Pipeline Model

A stream runs through an ordered **pipeline** of macro-phases. Each macro-phase runs the same iteration loop (implement → review → checkpoint → guidance), but with different prompts, tools, and behaviors. The pipeline is configurable per stream:

```
Default:    Plan → Decompose → Coding
Extended:   Research → Plan → Decompose → Coding
Minimal:    Coding  (skip planning for small tasks)
```

Phases are pluggable — adding a new one means defining its prompts, tools, and convergence criteria. The loop engine doesn't know what "planning" or "coding" means. It just runs cycles with the config the current macro-phase provides.

### Iteration Cycle

Each iteration within a macro-phase follows this sequence:

```
Implement → Autosquash (coding phase only) → Review → Checkpoint → Guidance
```

- **Implement step** — the agent acts (drafts a plan, creates beads, writes code — depends on macro-phase).
- **Autosquash step** — in the coding phase, `git rebase --autosquash` collapses fixup commits so the review step always sees clean history. No-op in other phases.
- **Review step** — the agent critiques the implement step's output and files beads for improvements.
- **Checkpoint step** — produce a snapshot, persist state.
- **Guidance step** — drain any queued human guidance, incorporate into next iteration.

### Macro-Phase Interface

```go
// internal/loop/phase.go

type PhaseContext struct {
    Stream       *stream.Stream
    Runtime      runtime.Runtime
    WorkDir      string
    Iteration    int
}

type MacroPhase interface {
    Name() string
    ImplementPrompt(ctx PhaseContext) string
    ReviewPrompt(ctx PhaseContext) string
    ImplementTools() []string
    ReviewTools() []string
    IsConverged(result IterationResult) bool
    BeforeReview(ctx PhaseContext) error   // e.g., autosquash
    TransitionMode() Transition           // pause | auto-advance
}
```

Pairing mode forces `TransitionMode` to always return `pause` regardless of what the phase returns. Autonomous mode respects the phase's preference.

### Macro-Phase Behaviors

**Plan** — The implement agent drafts/revises a plan file (`plan.md` in the working directory). The review agent reads the plan and files beads for suggested changes. The implement agent addresses those beads, updates the plan, and closes them. Converges when review files zero beads. No commits — the plan file is working state. Tool restrictions: implement gets `Bash,Read,Edit,Write,Glob,Grep`; review gets `Bash,Read,Glob,Grep`.

**Decompose** — The implement agent reads the converged plan and creates ordered child beads — one per logical step in the coding phase. The review agent checks scoping, ordering, and gaps. Converges when review is satisfied with the decomposition. Output: a set of ordered, actionable beads that become the coding phase's work queue. Tool restrictions: both get `Bash,Read,Glob,Grep` (decompose doesn't write code).

**Coding** — First pass: the implement agent works through step beads in order, committing as each is closed. Subsequent passes: the review agent files new beads (referencing the relevant commit in the description), the implement agent addresses them with `git commit --fixup=<sha>`. Before each review step, `git rebase --autosquash` collapses fixup commits so the reviewer always sees clean history. Converges when the review agent files zero new beads against clean history. Tool restrictions: implement gets `Bash,Read,Edit,Write,Glob,Grep`; review gets `Bash,Read,Glob,Grep`.

### Design Decisions

**Stay on Claude Code CLI.** The loop wraps the `claude` CLI rather than calling the Anthropic API directly. This keeps the same toolchain the team uses and avoids rebuilding Claude Code's tool layer. The CLI's `--allowedTools` flag pre-approves specific tools per invocation, eliminating permission prompts without requiring `--dangerously-skip-permissions`.

**Fresh session per iteration.** Each CLI invocation starts a new session (no `--resume`). This avoids context bloat — after several iterations of tool calls, file reads, and edits, the conversation history becomes noise. The agent reads the codebase fresh each iteration, which keeps context honest and grounded in reality.

**Beads-driven handoff.** Inter-iteration context passes through beads parent-child issues, not session continuity or prose summaries:

- Each stream is backed by a **parent beads issue** containing the task description.
- The **review step** files fine-grained, actionable **child issues** for each improvement needed (`bd create --parent <stream-id> --title "..." --type task`).
- The **implement step** reads open children (`bd show <parent> --children`), addresses each one, and closes it (`bd close <id>`).
- **Convergence** is measurable: when the review agent files zero new children, the macro-phase has converged.
- The user can inspect progress at any time via `bd show <parent> --children`.

Both agents interact with beads directly — the Go orchestrator creates the parent issue and passes the ID, but the agents navigate beads themselves.

**Per-step tool restrictions.** Tools are restricted at the CLI level, not just by prompt instruction:

| Step | `--allowedTools` | Rationale |
|------|-------------------|-----------|
| Implement | `Bash,Read,Edit,Write,Glob,Grep` | Full access to build things (varies by macro-phase) |
| Review | `Bash,Read,Glob,Grep` | Read-only — no `Edit`/`Write` — enforced at tool level |

Both steps get `Bash` for running `bd` commands and tests. `--permission-mode acceptEdits` auto-approves file edits as belt-and-suspenders for the implement step.

**Key CLI flags per invocation:**

```
claude -p \
  --output-format json \
  --permission-mode acceptEdits \
  --allowedTools "Bash,Read,Edit,Write,Glob,Grep" \
  --append-system-prompt "Phase-specific instructions..." \
  --max-budget-usd 2.00 \
  "The prompt for this iteration step"
```

### Prompt Design

Each macro-phase provides implement and review prompts. The prompts are injected via `--append-system-prompt` (phase-level rules) and the `-p` argument (iteration-specific instructions).

**Plan phase — implement prompt (iteration 1):**

> You are drafting a plan for a software task. Write or revise `plan.md` in the working directory.
>
> Task: {task description from parent issue}
>
> Rules:
> - Write a clear, step-by-step implementation plan.
> - Do not write code. Do not commit.
> - Do not create beads issues — that's the review step's job.

**Plan phase — implement prompt (iteration 2+):**

> You are revising a plan based on feedback. The feedback is tracked as child issues under the parent beads issue.
>
> Task: {task description}
> Parent issue: {beads parent ID}
>
> Steps:
> 1. Run: bd show {parent_id} --children
> 2. For each open child issue, read it, update plan.md accordingly, and close it with bd close.
>
> Rules:
> - Do not write code. Do not commit.
> - Do not create new beads issues.
> - Close each child issue as you address it.

**Plan phase — review prompt:**

> You are reviewing a plan for a software task. Your job is to file specific, actionable improvement issues — not to make changes yourself.
>
> Task: {task description}
> Parent issue: {beads parent ID}
>
> Steps:
> 1. Read plan.md.
> 2. Evaluate: Is the plan complete? Are steps well-ordered? Are there gaps, ambiguities, or unnecessary complexity?
> 3. For each improvement, file a child issue:
>    bd create --parent {parent_id} --title="<specific action>" --type=task --priority=2 --description="<what to change and why>"
> 4. If the plan is ready, respond with exactly: "No further improvements needed."
>
> Rules:
> - Do NOT edit any files.
> - Maximum 5 issues per review.

**Decompose phase — implement prompt:**

> You are breaking a plan into implementation steps. Each step will become one commit.
>
> Read plan.md and create one beads issue per logical step:
>   bd create --parent {parent_id} --title="Step N: <action>" --type=task --priority=2 --description="<what to do in this step>"
>
> Rules:
> - Steps should be small and independently meaningful.
> - Order matters — each step should build on the previous.
> - Each step should be completable in a single commit.
> - Do not write code. Do not commit.

**Decompose phase — review prompt:**

> You are reviewing the decomposition of a plan into implementation steps. Check the child issues under the parent.
>
> Parent issue: {beads parent ID}
>
> Run: bd show {parent_id} --children
>
> Evaluate: Are steps well-scoped? Well-ordered? Missing steps? Steps that should be split or merged?
>
> File child issues for any adjustments needed. If the decomposition is ready, respond with exactly: "No further improvements needed."

**Coding phase — implement prompt (first pass):**

> You are implementing a software task step by step. Each step is a child beads issue under the parent.
>
> Task: {task description}
> Parent issue: {beads parent ID}
>
> Steps:
> 1. Run: bd show {parent_id} --children
> 2. Work through open child issues in order.
> 3. For each: implement the change, run tests, commit with a descriptive message, then close the issue with bd close.
>
> Rules:
> - One commit per child issue.
> - Do not create new beads issues.
> - Run tests before each commit.

**Coding phase — implement prompt (refinement pass):**

> You are addressing review feedback on an implementation. The feedback is tracked as child issues under the parent beads issue.
>
> Task: {task description}
> Parent issue: {beads parent ID}
>
> Steps:
> 1. Run: bd show {parent_id} --children
> 2. For each open child issue: read the description (it references the relevant commit), make the fix, and create a fixup commit targeting the original: git commit --fixup=<sha>
> 3. Close the child issue with bd close.
>
> Rules:
> - Use fixup commits, not regular commits.
> - Run tests after all changes.
> - Do not create new beads issues.

**Coding phase — review prompt:**

> You are reviewing code that was just written or refined. Your job is to file specific, actionable improvement issues — not to make changes yourself.
>
> Task: {task description}
> Parent issue: {beads parent ID}
>
> Steps:
> 1. Read the relevant code (use Glob/Grep/Read to find what was changed).
> 2. Review the git log to understand the commit structure.
> 3. Evaluate against these criteria:
>    - Pattern conformance: Does this match how the codebase already does things?
>    - Simplicity: Can anything be removed or consolidated?
>    - Readability: Would a new developer understand this without comments?
>    - Correctness: Are there bugs, edge cases, or missing error handling?
> 4. For each improvement, file a child issue referencing the relevant commit SHA in the description:
>    bd create --parent {parent_id} --title="<specific action>" --type=task --priority=2 --description="Commit <sha>: <what to change and why>"
> 5. If no improvements needed, respond with exactly: "No further improvements needed."
>
> Rules:
> - Do NOT edit or write any files.
> - Each issue must be a single, actionable change.
> - Do not file issues about style/formatting that a linter would catch.
> - Do not file issues for missing features outside the task scope.
> - Maximum 5 issues per review.

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

Convergence is detected by the review agent's output. When the review files zero new child issues (checked by the Go loop via `bd show <parent> --children --status=open`), the macro-phase has converged and the stream's `Converged` flag is set. The loop does not auto-stop — it either transitions to the next macro-phase or pauses for human input depending on mode.

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
