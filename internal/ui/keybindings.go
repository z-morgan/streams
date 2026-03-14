package ui

import (
	"strings"

	"github.com/zmorgan/streams/internal/stream"
)

// Scope identifies where a keyboard shortcut is active.
type Scope int

const (
	ScopeGlobal            Scope = iota
	ScopeDashboard                       // list mode
	ScopeDashboardChannels               // channels mode
	ScopeDetail                          // iteration list
	ScopeDetailOutput                    // focused output/artifact pane
	ScopeBeadBrowse                      // bead navigation within a snapshot
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
	Key        string                 // display string: "j/k", "ctrl+c", ">"
	Action     string                 // "navigate", "quit", "wrap up"
	Scope      Scope
	Condition  string                 // human-readable, shown in ? modal; "" if always available
	ShowFunc   func(*DetailCtx) bool  // nil = always show in help bar
	ActionFunc func(*DetailCtx) string // nil = use Action; overrides Action for help bar
}

// Bindings is the declarative registry of all keyboard shortcuts.
// Order within each scope determines help bar display order.
var Bindings = []KeyBinding{
	// ── Global ──────────────────────────────────────────────────────
	{Key: "?", Action: "this help", Scope: ScopeGlobal},
	{Key: "ctrl+c", Action: "quit", Scope: ScopeGlobal},

	// ── Dashboard (list mode) ──────────────────────────────────────
	{Key: "j/k", Action: "navigate", Scope: ScopeDashboard},
	{Key: "enter", Action: "inspect", Scope: ScopeDashboard},
	{Key: "n", Action: "new", Scope: ScopeDashboard},
	{Key: "s", Action: "start", Scope: ScopeDashboard},
	{Key: "x", Action: "pause", Scope: ScopeDashboard},
	{Key: "X", Action: "kill", Scope: ScopeDashboard},
	{Key: "d", Action: "delete", Scope: ScopeDashboard},
	{Key: "D", Action: "diagnose", Scope: ScopeDashboard},
	{Key: "g", Action: "guidance", Scope: ScopeDashboard},
	{Key: "v", Action: "channels", Scope: ScopeDashboard},
	{Key: "q", Action: "quit", Scope: ScopeDashboard},

	// ── Dashboard (channels mode) ──────────────────────────────────
	{Key: "h/l", Action: "navigate", Scope: ScopeDashboardChannels},
	{Key: "enter", Action: "inspect", Scope: ScopeDashboardChannels},
	{Key: "n", Action: "new", Scope: ScopeDashboardChannels},
	{Key: "s", Action: "start", Scope: ScopeDashboardChannels},
	{Key: "x", Action: "pause", Scope: ScopeDashboardChannels},
	{Key: "X", Action: "kill", Scope: ScopeDashboardChannels},
	{Key: "d", Action: "delete", Scope: ScopeDashboardChannels},
	{Key: "D", Action: "diagnose", Scope: ScopeDashboardChannels},
	{Key: "g", Action: "guidance", Scope: ScopeDashboardChannels},
	{Key: "v", Action: "list", Scope: ScopeDashboardChannels},
	{Key: "q", Action: "quit", Scope: ScopeDashboardChannels},

	// ── Detail (iteration list) ────────────────────────────────────
	{Key: "j/k", Action: "iterations", Scope: ScopeDetail,
		ShowFunc: func(ctx *DetailCtx) bool { return ctx.Status != stream.StatusCompleted }},
	{Key: "enter", Action: "focus output", Scope: ScopeDetail,
		ShowFunc: func(ctx *DetailCtx) bool {
			return ctx.Status != stream.StatusCompleted && !ctx.AtReview
		}},
	{Key: "a", Action: "attach", Scope: ScopeDetail, Condition: "session exists",
		ShowFunc: func(ctx *DetailCtx) bool {
			return ctx.Status != stream.StatusCompleted && !ctx.AtReview
		}},
	{Key: "s", Action: "start", Scope: ScopeDetail,
		ShowFunc: func(ctx *DetailCtx) bool {
			return ctx.Status != stream.StatusRunning && ctx.Status != stream.StatusCompleted && !ctx.AtReview
		}},
	{Key: "c", Action: "complete", Scope: ScopeDetail, Condition: "paused at review",
		ShowFunc: func(ctx *DetailCtx) bool { return ctx.AtReview }},
	{Key: "w", Action: "wrap up", Scope: ScopeDetail, Condition: "stream is running",
		ShowFunc: func(ctx *DetailCtx) bool { return ctx.Status == stream.StatusRunning }},
	{Key: "x", Action: "pause", Scope: ScopeDetail, Condition: "stream is running",
		ShowFunc: func(ctx *DetailCtx) bool { return ctx.Status == stream.StatusRunning }},
	{Key: "X", Action: "kill", Scope: ScopeDetail, Condition: "stream is running",
		ShowFunc: func(ctx *DetailCtx) bool { return ctx.Status == stream.StatusRunning }},
	{Key: ">", Action: "skip phase", Scope: ScopeDetail, Condition: "paused, next phase available",
		ShowFunc: func(ctx *DetailCtx) bool { return ctx.CanAdvance }},
	{Key: "D", Action: "diagnose", Scope: ScopeDetail,
		ShowFunc: func(ctx *DetailCtx) bool { return ctx.Status != stream.StatusRunning }},
	{Key: "r", Action: "revise", Scope: ScopeDetail, Condition: "past first phase",
		ShowFunc: func(ctx *DetailCtx) bool {
			return ctx.CanRevise && ctx.Status != stream.StatusCompleted
		}},
	{Key: "g", Action: "guidance", Scope: ScopeDetail,
		ShowFunc: func(ctx *DetailCtx) bool { return ctx.Status != stream.StatusCompleted }},
	{Key: "b", Action: "config", Scope: ScopeDetail, Condition: "pipeline > 1 phase",
		ShowFunc: func(ctx *DetailCtx) bool { return ctx.Status != stream.StatusCompleted }},
	{Key: "d", Action: "delete", Scope: ScopeDetail,
		ShowFunc: func(ctx *DetailCtx) bool {
			return ctx.AtReview || ctx.Status == stream.StatusCompleted
		}},
	{Key: "q/esc", Action: "back", Scope: ScopeDetail},
	{Key: "f", Action: "toggle artifact", Scope: ScopeDetail, Condition: "snapshot has artifact",
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
	{Key: "j/k", Action: "scroll", Scope: ScopeDetailOutput},
	{Key: "G", Action: "bottom", Scope: ScopeDetailOutput},
	{Key: "f", Action: "back to summary", Scope: ScopeDetailOutput, Condition: "viewing artifact",
		ShowFunc: func(ctx *DetailCtx) bool { return ctx.ShowArtifact }},
	{Key: "esc", Action: "back to list", Scope: ScopeDetailOutput},

	// ── Bead browse ────────────────────────────────────────────────
	{Key: "j/k", Action: "select bead", Scope: ScopeBeadBrowse},
	{Key: "enter", Action: "show details", Scope: ScopeBeadBrowse},
	{Key: "esc", Action: "back to iterations", Scope: ScopeBeadBrowse},
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
// non-nil and ShowFunc returns true.
func HelpBarText(scope Scope, ctx *DetailCtx) string {
	var parts []string
	for _, b := range Bindings {
		if b.Scope != scope {
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
	return strings.Join(parts, "  ")
}
