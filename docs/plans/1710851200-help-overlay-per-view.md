# Plan: View-Scoped Help Overlay with Rich Descriptions

**Branch**: `help-overlay-per-view`

## Background

The `?` keyboard shortcut already opens a help modal that displays **all** scopes at once with terse labels (e.g., `start`, `navigate`). The user wants three things:

1. **View-scoped**: Show only shortcuts relevant to the current view (dashboard, streams/channels, inspect) instead of dumping every scope.
2. **Man-page descriptions**: Each shortcut gets a fuller description explaining what it does, not just a one-word action.
3. **Conditional shortcuts always shown**: Even shortcuts that aren't currently available (e.g., `D` — diagnose, only available when not running) should appear with a note about when they're available.

## Current State

- **`keybindings.go`**: Declarative `Bindings` registry with `Key`, `Action` (terse), `Scope`, `Condition`, `ShowFunc`, `ActionFunc`.
- **`renderHelpOverlay()`** in `app.go:2559-2619`: Iterates all scopes, renders each binding's `Action` + `Condition` in parentheses.
- **`HelpBarText()`**: Builds the compact bottom-bar string; uses `ShowFunc` to hide inapplicable shortcuts. This stays unchanged.

## Design Decisions

- Add a `Description` field to `KeyBinding` for the rich text. The existing `Action` field continues to serve the bottom help bar (compact). `Description` is used in the `?` modal.
- The `?` modal determines which scopes to show based on `m.view` and `m.dashboard.mode`:
  - `viewDashboard` + `modeList` → `ScopeGlobal` + `ScopeDashboard`
  - `viewDashboard` + `modeChannels` → `ScopeGlobal` + `ScopeDashboardChannels`
  - `viewDetail` → `ScopeGlobal` + `ScopeDetail` + `ScopeDetailOutput` + `ScopeBeadBrowse`
- Conditional shortcuts are **always** rendered in the modal (no `ShowFunc` filtering in the modal). Their `Condition` text explains availability.
- The modal title changes to reflect the current view: "Dashboard Shortcuts", "Channels Shortcuts", "Inspect Shortcuts".

## Implementation Steps

### Step 1: Add `Description` field to `KeyBinding` and populate all entries

**File**: `keybindings.go`

Add a `Description string` field to the `KeyBinding` struct. Then populate every binding in the `Bindings` slice with a man-page-style description. Examples:

```
{Key: "j/k", Action: "navigate", Description: "Move selection up or down through the stream list.", Scope: ScopeDashboard},
{Key: "D", Action: "diagnose", Description: "Launch a diagnostic Claude session for the selected stream. Available when the stream is not running.", Scope: ScopeDashboard},
{Key: "d", Action: "delete", Description: "Delete the selected stream permanently. Available when the stream is stopped.", Scope: ScopeDashboard},
```

For conditional shortcuts, fold the availability condition into the description rather than relying solely on the `Condition` field (though `Condition` is kept for the help bar).

### Step 2: Make `renderHelpOverlay` view-scoped and use descriptions

**File**: `app.go`

Changes to `renderHelpOverlay`:
- Accept the current `view` and `dashboardMode` as parameters (or derive scopes before calling).
- Instead of iterating a hardcoded `scopes` list, receive `[]Scope` determined by the caller.
- Render `Description` instead of `Action` + `Condition` for each binding.
- Update the overlay title to reflect the view name.

Changes at the call site (`View()` method around line 1937-1938):
- Pass `m.view` and `m.dashboard.mode` to `renderHelpOverlay`, or compute the scope list and title there and pass them in.

### Step 3: Show `?` in the bottom help bar

**File**: `keybindings.go`

`HelpBarText()` only renders bindings matching the requested scope, so `ScopeGlobal` bindings (like `?`) never appear in any view's bottom legend. Fix this by having `HelpBarText` also include `ScopeGlobal` bindings after the scope-specific ones. This way every view's legend ends with `? help`.

### Step 4: Update tests

**File**: `keybindings_test.go`

- Verify every binding has a non-empty `Description`.
- Verify `renderHelpOverlay` respects view scoping (dashboard mode only shows dashboard scopes, etc.).

---

## Descriptions Reference

Here are the proposed descriptions for each binding, grouped by scope:

### Global
| Key | Description |
|-----|-------------|
| `?` | Open this help screen showing keyboard shortcuts for the current view. |
| `ctrl+c` | Quit the application. Prompts for confirmation if streams are running. |

### Dashboard (list mode)
| Key | Description |
|-----|-------------|
| `j/k` | Move selection up or down through the stream list. |
| `enter` | Open the inspect view for the selected stream. |
| `n` | Create a new stream. Opens a multi-step wizard for title, task, phases, model, and breakpoints. |
| `s` | Start the selected stream. Begins execution from the current phase. |
| `x` | Pause the selected stream after the current step completes. |
| `X` | Kill the selected stream immediately without waiting for the current step. |
| `d` | Delete the selected stream permanently. Only available when the stream is stopped. |
| `D` | Launch a diagnostic Claude session in a new terminal tab for the selected stream. |
| `g` | Queue a guidance message for the selected stream. The message is delivered at the next iteration boundary. |
| `v` | Switch to the channels view, which displays streams organized by channel in a grid. |
| `q` | Quit the application. Prompts for confirmation if streams are running. |

### Dashboard (channels mode)
| Key | Description |
|-----|-------------|
| `h/l` | Move selection left or right across channels. |
| `enter` | Open the inspect view for the selected stream. |
| `n` | Create a new stream. Opens a multi-step wizard for title, task, phases, model, and breakpoints. |
| `s` | Start the selected stream. Begins execution from the current phase. |
| `x` | Pause the selected stream after the current step completes. |
| `X` | Kill the selected stream immediately without waiting for the current step. |
| `d` | Delete the selected stream permanently. Only available when the stream is stopped. |
| `D` | Launch a diagnostic Claude session in a new terminal tab for the selected stream. |
| `g` | Queue a guidance message for the selected stream. The message is delivered at the next iteration boundary. |
| `v` | Switch back to the list view. |
| `q` | Quit the application. Prompts for confirmation if streams are running. |

### Inspect (iteration list)
| Key | Description |
|-----|-------------|
| `j/k` | Move selection up or down through the iteration list. Not available after stream completion. |
| `enter` | Focus the output pane for the selected iteration, enabling scrolling through live output or artifacts. Not available at the review step. |
| `a` | Attach to the Claude session in a new terminal tab. If the stream is running, it is paused first. Only available when a session exists and the stream is not completed. |
| `s` | Start the stream. Available when the stream is paused or stopped, not at the review step. |
| `c` | Complete the stream and finalize its output. Only available when paused at the review step. |
| `w` | Initiate convergence (wrap-up), signaling the stream to finish its current work and proceed to review. Only available when the stream is running. |
| `x` | Pause the stream after the current step completes. Only available when the stream is running. |
| `X` | Kill the stream immediately. Only available when the stream is running. |
| `>` | Skip to the next pipeline phase. Only available when paused and a next phase exists. |
| `D` | Launch a diagnostic Claude session in a new terminal tab. Only available when the stream is not running. |
| `r` | Revise a completed phase by selecting it and providing feedback. Only available when the pipeline has more than one phase and the stream is not completed. |
| `g` | Queue a guidance message delivered at the next iteration boundary. Not available after stream completion. |
| `b` | Open the stream configuration panel for breakpoints, model, and notification settings. Not available after stream completion. |
| `d` | Delete the stream permanently. Only available at the review step or after completion. |
| `q/esc` | Return to the dashboard. |
| `f` | Toggle between the snapshot summary and the artifact file for the current phase. Only available when the snapshot has an artifact. |

### Output Pane
| Key | Description |
|-----|-------------|
| `j/k` | Scroll up or down through the output or artifact content. |
| `G` | Jump to the bottom of the output. |
| `f` | Switch back to the snapshot summary view. Only available when viewing an artifact. |
| `esc` | Return focus to the iteration list. |

### Bead Browse
| Key | Description |
|-----|-------------|
| `j/k` | Move selection up or down through the bead list. |
| `enter` | Show full details for the selected bead. |
| `esc` | Return to the iteration list. |
