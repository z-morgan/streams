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
	StatusCompleted
)

var statusNames = [...]string{
	"Created",
	"Running",
	"Paused",
	"Stopped",
	"Completed",
}

func (s Status) String() string {
	if int(s) < len(statusNames) {
		return statusNames[s]
	}
	return fmt.Sprintf("Status(%d)", int(s))
}

const maxOutputLines = 200

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
	Breakpoints   []int   // pipeline indices where the stream pauses after convergence
	IterStep      IterStep
	Iteration     int // iteration count within current macro-phase
	Converged     bool
	BeadsParentID string
	BaseSHA       string // commit the stream branched from; rebase target
	Branch        string // e.g. "streams/<stream-id>"
	WorkTree      string // absolute path to git worktree
	SessionID     string // most recent Claude session ID; used for --resume attach
	LastError     *LoopError
	Snapshots     []Snapshot
	Guidance      []Guidance
	OutputLines   []string // ring buffer of recent CLI output for tail view
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

func (s *Stream) GetIterStep() IterStep {
	s.mu.RLock()
	step := s.IterStep
	s.mu.RUnlock()
	return step
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

func (s *Stream) SetSessionID(id string) {
	s.mu.Lock()
	s.SessionID = id
	s.mu.Unlock()
}

func (s *Stream) GetSessionID() string {
	s.mu.RLock()
	id := s.SessionID
	s.mu.RUnlock()
	return id
}

func (s *Stream) SetBranch(branch string) {
	s.mu.Lock()
	s.Branch = branch
	s.UpdatedAt = time.Now()
	s.mu.Unlock()
}

func (s *Stream) SetWorkTree(path string) {
	s.mu.Lock()
	s.WorkTree = path
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

func (s *Stream) SetPipelineIndex(n int) {
	s.mu.Lock()
	s.PipelineIndex = n
	s.UpdatedAt = time.Now()
	s.mu.Unlock()
}

func (s *Stream) GetPipelineIndex() int {
	s.mu.RLock()
	n := s.PipelineIndex
	s.mu.RUnlock()
	return n
}

func (s *Stream) GetPipeline() []string {
	s.mu.RLock()
	p := s.Pipeline
	s.mu.RUnlock()
	return p
}

func (s *Stream) GetBreakpoints() []int {
	s.mu.RLock()
	bp := make([]int, len(s.Breakpoints))
	copy(bp, s.Breakpoints)
	s.mu.RUnlock()
	return bp
}

func (s *Stream) GetSnapshots() []Snapshot {
	s.mu.RLock()
	snaps := make([]Snapshot, len(s.Snapshots))
	copy(snaps, s.Snapshots)
	s.mu.RUnlock()
	return snaps
}

func (s *Stream) GetLastError() *LoopError {
	s.mu.RLock()
	e := s.LastError
	s.mu.RUnlock()
	return e
}

func (s *Stream) GetGuidanceCount() int {
	s.mu.RLock()
	n := len(s.Guidance)
	s.mu.RUnlock()
	return n
}

// AddGuidance appends a guidance item to the stream under the mutex.
func (s *Stream) AddGuidance(g Guidance) {
	s.mu.Lock()
	s.Guidance = append(s.Guidance, g)
	s.UpdatedAt = time.Now()
	s.mu.Unlock()
}

func (s *Stream) GetGuidance() []Guidance {
	s.mu.RLock()
	g := make([]Guidance, len(s.Guidance))
	copy(g, s.Guidance)
	s.mu.RUnlock()
	return g
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

// AppendOutput adds a line to the output ring buffer, dropping the oldest
// line if the buffer is full.
func (s *Stream) AppendOutput(line string) {
	s.mu.Lock()
	s.OutputLines = append(s.OutputLines, line)
	if len(s.OutputLines) > maxOutputLines {
		s.OutputLines = s.OutputLines[len(s.OutputLines)-maxOutputLines:]
	}
	s.mu.Unlock()
}

// ClearOutput resets the output ring buffer.
func (s *Stream) ClearOutput() {
	s.mu.Lock()
	s.OutputLines = nil
	s.mu.Unlock()
}

// GetOutputLines returns a copy of the output ring buffer.
func (s *Stream) GetOutputLines() []string {
	s.mu.RLock()
	lines := make([]string, len(s.OutputLines))
	copy(lines, s.OutputLines)
	s.mu.RUnlock()
	return lines
}
