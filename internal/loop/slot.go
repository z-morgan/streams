package loop

// SlotScope controls what context a polish slot receives.
type SlotScope string

const (
	ScopeDiff   SlotScope = "diff"   // agent sees whole-stream commit log and diff stat
	ScopeCommit SlotScope = "commit" // prompt includes per-commit SHAs, messages, and diffs
)

// Slot defines a single polish slot — one agent invocation with its own prompt and tool set.
type Slot struct {
	Name   string
	Scope  SlotScope
	Tools  []string
	Budget string // optional per-slot budget cap (e.g. "1.00"); empty = inherit stream budget
}

// SlottedPhase is a MacroPhase that runs a series of slots instead of the
// normal implement→review cycle.
type SlottedPhase interface {
	MacroPhase
	Slots() []Slot
}

// DefaultSlots returns the built-in polish slots in their default order.
func DefaultSlots() []Slot {
	return []Slot{
		{
			Name:  "lint",
			Scope: ScopeCommit,
			Tools: []string{"Bash", "Read", "Glob", "Grep", "Edit", "Write"},
		},
		{
			Name:  "rubocop",
			Scope: ScopeCommit,
			Tools: []string{"Bash", "Read", "Glob", "Grep", "Edit", "Write"},
		},
		{
			Name:  "commits",
			Scope: ScopeDiff,
			Tools: []string{"Bash", "Read", "Glob", "Grep"},
		},
	}
}
