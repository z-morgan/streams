# Bubble Tea v2 Upgrade

Upgrade from Bubble Tea v1.2.4 to v2, along with bubbles and lipgloss. The primary motivation is enabling `Cmd+s` (super+s) via the Kitty keyboard protocol, which v2 supports natively.

## Scope

6 files need changes:

- `cmd/streams/main.go`
- `internal/ui/app.go`
- `internal/ui/dashboard.go`
- `internal/ui/detail.go`
- `internal/ui/styles.go`
- `internal/ui/tail.go`

## Steps

### 1. Update dependencies

Replace module paths and fetch v2:

```
go get charm.land/bubbletea/v2@latest
go get charm.land/bubbles/v2@latest
go get charm.land/lipgloss/v2@latest
```

Then remove old `github.com/charmbracelet/*` entries from go.mod. Run `go mod tidy`.

**Commit**: "Update charmbracelet dependencies to v2"

### 2. Update import paths (all 6 files)

| Old | New |
|-----|-----|
| `github.com/charmbracelet/bubbletea` | `charm.land/bubbletea/v2` |
| `github.com/charmbracelet/bubbles/textarea` | `charm.land/bubbles/v2/textarea` |
| `github.com/charmbracelet/lipgloss` | `charm.land/lipgloss/v2` |

**Commit**: "Update charmbracelet import paths to v2"

### 3. Migrate `View()` return type

Every `View()` method and render helper currently returns `string`. The top-level `View()` must now return `tea.View`. There are two approaches:

- **Option A**: Change only the top-level `View()` to return `tea.View`, keep all render helpers returning `string`, and wrap at the boundary with `tea.NewView()`.
- **Option B**: Change all render functions to return `tea.View`.

Option A is simpler and lower-risk. The top-level `View()` in `app.go` dispatches to render functions that return strings — wrap the final result:

```go
func (m Model) View() tea.View {
    // ... existing rendering logic that produces a string ...
    return tea.NewView(rendered)
}
```

Set alt screen and keyboard enhancements on the View:

```go
v := tea.NewView(rendered)
v.AltScreen = true
return v
```

Remove `tea.WithAltScreen()` from `tea.NewProgram()` in `main.go` since it's now on the View.

**Commit**: "Migrate View() to return tea.View"

### 4. Migrate `tea.KeyMsg` → `tea.KeyPressMsg`

Find all `case tea.KeyMsg:` type assertions and change to `case tea.KeyPressMsg:`. There are ~15 of these in `app.go`.

**Commit**: "Replace tea.KeyMsg with tea.KeyPressMsg"

### 5. Fix key string changes

The space key changed from `" "` to `"space"` in v2. This affects:

- `updateNewStreamPipeline` — space toggles phase selection
- `updateNewStreamBreakpoints` — space toggles breakpoints
- `updateEditBreakpoints` — space toggles breakpoints

Change all `case " ":` to `case "space":` in these functions.

All other key strings (`"enter"`, `"esc"`, `"ctrl+s"`, `"ctrl+c"`, `"alt+enter"`, single letters like `"g"`, `"q"`, etc.) remain the same in v2.

**Commit**: "Update space key string for v2"

### 6. Migrate `tea.WindowSize()` → `tea.RequestWindowSize`

In `Init()`:

```go
// v1
return tea.Batch(tea.WindowSize(), spinnerTick())

// v2 — tea.RequestWindowSize is a Cmd, not a function
return tea.Batch(tea.RequestWindowSize, spinnerTick())
```

Note: Need to verify whether this is `tea.RequestWindowSize` (a Cmd value) or `tea.RequestWindowSize()` (a function). Check the v2 API.

**Commit**: "Migrate tea.WindowSize to v2 equivalent"

### 7. Verify `tea.ExecProcess` in v2

Two call sites in `app.go` (lines ~418 and ~784):

```go
tea.ExecProcess(c, func(err error) tea.Msg {
    return claudeExitMsg{err: err}
})
```

Check whether `tea.ExecProcess` still exists in v2 with the same signature. If renamed or changed, update both call sites.

**Commit** (if changes needed): "Update tea.ExecProcess for v2"

### 8. Verify `tea.Tick`, `tea.Batch`, `tea.Quit`

Three `tea.Tick` usages (`clearStatusAfter`, `tailTick`, `spinnerTick`), one `tea.Batch` usage, one `tea.Quit` usage. These are unlikely to have changed but verify they compile.

### 9. Check bubbles textarea v2 changes

Current textarea usage:
- `textarea.New()` — 7 calls
- `textarea.Blink` — 8 calls (used as a `tea.Cmd`)
- Field access: `.Placeholder`, `.CharLimit`, `.Prompt`, `.ShowLineNumbers`, `.SetHeight()`, `.SetWidth()`, `.Reset()`, `.Focus()`, `.Value()`, `.Update(msg)`

Per the bubbles v2 upgrade guide, Width/Height fields became getter/setter methods. Check if `.SetHeight()` and `.SetWidth()` signatures changed, and whether `.ShowLineNumbers` became a method.

**Commit** (if changes needed): "Update textarea usage for bubbles v2"

### 10. Check lipgloss v2 changes

Current lipgloss usage (extensive — 50+ `NewStyle()` calls across 5 files):
- `lipgloss.NewStyle()` with chain methods: `.Bold()`, `.Faint()`, `.Foreground()`, `.Background()`, `.Padding()`, `.PaddingLeft()`, `.PaddingRight()`, `.Border*()`, `.Margin*()`, `.Width()`, `.Height()`, `.Render()`
- `lipgloss.Place()` — 11 calls
- `lipgloss.Width()` — 10 calls
- `lipgloss.JoinHorizontal()` — 2 calls
- `lipgloss.Color()` — 10 calls
- Border types: `NormalBorder()`, `RoundedBorder()`, `Border{}`
- Constants: `lipgloss.Center`, `lipgloss.Top`

No `AdaptiveColor` used (removed in v2, so this is safe).

Key things to verify:
- Whether `lipgloss.Place()` signature changed
- Whether `lipgloss.Width()` still exists as a standalone function
- Whether `lipgloss.Border{}` struct literal still works
- Whether style chain methods are the same

**Commit** (if changes needed): "Update lipgloss usage for v2"

### 11. Change guidance submit keybinding to `super+s`

Now that v2 supports the Kitty keyboard protocol, change the guidance submit keybinding:

In `updateGuidance`:
```go
case "super+s":
```

Update the help text in `renderGuidanceOverlay`:
```go
helpStyle.Render("⌘s: send  esc: cancel")
```

Also update `updateComplete` and `updateRevise` which use the same `ctrl+s` pattern, for consistency.

**Commit**: "Change overlay submit keybinding to Cmd+s (super+s)"

### 12. Build and fix compile errors

Run `go build ./cmd/streams/` and fix any remaining compile errors iteratively. The compiler will catch most type mismatches and missing methods.

**Commit**: "Fix remaining v2 compile errors"

## Manual Verification via tmux

After the upgrade compiles, exercise every keybinding and overlay in tmux. Run from the `plentish` directory:

```bash
cd ../plentish
tmux new-session -d -s test -x 120 -y 40 '../streams/streams'
sleep 1
```

### Dashboard tests

```bash
# Verify dashboard renders
tmux capture-pane -t test -p

# Navigation: j/k, up/down
tmux send-keys -t test j && sleep 0.3 && tmux capture-pane -t test -p
tmux send-keys -t test k && sleep 0.3 && tmux capture-pane -t test -p

# View toggle: v
tmux send-keys -t test v && sleep 0.3 && tmux capture-pane -t test -p
tmux send-keys -t test v && sleep 0.3  # toggle back

# Channel navigation: h/l
tmux send-keys -t test h && sleep 0.3 && tmux capture-pane -t test -p
tmux send-keys -t test l && sleep 0.3 && tmux capture-pane -t test -p
```

### Guidance overlay test

```bash
# Open guidance modal
tmux send-keys -t test g && sleep 0.5 && tmux capture-pane -t test -p
# Verify: modal visible with title "Guidance", help text shows "⌘s: send  esc: cancel"

# Type text
tmux send-keys -t test "test guidance text" && sleep 0.3 && tmux capture-pane -t test -p
# Verify: text appears in textarea

# Cancel with esc
tmux send-keys -t test Escape && sleep 0.3 && tmux capture-pane -t test -p
# Verify: modal closed, no "[queued]" indicator

# Reopen and submit (note: super+s may not pass through tmux — test ctrl+s fallback too)
tmux send-keys -t test g && sleep 0.3
tmux send-keys -t test "real guidance" && sleep 0.3
tmux send-keys -t test C-s && sleep 0.3 && tmux capture-pane -t test -p
# Verify: modal closed, "[1 queued]" visible on stream card
```

### Detail view tests

```bash
# Enter detail view
tmux send-keys -t test Enter && sleep 0.5 && tmux capture-pane -t test -p
# Verify: detail view renders with iteration list

# Scroll iterations: j/k
tmux send-keys -t test j && sleep 0.3 && tmux capture-pane -t test -p
tmux send-keys -t test k && sleep 0.3 && tmux capture-pane -t test -p

# Focus right pane
tmux send-keys -t test Enter && sleep 0.3 && tmux capture-pane -t test -p
# Verify: right pane focused, scrollable

# Back out
tmux send-keys -t test Escape && sleep 0.3  # back to left pane
tmux send-keys -t test Escape && sleep 0.3 && tmux capture-pane -t test -p
# Verify: back to dashboard
```

### New stream overlay test

```bash
# Open new stream dialog
tmux send-keys -t test n && sleep 0.5 && tmux capture-pane -t test -p
# Verify: title input visible

# Cancel
tmux send-keys -t test Escape && sleep 0.3 && tmux capture-pane -t test -p
# Verify: overlay closed
```

### Edit breakpoints test (from detail view)

```bash
tmux send-keys -t test Enter && sleep 0.5  # enter detail
tmux send-keys -t test b && sleep 0.5 && tmux capture-pane -t test -p
# Verify: breakpoints overlay visible

# Space toggles breakpoint
tmux send-keys -t test Space && sleep 0.3 && tmux capture-pane -t test -p
# Verify: breakpoint toggled (crucial — tests "space" key string change)

tmux send-keys -t test Escape && sleep 0.3  # cancel
tmux send-keys -t test Escape && sleep 0.3  # back to dashboard
```

### Cleanup

```bash
tmux kill-session -t test
```

### What to verify at each step

- [ ] Dashboard renders with styles (colors, borders, bold text)
- [ ] All navigation keys work (j/k/up/down/h/l/enter/esc)
- [ ] Guidance modal opens, accepts text, submits, shows queued count
- [ ] Detail view renders iteration list and right pane content
- [ ] New stream overlay opens and closes
- [ ] Space key works for toggling in breakpoints/pipeline selectors
- [ ] View toggle (v) switches between channel and list modes
- [ ] Quit (q) exits cleanly

## Risk notes

- **`super+s` won't pass through tmux** — tmux doesn't support the Kitty keyboard protocol. The keybinding will only work in terminals that support it (Ghostty, Kitty, iTerm2, WezTerm, etc.) running the app directly (not inside tmux). Consider keeping `ctrl+s` as a fallback alongside `super+s`.
- **Lipgloss v2 is the highest-risk area** — 50+ style calls across 5 files. If the chain API changed, it's tedious to fix. But `NewStyle()` chain methods are unlikely to break since they're the core API.
- **`tea.ExecProcess`** — if removed or renamed in v2, the attach feature breaks. Verify early.
