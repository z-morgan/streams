# Plan: `?` Keyboard Help Modal

## Problem

Keyboard shortcuts are defined inline across switch statements in `app.go`, `dashboard.go`, and `detail.go`. Help text is maintained separately as hardcoded strings (`dashboardListHelp`, `dashboardChannelHelp`, `detailHelpText()`). There's no mechanism to prevent a new shortcut from being added to a switch statement without updating the help text, or vice versa.

We want a `?` shortcut that opens a modal listing every shortcut, grouped by scope, with conditions noted — and we want the system to resist going stale.

## Design

### Keybinding registry (`internal/ui/keybindings.go`)

Introduce a single declarative source of truth for all keyboard shortcuts:

```go
type Scope int

const (
    ScopeGlobal Scope = iota
    ScopeDashboard
    ScopeDashboardChannels
    ScopeDetail
    ScopeDetailOutput
    ScopeBeadBrowse
    // Overlay scopes omitted — overlays render their own inline help
    // and the ? modal is dismissed when an overlay opens.
)

type KeyBinding struct {
    Key         string // display string: "j/k", "ctrl+s", ">"
    Action      string // "navigate", "wrap up current phase"
    Scope       Scope
    Condition   string // "" if always available, otherwise e.g. "stream is paused"
}

var Bindings = []KeyBinding{ ... }
```

Every shortcut that appears in a switch statement gets an entry here. Overlay-internal shortcuts (esc to cancel, enter to confirm within a wizard step) are **not** included — they're transient, self-documenting in the overlay's own help line, and would clutter the reference.

### Refactor existing help text generation

Replace `dashboardListHelp`, `dashboardChannelHelp`, and `detailHelpText()` with functions that filter `Bindings` by scope and format the result using the existing `renderHelp()` function. This eliminates the second copy of shortcut documentation and ensures the bottom help bar and `?` modal always agree.

`detailHelpText()` currently uses runtime state (stream status, pipeline index, artifact presence) to conditionally include shortcuts. The refactored version will iterate `Bindings` filtered to `ScopeDetail`, checking each binding's `Condition` against the current state. To support this, add a `ShowFunc` field:

```go
type KeyBinding struct {
    Key       string
    Action    string
    Scope     Scope
    Condition string            // human-readable, shown in ? modal
    ShowFunc  func(*DetailCtx) bool // nil = always show in help bar
}

type DetailCtx struct {
    Status      stream.Status
    CanRevise   bool
    CanAdvance  bool
    AtReview    bool
    HasArtifact bool
    // ...
}
```

The help bar calls `ShowFunc` to decide what to display; the `?` modal ignores `ShowFunc` and shows everything with the `Condition` text.

### `?` modal rendering

A new overlay, consistent with the existing overlay pattern:

- **State**: `showHelp bool` on `Model`
- **Update**: `updateHelp(msg)` — only handles `esc` and `?` to dismiss, plus `j/k`/arrow keys to scroll (the list will be long)
- **View**: `renderHelpOverlay(bindings, width, height)` — groups bindings by scope, renders each group with a header

Layout sketch:

```
╭─ Keyboard Shortcuts ──────────────────────╮
│                                            │
│  Global                                    │
│    ?         this help                     │
│    q         quit                          │
│    ctrl+c    quit                          │
│                                            │
│  Dashboard                                 │
│    j/k       navigate streams              │
│    enter     inspect selected stream       │
│    n         create new stream             │
│    s         start selected stream         │
│    x         pause (stream is running)     │
│    ...                                     │
│                                            │
│  Inspect                                   │
│    j/k       navigate iterations           │
│    enter     focus output pane             │
│    a         attach to Claude session      │
│    r         revise (past first phase)     │
│    ...                                     │
│                                            │
│  Output Pane                               │
│    j/k       scroll                        │
│    G         jump to bottom                │
│    esc       back to iteration list        │
│                                            │
│              esc or ? to close             │
╰────────────────────────────────────────────╯
```

Bindings with a non-empty `Condition` render the condition in parentheses after the action, styled with `helpStyle` (muted):

```
    x         pause (stream is running)
    r         revise (past first phase)
    c         complete (paused at review)
```

### Scroll support

The full list will likely exceed terminal height. Add `helpScroll int` to `Model` and handle `j/k`/arrows in `updateHelp`. The rendered content is sliced by scroll offset before placing in the overlay box.

### Where `?` intercepts in Update()

Insert the `?` check right after the ephemeral message clear (line 340) and before overlay dispatch (line 342). This makes it globally available from dashboard and detail views. When any other overlay is already open, that overlay's handler runs first and consumes the keypress — so `?` naturally can't conflict.

```go
// line ~340, after clearing ephemeral messages
if m.showHelp {
    return m.updateHelp(msg)
}

// Check for ? keypress before overlay dispatch
if msg, ok := msg.(tea.KeyPressMsg); ok && msg.String() == "?" {
    m.showHelp = true
    m.helpScroll = 0
    return m, nil
}
```

One subtlety: this block must come **before** the existing overlay checks so that `showHelp` gets priority. But it must come **after** `showQuitConfirm` — you shouldn't be able to open help while the quit dialog is showing. The cleanest placement is: quit confirm → help → all other overlays.

### Preventing drift: `TestBindingsCoverage`

Add a test in `internal/ui/keybindings_test.go` that parses all `case` branches in the key-handling switch statements and verifies each handled key string has a corresponding entry in `Bindings`. This is the strongest practical guarantee that future changes won't forget to update the registry.

Approach:
1. Use `go/parser` + `go/ast` to walk `app.go`
2. Find switch statements inside functions named `updateDashboard`, `updateDetail`, etc.
3. Extract string literals from `case` clauses
4. Cross-reference against `Bindings` filtered by the corresponding scope
5. Fail if any key string exists in code but not in `Bindings`

This catches the most common failure mode: someone adds a `case "z":` to a handler but forgets to add a `KeyBinding` entry. It won't catch the reverse (stale registry entry for a removed shortcut), but that's lower risk — the `?` modal showing a shortcut that does nothing is less harmful than a working shortcut being invisible.

A simpler alternative to AST parsing: maintain a `var handledKeys = map[Scope][]string{...}` test fixture that lists every key string per scope, and assert `len(handledKeys[scope]) == len(filterBindings(scope))`. This is easier to implement and still forces the developer to touch `keybindings.go` when they add a key, because the test fixture and registry must agree.

The AST approach is more robust but also more brittle (refactors to the switch structure can break the parser). I'd recommend starting with the simpler fixture approach and noting AST parsing as a future option.

## Steps

### 1. Create `internal/ui/keybindings.go` with the binding registry

Define the `KeyBinding` struct, `Scope` enum, `DetailCtx`, and populate `Bindings` with every shortcut currently handled across `updateDashboard`, `updateDetail`, and the global scope in `Update`. Include `Condition` strings for conditional shortcuts. Add `ShowFunc` closures for bindings that are conditionally shown in the help bar.

Add helper functions:
- `BindingsForScope(scope Scope) []KeyBinding` — filter
- `HelpBarText(scope Scope, ctx *DetailCtx) string` — build the `"key: action  key: action"` string for the bottom bar, respecting `ShowFunc`

### 2. Refactor help text generation to use the registry

Replace `dashboardListHelp` and `dashboardChannelHelp` constants with calls to `HelpBarText(ScopeDashboard, nil)` and `HelpBarText(ScopeDashboardChannels, nil)`.

Replace `detailHelpText()` with a version that builds a `DetailCtx` from the current stream state and calls `HelpBarText(ScopeDetail, ctx)` (plus `ScopeDetailOutput` / `ScopeBeadBrowse` when those sub-views are active).

Verify the bottom help bar renders identically before and after. A snapshot test or manual tmux comparison would work here.

### 3. Add `showHelp` state and `updateHelp` handler

Add `showHelp bool` and `helpScroll int` to `Model`. Add `updateHelp(msg)` that handles:
- `esc`, `?` → dismiss
- `j`, `down` → scroll down
- `k`, `up` → scroll up
- `G` → scroll to bottom
- `g` → scroll to top

Insert the `showHelp` check in `Update()` after quit confirm, before other overlays.

### 4. Add `renderHelpOverlay`

Render all bindings grouped by scope. Each scope gets a styled header. Bindings with conditions show them parenthesized and muted. Apply scroll offset. Use the standard `overlayStyle` + `lipgloss.Place` centering pattern. Cap width at ~50 columns.

### 5. Wire into `viewString()`

Add `if m.showHelp { return renderHelpOverlay(...) }` in `viewString()`, placed after quit/restart/delete confirms and before other overlays. This ensures help can't render on top of critical confirmations but takes priority over everything else.

### 6. Add `TestBindingsCoverage`

Create `internal/ui/keybindings_test.go`. Define a test fixture that lists every key string handled per scope (extracted manually from the switch statements). Assert that each key in the fixture has a corresponding `KeyBinding` entry, and each `KeyBinding` entry has a corresponding key in the fixture. This bidirectional check catches both missing and stale entries.

### 7. Verify with tmux

Run the app in a tmux session. Press `?` from the dashboard and from the detail view. Verify all shortcuts render correctly, scroll works, and `esc`/`?` dismisses the modal. Verify the bottom help bar still renders correctly in all views.

## Options

1. **File beads & hand off** — Create beads issues for each step, provide a follow-up prompt, then stop.
2. **Clear context & implement** — Clear context and auto-accept flow.
3. **Implement now** — Start implementing in the current context.
