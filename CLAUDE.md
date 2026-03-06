## Auto-memory

Do not use MEMORY.md. Use `bd update --notes` for session context instead of memory files.

## Task Tracking (beads)

Use `bd` for all task tracking. Do not use TodoWrite, TaskCreate, or TaskList. See `bd prime` output for full command reference.

## TUI Testing with tmux

**REQUIRED**: After implementing any UI changes, you MUST verify them using tmux before committing. Do not skip this step — compilation alone is not sufficient.

Use tmux to run the app in a background session:

```bash
# cd to the plentish app directory to test against it
cd ../plentish

# Start the TUI in a detached session
tmux new-session -d -s test -x 120 -y 40 '../streams/streams'
sleep 1

# Read the screen
tmux capture-pane -t test -p

# Send keystrokes
tmux send-keys -t test Down Enter
sleep 0.5
tmux capture-pane -t test -p

# Clean up
tmux kill-session -t test
```

Always kill the session when done testing.

## Building

Run `go build` commands outside of the sandbox.

