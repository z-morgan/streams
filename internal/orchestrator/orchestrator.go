package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/zmorgan/streams/internal/loop"
	"github.com/zmorgan/streams/internal/runtime"
	"github.com/zmorgan/streams/internal/runtime/claude"
	"github.com/zmorgan/streams/internal/store"
	"github.com/zmorgan/streams/internal/stream"
)

// EventSink receives loop lifecycle events. The TUI implements this to update
// its model via tea.Program.Send().
type EventSink interface {
	Send(event Event)
}

// Event types sent to the TUI.
type Event struct {
	StreamID string
	Kind     EventKind
	Error    *stream.LoopError
}

type EventKind int

const (
	EventStarted    EventKind = iota // loop goroutine started
	EventCheckpoint                  // iteration completed (snapshot appended)
	EventConverged                   // phase converged
	EventError                       // loop paused due to error
	EventStopped                     // loop cancelled
)

// Config holds orchestrator-level settings.
type Config struct {
	MaxIterations int
	MaxBudgetUSD  string
	RepoDir       string // the main repository directory
}

// Orchestrator manages the lifecycle of multiple streams.
type Orchestrator struct {
	mu      sync.RWMutex
	streams map[string]*stream.Stream
	cancels map[string]context.CancelFunc
	snaps   map[string]int // persisted snapshot count per stream
	store   *store.Store
	sink    EventSink
	config  Config
}

func New(s *store.Store, config Config) *Orchestrator {
	return &Orchestrator{
		streams: make(map[string]*stream.Stream),
		cancels: make(map[string]context.CancelFunc),
		snaps:   make(map[string]int),
		store:   s,
		config:  config,
	}
}

// SetSink sets the event sink (typically the TUI). Can be nil for headless use.
func (o *Orchestrator) SetSink(sink EventSink) {
	o.mu.Lock()
	o.sink = sink
	o.mu.Unlock()
}

// LoadExisting loads all previously persisted streams into memory.
func (o *Orchestrator) LoadExisting() error {
	loaded, err := o.store.LoadAll()
	if err != nil {
		return fmt.Errorf("load streams: %w", err)
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, st := range loaded {
		o.streams[st.ID] = st
		o.snaps[st.ID] = len(st.Snapshots)
	}
	slog.Info("loaded existing streams", "count", len(loaded))
	return nil
}

// Create creates a new stream backed by a beads parent issue and git worktree.
func (o *Orchestrator) Create(task string) (*stream.Stream, error) {
	repoDir := o.config.RepoDir

	parentID, err := createBeadsParent(task, repoDir)
	if err != nil {
		return nil, fmt.Errorf("create beads parent: %w", err)
	}

	baseSHA, err := gitHead(repoDir)
	if err != nil {
		return nil, fmt.Errorf("git HEAD: %w", err)
	}

	streamID := parentID
	branch := "streams/" + streamID
	worktreePath := repoDir + "/.streams/worktrees/" + streamID

	if err := createWorktree(repoDir, worktreePath, branch); err != nil {
		return nil, fmt.Errorf("create worktree: %w", err)
	}

	st := &stream.Stream{
		ID:            streamID,
		Name:          task,
		Task:          task,
		Mode:          stream.ModeAutonomous,
		Status:        stream.StatusCreated,
		Pipeline:      []string{"coding"},
		PipelineIndex: 0,
		BeadsParentID: parentID,
		BaseSHA:       baseSHA,
		Branch:        branch,
		WorkTree:      worktreePath,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	o.mu.Lock()
	o.streams[st.ID] = st
	o.snaps[st.ID] = 0
	o.mu.Unlock()

	if _, err := o.store.Save(st, 0); err != nil {
		slog.Error("failed to persist new stream", "id", st.ID, "err", err)
	}

	return st, nil
}

// List returns all known streams.
func (o *Orchestrator) List() []*stream.Stream {
	o.mu.RLock()
	defer o.mu.RUnlock()
	result := make([]*stream.Stream, 0, len(o.streams))
	for _, st := range o.streams {
		result = append(result, st)
	}
	return result
}

// Get returns a stream by ID, or nil if not found.
func (o *Orchestrator) Get(id string) *stream.Stream {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.streams[id]
}

// Start launches the loop goroutine for a stream. No-op if already running.
func (o *Orchestrator) Start(id string) error {
	o.mu.Lock()
	st := o.streams[id]
	if st == nil {
		o.mu.Unlock()
		return fmt.Errorf("stream %q not found", id)
	}
	if _, running := o.cancels[id]; running {
		o.mu.Unlock()
		return nil // already running
	}

	ctx, cancel := context.WithCancel(context.Background())
	o.cancels[id] = cancel
	o.mu.Unlock()

	// Clear any previous error on resume.
	st.SetLastError(nil)

	rt := &budgetRuntime{
		inner:     &claude.Runtime{WorkDir: st.WorkTree},
		maxBudget: o.config.MaxBudgetUSD,
	}
	beads := &loop.CLIBeadsQuerier{WorkDir: st.WorkTree}
	phaseName := st.Pipeline[st.PipelineIndex]
	phase, err := loop.NewPhase(phaseName)
	if err != nil {
		return fmt.Errorf("create phase %q: %w", phaseName, err)
	}

	o.emit(Event{StreamID: id, Kind: EventStarted})

	go func() {
		loop.Run(ctx, st, phase, rt, beads, o.config.MaxIterations)

		// Persist final state.
		o.checkpoint(st)

		// Emit completion event.
		switch {
		case st.Converged:
			o.emit(Event{StreamID: id, Kind: EventConverged})
		case st.LastError != nil:
			o.emit(Event{StreamID: id, Kind: EventError, Error: st.LastError})
		default:
			o.emit(Event{StreamID: id, Kind: EventStopped})
		}

		o.mu.Lock()
		delete(o.cancels, id)
		o.mu.Unlock()
	}()

	return nil
}

// Stop cancels a running stream's loop goroutine.
func (o *Orchestrator) Stop(id string) {
	o.mu.Lock()
	cancel, ok := o.cancels[id]
	o.mu.Unlock()
	if ok {
		cancel()
	}
}

// SendGuidance queues a guidance message for a stream.
func (o *Orchestrator) SendGuidance(id string, text string) error {
	st := o.Get(id)
	if st == nil {
		return fmt.Errorf("stream %q not found", id)
	}
	g := stream.Guidance{
		Text:      text,
		Timestamp: time.Now(),
	}
	st.Guidance = append(st.Guidance, g)
	return nil
}

// IsRunning returns whether a stream's loop goroutine is active.
func (o *Orchestrator) IsRunning(id string) bool {
	o.mu.RLock()
	_, ok := o.cancels[id]
	o.mu.RUnlock()
	return ok
}

// checkpoint persists the stream and its new snapshots.
func (o *Orchestrator) checkpoint(st *stream.Stream) {
	o.mu.Lock()
	lastSnaps := o.snaps[st.ID]
	o.mu.Unlock()

	newSnaps, err := o.store.Save(st, lastSnaps)
	if err != nil {
		slog.Error("checkpoint save failed", "stream", st.ID, "err", err)
		return
	}

	o.mu.Lock()
	o.snaps[st.ID] = newSnaps
	o.mu.Unlock()
}

func (o *Orchestrator) emit(e Event) {
	o.mu.RLock()
	sink := o.sink
	o.mu.RUnlock()
	if sink != nil {
		sink.Send(e)
	}
}

// budgetRuntime wraps a runtime to inject max-budget-usd into every request.
type budgetRuntime struct {
	inner     *claude.Runtime
	maxBudget string
}

func (b *budgetRuntime) Run(ctx context.Context, req runtime.Request) (*runtime.Response, error) {
	if req.Options == nil {
		req.Options = make(map[string]string)
	}
	req.Options["maxBudgetUsd"] = b.maxBudget
	return b.inner.Run(ctx, req)
}

func createBeadsParent(task, workDir string) (string, error) {
	cmd := exec.Command("bd", "create", "--title", task, "--type", "task", "--priority", "2")
	cmd.Dir = workDir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("bd create: %w", err)
	}
	return parseBeadsID(strings.TrimSpace(string(out))), nil
}

func parseBeadsID(output string) string {
	fields := strings.Fields(output)
	for _, f := range fields {
		f = strings.TrimSuffix(f, ":")
		if strings.Contains(f, "-") && !strings.HasPrefix(f, "✓") && !strings.EqualFold(f, "Created") {
			return f
		}
	}
	return strings.TrimSpace(output)
}

func gitHead(workDir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = workDir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func createWorktree(repoDir, worktreePath, branch string) error {
	cmd := exec.Command("git", "worktree", "add", worktreePath, "-b", branch)
	cmd.Dir = repoDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree add: %s: %w", out, err)
	}
	return nil
}
