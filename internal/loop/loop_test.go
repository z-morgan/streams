package loop

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/zmorgan/streams/internal/runtime"
	"github.com/zmorgan/streams/internal/stream"
)

// mockRuntime returns configurable responses/errors per call.
type mockRuntime struct {
	calls   int
	results []mockResult
}

type mockResult struct {
	resp *runtime.Response
	err  error
}

func (m *mockRuntime) Run(_ context.Context, _ runtime.Request) (*runtime.Response, error) {
	i := m.calls
	m.calls++
	if i < len(m.results) {
		return m.results[i].resp, m.results[i].err
	}
	return &runtime.Response{Text: "ok"}, nil
}

// mockBeads returns configurable open ID lists per call and a fixed step list.
type mockBeads struct {
	openIDs   [][]string
	listCalls int
	steps     []Step
}

func (m *mockBeads) ListOpenChildren(_ string) ([]string, error) {
	i := m.listCalls
	m.listCalls++
	if i < len(m.openIDs) {
		return m.openIDs[i], nil
	}
	return nil, nil
}

func (m *mockBeads) FetchOrderedSteps(_ string) ([]Step, error) {
	return m.steps, nil
}

// mockGit returns a fixed HEAD SHA that increments per call.
type mockGit struct {
	headCalls int
}

func (m *mockGit) HeadSHA(_ string) (string, error) {
	m.headCalls++
	return fmt.Sprintf("sha%d", m.headCalls), nil
}

func (m *mockGit) DiffStat(_, _ string) (string, error) {
	return " 2 files changed, 10 insertions(+)", nil
}

func (m *mockGit) CommitsBetween(_, _, toSHA string) ([]string, error) {
	return []string{toSHA}, nil
}

// mockPhase is a minimal MacroPhase for testing.
type mockPhase struct{}

func (p *mockPhase) Name() string                                    { return "test" }
func (p *mockPhase) ImplementPrompt(_ PhaseContext) (string, error) { return "implement", nil }
func (p *mockPhase) ReviewPrompt(_ PhaseContext) (string, error)    { return "review", nil }
func (p *mockPhase) ImplementTools() []string              { return []string{"Bash"} }
func (p *mockPhase) ReviewTools() []string                 { return []string{"Bash"} }
func (p *mockPhase) IsConverged(r IterationResult) bool {
	return r.OpenAfterReview <= r.OpenBeforeReview
}
func (p *mockPhase) BeforeReview(_ PhaseContext) error { return nil }
func (p *mockPhase) TransitionMode() Transition        { return TransitionPause }
func (p *mockPhase) ArtifactFile() string              { return "" }

// mockAutoAdvancePhase returns TransitionAutoAdvance to test pipeline advancement.
type mockAutoAdvancePhase struct{ mockPhase }

func (p *mockAutoAdvancePhase) TransitionMode() Transition { return TransitionAutoAdvance }

// mockAutosquashFailPhase fails on BeforeReview to simulate autosquash conflict.
type mockAutosquashFailPhase struct{ mockPhase }

func (p *mockAutosquashFailPhase) BeforeReview(_ PhaseContext) error {
	return errors.New("autosquash rebase failed: CONFLICT in file.txt")
}

func newTestStream() *stream.Stream {
	return &stream.Stream{
		ID:            "test-1",
		BeadsParentID: "parent-1",
	}
}

func mockFactory(_ string) (MacroPhase, error) {
	return &mockPhase{}, nil
}

// ids generates a slice of bead IDs.
func ids(names ...string) []string { return names }

func TestRunConvergesOnFirstIteration(t *testing.T) {
	s := newTestStream()
	rt := &mockRuntime{
		results: []mockResult{
			{resp: &runtime.Response{Text: "implemented"}},
			{resp: &runtime.Response{Text: "no issues"}},
		},
	}
	// Per iteration: idsBefore, idsAfterImpl, idsAfterReview
	beads := &mockBeads{openIDs: [][]string{ids("b-1", "b-2"), nil, nil}}

	Run(context.Background(), s, &mockPhase{}, rt, beads, &mockGit{}, 0, mockFactory, nil)

	if s.GetStatus() != stream.StatusPaused {
		t.Errorf("expected StatusPaused, got %s", s.GetStatus())
	}
	if !s.Converged {
		t.Error("expected Converged=true")
	}
	if len(s.Snapshots) != 1 {
		t.Errorf("expected 1 snapshot, got %d", len(s.Snapshots))
	}
	if s.LastError != nil {
		t.Errorf("expected no error, got %v", s.LastError)
	}
}

func TestRunSnapshotPopulatesFields(t *testing.T) {
	s := newTestStream()
	rt := &mockRuntime{
		results: []mockResult{
			{resp: &runtime.Response{Text: "implemented"}},
			{resp: &runtime.Response{Text: "filed b-3"}},
		},
	}
	// idsBefore: b-1, b-2; idsAfterImpl: b-2 (b-1 closed); idsAfterReview: b-2, b-3 (b-3 opened)
	beads := &mockBeads{openIDs: [][]string{ids("b-1", "b-2"), ids("b-2"), ids("b-2", "b-3")}}

	// maxIterations=1 so it pauses after one iteration regardless of convergence.
	Run(context.Background(), s, &mockPhase{}, rt, beads, &mockGit{}, 1, mockFactory, nil)

	if len(s.Snapshots) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(s.Snapshots))
	}
	snap := s.Snapshots[0]
	if snap.DiffStat == "" {
		t.Error("expected DiffStat to be populated")
	}
	if len(snap.CommitSHAs) == 0 {
		t.Error("expected CommitSHAs to be populated")
	}
	if len(snap.BeadsClosed) != 1 || snap.BeadsClosed[0] != "b-1" {
		t.Errorf("expected BeadsClosed=[b-1], got %v", snap.BeadsClosed)
	}
	if len(snap.BeadsOpened) != 1 || snap.BeadsOpened[0] != "b-3" {
		t.Errorf("expected BeadsOpened=[b-3], got %v", snap.BeadsOpened)
	}
}

func TestRunMultipleIterations(t *testing.T) {
	s := newTestStream()
	rt := &mockRuntime{
		results: []mockResult{
			{resp: &runtime.Response{Text: "impl1"}},
			{resp: &runtime.Response{Text: "review1"}},
			{resp: &runtime.Response{Text: "impl2"}},
			{resp: &runtime.Response{Text: "review2"}},
		},
	}
	// Iteration 0: idsBefore=[b-1,b-2,b-3], idsAfterImpl=[b-3], idsAfterReview=[b-3,b-4,b-5] → not converged (3 > 1)
	// Iteration 1: idsBefore=[b-3,b-4,b-5], idsAfterImpl=[], idsAfterReview=[] → converged (0 <= 0)
	beads := &mockBeads{openIDs: [][]string{
		ids("b-1", "b-2", "b-3"), ids("b-3"), ids("b-3", "b-4", "b-5"),
		ids("b-3", "b-4", "b-5"), nil, nil,
	}}

	Run(context.Background(), s, &mockPhase{}, rt, beads, &mockGit{}, 0, mockFactory, nil)

	if s.GetStatus() != stream.StatusPaused {
		t.Errorf("expected StatusPaused, got %s", s.GetStatus())
	}
	if !s.Converged {
		t.Error("expected Converged=true")
	}
	if len(s.Snapshots) != 2 {
		t.Errorf("expected 2 snapshots, got %d", len(s.Snapshots))
	}
	if s.GetIteration() != 1 {
		t.Errorf("expected iteration=1, got %d", s.GetIteration())
	}
}

func TestRunContextCancellation(t *testing.T) {
	s := newTestStream()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancelled

	Run(ctx, s, &mockPhase{}, &mockRuntime{}, &mockBeads{}, &mockGit{}, 0, mockFactory, nil)

	if s.GetStatus() != stream.StatusStopped {
		t.Errorf("expected StatusStopped, got %s", s.GetStatus())
	}
}

func TestRunAutoAdvancesToNextPhase(t *testing.T) {
	s := newTestStream()
	s.Pipeline = []string{"test", "coding"}
	s.PipelineIndex = 0
	rt := &mockRuntime{
		results: []mockResult{
			{resp: &runtime.Response{Text: "impl1"}},
			{resp: &runtime.Response{Text: "review1"}},
			{resp: &runtime.Response{Text: "impl2"}},
			{resp: &runtime.Response{Text: "review2"}},
		},
	}
	beads := &mockBeads{openIDs: [][]string{
		ids("b-1", "b-2"), nil, nil,
		ids("b-3"), nil, nil,
	}}

	Run(context.Background(), s, &mockAutoAdvancePhase{}, rt, beads, &mockGit{}, 0, mockFactory, nil)

	if s.GetStatus() != stream.StatusPaused {
		t.Errorf("expected StatusPaused, got %s", s.GetStatus())
	}
	if !s.Converged {
		t.Error("expected Converged=true")
	}
	if s.PipelineIndex != 1 {
		t.Errorf("expected PipelineIndex=1, got %d", s.PipelineIndex)
	}
	if len(s.Snapshots) != 2 {
		t.Errorf("expected 2 snapshots, got %d", len(s.Snapshots))
	}
}

func TestRunAutoAdvancePausesWhenPipelineExhausted(t *testing.T) {
	s := newTestStream()
	s.Pipeline = []string{"test"}
	s.PipelineIndex = 0
	rt := &mockRuntime{
		results: []mockResult{
			{resp: &runtime.Response{Text: "impl"}},
			{resp: &runtime.Response{Text: "review"}},
		},
	}
	beads := &mockBeads{openIDs: [][]string{ids("b-1", "b-2"), nil, nil}}

	Run(context.Background(), s, &mockAutoAdvancePhase{}, rt, beads, &mockGit{}, 0, mockFactory, nil)

	if s.GetStatus() != stream.StatusPaused {
		t.Errorf("expected StatusPaused, got %s", s.GetStatus())
	}
	if !s.Converged {
		t.Error("expected Converged=true")
	}
	if s.PipelineIndex != 0 {
		t.Errorf("expected PipelineIndex=0 (not advanced), got %d", s.PipelineIndex)
	}
}

func TestRunPauseTransitionDoesNotAdvance(t *testing.T) {
	s := newTestStream()
	s.Pipeline = []string{"test", "coding"}
	s.PipelineIndex = 0
	rt := &mockRuntime{
		results: []mockResult{
			{resp: &runtime.Response{Text: "impl"}},
			{resp: &runtime.Response{Text: "review"}},
		},
	}
	beads := &mockBeads{openIDs: [][]string{ids("b-1", "b-2"), nil, nil}}

	Run(context.Background(), s, &mockPhase{}, rt, beads, &mockGit{}, 0, mockFactory, nil)

	if s.PipelineIndex != 0 {
		t.Errorf("expected PipelineIndex=0 (pause should not advance), got %d", s.PipelineIndex)
	}
	if s.GetStatus() != stream.StatusPaused {
		t.Errorf("expected StatusPaused, got %s", s.GetStatus())
	}
}

func TestRunStoresSessionID(t *testing.T) {
	s := newTestStream()
	rt := &mockRuntime{
		results: []mockResult{
			{resp: &runtime.Response{Text: "implemented", SessionID: "sess-abc"}},
			{resp: &runtime.Response{Text: "no issues"}},
		},
	}
	beads := &mockBeads{openIDs: [][]string{ids("b-1", "b-2"), nil, nil}}

	Run(context.Background(), s, &mockPhase{}, rt, beads, &mockGit{}, 0, mockFactory, nil)

	if s.GetSessionID() != "sess-abc" {
		t.Errorf("got session_id %q, want %q", s.GetSessionID(), "sess-abc")
	}
}

func TestRunBreakpointPausesAutoAdvance(t *testing.T) {
	s := newTestStream()
	s.Pipeline = []string{"test", "coding"}
	s.PipelineIndex = 0
	s.Breakpoints = []int{0} // breakpoint after first phase
	rt := &mockRuntime{
		results: []mockResult{
			{resp: &runtime.Response{Text: "impl"}},
			{resp: &runtime.Response{Text: "review"}},
		},
	}
	beads := &mockBeads{openIDs: [][]string{ids("b-1", "b-2"), nil, nil}}

	Run(context.Background(), s, &mockAutoAdvancePhase{}, rt, beads, &mockGit{}, 0, mockFactory, nil)

	// Should pause at breakpoint, NOT auto-advance to coding.
	if s.PipelineIndex != 0 {
		t.Errorf("expected PipelineIndex=0 (breakpoint should prevent advance), got %d", s.PipelineIndex)
	}
	if !s.Converged {
		t.Error("expected Converged=true")
	}
	if s.GetStatus() != stream.StatusPaused {
		t.Errorf("expected StatusPaused, got %s", s.GetStatus())
	}
}

func TestRunResumeFromConvergedAdvancesPhase(t *testing.T) {
	s := newTestStream()
	s.Pipeline = []string{"test", "coding"}
	s.PipelineIndex = 0
	s.Converged = true // simulate paused at a breakpoint
	rt := &mockRuntime{
		results: []mockResult{
			{resp: &runtime.Response{Text: "impl"}},
			{resp: &runtime.Response{Text: "review"}},
		},
	}
	beads := &mockBeads{openIDs: [][]string{ids("b-1"), nil, nil}}

	Run(context.Background(), s, &mockPhase{}, rt, beads, &mockGit{}, 0, mockFactory, nil)

	// Should have advanced to phase 1 and then converged.
	if s.PipelineIndex != 1 {
		t.Errorf("expected PipelineIndex=1 (advanced on resume), got %d", s.PipelineIndex)
	}
	if !s.Converged {
		t.Error("expected Converged=true after running phase 1")
	}
}

func TestRunResumeFromConvergedLastPhasePauses(t *testing.T) {
	s := newTestStream()
	s.Pipeline = []string{"coding"}
	s.PipelineIndex = 0
	s.Converged = true // already at last phase and converged

	Run(context.Background(), s, &mockPhase{}, &mockRuntime{}, &mockBeads{}, &mockGit{}, 0, mockFactory, nil)

	// Should immediately pause — nothing to advance to.
	if s.GetStatus() != stream.StatusPaused {
		t.Errorf("expected StatusPaused, got %s", s.GetStatus())
	}
	if s.PipelineIndex != 0 {
		t.Errorf("expected PipelineIndex=0, got %d", s.PipelineIndex)
	}
}

func TestRunRuntimeError(t *testing.T) {
	s := newTestStream()
	rt := &mockRuntime{
		results: []mockResult{
			{err: errors.New("connection refused")},
		},
	}
	beads := &mockBeads{openIDs: [][]string{ids("b-1", "b-2")}}

	Run(context.Background(), s, &mockPhase{}, rt, beads, &mockGit{}, 0, mockFactory, nil)

	if s.GetStatus() != stream.StatusPaused {
		t.Errorf("expected StatusPaused, got %s", s.GetStatus())
	}
	if s.LastError == nil {
		t.Fatal("expected LastError to be set")
	}
	if s.LastError.Kind != stream.ErrRuntime {
		t.Errorf("expected ErrRuntime, got %s", s.LastError.Kind)
	}
}

func TestRunContinuesPastAutosquashFailure(t *testing.T) {
	s := newTestStream()
	rt := &mockRuntime{
		results: []mockResult{
			{resp: &runtime.Response{Text: "implemented"}},
			{resp: &runtime.Response{Text: "no issues"}},
		},
	}
	// Per iteration: idsBefore, idsAfterImpl, idsAfterReview
	beads := &mockBeads{openIDs: [][]string{ids("b-1", "b-2"), nil, nil}}

	Run(context.Background(), s, &mockAutosquashFailPhase{}, rt, beads, &mockGit{}, 0, mockFactory, nil)

	// Loop should converge normally despite autosquash failure.
	if s.GetStatus() != stream.StatusPaused {
		t.Errorf("expected StatusPaused, got %s", s.GetStatus())
	}
	if !s.Converged {
		t.Error("expected Converged=true")
	}
	if s.LastError != nil {
		t.Errorf("expected no terminal error, got %v", s.LastError)
	}
	if len(s.Snapshots) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(s.Snapshots))
	}
	// Snapshot should record the autosquash failure.
	if s.Snapshots[0].AutosquashErr == "" {
		t.Error("expected AutosquashErr to be populated in snapshot")
	}
}

func TestRunConvergesWhenAllBeadsAlreadyClosed(t *testing.T) {
	// Simulates resuming after an autosquash failure where the implement
	// step had already closed all beads in a prior run.
	s := newTestStream()
	s.Iteration = 6 // not the first iteration
	rt := &mockRuntime{
		results: []mockResult{
			{resp: &runtime.Response{Text: "nothing to do"}},
			{resp: &runtime.Response{Text: "looks good"}},
		},
	}
	// All beads already closed: idsBefore=[], idsAfterImpl=[], idsAfterReview=[]
	beads := &mockBeads{openIDs: [][]string{nil, nil, nil}}

	Run(context.Background(), s, &mockPhase{}, rt, beads, &mockGit{}, 0, mockFactory, nil)

	if s.GetStatus() != stream.StatusPaused {
		t.Errorf("expected StatusPaused, got %s", s.GetStatus())
	}
	if !s.Converged {
		t.Error("expected Converged=true")
	}
	if s.LastError != nil {
		t.Errorf("expected no error, got %v", s.LastError)
	}
}

// mockNoReviewPhase returns "" from ReviewPrompt to test review-skip behavior.
type mockNoReviewPhase struct{ mockPhase }

func (p *mockNoReviewPhase) ReviewPrompt(_ PhaseContext) (string, error) { return "", nil }

func TestRunSkipsReviewWhenPromptEmpty(t *testing.T) {
	s := newTestStream()
	rt := &mockRuntime{
		results: []mockResult{
			// Only the implement step should call the runtime.
			{resp: &runtime.Response{Text: "implemented"}},
		},
	}
	beads := &mockBeads{openIDs: [][]string{ids("b-1", "b-2"), nil, nil}}

	Run(context.Background(), s, &mockNoReviewPhase{}, rt, beads, &mockGit{}, 0, mockFactory, nil)

	if s.GetStatus() != stream.StatusPaused {
		t.Errorf("expected StatusPaused, got %s", s.GetStatus())
	}
	if !s.Converged {
		t.Error("expected Converged=true")
	}
	// Runtime should have been called exactly once (implement only, review skipped).
	if rt.calls != 1 {
		t.Errorf("expected 1 runtime call (implement only), got %d", rt.calls)
	}
	if len(s.Snapshots) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(s.Snapshots))
	}
	if s.Snapshots[0].Review != "" {
		t.Errorf("expected empty review in snapshot, got %q", s.Snapshots[0].Review)
	}
}
