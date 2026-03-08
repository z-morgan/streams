# Persistent Configuration for Streams

## Problem

All streams settings are CLI flags with hardcoded defaults. Users must pass flags
every invocation, and there's no way to set per-project or per-user defaults.
The per-step budget ($2.00, now $5.00) is a common pain point — it's too low for
implement steps but there's no persistent way to change or disable it.

## Design

### Config file location and precedence

Follow the same pattern as prompt overrides (`~/.config/streams/prompts/`):

1. **CLI flags** — highest priority, override everything
2. **Project config** — `.streams/config.toml` in the data directory
3. **User config** — `~/.config/streams/config.toml`
4. **Built-in defaults** — hardcoded in the binary

A CLI flag is considered "set" only if the user explicitly passed it. Unset flags
fall through to file-based config. This requires switching from `flag.String()` to
checking `flag.Visit()` after parse, or using a sentinel value.

### File format

TOML — lightweight, human-editable, no external dependencies needed (a small
pure-Go TOML parser like `BurntSushi/toml` or `pelletier/go-toml`, or even a
hand-rolled key=value parser for the minimal surface area we need).

```toml
# ~/.config/streams/config.toml  (user defaults)
# .streams/config.toml           (project defaults)

max-budget-per-step = "5.00"   # "" or "0" to disable
max-iterations = 10
pipeline = "coding"
```

### Budget: disable by default or keep a default?

**Recommendation: keep the $5 default, allow disabling with `"0"` or `""`.**

Rationale:
- A budget guard prevents runaway costs from bugs or stuck loops
- Power users can disable it per-project or globally
- `--max-budget-per-step 0` on the CLI disables it for a single run

The `BudgetRuntime` wrapper is skipped entirely when the budget is empty/zero,
so there's no behavioral difference from "no budget feature at all."

### Config CLI subcommand

```bash
streams config                    # show effective config (merged)
streams config --edit             # open user config in $EDITOR
streams config --edit --project   # open project config in $EDITOR
streams config set <key> <value>  # set a value in project config
streams config set --global <key> <value>  # set a value in user config
```

## Implementation plan

### Step 1: Config loading and merging

Add `internal/config/config.go`:

```go
type Config struct {
    MaxBudgetPerStep string `toml:"max-budget-per-step"`
    MaxIterations    int    `toml:"max-iterations"`
    Pipeline         string `toml:"pipeline"`
}
```

- `LoadUser()` reads `~/.config/streams/config.toml`
- `LoadProject(dataDir)` reads `<dataDir>/config.toml`
- `Merge(user, project, flags)` produces final config with precedence
- Use a minimal TOML parser or plain `key = value` line parser (no dep needed
  for this surface area)

**Test**: unit tests for merge precedence, missing files, empty values.

### Step 2: Wire config into main.go

Replace direct flag→Config wiring with:

```go
fileCfg := config.Load(storeRoot)          // merges user + project
finalCfg := config.ApplyFlags(fileCfg, flagSet)  // CLI overrides
```

Pass `finalCfg` to orchestrator. Handle budget="0" by skipping `BudgetRuntime`.

**Test**: integration test — config file with budget=0 results in no budget wrapper.

### Step 3: `streams config` subcommand

Add `cmd/streams/config.go` (or extend the subcommand dispatch in main.go):
- `streams config` — print effective config as TOML
- `streams config set <key> <value>` — write to project config
- `streams config set --global <key> <value>` — write to user config

**Test**: round-trip set → load.

### Step 4: TUI config display (optional, low priority)

Show effective budget/iterations in the dashboard footer or stream detail view
so users can see what limits are active without checking the CLI.

## Alternatives considered

- **Environment variables**: Doesn't give per-project config. Could be added later
  as another precedence layer between user config and CLI flags.
- **JSON/YAML**: TOML is more human-friendly for flat key-value config. JSON
  requires trailing comma discipline; YAML has footguns.
- **No config file, just a dotfile with flags**: e.g. `.streams/flags` containing
  `--max-budget-per-step 10`. Simpler but hacky and harder to merge/validate.
- **Disable budget by default**: Removes a useful safety net. Better to keep a
  reasonable default and make it easy to override.
