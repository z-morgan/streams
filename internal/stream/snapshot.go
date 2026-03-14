package stream

import "time"

// Snapshot records the outcome of a single iteration within a macro-phase.
// Snapshots are append-only; each iteration produces exactly one.
type Snapshot struct {
	Phase            string // macro-phase that produced this snapshot (e.g. "plan", "coding")
	Iteration        int
	Summary          string // implement step's text output
	Review           string // review step's text output
	Artifact         string // contents of phase artifact file (e.g. plan.md) captured after implement
	CostUSD          float64
	DiffStat         string            // git diff --stat output (coding phase only)
	CommitSHAs       []string          // commits made this iteration (coding phase only)
	BeadsClosed      []string          // bead IDs closed by implement step
	BeadsOpened      []string          // bead IDs opened by review step
	BeadTitles       map[string]string // bead ID → title (nil for old snapshots)
	SlotName         string            // polish phase slot name (empty for non-polish phases)
	ReviseFrom       string            // phase name we revised FROM (empty = not a revision)
	ReviseFeedback   string            // user's feedback prompt when requesting the revision
	AutosquashErr    string            // non-empty if autosquash failed but loop continued
	GuidanceConsumed []Guidance
	UsedFallback     bool              // true if rate limit fallback was used this iteration
	FallbackModel    string            // model name used for fallback (empty if no fallback)
	Convergence      *SnapshotConvergence `json:",omitempty"` // convergence state at this iteration
	Error            *LoopError        // non-nil if iteration ended in error (partial snapshot)
	Timestamp        time.Time
}

// SnapshotConvergence captures convergence state for observability.
type SnapshotConvergence struct {
	Mode               string                       `json:"mode"`
	RefinementCapReached bool                        `json:"refinement_cap_reached"`
	Sections           map[string]SnapshotSection    `json:"sections,omitempty"`
	BlockingIssues     []string                      `json:"blocking_issues,omitempty"`
	AdvisoryIssues     []string                      `json:"advisory_issues,omitempty"`
}

// SnapshotSection captures per-section state at a point in time.
type SnapshotSection struct {
	RevisionCount int    `json:"revision_count"`
	Frozen        bool   `json:"frozen"`
	FrozenAtIter  int    `json:"frozen_at_iteration,omitempty"`
	LastEditType  string `json:"last_edit_type,omitempty"`
}

// Guidance holds a user-submitted steering message for the loop.
type Guidance struct {
	Text      string
	Timestamp time.Time
}

// MaxIterHint returns a diagnostic hint for a MaxIterations error based on
// snapshot analysis. Returns empty string if no hint can be determined.
func MaxIterHint(snaps []Snapshot, phase string) string {
	var phaseSnaps []Snapshot
	for _, snap := range snaps {
		if snap.Phase == phase && snap.Error == nil {
			phaseSnaps = append(phaseSnaps, snap)
		}
	}
	if len(phaseSnaps) == 0 {
		return ""
	}

	total := len(phaseSnaps)

	// Count iterations where review filed new issues.
	reviewFiledCount := 0
	for _, snap := range phaseSnaps {
		if len(snap.BeadsOpened) > 0 {
			reviewFiledCount++
		}
	}

	// Pattern 1: review filed on every iteration — review is the bottleneck.
	if reviewFiledCount == total {
		return "Review never converged \u2014 consider adjusting the review prompt\u2019s convergence bar or increasing the iteration limit"
	}

	// Pattern 3: implement step closed 0 beads on the last iteration.
	// Only check after the first iteration (iteration 0 might legitimately
	// close nothing when bootstrapping).
	lastSnap := phaseSnaps[total-1]
	if total > 1 && len(lastSnap.BeadsClosed) == 0 {
		return "Agent couldn't make progress \u2014 check if the implement prompt or task description is too ambiguous"
	}

	// Pattern 2: review filed issues only on the last 1-2 iterations.
	if reviewFiledCount > 0 && reviewFiledCount <= 2 {
		allAtEnd := true
		for i, snap := range phaseSnaps {
			if len(snap.BeadsOpened) > 0 && i < total-2 {
				allAtEnd = false
				break
			}
		}
		if allAtEnd {
			return "Nearly converged \u2014 consider increasing the iteration limit by 2-3"
		}
	}

	return ""
}
