package stream

import (
	"fmt"
	"sync"
	"time"
)

// IterStep tracks where we are within a single iteration.
type IterStep int

const (
	StepImplement  IterStep = iota
	StepAutosquash          // coding phase only
	StepReview
	StepCheckpoint
	StepGuidance
)

var iterStepNames = [...]string{
	"Implement",
	"Autosquash",
	"Review",
	"Checkpoint",
	"Guidance",
}

func (s IterStep) String() string {
	if int(s) < len(iterStepNames) {
		return iterStepNames[s]
	}
	return fmt.Sprintf("IterStep(%d)", int(s))
}

// Mode controls how the loop handles macro-phase transitions.
type Mode int

const (
	ModeAutonomous Mode = iota
	ModePairing
)

var modeNames = [...]string{
	"Autonomous",
	"Pairing",
}

func (m Mode) String() string {
	if int(m) < len(modeNames) {
		return modeNames[m]
	}
	return fmt.Sprintf("Mode(%d)", int(m))
}

// Status represents the lifecycle state of a stream.
type Status int

const (
	StatusCreated Status = iota
	StatusRunning
	StatusPaused
	StatusStopped
)

var statusNames = [...]string{
	"Created",
	"Running",
	"Paused",
	"Stopped",
}

func (s Status) String() string {
	if int(s) < len(statusNames) {
		return statusNames[s]
	}
	return fmt.Sprintf("Status(%d)", int(s))
}

// Stream is the central state model for a running autonomous code generation loop.
// Thread-safe via sync.RWMutex — the TUI reads state while the loop goroutine writes.
type Stream struct {
	mu            sync.RWMutex
	ID            string
	Name          string
	Task          string
	Mode          Mode
	Status        Status
	Pipeline      []string // ordered macro-phase names, e.g. ["plan","decompose","coding"]
	PipelineIndex int      // which macro-phase is active
	IterStep      IterStep
	Iteration     int // iteration count within current macro-phase
	Converged     bool
	BeadsParentID string
	BaseSHA       string // commit the stream branched from; rebase target
	Branch        string // e.g. "streams/<stream-id>"
	WorkTree      string // absolute path to git worktree
	LastError     *LoopError
	Snapshots     []Snapshot
	Guidance      []Guidance
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (s *Stream) SetStatus(status Status) {
	s.mu.Lock()
	s.Status = status
	s.UpdatedAt = time.Now()
	s.mu.Unlock()
}

func (s *Stream) GetStatus() Status {
	s.mu.RLock()
	status := s.Status
	s.mu.RUnlock()
	return status
}

func (s *Stream) SetIterStep(step IterStep) {
	s.mu.Lock()
	s.IterStep = step
	s.UpdatedAt = time.Now()
	s.mu.Unlock()
}

func (s *Stream) SetIteration(n int) {
	s.mu.Lock()
	s.Iteration = n
	s.UpdatedAt = time.Now()
	s.mu.Unlock()
}

func (s *Stream) GetIteration() int {
	s.mu.RLock()
	n := s.Iteration
	s.mu.RUnlock()
	return n
}

func (s *Stream) SetConverged(v bool) {
	s.mu.Lock()
	s.Converged = v
	s.UpdatedAt = time.Now()
	s.mu.Unlock()
}

func (s *Stream) SetLastError(err *LoopError) {
	s.mu.Lock()
	s.LastError = err
	s.UpdatedAt = time.Now()
	s.mu.Unlock()
}

func (s *Stream) AppendSnapshot(snap Snapshot) {
	s.mu.Lock()
	s.Snapshots = append(s.Snapshots, snap)
	s.UpdatedAt = time.Now()
	s.mu.Unlock()
}

// DrainGuidance atomically moves all queued guidance items out of the stream
// and returns them. The stream's guidance queue is emptied.
func (s *Stream) DrainGuidance() []Guidance {
	s.mu.Lock()
	g := s.Guidance
	s.Guidance = nil
	s.mu.Unlock()
	return g
}
