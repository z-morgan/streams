# Diagnose Agent — Design Plan

## Context

Streams captures a rich audit trail per stream (snapshots, artifacts, beads, git history, guidance, errors). When a stream veers off course, users currently have to manually inspect this data and figure out what went wrong. This feature adds a keyboard shortcut that spawns an interactive Agent chat pre-loaded with the stream's full history, enabling the user to diagnose issues and have the agent fix prompts, beads, and pipeline config.

## Design Decisions

- **Chat UX**: External `claude` CLI session with injected context (simplest, full interactive chat for free, can evolve to TUI-integrated later)
- **Fix scope**: Agent chooses scope (per-stream, project, or global) based on whether the issue is stream-specific or systemic, user confirms
- **Agent powers**: Full control minus replay — prompts, pipeline config, beads management (all are just file edits and `bd` commands, no extra complexity over prompts-only)

## How It Works

### User Flow

1. User is in Detail View (or Dashboard), presses `D` (diagnose) on a paused/errored/completed stream
2. Streams builds a **diagnosis context document** from the stream's history
3. Streams spawns `claude` in interactive mode in a new terminal window, with the context as a system prompt
4. User has a free-form conversation with the agent about what went wrong
5. Agent analyzes history and proposes fixes (prompt edits, beads cleanup, pipeline changes)
6. Agent applies approved changes by writing files / running `bd` commands
7. User exits the claude session, returns to the TUI
8. On next resume, the stream picks up the updated prompts/config

### Diagnosis Context Document

The core engineering work. A structured summary injected as the system prompt:

```
# Stream Diagnosis Context

## Task
<original task description>

## Pipeline
<phases that ran, with breakpoints>

## Iteration History
### Research Phase
#### Iteration 0
- Summary: <implement summary>
- Review: <review text>
- Beads opened: <count>
- Beads closed: <count>
- Cost: $X.XX

#### Iteration 1
...

### Plan Phase
...

## Current State
- Status: <paused/errored/completed>
- Current phase: <phase name>
- Last error: <if any>
- Open beads: <list with titles>

## Artifacts
### research.md
<contents>

### plan.md
<contents>

## Prompt Templates in Use
### coding-implement.tmpl
<current template — embedded or override>

### coding-review.tmpl
<current template>

## Override Locations
- Per-stream: <stream data dir>/prompts/
- Project: .streams/prompts/
- Global: ~/.config/streams/prompts/
```

### What the Agent Can Do

| Action | How | Complexity |
|--------|-----|-----------|
| Edit prompt templates | Write to override dirs (per-stream, project, or global) | Core feature |
| Close stale beads | `bd close <id> --reason "..."` | Free (Bash access) |
| File new beads | `bd create ...` | Free (Bash access) |
| Adjust pipeline | Edit `stream.json` (pipeline, breakpoints) | Low (file edit) |
| Adjust iteration limits | Edit config | Low |
| Queue guidance | Append to stream's guidance file | Low |

### Scope Selection

The agent's system prompt instructs it to:
1. Diagnose the root cause (prompt issue vs. task ambiguity vs. pipeline misconfiguration)
2. Propose fixes with a recommended scope:
   - **Per-stream**: "This is specific to this task — I'll override the coding-implement prompt just for this stream"
   - **Project**: "This repo's conventions need specific prompt guidance — I'll add a project-level override"
   - **Global**: "This is a systemic issue with the default prompts — I'll update the global override"
3. Explain the recommendation and let the user confirm or redirect

### Per-Stream Prompt Overrides (New Capability)

Currently only global overrides exist (`~/.config/streams/prompts/`). Need to add:

1. **Project-level**: `.streams/prompts/<phase>-<step>.tmpl` — checked before global
2. **Per-stream**: `<stream data dir>/prompts/<phase>-<step>.tmpl` — checked before project

Update `LoadPrompt()` in `internal/loop/prompts.go` to check:
```
per-stream override → project override → global override → embedded default
```

The stream data dir path needs to be passed through to the prompt loader (currently it only knows about the global config dir).

## Implementation Steps

### 1. Add prompt override hierarchy
- Update `LoadPrompt()` to accept stream-level and project-level override paths
- Check per-stream → project → global → embedded
- Thread the stream's data dir path through to the prompt loader

### 2. Build the diagnosis context builder
- New package or file: `internal/diagnosis/context.go`
- `BuildContext(stream, snapshots, config) string`
- Reads snapshots, artifacts, beads state, current prompts, pipeline config
- Produces the structured markdown document

### 3. Build the diagnosis system prompt
- `internal/diagnosis/prompt.go` or embedded template
- Instructs the agent on its role, available actions, scope selection rules
- Includes the context document

### 4. Add the spawn command
- `internal/diagnosis/spawn.go`
- Constructs the `claude` CLI invocation with system prompt
- Handles terminal spawning (macOS: `open -a Terminal`, or detect tmux and split)

### 5. Wire up the keyboard shortcut
- Add `D` key binding in detail view and dashboard
- Only enabled when stream is paused/errored/completed
- Calls orchestrator method to build context and spawn session

### 6. Orchestrator integration
- `Orchestrator.Diagnose(streamID string) error`
- Loads stream, snapshots, config
- Calls diagnosis context builder
- Spawns the claude session

## Files to Modify

- `internal/loop/prompts.go` — prompt override hierarchy
- `internal/loop/loop.go` — pass stream data dir to prompt loader
- `internal/ui/detail.go` — `D` key binding
- `internal/ui/dashboard.go` — `D` key binding
- `internal/orchestrator/orchestrator.go` — `Diagnose()` method

## Files to Create

- `internal/diagnosis/context.go` — context document builder
- `internal/diagnosis/prompt.go` — system prompt for the diagnosis agent
- `internal/diagnosis/spawn.go` — claude CLI spawning

## Verification

1. Create a stream, run it through a few iterations, pause it
2. Press `D` — verify claude session opens with full context
3. Ask the agent "why did coding take 5 iterations?" — verify it references actual snapshot data
4. Ask it to fix a prompt — verify it writes to the correct override location
5. Resume the stream — verify the updated prompt is picked up
6. Test per-stream, project, and global override scopes

## Beads

- Epic: `streams-6ln` — Diagnose Agent
  - `streams-6ln.1` — Add prompt override hierarchy (no deps, ready)
  - `streams-6ln.2` — Build diagnosis context builder (no deps, ready)
  - `streams-6ln.3` — Build diagnosis system prompt (no deps, ready)
  - `streams-6ln.4` — Spawn claude CLI session (depends on .2 and .3)
  - `streams-6ln.5` — Wire up D keyboard shortcut (depends on .4 and .1)
