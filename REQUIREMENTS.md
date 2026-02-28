# Streams — High-Level Project Requirements

## Context

Managing multiple Claude Code agents across terminal instances is disorienting and inefficient. Agents stop at "functional correctness" (tests pass, requirements met) but don't meet the quality bar for taste, readability, and maintainability — leading to many manual review-and-redirect cycles with long wait times between turns. There's no good way to see what each agent is doing, and no way to let one agent keep working while you focus on another.

**Streams** is a TUI application for orchestrating multiple AI coding agents working in parallel, each in a perpetual refinement loop. The goal: manage n streams of parallel work while freely moving between them — going deep on one while the others keep running.

## Core Concepts

### Stream
An autonomous unit of work represented as a vertical channel in the TUI. Each stream wraps an agent working on a task, cycling through a perpetual refinement loop. Streams are independent of each other.

### Loop
Each stream runs a perpetual refinement cycle. After meeting functional requirements, the agent doesn't stop — it enters self-review and refinement phases, continually improving the implementation. Each cycle produces a checkpoint snapshot the user can inspect.

### Two Gears

**Autonomous mode** — The agent is in its loop. The user observes snapshots and optionally injects guidance. The agent keeps working regardless of whether the user is watching.

**Pairing mode** — Real-time, chat-like interaction. For fuzzy requirements, architecture discussions, or collaborative refactoring. The user is actively involved in the conversation.

The user can shift between gears at any time. A stream might start in pairing (talking through the problem) and shift to autonomous (agent goes and builds it), or vice versa. While pairing with one stream, all other streams continue in autonomous mode.

## Requirements

### R1: Stream Lifecycle
- Create a new stream with either a well-defined task or a vague starting point
- Streams are independent (inter-stream coordination is a future possibility)
- Support 1-5 concurrent streams initially, designed to scale beyond
- Each stream wraps an agent runtime (initially Claude Code CLI)
- Agent runtime is abstracted — different streams could use different runtimes in the future
- Streams persist across sessions: history, snapshots, and context survive TUI restarts
- Manually restart agent loops when resuming a session (loops don't auto-resume)

### R2: Perpetual Refinement Loop (Autonomous Mode)
- After initial implementation, agent enters a refinement loop automatically
- Loop phases (per iteration):
  1. **Implement/Refine** — work on the task or apply refinements
  2. **Self-review** — evaluate against quality gates
  3. **Checkpoint** — produce a snapshot summarizing current state and changes
  4. **Check for guidance** — incorporate any user input, then continue
- Loop continues until the user explicitly stops or pauses the stream
- If the loop converges (refinements stop improving), surface this to the user

### R3: Quality Gates
The key differentiator. Gates go beyond functional correctness:
- **Pattern conformance** — does this match how the codebase already does things?
- **Simplicity** — can anything be removed or consolidated?
- **Readability** — would a new developer understand this without comments?
- **Hindsight check** — "knowing what I know now, would I approach this differently?"
- Gates are configurable (the user should be able to tune what the agent evaluates)

### R4: User Interaction
- **Observe** — view the latest snapshot of any stream without interrupting the agent
- **Guide** — inject guidance that redirects the agent's next iteration (agent scraps current iteration, starts new one with latest result + guidance)
- **Pair** — enter real-time conversational mode within a stream
- **Shift gears** — transition fluidly between autonomous and pairing modes
- **Stop/Pause** — halt a stream's loop, preserving its state

### R5: TUI
- Dashboard view showing all active streams
- Each stream displayed as a vertical channel with current status
- Visual indicators for stream state: which loop phase, whether attention is needed, convergence
- Inspect a stream to see its latest snapshot
- Interact with a stream (inject guidance, enter pairing, stop/start)
- No sound/system notifications — visual indicators within the TUI only

### R6: Architecture
- **Pluggable agent runtime** — abstract interface for driving agents; Claude Code CLI is the first implementation
- Clean separation: orchestration logic / agent runtime / TUI rendering / persistence
- State persisted to disk (snapshots, conversation history, stream config)

## Open Questions (to revisit later)
- Tech stack for TUI and orchestrator (evaluate once requirements are solid)
- Inter-stream coordination / dependencies (when use-cases emerge)
- How quality gates are configured per-project or per-stream
- Convergence detection strategy (how to know refinements aren't improving things)
- How Claude Code CLI is driven programmatically for the loop (--print, --resume, SDK)
