package stream

import "time"

// Snapshot records the outcome of a single iteration within a macro-phase.
// Snapshots are append-only; each iteration produces exactly one.
type Snapshot struct {
	Phase            string       // macro-phase that produced this snapshot (e.g. "plan", "coding")
	Iteration        int
	Summary          string       // implement step's text output
	Review           string       // review step's text output
	GateResults      []GateResult // per-gate pass/fail + detail
	CostUSD          float64
	DiffStat         string   // git diff --stat output (coding phase only)
	CommitSHAs       []string // commits made this iteration (coding phase only)
	BeadsClosed      []string // bead IDs closed by implement step
	BeadsOpened      []string // bead IDs opened by review step
	GuidanceConsumed []Guidance
	Error            *LoopError // non-nil if iteration ended in error (partial snapshot)
	Timestamp        time.Time
}

// Guidance holds a user-submitted steering message for the loop.
type Guidance struct {
	Text      string
	Timestamp time.Time
}

// GateResult records one quality gate's evaluation from a review step.
type GateResult struct {
	Gate   string
	Passed bool
	Detail string
}
