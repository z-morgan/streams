# Streams

A terminal UI for orchestrating parallel pipelines of [Claude Code](https://docs.anthropic.com/en/docs/claude-code) agents. Each stream runs a series of specialized agents in a continuous refinement loop — implementing, reviewing, and iterating toward convergence.

## Why

AI coding agents generate functional code, but the gap between "works" and "ready to ship" is where your review time goes — and it scales linearly with the number of agents you run.

Streams closes that gap automatically. Each stream runs an implement → review loop where a dedicated review agent critiques the work and files concrete improvement items as [beads](https://github.com/zmorgan/beads). The implement agent addresses them, the reviewer evaluates again, and the cycle continues until there's nothing left to improve. By the time you look at it, the code has already been through multiple rounds of self-review.

The TUI gives you a single surface to monitor all your streams, inspect their progress, and inject guidance — without context switching between agents.

Every stream produces a reviewable branch with clean commit history. You review it, sign off, and merge.

## How it works

A **stream** is a pipeline of specialized Claude Code agents that run serially in a continuous loop. Each stream gets its own git branch and worktree, so multiple streams work in parallel without interference.

Agents coordinate through [beads](https://github.com/zmorgan/beads) — a git-backed issue tracker that lives in your repo. The review agent files beads for improvements; the implement agent reads and addresses them, closing each one. This keeps handoffs concrete and progress measurable.

The pipeline is an ordered sequence of **phases** (default: `coding`). Configurable to include research, planning, decomposition, and more:

```
research → plan → decompose → coding → polish → human review
```

Within each phase, the agents run an **implement → review** loop. When the reviewer files zero new beads, the phase has **converged** and the pipeline advances.

```
Iteration 1: Implement → Review → 3 beads filed
Iteration 2: Fix beads → Review → 1 bead filed
Iteration 3: Final fix → Review → 0 beads → converged ✓
```

## Quick start

### Install

```bash
brew install z-morgan/tap/streams
```

Or with Go:

```bash
go install github.com/zmorgan/streams/cmd/streams@latest
```

### Prerequisites

- [Claude Code CLI](https://docs.anthropic.com/en/docs/claude-code) installed and authenticated
- [beads](https://github.com/zmorgan/beads) initialized in your target repo (`bd init`)

### Run

```bash
cd your-project
streams
```

Press `n` to create your first stream. Run `streams --help` for all CLI options.

### Headless mode

Run a single stream without the TUI:

```bash
streams --headless --task "Add input validation to the API endpoints"
```

## TUI

The **dashboard** shows all streams in a channel view — vertical columns with each stream's iteration history side by side. A compact list view is also available (toggle with `v`).

<!-- TODO: replace with actual screenshot -->
```
Streams  2 running · 1 completed
┌──────────────────────┐┌──────────────────────┐┌──────────────────────┐
│ auth-endpoints       ││ fix-logging          ││ refactor-db          │
│ streams/auth         ││ streams/logs         ││ streams/db           │
│ Running   coding     ││ ✓ Completed  coding  ││ Running   plan       │
│ ──────────────────── ││ ──────────────────── ││ ──────────────────── │
│ coding 1             ││ coding 1             ││ plan 1               │
│ coding 2             ││ coding 2             ││ plan 2               │
│ coding 3             ││ coding 3             ││ ◐ plan 3...          │
│ ◐ coding 4 · review  ││ coding 4             ││                      │
│                      ││ coding 5             ││                      │
└──────────────────────┘└──────────────────────┘└──────────────────────┘
```

The **detail view** (press `enter`) shows a two-pane layout: iteration list on the left, snapshot details on the right — including agent reports, beads opened/closed, diff stats, and cost.

Press `g` from any view to inject **guidance** — free-text direction queued for the agent's next implement step. No need to stop the stream.

## Configuration

Layered config: CLI flags > project config (`.streams/config.toml`) > user config (`~/.config/streams/config.toml`) > defaults.

```bash
streams config                           # View effective config
streams config set max-iterations 20     # Project-level
streams config set --global pipeline "plan,coding"  # User-level
```

Prompt templates are fully customizable — run `streams prompts --list` to see available templates, and `streams prompts --export <name>` to override them. See `streams --help` and `streams config --help` for full details.
