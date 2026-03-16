# Compose Scaffolding Agent — Guided Environment Setup

## Problem

The stream environments system (see `1773420911-stream-environments.md`) requires two user-provided files: `.streams/environment.yml` and a Docker Compose file. Writing these from scratch is tedious and error-prone — the user needs to know Docker Compose syntax, understand bind-mount patterns for their framework, set up the right database image, wire health checks, and remember the `STREAMS_PORT` convention.

An interactive scaffolding agent can inspect the project, ask targeted questions, and generate working files in under a minute. This removes the biggest adoption barrier for stream environments.

## Design Principles

1. **Inspect first, ask second.** The agent should extract as much as it can from project signals before asking anything. Questions should be confirmations and gap-fillers, not interrogations.
2. **Opinionated defaults, easy overrides.** Pick sensible defaults for the detected stack (e.g., Postgres for Rails, health check at `/up` for Rails, `/api/health` for Node). Let the user override.
3. **Don't reinvent Dockerfiles.** If the project already has a Dockerfile or docker-compose.yml, use them as the starting point. Only generate from scratch when nothing exists.
4. **Output is editable.** The generated files are plain YAML that the user owns. The agent explains what it generated and why, so the user can modify it later.

## Agent Workflow

### Phase 1: Project Inspection (no user interaction)

Scan the project root for signals. Build a `ProjectProfile` struct:

```go
type ProjectProfile struct {
    // Framework detection
    Framework    string   // "rails", "django", "nextjs", "phoenix", "laravel", "go", "unknown"
    Language     string   // "ruby", "python", "javascript", "typescript", "elixir", "go", etc.

    // Package/dependency signals
    Gemfile      bool
    PackageJSON  bool
    GoMod        bool
    RequirementsTxt bool
    MixExs       bool
    ComposerJSON bool

    // Existing container signals (used as hint sources, not generation inputs)
    Dockerfile       bool
    Devcontainer     bool    // .devcontainer/

    // Database signals
    DatabaseAdapter  string   // "postgresql", "mysql", "sqlite", "mongodb", "none", "unknown"

    // Other services detected
    Redis        bool
    Elasticsearch bool
    Memcached    bool
    Sidekiq      bool     // or other background job processor

    // App server signals
    DevPort      int      // port the framework typically listens on (3000, 8000, 4000, etc.)
    DevCommand   string   // inferred dev server command
    HealthPath   string   // inferred health check endpoint
    WorkDir      string   // app directory inside container (usually /app)
}
```

#### Signal Sources & Detection Rules

| Signal File | What It Tells Us |
|---|---|
| `Gemfile` | Ruby project. Scan for `rails`, `pg`/`mysql2`/`sqlite3`, `redis`, `sidekiq` gems. |
| `Gemfile.lock` | More reliable version pins. Faster to grep than Gemfile for gem presence. |
| `config/database.yml` | Rails DB config. Parse adapter field directly. |
| `package.json` | Node project. Scan `dependencies` for `next`, `express`, `prisma`, `@prisma/client`, `pg`, `mysql2`, `mongoose`, `redis`, `ioredis`. Check `scripts.dev` for dev command. |
| `requirements.txt` / `Pipfile` / `pyproject.toml` | Python. Scan for `django`, `flask`, `fastapi`, `psycopg2`, `mysqlclient`, `pymongo`, `redis`, `celery`. |
| `mix.exs` | Elixir. Scan deps for `:phoenix`, `:ecto`, `:postgrex`, `:myxql`. |
| `composer.json` | PHP/Laravel. Scan for `laravel/framework`, database extensions. |
| `go.mod` | Go. Harder to infer framework — check for `gin`, `echo`, `fiber`, `chi`. |
| `Dockerfile` | Hints only: parse `EXPOSE` for port, `WORKDIR` for app dir. Not used as generation input. |
| `.devcontainer/devcontainer.json` | Hints: `forwardPorts` (app port), `postCreateCommand` (setup command). |
| `Procfile` / `Procfile.dev` | Heroku-style. Parse `web:` line for server command and port. |
| `.env` / `.env.example` | Environment variable hints. Look for `PORT`, `DATABASE_URL`, `REDIS_URL`. |
| `bin/dev` | Rails 7+ dev script. May reference `foreman` or `overmind`. |

#### Framework-Specific Heuristics

**Rails:**
- Default port: 3000
- Health check: `/up` (Rails 7.1+) or `/`
- Dev command: `bin/rails server -b 0.0.0.0`
- Setup command: `bin/rails db:prepare` (idempotent, handles both create+migrate and just migrate)
- DB adapter: read from `config/database.yml` → `default.adapter` or `development.adapter`

**Next.js / Node:**
- Default port: 3000 (Next.js), or `PORT` env var
- Health check: `/api/health` (if it exists) or `/`
- Dev command: from `package.json` scripts.dev, usually `next dev` or `npm run dev`
- DB: infer from Prisma schema (`prisma/schema.prisma` → `datasource.provider`)

**Django:**
- Default port: 8000
- Health check: `/admin/` (exists by default) or `/`
- Dev command: `python manage.py runserver 0.0.0.0:8000`
- Setup command: `python manage.py migrate`
- DB: parse `DATABASES.default.ENGINE` from settings.py

**Phoenix:**
- Default port: 4000
- Health check: `/` or custom
- Dev command: `mix phx.server`
- Setup command: `mix ecto.setup`
- DB: parse `config/dev.exs` for Repo adapter

**Go (with web framework):**
- Default port: 8080
- Health check: `/health` or `/`
- Dev command: needs air or similar watcher, or just `go run .`
- No standard setup command

### Phase 2: Interactive Confirmation

Present the detected profile as a summary and ask targeted questions only for gaps. The interaction model is a single-screen TUI form, not a multi-step wizard.

#### What the user sees:

```
┌─ Environment Setup ──────────────────────────────────────────┐
│                                                              │
│  Detected: Rails 7 + PostgreSQL + Redis + Sidekiq            │
│                                                              │
│  ┌─ Confirm or adjust ─────────────────────────────────────┐ │
│  │  App service port:     3000                             │ │
│  │  Health check path:    /up                              │ │
│  │  Setup command:        bin/rails db:prepare             │ │
│  │  Health timeout:       60s                              │ │
│  └─────────────────────────────────────────────────────────┘ │
│                                                              │
│  Additional services:                                        │
│  [x] PostgreSQL 16                                           │
│  [x] Redis 7                                                 │
│  [ ] Elasticsearch                                           │
│  [ ] Memcached                                               │
│                                                              │
│           [ Generate ]    [ Cancel ]                         │
└──────────────────────────────────────────────────────────────┘
```

#### Question Priority (only ask what we can't infer)

1. **Always ask:** Confirm detected framework + services (could be wrong)
2. **Ask if ambiguous:** Database adapter (e.g., Gemfile has both `pg` and `sqlite3`)
3. **Ask if missing:** Health check path (no standard path detected)
4. **Never ask:** Port allocation (streams handles this), compose project naming, volume mount paths (derived from framework)

### Phase 3: Generation

Always generate both files fresh from the confirmed profile. Existing Dockerfiles and compose files are treated as signal sources during detection (Phase 1), not as starting points for generation. This keeps the generator simple — one path, one output shape.

Generated files:

1. **`Dockerfile.streams`** — dev-optimized Dockerfile at project root:
   - Framework-specific base image (ruby:3.x, node:20, python:3.x, elixir:1.x)
   - Install system dependencies (for native extensions, database client libs)
   - Copy dependency manifest + install (Gemfile, package.json, etc.)
   - Set WORKDIR, EXPOSE, CMD for the dev server
   - Dev-focused: skip asset precompilation, include dev dependencies, run in development mode
   - Named `Dockerfile.streams` to avoid clobbering any existing `Dockerfile`

2. **`docker-compose.streams.yml`** — compose file referencing `Dockerfile.streams`:
   - App service: builds from `Dockerfile.streams`, bind-mounts `.:/app`, maps `${STREAMS_PORT}:<internal-port>`
   - Database service (if detected): stock image with health check, credentials wired into app env vars
   - Auxiliary services (Redis, etc.): stock images as needed
   - All services use `docker compose` v2 syntax

3. **`.streams/environment.yml`** — streams config pointing at the compose file

#### Template Design

Rather than raw string templates, use a structured intermediate representation:

```go
type ComposeSpec struct {
    Services []ServiceSpec
}

type ServiceSpec struct {
    Name        string
    Image       string            // for pre-built services (postgres, redis)
    Build       *BuildSpec        // for app service
    Ports       []PortMapping
    Volumes     []VolumeMount
    Environment map[string]string
    DependsOn   map[string]DependsOnCondition
    HealthCheck *HealthCheckSpec
    Command     string
}
```

Marshal this to YAML. This avoids string template bugs (quoting, indentation) and makes it easy to add/remove services programmatically.

#### Generated File Conventions

- `docker-compose.streams.yml` at project root
- `Dockerfile.streams` at project root
- `.streams/environment.yml` in the streams config directory
- All generated files include a header comment: `# Generated by streams. Edit freely — this file is yours.`
- All three files should be committed to the repo (shared team config, not per-developer)

### Phase 4: Validation

After generation, the agent does a quick sanity check:

1. **Syntax check:** `docker compose -f docker-compose.streams.yml config --quiet` — catches YAML errors and invalid compose syntax
2. **Build check (optional, user can skip):** `docker compose -f docker-compose.streams.yml build` — catches Dockerfile errors
3. **Report:** Show the user what was generated, where the files are, and what to do next

No full `up` — that happens when a stream actually needs an environment.

## Edge Cases

### Monorepo / Subdirectory Apps

If the app lives in a subdirectory (e.g., `apps/web/`), the compose file's build context and volume mounts need to be relative to the project root (where streams runs), not the app directory. Detect this by checking if `Gemfile`/`package.json` is in a subdirectory rather than root. Ask the user to confirm the app directory.

### Multiple Services in One Repo

Some projects have both a frontend and backend (e.g., `client/` + `server/`). The agent should detect this and ask which service is the "primary" one that streams should verify against. The compose file can include both, but `.streams/environment.yml` points to one `service` name.

### Existing Generated Files

If `docker-compose.streams.yml` or `Dockerfile.streams` already exists, don't overwrite silently. Offer to regenerate (with confirmation) or abort.

### Unsupported / Unknown Stack

If the agent can't detect the framework:
- Ask the user directly: "What framework/language is this project using?"
- If truly custom, generate a minimal template with clear TODO comments
- Don't block — a partial scaffold is better than nothing

### No Database

Valid case (static sites, APIs with external DBs, etc.). Skip database service generation entirely. The compose file just has the app service.

### Docker Not Installed

Detect early: `docker compose version`. If Docker isn't available, explain the requirement and exit gracefully. Don't generate files that can't be used.

## Implementation Considerations

### Where This Lives

`internal/environment/scaffold/` — separate from the runtime environment management:

```
internal/environment/scaffold/
├── detect.go       // ProjectProfile detection logic
├── profile.go      // ProjectProfile type + framework heuristics
├── confirm.go      // TUI confirmation form (Bubble Tea model)
├── generate.go     // ComposeSpec → YAML generation
├── templates.go    // Framework-specific service templates
└── validate.go     // Post-generation validation
```

### When It Runs

Two entry points:

1. **`streams init`** — explicit CLI command. Runs the full workflow. Good for first-time setup or regenerating.
2. **Auto-detect on stream creation** — if `.streams/environment.yml` doesn't exist but the environment feature is enabled (or hinted at in config), offer to run the scaffolding agent. This is a gentle nudge, not a forced flow:
   ```
   No environment configured. Run `streams init` to set up
   Docker Compose for browser verification. [Skip for now]
   ```

### Generation via LLM vs. Deterministic Templates

Two approaches, not mutually exclusive:

**Deterministic templates (recommended for v1):** Go code generates the compose file from the detected profile. Predictable, fast, no API cost. Covers 90% of cases.

**LLM-assisted generation (future enhancement):** For unusual stacks or complex setups, pass the detected profile + project files to Claude and ask it to generate the compose file. More flexible, handles edge cases, but slower and costs money.

v1 should be deterministic templates. The LLM path is a natural extension — the `ProjectProfile` struct is the same input regardless of which generator produces the output.

### Testing Strategy

- **Detection tests:** Fixture directories with known project structures → assert correct `ProjectProfile`
- **Generation tests:** `ProjectProfile` → assert valid compose YAML with expected services, ports, volumes
- **Integration test:** Full scaffold in a temp directory → `docker compose config` validates output
- **No actual Docker builds in CI** — too slow and requires Docker daemon

## Stack Templates (v1 Scope)

Priority order based on likely user base:

1. **Rails + PostgreSQL** — most common streams user profile
2. **Next.js + PostgreSQL (via Prisma)** — second most common
3. **Django + PostgreSQL** — Python web standard
4. **Phoenix + PostgreSQL** — Elixir niche but Docker-friendly
5. **Generic Node.js (Express/Fastify)** — catch-all for Node
6. **Go web server** — needs special handling (compiled, no file watching)
7. **Generic fallback** — minimal template with TODOs

Each template is a function that takes a `ProjectProfile` and returns a `ComposeSpec`. Adding a new stack means writing one function.

## Resolved Decisions

1. **Commit all generated files.** `docker-compose.streams.yml`, `Dockerfile.streams`, and `.streams/environment.yml` are shared team config. No gitignore entries.
2. **Docker Compose v2 only.** Shell out to `docker compose` (space). No fallback to the deprecated `docker-compose` binary.
3. **Always generate `Dockerfile.streams`.** Don't try to adapt existing Dockerfiles or detect dev stages. The generated file is dev-optimized for the detected stack. Users can modify it afterward.
