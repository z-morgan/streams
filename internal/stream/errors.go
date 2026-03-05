package stream

import "fmt"

// ErrorKind classifies loop errors for appropriate user action.
type ErrorKind int

const (
	ErrRuntime    ErrorKind = iota // CLI exited non-zero (crash, timeout)
	ErrBudget                      // Budget limit hit
	ErrAutosquash                  // git rebase --autosquash failed (merge conflict)
	ErrNoProgress                  // Implement step closed zero beads
	ErrInfra                       // Disk, git worktree, beads CLI failure
)

var errorKindNames = [...]string{
	"Runtime",
	"Budget",
	"Autosquash",
	"NoProgress",
	"Infra",
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
	Step    IterStep
	Message string // one-line human summary
	Detail  string // stderr, conflict file list, etc.
}

func (e *LoopError) Error() string {
	return fmt.Sprintf("%s at %s: %s", e.Kind, e.Step, e.Message)
}
