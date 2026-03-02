## Auto-memory

Do not use MEMORY.md. Use beads for all project context and task tracking.

## Task Tracking (beads)

Use `bd` for all task tracking. Do not use TodoWrite or TaskCreate.

Workflow:
- `bd ready` — find unblocked work (check at start of each session)
- `bd show <id>` — review issue details before starting
- `bd update <id> --status in_progress` — claim work before starting
- `bd close <id>` — mark done after completing
- `bd q "Title" --priority 2` — quick-create an issue
- Never use `bd edit` (blocks agents by opening $EDITOR)
- Priorities: 0=critical, 1=high, 2=medium, 3=low, 4=backlog
- Run `bd sync` before ending a session
