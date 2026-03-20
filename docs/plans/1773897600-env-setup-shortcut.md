# Environment Setup Shortcut

## Problem

Multi-environment support requires the user to create three files (`docker-compose.streams.yml`, `Dockerfile.streams`, `.streams/environment.json`) with framework-specific configuration. The `streams init` CLI command exists but is only discoverable outside the TUI. Users inside the TUI have no way to set up or learn about environment configuration.

## Design

Add an `e` keyboard shortcut to the Dashboard and Detail views that launches an interactive Claude Code session in a new terminal tab. The session is pre-loaded with a system prompt containing:

1. The detected project profile (framework, database, services) from `scaffold.DetectProfile()`
2. The environment config schema and examples
3. Instructions to analyze the project and generate the three required files

This is the "smart" version of environment setup — Claude inspects the actual codebase and tailors the config, rather than relying only on template matching. The existing `streams init` template-based approach handles common frameworks well, but Claude can handle edge cases (monorepos, custom stacks, non-standard layouts) and explain its decisions interactively.

### Why not an overlay?

An overlay with static documentation would be quick to build but less useful — the user still has to write the files themselves. The Claude session approach lets the user ask questions, iterate on the config, and get a working setup without leaving the terminal. It follows the same pattern as the existing `D` (diagnose) shortcut.

### Key choice: `C`

`C` (uppercase) is available on both Dashboard and Detail views. Mnemonic: **C**onfigure.

### Conditional visibility

The shortcut is always available but the help bar action text adapts:
- When `.streams/environment.json` doesn't exist: `e: multi-env setup`
- When it does exist: `e: multi-env config` (for reconfiguration/troubleshooting)

## Implementation Plan

### 1. Extract tab-launching into a shared package

The tab-launching logic (tmux detection, Ghostty/iTerm/Terminal.app AppleScript) currently lives in `internal/diagnosis/tab.go` as private functions. Extract the terminal-detection and tab-spawning logic into `internal/terminal/tab.go` so both diagnosis and environment setup can use it.

**New file:** `internal/terminal/tab.go`
- `LaunchScript(scriptPath, title string) error` — detects terminal environment and opens a new tab running the script
- Move `launchTmuxWindow`, `launchGhosttyTab`, `launchITermTab`, `launchTerminalTab` here (exported)

**Update:** `internal/diagnosis/tab.go`
- Replace private tab-launching calls with `terminal.LaunchScript()`
- Keep `LaunchInTab()` as the diagnosis-specific entry point (builds prompt, writes temp files, calls `terminal.LaunchScript`)

### 2. Build the environment setup system prompt

**New file:** `internal/environment/setup.go`
- `BuildSetupPrompt(profile *scaffold.ProjectProfile, projectDir string, hasExistingConfig bool) string`
- Prompt includes:
  - What stream environments are and why they exist
  - The three files that need to be created (or updated) and where they go
  - The detected project profile as structured context (framework, language, database, services, ports)
  - The `environment.json` schema with field descriptions
  - A complete example for the detected framework (pulled from scaffold templates as reference)
  - The `STREAMS_PORT` convention for host port mapping
  - If existing config: instructions to review and troubleshoot
  - If no config: instructions to inspect the project and generate all three files

### 3. Add the launch function

**New file:** `internal/environment/launch.go`
- `LaunchSetupInTab(projectDir string) error`
  1. Call `scaffold.DetectProfile(projectDir)` to gather project info
  2. Check if `.streams/environment.json` already exists
  3. Call `BuildSetupPrompt()` with the profile and existing-config flag
  4. Write prompt to temp file, write launcher script (same pattern as diagnosis)
  5. Launcher script: `cd <projectDir> && exec claude --system-prompt "$prompt"`
  6. Call `terminal.LaunchScript()` to open the tab

### 4. Wire up the orchestrator

**Update:** `internal/orchestrator/orchestrator.go`
- Add `LaunchEnvSetup() error` method
  - Calls `environment.LaunchSetupInTab(orch.projectDir)`
  - No stream context needed — this is a project-level operation

### 5. Add the keybinding and TUI handler

**Update:** `internal/ui/keybindings.go`
- Add `e` binding for `ScopeDashboard`, `ScopeDashboardChannels`, and `ScopeDetail`
- Action: "env setup" / "env config" (dynamic via `ActionFunc` based on whether config exists)
- Description: "Launch an interactive Claude session to configure containerized environments for streams."

**Update:** `internal/ui/app.go`
- Add `hasEnvConfig bool` field to `Model` (set at startup, toggled after setup launches)
- In `updateDashboard()` and `updateDetail()`: handle `e` key press
  - Call `m.orch.LaunchEnvSetup()`
  - Show status message: "Environment setup launched in new tab."
- Pass `hasEnvConfig` through to `DetailCtx` for the help bar `ActionFunc`

### 6. Update DetailCtx for conditional action text

**Update:** `internal/ui/keybindings.go`
- Add `HasEnvConfig bool` to `DetailCtx`

The binding:
```go
{Key: "e", Action: "multi-env setup", Scope: ScopeDashboard,
    Description: "Launch an interactive Claude session to set up or reconfigure containerized stream environments.",
    ActionFunc: func(ctx *DetailCtx) string {
        if ctx != nil && ctx.HasEnvConfig {
            return "multi-env config"
        }
        return "multi-env setup"
    }},
```

(Repeated for `ScopeDashboardChannels` and `ScopeDetail`.)

## Files Changed

| File | Change | Risk |
|---|---|---|
| `internal/terminal/tab.go` | **New** — extracted tab-launching logic | None (refactor) |
| `internal/diagnosis/tab.go` | Use `terminal.LaunchScript()` instead of private functions | Low — behavior unchanged |
| `internal/environment/setup.go` | **New** — system prompt builder | None |
| `internal/environment/launch.go` | **New** — orchestrates detection + prompt + tab launch | None |
| `internal/orchestrator/orchestrator.go` | Add `LaunchEnvSetup()` method | Low — additive |
| `internal/ui/keybindings.go` | Add `e` bindings, `HasEnvConfig` to `DetailCtx` | Low — additive |
| `internal/ui/app.go` | Handle `e` key, add `hasEnvConfig` field | Low — follows existing patterns |

## What This Does NOT Include

- **Running `streams init` inline.** The existing template-based scaffold is a separate Bubble Tea program. Embedding it inside the main TUI would require significant refactoring. The Claude session approach is both simpler to implement and more capable.
- **Auto-provisioning after setup.** After Claude generates the config files, the user still needs to restart streams for the config to be picked up. A future enhancement could watch for file changes.
- **Docker status indicators.** No new UI for showing Docker container status. That's part of the existing environment lifecycle, not this feature.
