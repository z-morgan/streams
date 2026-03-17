package ui

import (
	"strings"

	"github.com/zmorgan/streams/internal/stream"
)

// Scope identifies where a keyboard shortcut is active.
type Scope int

const (
	ScopeGlobal            Scope = iota
	ScopeDashboard               // list mode
	ScopeDashboardChannels       // channels mode
	ScopeDetail                  // iteration list
	ScopeDetailOutput            // focused output/artifact pane
	ScopeBeadBrowse              // bead navigation within a snapshot
)

// Label returns the human-readable name for a scope.
func (s Scope) Label() string {
	switch s {
	case ScopeGlobal:
		return "Global"
	case ScopeDashboard:
		return "Dashboard"
	case ScopeDashboardChannels:
		return "Dashboard · Channels"
	case ScopeDetail:
		return "Inspect"
	case ScopeDetailOutput:
		return "Output Pane"
	case ScopeBeadBrowse:
		return "Bead Browse"
	default:
		return ""
	}
}

// DetailCtx provides stream state for conditional help bar rendering.
type DetailCtx struct {
	Status        stream.Status
	CanRevise     bool
	CanAdvance    bool
	AtReview      bool
	HasArtifact   bool
	ShowArtifact  bool
	ArtifactPhase string
}

// KeyBinding describes a single keyboard shortcut.
type KeyBinding struct {
	Key         string // display string: "j/k", "ctrl+c", ">"
	Action      string // "navigate", "quit", "wrap up"
	Description string // man-page-style description for the ? modal
	Scope       Scope
	Condition   string                  // human-readable, shown in ? modal; "" if always available
	ShowFunc    func(*DetailCtx) bool   // nil = always show in help bar
	ActionFunc  func(*DetailCtx) string // nil = use Action; overrides Action for help bar
}

// Bindings is the declarative registry of all keyboard shortcuts.
// Order within each scope determines help bar display order.
var Bindings = []KeyBinding{
	// ── Global ──────────────────────────────────────────────────────
	{Key: "?", Action: "this help", Description: "Open this help screen showing keyboard shortcuts for the current view.", Scope: ScopeGlobal},
	{Key: "ctrl+c", Action: "quit", Description: "Quit the application. Prompts for confirmation if streams are running.", Scope: ScopeGlobal},

	// ── Dashboard (list mode) ──────────────────────────────────────
	{Key: "j/k", Action: "navigate", Description: "Move selection up or down through the stream list.", Scope: ScopeDashboard},
	{Key: "enter", Action: "inspect", Description: "Open the inspect view for the selected stream.", Scope: ScopeDashboard},
	{Key: "n", Action: "new", Description: "Create a new stream. Opens a multi-step wizard for title, task, phases, model, and breakpoints.", Scope: ScopeDashboard},
	{Key: "s", Action: "start", Description: "Start the selected stream. Begins execution from the current phase.", Scope: ScopeDashboard},
	{Key: "x", Action: "pause", Description: "Pause the selected stream after the current step completes.", Scope: ScopeDashboard},
	{Key: "X", Action: "kill", Description: "Kill the selected stream immediately without waiting for the current step.", Scope: ScopeDashboard},
	{Key: "d", Action: "delete", Description: "Delete the selected stream permanently. Only available when the stream is stopped.", Scope: ScopeDashboard},
	{Key: "D", Action: "diagnose", Description: "Launch a diagnostic Claude session in a new terminal tab for the selected stream.", Scope: ScopeDashboard},
	{Key: "g", Action: "guidance", Description: "Queue a guidance message for the selected stream. The message is delivered at the next iteration boundary.", Scope: ScopeDashboard},
	{Key: "b", Action: "blockers", Description: "Set blocker dependencies for the selected stream. Blocked streams auto-start when all blockers stop running.", Scope: ScopeDashboard},
	{Key: "v", Action: "channels", Description: "Switch to the channels view, which displays streams organized by channel in a grid.", Scope: ScopeDashboard},
	{Key: "q", Action: "quit", Description: "Quit the application. Prompts for confirmation if streams are running.", Scope: ScopeDashboard},

	// ── Dashboard (channels mode) ──────────────────────────────────
	{Key: "h/l", Action: "navigate", Description: "Move selection left or right across channels.", Scope: ScopeDashboardChannels},
	{Key: "enter", Action: "inspect", Description: "Open the inspect view for the selected stream.", Scope: ScopeDashboardChannels},
	{Key: "n", Action: "new", Description: "Create a new stream. Opens a multi-step wizard for title, task, phases, model, and breakpoints.", Scope: ScopeDashboardChannels},
	{Key: "s", Action: "start", Description: "Start the selected stream. Begins execution from the current phase.", Scope: ScopeDashboardChannels},
	{Key: "x", Action: "pause", Description: "Pause the selected stream after the current step completes.", Scope: ScopeDashboardChannels},
	{Key: "X", Action: "kill", Description: "Kill the selected stream immediately without waiting for the current step.", Scope: ScopeDashboardChannels},
	{Key: "d", Action: "delete", Description: "Delete the selected stream permanently. Only available when the stream is stopped.", Scope: ScopeDashboardChannels},
	{Key: "D", Action: "diagnose", Description: "Launch a diagnostic Claude session in a new terminal tab for the selected stream.", Scope: ScopeDashboardChannels},
	{Key: "g", Action: "guidance", Description: "Queue a guidance message for the selected stream. The message is delivered at the next iteration boundary.", Scope: ScopeDashboardChannels},
	{Key: "b", Action: "blockers", Description: "Set blocker dependencies for the selected stream. Blocked streams auto-start when all blockers stop running.", Scope: ScopeDashboardChannels},
	{Key: "v", Action: "list", Description: "Switch back to the list view.", Scope: ScopeDashboardChannels},
	{Key: "q", Action: "quit", Description: "Quit the application. Prompts for confirmation if streams are running.", Scope: ScopeDashboardChannels},

	// ── Detail (iteration list) ────────────────────────────────────
	{Key: "j/k", Action: "iterations", Description: "Move selection up or down through the iteration list. Not available after stream completion.", Scope: ScopeDetail,
		ShowFunc: func(ctx *DetailCtx) bool { return ctx.Status != stream.StatusCompleted }},
	{Key: "enter", Action: "focus output", Description: "Focus the output pane for the selected iteration, enabling scrolling through live output or artifacts. Not available at the review step.", Scope: ScopeDetail,
		ShowFunc: func(ctx *DetailCtx) bool {
			return ctx.Status != stream.StatusCompleted && !ctx.AtReview
		}},
	{Key: "a", Action: "attach", Description: "Attach to the Claude session in a new terminal tab. If the stream is running, it is paused first. Only available when a session exists and the stream is not completed.", Scope: ScopeDetail, Condition: "session exists",
		ShowFunc: func(ctx *DetailCtx) bool {
			return ctx.Status != stream.StatusCompleted && !ctx.AtReview
		}},
	{Key: "s", Action: "start", Description: "Start the stream. Available when the stream is paused or stopped, not at the review step.", Scope: ScopeDetail,
		ShowFunc: func(ctx *DetailCtx) bool {
			return ctx.Status != stream.StatusRunning && ctx.Status != stream.StatusCompleted && !ctx.AtReview
		}},
	{Key: "c", Action: "complete", Description: "Complete the stream and finalize its output. Only available when paused at the review step.", Scope: ScopeDetail, Condition: "paused at review",
		ShowFunc: func(ctx *DetailCtx) bool { return ctx.AtReview }},
	{Key: "w", Action: "wrap up", Description: "Initiate convergence (wrap-up), signaling the stream to finish its current work and proceed to review. Only available when the stream is running.", Scope: ScopeDetail, Condition: "stream is running",
		ShowFunc: func(ctx *DetailCtx) bool { return ctx.Status == stream.StatusRunning }},
	{Key: "x", Action: "pause", Description: "Pause the stream after the current step completes. Only available when the stream is running.", Scope: ScopeDetail, Condition: "stream is running",
		ShowFunc: func(ctx *DetailCtx) bool { return ctx.Status == stream.StatusRunning }},
	{Key: "X", Action: "kill", Description: "Kill the stream immediately. Only available when the stream is running.", Scope: ScopeDetail, Condition: "stream is running",
		ShowFunc: func(ctx *DetailCtx) bool { return ctx.Status == stream.StatusRunning }},
	{Key: ">", Action: "skip phase", Description: "Skip to the next pipeline phase. Only available when paused and a next phase exists.", Scope: ScopeDetail, Condition: "paused, next phase available",
		ShowFunc: func(ctx *DetailCtx) bool { return ctx.CanAdvance }},
	{Key: "D", Action: "diagnose", Description: "Launch a diagnostic Claude session in a new terminal tab. Only available when the stream is not running.", Scope: ScopeDetail,
		ShowFunc: func(ctx *DetailCtx) bool { return ctx.Status != stream.StatusRunning }},
	{Key: "r", Action: "revise", Description: "Revise a completed phase by selecting it and providing feedback. Only available when the pipeline has more than one phase and the stream is not completed.", Scope: ScopeDetail, Condition: "past first phase",
		ShowFunc: func(ctx *DetailCtx) bool {
			return ctx.CanRevise && ctx.Status != stream.StatusCompleted
		}},
	{Key: "g", Action: "guidance", Description: "Queue a guidance message delivered at the next iteration boundary. Not available after stream completion.", Scope: ScopeDetail,
		ShowFunc: func(ctx *DetailCtx) bool { return ctx.Status != stream.StatusCompleted }},
	{Key: "b", Action: "config", Description: "Open the stream configuration panel for breakpoints, model, and notification settings. Not available after stream completion.", Scope: ScopeDetail, Condition: "pipeline > 1 phase",
		ShowFunc: func(ctx *DetailCtx) bool { return ctx.Status != stream.StatusCompleted }},
	{Key: "d", Action: "delete", Description: "Delete the stream permanently. Only available at the review step or after completion.", Scope: ScopeDetail,
		ShowFunc: func(ctx *DetailCtx) bool {
			return ctx.AtReview || ctx.Status == stream.StatusCompleted
		}},
	{Key: "q/esc", Action: "back", Description: "Return to the dashboard.", Scope: ScopeDetail},
	{Key: "f", Action: "toggle artifact", Description: "Toggle between the snapshot summary and the artifact file for the current phase. Only available when the snapshot has an artifact.", Scope: ScopeDetail, Condition: "snapshot has artifact",
		ShowFunc: func(ctx *DetailCtx) bool { return ctx.HasArtifact },
		ActionFunc: func(ctx *DetailCtx) string {
			if ctx.ShowArtifact {
				return "show summary"
			}
			if ctx.ArtifactPhase != "" {
				return "show " + ctx.ArtifactPhase + ".md"
			}
			return "toggle artifact"
		}},

	// ── Detail output/artifact pane ────────────────────────────────
	{Key: "j/k", Action: "scroll", Description: "Scroll up or down through the output or artifact content.", Scope: ScopeDetailOutput},
	{Key: "G", Action: "bottom", Description: "Jump to the bottom of the output.", Scope: ScopeDetailOutput},
	{Key: "f", Action: "back to summary", Description: "Switch back to the snapshot summary view. Only available when viewing an artifact.", Scope: ScopeDetailOutput, Condition: "viewing artifact",
		ShowFunc: func(ctx *DetailCtx) bool { return ctx.ShowArtifact }},
	{Key: "esc", Action: "back to list", Description: "Return focus to the iteration list.", Scope: ScopeDetailOutput},

	// ── Bead browse ────────────────────────────────────────────────
	{Key: "j/k", Action: "select bead", Description: "Move selection up or down through the bead list.", Scope: ScopeBeadBrowse},
	{Key: "enter", Action: "show details", Description: "Show full details for the selected bead.", Scope: ScopeBeadBrowse},
	{Key: "esc", Action: "back to iterations", Description: "Return to the iteration list.", Scope: ScopeBeadBrowse},
}

// BindingsForScope returns all bindings for the given scope, preserving order.
func BindingsForScope(scope Scope) []KeyBinding {
	var result []KeyBinding
	for _, b := range Bindings {
		if b.Scope == scope {
			result = append(result, b)
		}
	}
	return result
}

// HelpBarText builds a "key: action  key: action" string for the bottom
// help bar. Bindings with a non-nil ShowFunc are included only when ctx is
// non-nil and ShowFunc returns true. Global bindings (like ?) are appended
// after the scope-specific ones.
func HelpBarText(scope Scope, ctx *DetailCtx) string {
	var parts []string
	scopes := []Scope{scope}
	if scope != ScopeGlobal {
		scopes = append(scopes, ScopeGlobal)
	}
	for _, s := range scopes {
		for _, b := range Bindings {
			if b.Scope != s {
				continue
			}
			if b.ShowFunc != nil {
				if ctx == nil || !b.ShowFunc(ctx) {
					continue
				}
			}
			action := b.Action
			if b.ActionFunc != nil && ctx != nil {
				action = b.ActionFunc(ctx)
			}
			parts = append(parts, b.Key+": "+action)
		}
	}
	return strings.Join(parts, "  ")
}
