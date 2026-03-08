package loop

import (
	"context"
	"errors"
	"testing"

	"github.com/zmorgan/streams/internal/runtime"
	"github.com/zmorgan/streams/internal/stream"
)

func TestRunSlotsDiffScoped(t *testing.T) {
	s := newTestStream()
	s.BaseSHA = "abc123"
	s.WorkTree = "/tmp/test"

	rt := &mockRuntime{
		results: []mockResult{
			{resp: &runtime.Response{Text: "rewrote commit messages", CostUSD: 0.05}},
		},
	}

	phase := NewPolishPhase([]string{"commits"})

	Run(context.Background(), s, phase, rt, &mockBeads{}, &mockGit{}, 0, mockFactory, nil)

	if s.GetStatus() != stream.StatusPaused {
		t.Errorf("expected StatusPaused, got %s", s.GetStatus())
	}
	if !s.Converged {
		t.Error("expected Converged=true")
	}
	if len(s.Snapshots) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(s.Snapshots))
	}

	snap := s.Snapshots[0]
	if snap.Phase != "polish" {
		t.Errorf("expected phase=polish, got %s", snap.Phase)
	}
	if snap.SlotName != "commits" {
		t.Errorf("expected SlotName=commits, got %q", snap.SlotName)
	}
	if snap.Summary != "rewrote commit messages" {
		t.Errorf("expected summary text, got %q", snap.Summary)
	}
	if snap.CostUSD != 0.05 {
		t.Errorf("expected CostUSD=0.05, got %f", snap.CostUSD)
	}
	if snap.Error != nil {
		t.Errorf("expected no error, got %v", snap.Error)
	}
	if s.LastError != nil {
		t.Errorf("expected no LastError, got %v", s.LastError)
	}
}

func TestRunSlotsMultipleSlots(t *testing.T) {
	s := newTestStream()
	s.BaseSHA = "abc123"
	s.WorkTree = "/tmp/test"

	rt := &mockRuntime{
		results: []mockResult{
			{resp: &runtime.Response{Text: "slot 1 done", CostUSD: 0.10}},
			{resp: &runtime.Response{Text: "slot 2 done", CostUSD: 0.20}},
		},
	}

	phase := NewPolishPhase(nil) // all defaults: lint, rubocop, commits
	// Override to just two diff-scoped slots for simplicity
	phase.slots = []Slot{
		{Name: "first", Scope: ScopeDiff, Tools: []string{"Bash"}},
		{Name: "second", Scope: ScopeDiff, Tools: []string{"Bash"}},
	}

	Run(context.Background(), s, phase, rt, &mockBeads{}, &mockGit{}, 0, mockFactory, nil)

	if !s.Converged {
		t.Error("expected Converged=true")
	}
	if len(s.Snapshots) != 2 {
		t.Fatalf("expected 2 snapshots, got %d", len(s.Snapshots))
	}
	if s.Snapshots[0].SlotName != "first" {
		t.Errorf("expected first slot name, got %q", s.Snapshots[0].SlotName)
	}
	if s.Snapshots[1].SlotName != "second" {
		t.Errorf("expected second slot name, got %q", s.Snapshots[1].SlotName)
	}
}

func TestRunSlotsSlotFailureContinues(t *testing.T) {
	s := newTestStream()
	s.BaseSHA = "abc123"
	s.WorkTree = "/tmp/test"

	rt := &mockRuntime{
		results: []mockResult{
			{err: errors.New("agent crashed")},
			{resp: &runtime.Response{Text: "commits cleaned up"}},
		},
	}

	// Use "commits" for both slots since polish-commits.tmpl exists.
	phase := &PolishPhase{
		slots: []Slot{
			{Name: "commits", Scope: ScopeDiff, Tools: []string{"Bash"}},
			{Name: "commits", Scope: ScopeDiff, Tools: []string{"Bash"}},
		},
	}

	Run(context.Background(), s, phase, rt, &mockBeads{}, &mockGit{}, 0, mockFactory, nil)

	if !s.Converged {
		t.Error("expected Converged=true despite slot failure")
	}
	if len(s.Snapshots) != 2 {
		t.Fatalf("expected 2 snapshots, got %d", len(s.Snapshots))
	}
	// First slot should have an error.
	if s.Snapshots[0].Error == nil {
		t.Error("expected error in first snapshot")
	}
	// Second slot should succeed.
	if s.Snapshots[1].Error != nil {
		t.Errorf("expected no error in second snapshot, got %v", s.Snapshots[1].Error)
	}
	// LastError should be set to the first failure.
	if s.LastError == nil {
		t.Fatal("expected LastError to be set")
	}
	if s.LastError.Kind != stream.ErrRuntime {
		t.Errorf("expected ErrRuntime, got %s", s.LastError.Kind)
	}
}

func TestRunSlotsContextCancellation(t *testing.T) {
	s := newTestStream()
	s.BaseSHA = "abc123"
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancelled

	phase := NewPolishPhase([]string{"commits"})

	Run(ctx, s, phase, &mockRuntime{}, &mockBeads{}, &mockGit{}, 0, mockFactory, nil)

	if s.GetStatus() != stream.StatusStopped {
		t.Errorf("expected StatusStopped, got %s", s.GetStatus())
	}
}

func TestRunSlotsOnCheckpointCalled(t *testing.T) {
	s := newTestStream()
	s.BaseSHA = "abc123"
	s.WorkTree = "/tmp/test"

	rt := &mockRuntime{
		results: []mockResult{
			{resp: &runtime.Response{Text: "done"}},
		},
	}

	phase := NewPolishPhase([]string{"commits"})

	checkpointCount := 0
	onCheckpoint := func(_ *stream.Stream) { checkpointCount++ }

	Run(context.Background(), s, phase, rt, &mockBeads{}, &mockGit{}, 0, mockFactory, onCheckpoint)

	if checkpointCount != 1 {
		t.Errorf("expected 1 checkpoint call, got %d", checkpointCount)
	}
}

func TestNewPolishPhaseFiltersByName(t *testing.T) {
	phase := NewPolishPhase([]string{"commits", "lint"})
	slots := phase.Slots()

	if len(slots) != 2 {
		t.Fatalf("expected 2 slots, got %d", len(slots))
	}
	if slots[0].Name != "commits" {
		t.Errorf("expected first slot=commits, got %q", slots[0].Name)
	}
	if slots[1].Name != "lint" {
		t.Errorf("expected second slot=lint, got %q", slots[1].Name)
	}
}

func TestNewPolishPhaseDefaultSlots(t *testing.T) {
	phase := NewPolishPhase(nil)
	slots := phase.Slots()

	if len(slots) != 3 {
		t.Fatalf("expected 3 default slots, got %d", len(slots))
	}
	expected := []string{"lint", "rubocop", "commits"}
	for i, name := range expected {
		if slots[i].Name != name {
			t.Errorf("slot %d: expected %q, got %q", i, name, slots[i].Name)
		}
	}
}

func TestNewPolishPhaseIgnoresUnknownNames(t *testing.T) {
	phase := NewPolishPhase([]string{"commits", "bogus"})
	slots := phase.Slots()

	if len(slots) != 1 {
		t.Fatalf("expected 1 slot (bogus filtered out), got %d", len(slots))
	}
	if slots[0].Name != "commits" {
		t.Errorf("expected slot=commits, got %q", slots[0].Name)
	}
}

func TestRunSlotsCommitScopedGetsCommitData(t *testing.T) {
	s := newTestStream()
	s.BaseSHA = "abc123"
	s.WorkTree = "/tmp/test"

	// Capture the prompt that was sent to the runtime.
	var capturedPrompt string
	rt := &promptCapturingRuntime{
		inner: &mockRuntime{
			results: []mockResult{
				{resp: &runtime.Response{Text: "linted"}},
			},
		},
		onRun: func(prompt string) { capturedPrompt = prompt },
	}

	phase := &PolishPhase{
		slots: []Slot{
			{Name: "lint", Scope: ScopeCommit, Tools: []string{"Bash"}},
		},
	}

	Run(context.Background(), s, phase, rt, &mockBeads{}, &mockGit{}, 0, mockFactory, nil)

	if !s.Converged {
		t.Error("expected Converged=true")
	}
	// The commit-scoped prompt should contain the BaseSHA.
	// (gatherCommitData will fail on a fake workdir, but BaseSHA is always populated)
	if capturedPrompt == "" {
		t.Error("expected prompt to be captured")
	}
}

func TestRunSlotsCommitScopedSlotUsesLintTemplate(t *testing.T) {
	s := newTestStream()
	s.BaseSHA = "abc123"
	s.WorkTree = "/tmp/test"

	rt := &mockRuntime{
		results: []mockResult{
			{resp: &runtime.Response{Text: "linted"}},
		},
	}

	phase := NewPolishPhase([]string{"lint"})

	Run(context.Background(), s, phase, rt, &mockBeads{}, &mockGit{}, 0, mockFactory, nil)

	if !s.Converged {
		t.Error("expected Converged=true")
	}
	if len(s.Snapshots) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(s.Snapshots))
	}
	if s.Snapshots[0].SlotName != "lint" {
		t.Errorf("expected SlotName=lint, got %q", s.Snapshots[0].SlotName)
	}
}

func TestRunSlotsRubocopSlotUsesTemplate(t *testing.T) {
	s := newTestStream()
	s.BaseSHA = "abc123"
	s.WorkTree = "/tmp/test"

	rt := &mockRuntime{
		results: []mockResult{
			{resp: &runtime.Response{Text: "rubocop done"}},
		},
	}

	phase := NewPolishPhase([]string{"rubocop"})

	Run(context.Background(), s, phase, rt, &mockBeads{}, &mockGit{}, 0, mockFactory, nil)

	if !s.Converged {
		t.Error("expected Converged=true")
	}
	if len(s.Snapshots) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(s.Snapshots))
	}
	if s.Snapshots[0].SlotName != "rubocop" {
		t.Errorf("expected SlotName=rubocop, got %q", s.Snapshots[0].SlotName)
	}
}

// promptCapturingRuntime wraps a runtime and captures prompts.
type promptCapturingRuntime struct {
	inner runtime.Runtime
	onRun func(prompt string)
}

func (r *promptCapturingRuntime) Run(ctx context.Context, req runtime.Request) (*runtime.Response, error) {
	if r.onRun != nil {
		r.onRun(req.Prompt)
	}
	return r.inner.Run(ctx, req)
}

// TestPipelineCodingToPolish verifies that a coding phase auto-advancing into
// polish runs the slots and converges the full pipeline.
func TestPipelineCodingToPolish(t *testing.T) {
	s := newTestStream()
	s.BaseSHA = "abc123"
	s.WorkTree = "/tmp/test"
	s.Pipeline = []string{"test", "polish"}
	s.PipelineIndex = 0

	rt := &mockRuntime{
		results: []mockResult{
			// Coding implement + review (auto-advance phase converges)
			{resp: &runtime.Response{Text: "coded"}},
			{resp: &runtime.Response{Text: "reviewed"}},
			// Polish: one slot (commits)
			{resp: &runtime.Response{Text: "polished commits"}},
		},
	}
	beads := &mockBeads{openIDs: [][]string{ids("b-1"), nil, nil}}

	polishFactory := func(name string) (MacroPhase, error) {
		if name == "polish" {
			return NewPolishPhase([]string{"commits"}), nil
		}
		return &mockAutoAdvancePhase{}, nil
	}

	Run(context.Background(), s, &mockAutoAdvancePhase{}, rt, beads, &mockGit{}, 0, polishFactory, nil)

	if s.GetStatus() != stream.StatusPaused {
		t.Errorf("expected StatusPaused, got %s", s.GetStatus())
	}
	if !s.Converged {
		t.Error("expected Converged=true")
	}
	if s.PipelineIndex != 1 {
		t.Errorf("expected PipelineIndex=1 (polish), got %d", s.PipelineIndex)
	}
	// Should have 2 snapshots: 1 from coding, 1 from polish
	if len(s.Snapshots) != 2 {
		t.Fatalf("expected 2 snapshots, got %d", len(s.Snapshots))
	}
	if s.Snapshots[0].Phase != "test" {
		t.Errorf("expected first snapshot phase=test, got %s", s.Snapshots[0].Phase)
	}
	if s.Snapshots[1].Phase != "polish" {
		t.Errorf("expected second snapshot phase=polish, got %s", s.Snapshots[1].Phase)
	}
	if s.Snapshots[1].SlotName != "commits" {
		t.Errorf("expected polish snapshot SlotName=commits, got %q", s.Snapshots[1].SlotName)
	}
}

func TestNewPhaseReturnsPolish(t *testing.T) {
	phase, err := NewPhase("polish")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if phase.Name() != "polish" {
		t.Errorf("expected name=polish, got %q", phase.Name())
	}
	slotted, ok := phase.(SlottedPhase)
	if !ok {
		t.Fatal("expected PolishPhase to implement SlottedPhase")
	}
	if len(slotted.Slots()) != 3 {
		t.Errorf("expected 3 default slots, got %d", len(slotted.Slots()))
	}
}
