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
  loop/errors.go                       — LoopError type + ErrorKind enum
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
    LastError     *LoopError  // set on error, cleared on resume
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
    Phase       string       // macro-phase that produced this snapshot (e.g. "plan", "coding")
    Iteration   int
    Summary     string       // CLI result text — final text output from claude -p --output-format json
    Review      string       // reviewer's text output
    CostUSD     float64
    DiffStat    string       // git diff --stat output (coding phase only)
    CommitSHAs  []string     // commits made this iteration (coding phase only)
    BeadsClosed []string     // bead IDs closed by implement step
    BeadsOpened []string     // bead IDs opened by review step
    GuidanceConsumed []Guidance  // guidance items injected into this iteration's implement step
    Error       *LoopError  // non-nil if iteration ended in error (partial snapshot)
    Timestamp   time.Time
}

type Guidance struct {
    Text      string
    Timestamp time.Time
}
```

**Summary source:** The `result` field from `--output-format json`. Claude naturally summarizes its work, so this is compact without storing the full tool-call trace.

**No full diffs stored.** Snapshots store commit SHAs and diffstat only. Full diffs are rendered live via `git show <sha>` when the user drills down in the TUI.

**Beads delta** tracks IDs opened/closed per iteration — gives a quick "work done / work remaining" signal without querying beads.

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
Implement → Autosquash (coding phase only) → Review → Converge? → Checkpoint → Guidance
```

- **Implement step** — the agent acts (drafts a plan, creates beads, writes code — depends on macro-phase).
- **Autosquash step** — in the coding phase, `git rebase --autosquash` collapses fixup commits so the review step always sees clean history. No-op in other phases.
- **Review step** — the Go loop counts open children before invoking the review agent, then counts again after. The agent critiques the implement step's output and files beads for improvements. The delta populates `IterationResult`.
- **Convergence check** — the loop calls `MacroPhase.IsConverged(result)`. Default implementation: `result.OpenChildrenAfter <= result.OpenChildrenBefore` (review didn't add work).
- **Checkpoint step** — produce a snapshot, persist state.
- **Guidance step** — drain any queued human guidance, incorporate into next iteration. See [Guidance Injection](#guidance-injection) for details.

### Macro-Phase Interface

```go
// internal/loop/phase.go

type PhaseContext struct {
    Stream       *stream.Stream
    Runtime      runtime.Runtime
    WorkDir      string
    Iteration    int
}

// IterationResult captures the outcome of a single iteration for convergence
// detection. The Go loop populates this — agents don't produce it directly.
type IterationResult struct {
    ReviewText        string   // reviewer's text output
    OpenChildrenBefore int     // open children count before the review step
    OpenChildrenAfter  int     // open children count after the review step
    BeadsClosed       []string // bead IDs closed by implement step
    BeadsOpened       []string // bead IDs opened by review step
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

**Plan** — The implement agent drafts/revises a plan file (`plan.md` in the working directory). The review agent reads the plan and files beads for suggested changes. The implement agent addresses those beads, updates the plan, and closes them. Converges when open children count doesn't increase after a review step. No commits — the plan file is working state. Tool restrictions: implement gets `Bash,Read,Edit,Write,Glob,Grep`; review gets `Bash,Read,Glob,Grep`.

**Decompose** — The implement agent reads the converged plan and creates child beads — one per logical step in the coding phase — with execution order encoded in `metadata.sequence`. The review agent checks scoping, ordering, and gaps. Converges when open children count doesn't increase after a review step. Output: a set of sequenced, actionable beads that become the coding phase's work queue. Tool restrictions: both get `Bash,Read,Glob,Grep` (decompose doesn't write code).

**Coding** — First pass: the Go loop sorts step beads by `metadata.sequence` and injects the ordered list into the implement prompt; the agent works through them in order, committing as each is closed. Subsequent passes: the review agent files new beads (referencing the relevant commit in the description), the implement agent addresses them with `git commit --fixup=<sha>`. Before each review step, `git rebase --autosquash` collapses fixup commits so the reviewer always sees clean history. Converges when open children count doesn't increase after a review step (i.e., review files zero new beads against clean history). Tool restrictions: implement gets `Bash,Read,Edit,Write,Glob,Grep`; review gets `Bash,Read,Glob,Grep`.

### Design Decisions

**Stay on Claude Code CLI.** The loop wraps the `claude` CLI rather than calling the Anthropic API directly. This keeps the same toolchain the team uses and avoids rebuilding Claude Code's tool layer. The CLI's `--allowedTools` flag pre-approves specific tools per invocation, and `--permission-mode dontAsk` silently blocks anything not in the allow list — no permission prompts, no hanging.

**Fresh session per iteration.** Each CLI invocation starts a new session (no `--resume`). This avoids context bloat — after several iterations of tool calls, file reads, and edits, the conversation history becomes noise. The agent reads the codebase fresh each iteration, which keeps context honest and grounded in reality.

**Beads-driven handoff.** Inter-iteration context passes through beads parent-child issues, not session continuity or prose summaries:

- Each stream is backed by a **parent beads issue** containing the task description.
- The **review step** files fine-grained, actionable **child issues** for each improvement needed (`bd create --parent <stream-id> --title "..." --type task`).
- The **implement step** reads open children (`bd show <parent> --children`), addresses each one, and closes it (`bd close <id>`).
- **Convergence** is measurable: when the review agent files zero new children, the macro-phase has converged.
- The user can inspect progress at any time via `bd show <parent> --children`.

Both agents interact with beads directly — the Go orchestrator creates the parent issue and passes the ID, but the agents navigate beads themselves.

**Step ordering via metadata.** Beads has no native ordering field. Step execution order is encoded in each issue's metadata as `{"sequence": N}`. The decompose implement agent assigns sequential integers starting at 1. The Go loop — not the agent — fetches children, sorts by `metadata.sequence`, and injects the ordered list into the coding implement prompt. This design:
- Avoids fragile title-prefix parsing ("Step N: ...").
- Keeps sorting logic in deterministic Go code, not agent behavior.
- Doesn't conflate ordering with blocking (dependency chains would make steps show as "blocked" in `bd ready`).
- Survives renumbering during decompose review — the next implement iteration simply updates sequence values.

**Per-step tool restrictions.** Tools are restricted at the CLI level, not just by prompt instruction:

| Step | `--allowedTools` | Rationale |
|------|-------------------|-----------|
| Implement | `Bash,Read,Edit,Write,Glob,Grep` | Full access to build things (varies by macro-phase) |
| Review | `Bash,Read,Glob,Grep` | Read-only — no `Edit`/`Write` — enforced at tool level |

Both steps get `Bash` for running `bd` commands and tests. `--permission-mode dontAsk` ensures no tool hangs waiting for approval — tools in `--allowedTools` run freely, everything else is silently denied. Note: `acceptEdits` only auto-approves file edits, not Bash; `dontAsk` is required for fully autonomous operation.

**CLAUDE.md conflict handling.** The target repo's CLAUDE.md is automatically loaded by Claude CLI. This is desirable — project coding style, conventions, and patterns should inform the agent's work. However, CLAUDE.md files written for interactive human-supervised sessions may contain operational instructions that conflict with the stream loop (e.g., "always commit", "never use beads", "run the formatter before committing", specific git workflows).

The `--append-system-prompt` includes a priority section that overrides only operational workflow instructions while preserving project coding conventions:

```
## Stream Orchestrator Rules (these override any conflicting CLAUDE.md instructions)
- Only commit when this prompt explicitly instructs you to.
- Do NOT push to any remote.
- Do NOT create, update, or close beads/bd issues unless this prompt explicitly instructs you to.
- Do NOT start, stop, or restart dev servers.
- Do NOT run formatters, linters, or other pre-commit tooling unless this prompt explicitly instructs you to.
- Follow the tool restrictions enforced by --allowedTools. Do not attempt to use tools outside that list.
- All other CLAUDE.md instructions (coding style, naming conventions, test patterns, project structure) remain in effect.
```

This inverts normal CLAUDE.md precedence (project-specific normally overrides global), but is justified because the execution context is fundamentally different — a controlled autonomous loop, not an interactive session.

**Escape hatch:** If a project's CLAUDE.md causes persistent conflicts, pass `--settings '{"claudeMdExcludes": ["CLAUDE.md"]}'` to suppress it entirely. This loses project context, so it's a last resort.

**Key CLI flags per invocation:**

```
claude -p \
  --output-format json \
  --permission-mode dontAsk \
  --allowedTools "Bash,Read,Edit,Write,Glob,Grep" \
  --append-system-prompt "Stream orchestrator rules + phase-specific instructions..." \
  --max-budget-usd 2.00 \
  "The prompt for this iteration step"
```

### Prompt Design

Each macro-phase provides implement and review prompts. The prompts are injected via `--append-system-prompt` (phase-level rules) and the `-p` argument (iteration-specific instructions).

Every `--append-system-prompt` begins with the shared orchestrator override block (see "CLAUDE.md conflict handling" above), followed by the phase-specific instructions below.

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
> Read plan.md and create one beads issue per logical step, using the metadata field to encode execution order:
>   bd create --parent {parent_id} --title="<descriptive action>" --type=task --priority=2 --metadata '{"sequence":N}' --description="<what to do in this step>"
>
> Example:
>   bd create --parent {parent_id} --title="Scaffold Go module and directory layout" --type=task --priority=2 --metadata '{"sequence":1}' --description="..."
>   bd create --parent {parent_id} --title="Define runtime interface" --type=task --priority=2 --metadata '{"sequence":2}' --description="..."
>
> Rules:
> - Steps should be small and independently meaningful.
> - Order matters — assign sequential `sequence` values starting at 1.
> - Each step should be completable in a single commit.
> - Titles should be descriptive actions, not numbered prefixes.
> - Do not write code. Do not commit.

**Decompose phase — review prompt:**

> You are reviewing the decomposition of a plan into implementation steps. Check the child issues under the parent.
>
> Parent issue: {beads parent ID}
>
> Run: bd show {parent_id} --children
>
> Evaluate: Are steps well-scoped? Well-ordered (check metadata.sequence values)? Missing steps? Steps that should be split or merged?
>
> If steps need reordering, renumbering, insertion, or removal, file a child issue describing the change. The next implement iteration will update the sequence metadata accordingly.
>
> File child issues for any adjustments needed. If the decomposition is ready, respond with exactly: "No further improvements needed."

**Coding phase — implement prompt:**

> You are implementing a software task. Work items are tracked as child beads issues under the parent.
>
> Task: {task description}
> Parent issue: {beads parent ID}
>
> Work through these steps in order:
> {ordered_steps}
>
> For each step:
> 1. Implement the change described in the issue.
> 2. Run tests.
> 3. Commit with a descriptive message.
> 4. Close the issue: bd close {step_id}
>
> If there are also open review feedback issues (description references a commit SHA), address those with fixup commits: git commit --fixup=<sha>
>
> Rules:
> - One commit per issue.
> - Run tests before committing.
> - Do not create new beads issues.

The `{ordered_steps}` placeholder is populated by the Go loop. Before invoking the implement agent, the loop fetches children via `bd show {parent_id} --children --json`, parses each child's `metadata.sequence` field, sorts numerically, and renders an ordered list:

```
1. streams-abc — Scaffold Go module and directory layout
2. streams-def — Define runtime interface
3. streams-ghi — Implement Claude CLI wrapper
```

This keeps ordering logic in Go (deterministic) rather than relying on the agent to sort.

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

**Convergence detection** is performed by the Go loop, not by parsing agent output. The loop counts open children before and after each review step via `bd show <parent> --children --status=open`. If the count did not increase, the phase has converged — the review found nothing to improve. The loop passes these counts to `MacroPhase.IsConverged(result)` via `IterationResult`, and the phase sets the stream's `Converged` flag. The "No further improvements needed" string in review prompts is guidance to the agent (so it doesn't hallucinate issues when satisfied), not a signal parsed by Go. The loop does not auto-stop — it either transitions to the next macro-phase or pauses for human input depending on mode.

### Error Handling

```go
// internal/loop/errors.go

type ErrorKind int
const (
    ErrRuntime    ErrorKind = iota // CLI exited non-zero (crash, timeout)
    ErrBudget                      // Budget limit hit (specific runtime case)
    ErrAutosquash                  // git rebase --autosquash failed (merge conflict)
    ErrNoProgress                  // Implement step closed zero beads
    ErrInfra                       // Disk, git worktree, beads CLI failure
)

type LoopError struct {
    Kind    ErrorKind
    Step    IterStep   // which step failed
    Message string     // one-line human summary
    Detail  string     // stderr, conflict file list, etc.
}
```

`ErrBudget` is split from `ErrRuntime` because the user action differs (increase budget vs. retry). Detection: parse stderr for "budget exceeded" or equivalent keyword when the CLI exits non-zero.

#### Error detection

| Scenario | Step | ErrorKind | Detection |
|----------|------|-----------|-----------|
| CLI exits non-zero | Implement or Review | `ErrRuntime` | `exec.ExitError` from `Runtime.Run()` |
| Budget exceeded | Implement or Review | `ErrBudget` | Parse stderr for "budget" keyword + non-zero exit |
| Autosquash fails | Autosquash (BeforeReview) | `ErrAutosquash` | Non-zero exit from `git rebase --autosquash`; detail = conflicting file list |
| No beads closed | After Implement | `ErrNoProgress` | Compare open child count before/after implement step |
| Disk/git/beads failure | Any | `ErrInfra` | Catch-all for non-Runtime errors (file I/O, git commands, bd CLI) |

#### Loop behavior on error

On any error during the iteration cycle:

1. Build a `LoopError` with kind, step, message, detail.
2. Create an error snapshot (partial — only fields available up to the failure point).
3. Set `stream.LastError = &loopError`.
4. Set `stream.Status = StatusPaused`.
5. Persist state (checkpoint).
6. Send `ErrorEvent{StreamID, LoopError}` to TUI.
7. Return from the loop goroutine.

**No automatic retries in v1.** Every error pauses the stream. The user always inspects what happened and decides. Automatic retries can be layered on later for specific `ErrorKind` values.

#### Restart semantics

When the user resumes a paused stream (presses `s`):

1. Clear `stream.LastError`.
2. Set `stream.Status = StatusRunning`.
3. Restart the iteration **from the beginning** (Implement step).

Why restart the whole iteration instead of the failed step:
- Implement is idempotent — it reads open beads and works on them. If some were already closed before the error, the re-run just sees fewer open beads.
- Avoids complexity of checkpointing mid-iteration step state.
- For autosquash failures after a successful implement, the re-run will re-do implement (harmless — beads are already closed) and then retry autosquash after the user has resolved conflicts.

#### TUI error display

- **Detail view:** When `stream.LastError != nil`, render an error block above the snapshot list showing Kind, Step, Message, and (expandable) Detail.
- **Dashboard:** Paused-with-error streams show a distinct indicator (e.g., `!` or red status) vs. paused-by-user streams.

### Guidance Injection

Guidance is the mechanism for a human to steer an autonomous stream without stopping it. The user types free-text direction via the TUI's guidance overlay (`g` key), and the loop incorporates it into the next iteration.

#### Design Principles

- **Implement-only.** Guidance injects into the implement step's prompt. The review step's role is fixed: evaluate and file issues. Steering the reviewer would muddy the separation between "do work" and "critique work."
- **Additive, not override.** Guidance adds context to the existing prompt — it does not replace the system prompt or phase-level rules. The `--append-system-prompt` flag carries invariant phase instructions; guidance is ephemeral, iteration-specific direction.
- **Content direction, not meta-control.** Guidance does not force phase transitions, skip phases, or change the pipeline. Those are separate TUI actions (future: a key binding to force-advance the pipeline index). Guidance is about *what* the agent should focus on, not *how* the loop should behave.
- **Loop concern, not phase concern.** The loop handles injection generically. Phases provide the base prompt; the loop appends guidance. The `MacroPhase` interface does not change.

#### Data Flow

1. **User submits guidance.** TUI pushes `Guidance{Text, Timestamp}` into `stream.Guidance` (thread-safe via mutex). Multiple items can queue up between iterations.
2. **Loop drains at StepGuidance.** At the end of each iteration, the loop acquires the lock, moves all items from `stream.Guidance` into a local `pendingGuidance []Guidance` variable, and clears the queue.
3. **Next iteration injects.** At StepImplement, if `pendingGuidance` is non-empty, the loop appends a guidance section to the implement prompt before passing it to `Runtime.Run()`.
4. **Snapshot records.** The consumed guidance items are stored in `snapshot.GuidanceConsumed` for history. After the implement step runs, `pendingGuidance` is cleared.

```go
// Inside the loop goroutine (simplified)
var pendingGuidance []Guidance

for {
    // StepImplement
    prompt := phase.ImplementPrompt(ctx)
    if len(pendingGuidance) > 0 {
        prompt = appendGuidanceSection(prompt, pendingGuidance)
    }
    resp, err := runtime.Run(ctx, Request{Prompt: prompt, ...})

    // ... autosquash, review, convergence, checkpoint ...

    // Record consumed guidance in snapshot
    snapshot.GuidanceConsumed = pendingGuidance

    // StepGuidance — drain queue for next iteration
    stream.mu.Lock()
    pendingGuidance = stream.Guidance
    stream.Guidance = nil
    stream.mu.Unlock()
}
```

#### Prompt Format

When guidance is present, the loop appends this section to the implement prompt:

```
---

## Human Guidance

The user has provided the following guidance for this iteration:

1. [guidance text] (received [timestamp])
2. [guidance text] (received [timestamp])

Prioritize addressing this guidance alongside your normal work items.
```

Multiple items are numbered in chronological order. The section is appended (not prepended) so the agent sees the phase's core instructions first.

#### Timing

Guidance submitted while the implement step is running sits in the queue until `StepGuidance` at the end of the iteration. The loop does not cancel a running step to inject mid-flight. This is intentional for v1:

- Canceling and restarting wastes the work already done.
- The agent will pick up the guidance on the next iteration, typically within minutes.
- The TUI shows queued guidance count so the user knows their input is pending.

**Future:** Add mid-flight cancellation — when guidance arrives during a running implement step, cancel the CLI process via context cancellation and restart the iteration with the guidance injected. This gives the user immediate steering rather than waiting for the current iteration to finish.

#### What Guidance Is Not

| Not This | Use This Instead |
|----------|------------------|
| Force phase transition | Future: TUI key binding to advance pipeline index |
| Stop the stream | Press `x` to stop |
| Override phase rules | Edit the phase's system prompt |
| Direct the reviewer | File beads issues manually, or adjust the review prompt |

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
| **Dashboard** | Stream list with cursor. One row per stream: name, current phase, iteration count. Minimal — scan status at a glance. |
| **Detail** | Two-pane layout. Left pane: vertical list of all snapshots across all phases (e.g., "Plan 1", "Plan 2", "Coding 1"...). Right pane: selected snapshot's details (summary, review, diffstat, beads delta). v1 highlights the latest snapshot; future adds j/k traversal of the left pane. |
| **Guidance overlay** | Textarea overlay triggered by `g`. Submit with `ctrl+s`, cancel with `esc`. |

### Key Bindings

| Key | Dashboard | Detail |
|-----|-----------|--------|
| `j` / `k` | Move cursor up/down | Navigate snapshot list (future) |
| `enter` | Inspect selected stream | — |
| `n` | New stream | — |
| `s` | Start/resume selected stream | — |
| `x` | Stop selected stream | — |
| `g` | Open guidance input | Open guidance input |
| `d` | — | Show full diff for selected snapshot (future) |
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
