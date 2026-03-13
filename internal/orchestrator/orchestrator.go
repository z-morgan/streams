package orchestrator

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/zmorgan/streams/internal/diagnosis"
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
	RepoDir       string   // the main repository directory
	Pipeline      []string // ordered macro-phase names; defaults to ["coding"]
	PolishSlots   []string // nil = use built-in defaults; explicit list replaces defaults
}

// Orchestrator manages the lifecycle of multiple streams.
type Orchestrator struct {
	mu      sync.RWMutex
	streams map[string]*stream.Stream
	cancels map[string]context.CancelFunc
	done    map[string]chan struct{} // closed when loop goroutine exits
	snaps   map[string]int           // persisted snapshot count per stream
	store   *store.Store
	sink    EventSink
	config  Config
}

func New(s *store.Store, config Config) *Orchestrator {
	return &Orchestrator{
		streams: make(map[string]*stream.Stream),
		cancels: make(map[string]context.CancelFunc),
		done:    make(map[string]chan struct{}),
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

// NeedsBeadsInit returns true if the target repo doesn't have beads initialized.
func (o *Orchestrator) NeedsBeadsInit() bool {
	_, err := os.Stat(filepath.Join(o.config.RepoDir, ".beads"))
	return os.IsNotExist(err)
}

// InitBeads initializes beads in the target repo.
// If stealth is true, beads files are kept out of git history.
func (o *Orchestrator) InitBeads(stealth bool) error {
	args := []string{"init", "--skip-hooks", "--quiet"}
	if stealth {
		args = append(args, "--stealth")
	}
	cmd := exec.Command("bd", args...)
	cmd.Dir = o.config.RepoDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("bd init: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// Create creates a new stream backed by a beads parent issue and git worktree.
// If pipeline is nil/empty, the global config pipeline is used.
func (o *Orchestrator) Create(title, task string, pipeline []string, breakpoints []int, notify stream.NotifySettings) (*stream.Stream, error) {
	repoDir := o.config.RepoDir

	parentID, err := createBeadsParent(title, repoDir)
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

	if len(pipeline) == 0 {
		pipeline = o.config.Pipeline
	}
	if len(pipeline) == 0 {
		pipeline = []string{"coding"}
	}

	if err := ValidatePipeline(pipeline); err != nil {
		return nil, err
	}

	st := &stream.Stream{
		ID:            streamID,
		Name:          title,
		Task:          task,
		Mode:          stream.ModeAutonomous,
		Status:        stream.StatusCreated,
		Pipeline:      pipeline,
		PipelineIndex: 0,
		Breakpoints:   breakpoints,
		Notify:        notify,
		BeadsParentID: parentID,
		BaseSHA:       baseSHA,
		Branch:        branch,
		WorkTree:      worktreePath,
		SessionID:     newUUID(),
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
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.Before(result[j].CreatedAt)
	})
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
	doneCh := make(chan struct{})
	o.done[id] = doneCh
	o.mu.Unlock()

	// Clear any previous error on resume and assign a fresh session ID so we
	// don't collide with a stale Claude CLI process from a previous run.
	st.SetLastError(nil)
	st.SetSessionID(newUUID())

	// Re-iterate: if converged with pending guidance, reset convergence
	// so the loop re-runs the current phase with the feedback injected.
	if st.Converged && st.GetGuidanceCount() > 0 {
		st.SetConverged(false)
	}

	var rt runtime.Runtime = &claude.Runtime{WorkDir: st.WorkTree}
	if o.config.MaxBudgetUSD != "" {
		rt = &runtime.BudgetRuntime{Inner: rt, MaxBudget: o.config.MaxBudgetUSD}
	}
	beads := &loop.CLIBeadsQuerier{WorkDir: st.WorkTree}
	git := &loop.CLIGitQuerier{}
	factory := o.phaseFactory()
	phaseName := st.Pipeline[st.PipelineIndex]
	phase, err := factory(phaseName)
	if err != nil {
		return fmt.Errorf("create phase %q: %w", phaseName, err)
	}

	o.emit(Event{StreamID: id, Kind: EventStarted})

	go func() {
		defer close(doneCh)

		promptDirs := o.promptOverrideDirs(st.ID)
		loop.Run(ctx, st, phase, rt, beads, git, o.config.MaxIterations, factory, func(s *stream.Stream) {
			o.checkpoint(s)
			o.emit(Event{StreamID: s.ID, Kind: EventCheckpoint})
		}, promptDirs...)

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

		// Fire user-configured notifications.
		notify := st.GetNotify()
		switch {
		case st.Converged:
			fireNotifications(st.Name, EventConverged, notify)
		case st.LastError != nil:
			fireNotifications(st.Name, EventError, notify)
		}

		o.mu.Lock()
		delete(o.cancels, id)
		delete(o.done, id)
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

// Interrupt cancels a running stream's loop and blocks until the goroutine
// exits or a 10-second timeout is reached. Returns an error if the stream is
// not running or if the timeout expires.
func (o *Orchestrator) Interrupt(id string) error {
	o.mu.Lock()
	cancel, ok := o.cancels[id]
	doneCh := o.done[id]
	o.mu.Unlock()
	if !ok {
		return fmt.Errorf("stream %q is not running", id)
	}
	cancel()
	select {
	case <-doneCh:
		return nil
	case <-time.After(10 * time.Second):
		return fmt.Errorf("timeout waiting for stream %q to stop", id)
	}
}

// Delete removes a stream's worktree and disk data. If cleanup is true, the
// git branch and beads issue are also removed. Returns an error if the stream
// is currently running.
func (o *Orchestrator) Delete(id string, cleanup bool) error {
	o.mu.Lock()
	st := o.streams[id]
	if st == nil {
		o.mu.Unlock()
		return fmt.Errorf("stream %q not found", id)
	}
	if _, running := o.cancels[id]; running {
		o.mu.Unlock()
		return fmt.Errorf("stream %q is still running — stop it first", id)
	}
	delete(o.streams, id)
	delete(o.snaps, id)
	worktree := st.WorkTree
	branch := st.Branch
	beadsID := st.BeadsParentID
	o.mu.Unlock()

	// Remove git worktree.
	cmd := exec.Command("git", "worktree", "remove", worktree, "--force")
	cmd.Dir = o.config.RepoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		slog.Warn("git worktree remove failed", "path", worktree, "err", err, "output", strings.TrimSpace(string(out)))
	}

	if cleanup {
		// Delete the git branch.
		if branch != "" {
			cmd = exec.Command("git", "branch", "-D", branch)
			cmd.Dir = o.config.RepoDir
			if out, err := cmd.CombinedOutput(); err != nil {
				slog.Warn("git branch delete failed", "branch", branch, "err", err, "output", strings.TrimSpace(string(out)))
			}
		}

		// Close and delete the beads issue.
		if beadsID != "" {
			cmd = exec.Command("bd", "close", beadsID, "--reason", "stream deleted")
			cmd.Dir = o.config.RepoDir
			if out, err := cmd.CombinedOutput(); err != nil {
				slog.Warn("bd close failed", "id", beadsID, "err", err, "output", strings.TrimSpace(string(out)))
			}

			cmd = exec.Command("bd", "delete", beadsID, "--force")
			cmd.Dir = o.config.RepoDir
			if out, err := cmd.CombinedOutput(); err != nil {
				slog.Warn("bd delete failed", "id", beadsID, "err", err, "output", strings.TrimSpace(string(out)))
			}
		}
	}

	if err := o.store.Delete(id); err != nil {
		return fmt.Errorf("delete store data: %w", err)
	}

	return nil
}

// Complete finalizes a stream by renaming its branch, removing the worktree,
// and setting status to StatusCompleted.
func (o *Orchestrator) Complete(id, branchName string) error {
	o.mu.Lock()
	st := o.streams[id]
	if st == nil {
		o.mu.Unlock()
		return fmt.Errorf("stream %q not found", id)
	}
	if _, running := o.cancels[id]; running {
		o.mu.Unlock()
		return fmt.Errorf("stream %q is still running — stop it first", id)
	}
	oldBranch := st.Branch
	worktree := st.WorkTree
	baseSHA := st.BaseSHA
	o.mu.Unlock()

	// Refuse to complete if there are uncommitted changes in the worktree.
	// --force would silently destroy them.
	if worktree != "" {
		statusCmd := exec.Command("git", "status", "--porcelain")
		statusCmd.Dir = worktree
		statusOut, err := statusCmd.Output()
		if err == nil && len(strings.TrimSpace(string(statusOut))) > 0 {
			return fmt.Errorf("worktree has uncommitted changes — commit or discard them first")
		}
	}

	// Refuse to complete if no commits were made beyond the base.
	if worktree != "" && baseSHA != "" {
		logCmd := exec.Command("git", "log", "--oneline", baseSHA+"..HEAD")
		logCmd.Dir = worktree
		logOut, err := logCmd.Output()
		if err == nil && len(strings.TrimSpace(string(logOut))) == 0 {
			return fmt.Errorf("no commits on branch — nothing to complete")
		}
	}

	// Check that the target branch name doesn't already exist.
	if oldBranch != branchName {
		checkCmd := exec.Command("git", "rev-parse", "--verify", "refs/heads/"+branchName)
		checkCmd.Dir = o.config.RepoDir
		if err := checkCmd.Run(); err == nil {
			return fmt.Errorf("branch %q already exists — choose a different name", branchName)
		}
	}

	// Rename the worktree branch to the user-chosen name.
	if oldBranch != branchName {
		cmd := exec.Command("git", "branch", "-m", oldBranch, branchName)
		cmd.Dir = o.config.RepoDir
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git branch rename: %s", strings.TrimSpace(string(out)))
		}
	}

	// Remove the worktree.
	cmd := exec.Command("git", "worktree", "remove", worktree, "--force")
	cmd.Dir = o.config.RepoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git worktree remove: %s", strings.TrimSpace(string(out)))
	}

	st.SetBranch(branchName)
	st.SetWorkTree("")
	st.SetStatus(stream.StatusCompleted)

	o.checkpoint(st)
	return nil
}

// Revise resets a paused stream to an earlier pipeline phase with optional feedback,
// then restarts it. The current code state is preserved — no commits are reverted.
func (o *Orchestrator) Revise(id string, targetPhaseIndex int, feedback string) error {
	o.mu.Lock()
	st := o.streams[id]
	if st == nil {
		o.mu.Unlock()
		return fmt.Errorf("stream %q not found", id)
	}
	if _, running := o.cancels[id]; running {
		o.mu.Unlock()
		return fmt.Errorf("stream %q is still running — stop it first", id)
	}
	o.mu.Unlock()

	currentIdx := st.GetPipelineIndex()
	if targetPhaseIndex < 0 || targetPhaseIndex >= currentIdx {
		return fmt.Errorf("target phase index %d must be less than current index %d", targetPhaseIndex, currentIdx)
	}

	st.SetPipelineIndex(targetPhaseIndex)
	st.SetConverged(false)
	st.SetIteration(0)

	if feedback != "" {
		st.AddGuidance(stream.Guidance{
			Text:      feedback,
			Timestamp: time.Now(),
		})
	}

	return o.Start(id)
}

// ForceAdvance skips the current pipeline phase and starts the next one.
// The stream must be paused or stopped and not at the last phase.
func (o *Orchestrator) ForceAdvance(id string) error {
	o.mu.Lock()
	st := o.streams[id]
	if st == nil {
		o.mu.Unlock()
		return fmt.Errorf("stream %q not found", id)
	}
	if _, running := o.cancels[id]; running {
		o.mu.Unlock()
		return fmt.Errorf("stream %q is still running — stop it first", id)
	}
	o.mu.Unlock()

	pipeline := st.GetPipeline()
	nextIdx := st.GetPipelineIndex() + 1
	if nextIdx >= len(pipeline) {
		return fmt.Errorf("stream %q is already at the last pipeline phase", id)
	}

	st.SetPipelineIndex(nextIdx)
	st.SetConverged(false)
	st.SetIteration(0)
	st.SetLastError(nil)

	return o.Start(id)
}

// Converge sets the ConvergeASAP flag on a running stream, causing the loop
// to skip the next review step and converge the current phase.
func (o *Orchestrator) Converge(id string) error {
	o.mu.RLock()
	st := o.streams[id]
	_, running := o.cancels[id]
	o.mu.RUnlock()
	if st == nil {
		return fmt.Errorf("stream %q not found", id)
	}
	if !running {
		return fmt.Errorf("stream %q is not running", id)
	}
	st.SetConvergeASAP(true)
	return nil
}

// Diagnose returns an exec.Cmd for an interactive claude CLI session pre-loaded
// with the stream's diagnosis context. The caller executes the command
// (e.g., via tea.ExecProcess). Only valid for non-running streams.
func (o *Orchestrator) Diagnose(id string) (*exec.Cmd, error) {
	o.mu.RLock()
	st := o.streams[id]
	_, running := o.cancels[id]
	o.mu.RUnlock()
	if st == nil {
		return nil, fmt.Errorf("stream %q not found", id)
	}
	if running {
		return nil, fmt.Errorf("stream %q is running — stop it first", id)
	}
	return diagnosis.SpawnCmd(st, o.store.Root, o.config.RepoDir), nil
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
	st.AddGuidance(g)
	return nil
}

// DefaultPipeline returns the global default pipeline from config.
func (o *Orchestrator) DefaultPipeline() []string {
	return o.config.Pipeline
}

// IsRunning returns whether a stream's loop goroutine is active.
func (o *Orchestrator) IsRunning(id string) bool {
	o.mu.RLock()
	_, ok := o.cancels[id]
	o.mu.RUnlock()
	return ok
}

// fireNotifications sends enabled notification types for the given stream event.
func fireNotifications(name string, kind EventKind, notify stream.NotifySettings) {
	var label string
	switch kind {
	case EventConverged:
		label = "converged"
	case EventError:
		label = "error"
	default:
		return
	}

	if notify.Bell {
		if f, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0); err == nil {
			f.Write([]byte("\a"))
			f.Close()
		}
	}

	if notify.Flash {
		if f, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0); err == nil {
			f.Write([]byte("\033[?5h"))
			time.Sleep(150 * time.Millisecond)
			f.Write([]byte("\033[?5l"))
			f.Close()
		}
	}

	if notify.System {
		title := fmt.Sprintf("Stream %s: %s", label, name)
		exec.Command("osascript", "-e",
			fmt.Sprintf(`display notification "%s" with title "Streams"`, title),
		).Run()
	}
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
		go sink.Send(e)
	}
}

// promptOverrideDirs returns the per-stream and project prompt override directories.
// Checked in order: per-stream → project (before global user dir).
func (o *Orchestrator) promptOverrideDirs(streamID string) []string {
	return []string{
		filepath.Join(o.store.Root, "streams", streamID, "prompts"),
		filepath.Join(o.store.Root, "prompts"),
	}
}

// StreamDataDir returns the on-disk data directory for a stream.
func (o *Orchestrator) StreamDataDir(streamID string) string {
	return filepath.Join(o.store.Root, "streams", streamID)
}

// StoreRoot returns the store's root directory.
func (o *Orchestrator) StoreRoot() string {
	return o.store.Root
}

// RepoDir returns the repository directory.
func (o *Orchestrator) RepoDir() string {
	return o.config.RepoDir
}

// phaseFactory returns a PhaseFactory that handles config-driven phases.
func (o *Orchestrator) phaseFactory() loop.PhaseFactory {
	polishSlots := o.config.PolishSlots
	return func(name string) (loop.MacroPhase, error) {
		if name == "polish" {
			return loop.NewPolishPhase(polishSlots), nil
		}
		return loop.NewPhase(name)
	}
}

func ValidatePipeline(phases []string) error {
	for _, name := range phases {
		if _, err := loop.NewPhase(name); err != nil {
			return fmt.Errorf("invalid pipeline phase %q: %w", name, err)
		}
	}
	return nil
}

func createBeadsParent(task, workDir string) (string, error) {
	cmd := exec.Command("bd", "create", "--title", task, "--type", "task", "--priority", "2", "--json")
	cmd.Dir = workDir
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("bd create: %s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", fmt.Errorf("bd create: %w", err)
	}
	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return "", fmt.Errorf("parse bd create output: %w", err)
	}
	if result.ID == "" {
		return "", fmt.Errorf("bd create returned empty ID")
	}
	return result.ID, nil
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

// newUUID generates a random UUID v4 string without external dependencies.
func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
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
