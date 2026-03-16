# MCP Server Passthrough

Stream agents run via `claude -p` with `--allowedTools` and `--permission-mode dontAsk`, but they can't access MCP servers. The plumbing exists in the runtime (`--mcp-config` support in `claude.go:256-258`) but nothing in the loop ever uses it. Three gaps:

1. `buildRequest()` never sets the `mcpConfig` option
2. Phase tool lists are static and exclude MCP tool patterns (`mcp__<server>__*`)
3. There's no way to configure MCP for a stream/project

## Design

MCP config is **project-level** — all streams in a project share the same MCP servers. The user creates a standard Claude MCP config file at `.streams/mcp.json`:

```json
{
  "mcpServers": {
    "chrome-devtools": {
      "command": "npx",
      "args": ["@anthropic-ai/chrome-devtools-mcp@latest"]
    }
  }
}
```

The system automatically derives tool patterns from server names (e.g. `mcp__chrome-devtools__*`). These patterns are appended to every phase's `--allowedTools`. The file path is passed via `--mcp-config`.

The MCP config path is stored on each `Stream` for persistence/observability, but tool patterns are derived fresh from the file at each loop start (so adding a new server takes effect on restart).

## Steps

### 1. Load MCP config and derive tool patterns

Add a function to parse `.streams/mcp.json`, validate it, and extract server names. Return a struct with the file path and derived tool patterns.

**Where**: New file `internal/mcp/config.go`. Small package — just config loading and pattern derivation. Keeps MCP concerns separate from the environment (Docker) package.

```go
// internal/mcp/config.go
type Config struct {
    Path         string   // absolute path to the mcp.json file
    ToolPatterns []string // e.g. ["mcp__chrome-devtools__*"]
}

func LoadConfig(projectDir string) (*Config, error)
```

The function reads `.streams/mcp.json`, unmarshals to get the `mcpServers` map keys, and builds `mcp__<key>__*` for each. Returns nil (not error) if the file doesn't exist — MCP is opt-in.

**Files**: `internal/mcp/config.go`, `internal/mcp/config_test.go`

### 2. Add MCPConfigPath to Stream model and store

Add `MCPConfigPath string` to the `Stream` struct (`internal/stream/stream.go:123 area`). Add thread-safe getter/setter following the `EnvironmentPort` pattern.

Add `MCPConfigPath string` to `streamData` (`internal/store/store.go:38 area`) with JSON tag `mcp_config_path,omitempty`. Wire through `toStreamData()` and `fromStreamData()`.

**Files**: `internal/stream/stream.go`, `internal/store/store.go`

### 3. Wire MCP config through orchestrator

- Load MCP config in `main.go` alongside environment config (same pattern: `mcp.LoadConfig(repoDir)`)
- Store `*mcp.Config` on the `Orchestrator` struct (like `envManager`)
- In `Create()`, set `MCPConfigPath` on new streams from the loaded config
- Pass MCP config into `loop.Run()` — add an `mcpConfigPath string` parameter (or bundle it into a struct if the param list is too long)

**Files**: `internal/orchestrator/orchestrator.go`, `cmd/streams/main.go` (or wherever the orchestrator is initialized)

### 4. Update buildRequest() to forward MCP config and append tool patterns

Change `buildRequest()` signature to accept MCP config path and tool patterns:

```go
func buildRequest(prompt string, tools []string, envPort int, mcpConfigPath string, mcpToolPatterns []string) runtime.Request
```

Inside:
- If `mcpConfigPath != ""`, set `req.Options["mcpConfig"] = mcpConfigPath`
- Append `mcpToolPatterns` to the `tools` slice before joining into `allowedTools`

Update all 5 call sites:
- `loop.go:138` — implement step
- `loop.go:212` — review step
- `loop.go:479` — slot runner (`runSlots`)
- `coding.go:137` — rebase agent (can skip MCP here since rebase doesn't need browser)
- `coding.go:164` — also rebase agent

The loop needs access to the MCP config. Options:
- Add `MCPConfigPath` and `MCPToolPatterns` to `PhaseContext`
- Derive tool patterns at the start of `Run()` by reading the config file from `MCPConfigPath`

Go with `PhaseContext` — it already holds the stream and runtime, adding two more fields is clean.

**Files**: `internal/loop/loop.go`, `internal/loop/coding.go`, `internal/loop/phase.go`

### 5. Broaden system prompt for MCP tools

Currently the chrome-devtools system prompt only fires when `envPort > 0`. Split into two concerns:

1. **Environment prompt** (when `envPort > 0`): "A live application server is running at http://localhost:{port}."
2. **MCP browser prompt** (when MCP tool patterns include browser-related servers): "Use the chrome-devtools MCP tool to open pages, inspect elements, and verify your UI changes in the browser."

When both conditions are true, combine them. When only MCP is configured (no environment), just tell the agent about the MCP tools. When only environment is configured (no MCP), keep the current behavior.

Update `buildRequest()` to check the MCP tool patterns for browser-related servers (e.g. pattern contains "chrome-devtools" or "playwright") and conditionally append the appropriate system prompt section.

**Files**: `internal/loop/loop.go`

## Commit plan

1. Step 1: `internal/mcp/config.go` + tests
2. Steps 2-3: Stream model + store + orchestrator wiring
3. Steps 4-5: buildRequest changes + system prompt broadening + loop wiring
