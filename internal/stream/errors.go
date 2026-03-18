package stream

import "fmt"

// ErrorKind classifies loop errors for appropriate user action.
type ErrorKind int

const (
	ErrRuntime       ErrorKind = iota // CLI exited non-zero (crash, timeout)
	ErrBudget                         // Budget limit hit
	ErrAutosquash                     // git rebase --autosquash failed (merge conflict)
	ErrNoProgress                     // Implement step closed zero beads
	ErrInfra                          // Disk, git worktree, beads CLI failure
	ErrMaxIterations                  // Iteration limit reached
	ErrRateLimit                      // API rate limit or usage cap hit
	ErrUnavailable                    // Model temporarily unavailable (5xx server error)
)

var errorKindNames = [...]string{
	"Runtime",
	"Budget",
	"Autosquash",
	"NoProgress",
	"Infra",
	"MaxIterations",
	"RateLimit",
	"Unavailable",
}

func (k ErrorKind) String() string {
	if int(k) < len(errorKindNames) {
		return errorKindNames[k]
	}
	return fmt.Sprintf("ErrorKind(%d)", int(k))
}

// LoopError captures a structured error from the iteration loop.
// Lives in the stream package because Stream.LastError references it directly.
type LoopError struct {
	Kind    ErrorKind
	Phase   string // pipeline phase name (e.g. "coding", "plan")
	Step    IterStep
	Message string // one-line human summary
	Detail  string // stderr, conflict file list, etc.
}

func (e *LoopError) Error() string {
	if e.Phase != "" {
		return fmt.Sprintf("%s at %s/%s: %s", e.Kind, e.Phase, e.Step, e.Message)
	}
	return fmt.Sprintf("%s at %s: %s", e.Kind, e.Step, e.Message)
}
