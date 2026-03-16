# Stream Environments — Containerized App Servers for Verification

## Problem

Streams gives each stream an isolated git worktree, but there's no way for a stream's agent to verify UI changes in a running application. Verification tools like Chrome DevTools MCP need a live app server serving the worktree's code. This must work across any tech stack and any local development setup without streams knowing the internals.

## Design

Each stream can optionally spin up a **containerized environment** — a Docker Compose stack that runs the full application from the stream's worktree. Environments are provisioned on demand (lazy), not at stream creation time.

### Container Strategy

Docker Compose project namespacing provides full isolation for free:

```
Stream abc123:  docker compose -p streams-abc123 up -d
Stream def456:  docker compose -p streams-def456 up -d
```

Each project gets its own:
- Network namespace (no port conflicts between containers internally)
- Volumes (database data is per-stream)
- Container names (namespaced by project)

The worktree is bind-mounted into the app container. Frameworks with dev-mode file watching (Rails, Next.js, Phoenix) pick up code changes automatically.

### User Configuration

The user provides two things:

1. A Docker Compose file defining their full stack
2. A small streams config describing how to use it

```yaml
# .streams/environment.yml
compose_file: docker-compose.streams.yml
service: app                    # which service is the application server
setup: bin/rails db:reset       # runs inside container after services are healthy
port: 3000                      # internal port the app listens on
health_check: /up               # path to GET for readiness (on the mapped host port)
health_timeout: 60s             # how long to wait for health check (default 60s)
```

```yaml
# docker-compose.streams.yml
services:
  app:
    build: .
    volumes:
      - .:/app
    ports:
      - "${STREAMS_PORT}:3000"
    depends_on:
      db:
        condition: service_healthy
    environment:
      RAILS_ENV: development
      DATABASE_URL: postgres://postgres:postgres@db/app_dev

  db:
    image: postgres:16
    environment:
      POSTGRES_PASSWORD: postgres
    healthcheck:
      test: pg_isready -U postgres
      interval: 2s
      timeout: 5s
      retries: 10
```

The `STREAMS_PORT` env var is the only coupling point — streams sets it to a unique host port per stream. Everything else is the user's standard Docker Compose setup.

Database isolation happens naturally: each Compose project has its own `db` container with its own volume. The `setup` command (e.g., `bin/rails db:reset`) bootstraps the database from scratch inside the container — no filesystem copies, no template databases.

### Port Allocation

Deterministic from a base port + pool index:

```
Pool: ports 4010–4017 (8 slots)
Stream 0 → STREAMS_PORT=4010
Stream 1 → STREAMS_PORT=4011
...
```

Ports are allocated from a pool when an environment is provisioned and returned when torn down. The pool is sized to match the maximum concurrent environments (configurable, default 8).

### Lifecycle

**Provision** (on demand, when a phase needs verification):

```
1. Allocate port from pool
2. cd <worktree>
3. STREAMS_PORT=<port> docker compose -p streams-<id> -f <compose_file> up -d
4. Poll health check: GET http://localhost:<port><health_path>
5. docker compose -p streams-<id> exec <service> <setup_command>
6. Mark environment Ready
```

**During agent invocation** (when environment is Ready):

```bash
claude -p \
  --mcp-config '<config with chrome-devtools pointing at localhost:port>' \
  --append-system-prompt "App server running at http://localhost:<port>. Use chrome-devtools to verify UI changes in the browser." \
  ...
```

**Teardown** (on stream delete, complete, or explicit teardown):

```
1. docker compose -p streams-<id> down -v    # -v removes volumes
2. Return port to pool
3. Clear environment from stream
```

**Error recovery**: If provisioning fails (build error, health check timeout), the environment is marked Failed with the error. The stream continues without verification tools — it degrades gracefully. The user can retry provisioning or fix the Compose file.

## Implementation Plan

### 1. Config loading and types

Add `internal/environment/config.go`:
- Parse `.streams/environment.yml` from the target project directory
- Define `Config` struct matching the YAML schema
- Validate: compose file exists, port is valid, service name is non-empty
- Return nil config if no `.streams/environment.yml` exists (feature is opt-in)

Add `internal/environment/environment.go`:
- Define `Environment` struct:
  ```go
  type Environment struct {
      ProjectName string
      HostPort    int
      Status      Status  // Provisioning | Ready | Failed | Down
      Error       error
  }

  type Status int
  const (
      StatusProvisioning Status = iota
      StatusReady
      StatusFailed
      StatusDown
  )
  ```

### 2. Port pool

Add `internal/environment/pool.go`:
- Atomic port allocator: `Allocate() (int, error)` and `Release(port int)`
- Configurable base port (default 4010) and pool size (default 8)
- Thread-safe (multiple streams may provision concurrently)

### 3. Docker Compose lifecycle

Add `internal/environment/compose.go`:
- `Up(ctx, workdir, config, port) error` — runs `docker compose -p ... up -d`
- `Exec(ctx, projectName, service, command) error` — runs setup command
- `Down(ctx, projectName) error` — tears down with `-v`
- `HealthCheck(ctx, port, path, timeout) error` — polls GET until 200 or timeout
- All commands use `os/exec` with context for cancellation

### 4. Manager (provision/teardown orchestration)

Add `internal/environment/manager.go`:
- `Manager` struct holds the config, port pool, and active environments
- `Ensure(ctx, streamID, workdir) (*Environment, error)` — idempotent: returns existing if Ready, provisions if not
- `Teardown(ctx, streamID) error` — tears down and releases port
- `TeardownAll(ctx) error` — cleanup on application exit
- Thread-safe map of streamID → Environment

### 5. Stream model integration

In `internal/stream/stream.go`:
- Add `EnvironmentPort int` field (0 = no environment). Persisted in `stream.json`.
- No pointer to the Environment struct itself — the manager owns that. The port is enough for the runtime to build the MCP config.

### 6. Runtime MCP injection

In `internal/runtime/runtime.go`:
- Add `MCPConfig` field to `Request.Options` (or as a dedicated `Request` field)

In `internal/runtime/claude/claude.go`:
- If `MCPConfig` option is set, append `--mcp-config` flag to the CLI invocation

### 7. Phase integration

Two options for how phases request an environment:

**Option A: Phase declares need, loop provisions.** Add an optional interface:
```go
type EnvironmentAware interface {
    NeedsEnvironment() bool
}
```
The loop checks this before the implement step. If true and no environment exists, calls `manager.Ensure()`. The environment's host port is used to build MCP config for the runtime request.

**Option B: Explicit verify phase.** A new `verify` macro-phase that always provisions an environment, runs browser-based verification, and tears down after convergence.

Recommendation: **Option A** for flexibility. Any phase can opt in. A dedicated verify phase can be added later as a thin wrapper that always returns `NeedsEnvironment() = true`.

### 8. Orchestrator integration

In `internal/orchestrator/orchestrator.go`:
- Construct `environment.Manager` on startup (pass config loaded from target project)
- In `Delete()` and `Complete()`: call `manager.Teardown(streamID)`
- In `Start()`: pass manager reference to the loop so it can call `Ensure()`
- On application exit: call `manager.TeardownAll()`

### 9. System prompt additions

When an environment is active, append to the agent's system prompt:

```
## Application Server

A live application server is running at http://localhost:<port>.
Use the chrome-devtools MCP tool to open pages, inspect elements, and verify your UI changes in the browser.
After making code changes, the server will automatically reload — just refresh the page.
```

### 10. TUI indicators (minimal)

- Show environment status on the detail view (e.g., "Server: ready on :4012" or "Server: provisioning...")
- No new key bindings needed for v1 — provisioning is automatic

## What This Does NOT Include

- **Compose file generation.** The user writes their own compose file. A future feature (see linked bead) could provide an AI agent that helps scaffold the compose file for common stacks.
- **Image caching / pre-building.** First provision for each stream does a full `docker compose up` including any builds. Future optimization: pre-build the image once and share across streams.
- **MCP server management.** This plan assumes Chrome DevTools MCP is configured in the `--mcp-config` flag. The actual MCP server process management (starting Chrome, connecting DevTools protocol) is handled by the MCP server package, not by streams.
- **Hot-reload for compiled languages.** Frameworks with file watchers work automatically. For compiled languages (Go, Rust), the agent would need to rebuild inside the container. This works but isn't optimized.

## Risk Assessment

This is a modular add-on. The `internal/environment/` package is entirely new. Existing code changes are minimal:

| Existing file | Change | Risk |
|---|---|---|
| `stream/stream.go` | Add `EnvironmentPort int` field | None — zero value means no environment |
| `orchestrator.go` | Construct manager, call teardown on delete/complete | Low — guarded by nil checks |
| `runtime/runtime.go` | Add MCPConfig to Request options | None — empty = no change |
| `runtime/claude/claude.go` | Conditionally append `--mcp-config` | None — conditional |
| `store/store.go` | Persist `EnvironmentPort` | Low — additive JSON field |

The iteration loop, snapshot system, TUI rendering, and all existing phases are untouched.
