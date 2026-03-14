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

// NotifySettings controls which notifications fire on converge/error events.
type NotifySettings struct {
	Bell   bool // terminal bell (\a to /dev/tty)
	Flash  bool // reverse-video flash escape sequence
	System bool // macOS system notification via osascript
}

// ModelConfig holds per-phase model selections.
type ModelConfig struct {
	Default  string            `json:"default,omitempty"`    // model for all phases unless overridden
	PerPhase map[string]string `json:"per_phase,omitempty"` // phase name → model override
}

// ModelForPhase returns the model to use for a given phase.
// Resolution: per-phase override → default → "" (CLI default).
func (mc ModelConfig) ModelForPhase(phase string) string {
	if m, ok := mc.PerPhase[phase]; ok && m != "" && m != "default" {
		return m
	}
	if mc.Default != "" && mc.Default != "default" {
		return mc.Default
	}
	return "" // empty = don't pass --model, use CLI default
}

// PendingRevise stores a queued revise request for a running stream.
// The loop checks this between iterations and applies it if set.
type PendingRevise struct {
	TargetPhaseIndex int
	Feedback         string
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
	Breakpoints   []int    // pipeline indices where the stream pauses after convergence
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
	ConvergeASAP  bool           // one-shot flag: skip next review to force convergence
	PauseRequested bool          // set by UI; loop checks at safe points and pauses gracefully
	PendingRevise *PendingRevise // queued revise for running streams
	Models        ModelConfig     // per-phase model selections
	Notify        NotifySettings // notification preferences for converge/error events
	MCPConfigPath   string         // absolute path to .streams/mcp.json (empty = no MCP)
	EnvironmentPort int            // host port for containerized app server (0 = no environment)
	OutputLines    []string       // ring buffer of recent CLI output for tail view
	reviseFrom     string         // transient: phase name we revised FROM (consumed by next snapshot)
	reviseFeedback string         // transient: user feedback that triggered the revision
	CreatedAt      time.Time
	UpdatedAt      time.Time
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

func (s *Stream) SetBreakpoints(bp []int) {
	s.mu.Lock()
	s.Breakpoints = make([]int, len(bp))
	copy(s.Breakpoints, bp)
	s.UpdatedAt = time.Now()
	s.mu.Unlock()
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

func (s *Stream) SetConvergeASAP(v bool) {
	s.mu.Lock()
	s.ConvergeASAP = v
	s.mu.Unlock()
}

func (s *Stream) GetConvergeASAP() bool {
	s.mu.RLock()
	v := s.ConvergeASAP
	s.mu.RUnlock()
	return v
}

// DrainConvergeASAP atomically reads and clears the ConvergeASAP flag.
func (s *Stream) DrainConvergeASAP() bool {
	s.mu.Lock()
	v := s.ConvergeASAP
	s.ConvergeASAP = false
	s.mu.Unlock()
	return v
}

func (s *Stream) SetPauseRequested(v bool) {
	s.mu.Lock()
	s.PauseRequested = v
	s.mu.Unlock()
}

func (s *Stream) GetPauseRequested() bool {
	s.mu.RLock()
	v := s.PauseRequested
	s.mu.RUnlock()
	return v
}

// DrainPauseRequested atomically reads and clears the PauseRequested flag.
func (s *Stream) DrainPauseRequested() bool {
	s.mu.Lock()
	v := s.PauseRequested
	s.PauseRequested = false
	s.mu.Unlock()
	return v
}

func (s *Stream) SetPendingRevise(pr *PendingRevise) {
	s.mu.Lock()
	s.PendingRevise = pr
	s.mu.Unlock()
}

func (s *Stream) GetPendingRevise() *PendingRevise {
	s.mu.RLock()
	pr := s.PendingRevise
	s.mu.RUnlock()
	return pr
}

// SetReviseContext records the source phase and feedback for the next snapshot.
func (s *Stream) SetReviseContext(from, feedback string) {
	s.mu.Lock()
	s.reviseFrom = from
	s.reviseFeedback = feedback
	s.mu.Unlock()
}

// DrainReviseContext atomically reads and clears the revision context fields.
func (s *Stream) DrainReviseContext() (from, feedback string) {
	s.mu.Lock()
	from = s.reviseFrom
	feedback = s.reviseFeedback
	s.reviseFrom = ""
	s.reviseFeedback = ""
	s.mu.Unlock()
	return
}

// DrainPendingRevise atomically reads and clears the PendingRevise field.
func (s *Stream) DrainPendingRevise() *PendingRevise {
	s.mu.Lock()
	pr := s.PendingRevise
	s.PendingRevise = nil
	s.mu.Unlock()
	return pr
}

func (s *Stream) SetMCPConfigPath(path string) {
	s.mu.Lock()
	s.MCPConfigPath = path
	s.UpdatedAt = time.Now()
	s.mu.Unlock()
}

func (s *Stream) GetMCPConfigPath() string {
	s.mu.RLock()
	path := s.MCPConfigPath
	s.mu.RUnlock()
	return path
}

func (s *Stream) SetEnvironmentPort(port int) {
	s.mu.Lock()
	s.EnvironmentPort = port
	s.UpdatedAt = time.Now()
	s.mu.Unlock()
}

func (s *Stream) GetEnvironmentPort() int {
	s.mu.RLock()
	port := s.EnvironmentPort
	s.mu.RUnlock()
	return port
}

func (s *Stream) SetNotify(n NotifySettings) {
	s.mu.Lock()
	s.Notify = n
	s.UpdatedAt = time.Now()
	s.mu.Unlock()
}

func (s *Stream) GetNotify() NotifySettings {
	s.mu.RLock()
	n := s.Notify
	s.mu.RUnlock()
	return n
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

func (s *Stream) SetModels(mc ModelConfig) {
	s.mu.Lock()
	s.Models = mc
	s.UpdatedAt = time.Now()
	s.mu.Unlock()
}

func (s *Stream) GetModels() ModelConfig {
	s.mu.RLock()
	mc := s.Models
	s.mu.RUnlock()
	return mc
}
