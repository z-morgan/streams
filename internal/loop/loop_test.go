package loop

import (
	"context"
	"errors"
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
	countCalls int
	steps      []Step
}

func (m *mockBeads) ListOpenChildren(_ string) ([]string, error) {
	i := m.countCalls
	m.countCalls++
	if i < len(m.openIDs) {
		return m.openIDs[i], nil
	}
	return nil, nil
}

func (m *mockBeads) FetchOrderedSteps(_ string) ([]Step, error) {
	return m.steps, nil
}

// mockGit is a no-op GitQuerier for testing.
type mockGit struct{}

func (m *mockGit) HeadSHA(_ string) (string, error)                         { return "abc123", nil }
func (m *mockGit) DiffStat(_, _ string) (string, error)                     { return "", nil }
func (m *mockGit) CommitsBetween(_, _, _ string) ([]string, error)           { return nil, nil }

// mockPhase is a minimal MacroPhase for testing.
type mockPhase struct{}

func (p *mockPhase) Name() string                          { return "test" }
func (p *mockPhase) ImplementPrompt(_ PhaseContext) string { return "implement" }
func (p *mockPhase) ReviewPrompt(_ PhaseContext) string    { return "review" }
func (p *mockPhase) ImplementTools() []string              { return []string{"Bash"} }
func (p *mockPhase) ReviewTools() []string                 { return []string{"Bash"} }
func (p *mockPhase) IsConverged(r IterationResult) bool {
	return r.OpenChildrenAfter <= r.OpenChildrenBefore
}
func (p *mockPhase) BeforeReview(_ PhaseContext) error { return nil }
func (p *mockPhase) TransitionMode() Transition { return TransitionPause }

// mockAutoAdvancePhase returns TransitionAutoAdvance to test pipeline advancement.
type mockAutoAdvancePhase struct{ mockPhase }

func (p *mockAutoAdvancePhase) TransitionMode() Transition { return TransitionAutoAdvance }

func newTestStream() *stream.Stream {
	return &stream.Stream{
		ID:            "test-1",
		BeadsParentID: "parent-1",
	}
}

func mockFactory(_ string) (MacroPhase, error) {
	return &mockPhase{}, nil
}

func TestRunConvergesOnFirstIteration(t *testing.T) {
	s := newTestStream()
	rt := &mockRuntime{
		results: []mockResult{
			{resp: &runtime.Response{Text: "implemented"}},
			{resp: &runtime.Response{Text: "no issues"}},
		},
	}
	// Per iteration: openBefore, openAfterImpl, openAfterReview
	beads := &mockBeads{openIDs: [][]string{{"a", "b"}, {}, {}}}

	Run(context.Background(), s, &mockPhase{}, rt, beads, &mockGit{}, 0, mockFactory)

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
	// Iteration 0: openBefore=3, openAfterImpl=1, openAfterReview=3 → not converged (3 > 1)
	// Iteration 1: openBefore=3, openAfterImpl=0, openAfterReview=0 → converged (0 <= 0)
	beads := &mockBeads{openIDs: [][]string{{"a", "b", "c"}, {"a"}, {"a", "b", "c"}, {"a", "b", "c"}, {}, {}}}

	Run(context.Background(), s, &mockPhase{}, rt, beads, &mockGit{}, 0, mockFactory)

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

	Run(ctx, s, &mockPhase{}, &mockRuntime{}, &mockBeads{}, &mockGit{}, 0, mockFactory)

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
			// Phase "test" iteration 0: implement + review
			{resp: &runtime.Response{Text: "impl1"}},
			{resp: &runtime.Response{Text: "review1"}},
			// Phase "coding" iteration 0: implement + review
			{resp: &runtime.Response{Text: "impl2"}},
			{resp: &runtime.Response{Text: "review2"}},
		},
	}
	// Phase "test": openBefore=2, openAfterImpl=0, openAfterReview=0 → converged
	// Phase "coding": openBefore=1, openAfterImpl=0, openAfterReview=0 → converged
	beads := &mockBeads{openIDs: [][]string{{"a", "b"}, {}, {}, {"c"}, {}, {}}}

	Run(context.Background(), s, &mockAutoAdvancePhase{}, rt, beads, &mockGit{}, 0, mockFactory)

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
	beads := &mockBeads{openIDs: [][]string{{"a", "b"}, {}, {}}}

	Run(context.Background(), s, &mockAutoAdvancePhase{}, rt, beads, &mockGit{}, 0, mockFactory)

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
	beads := &mockBeads{openIDs: [][]string{{"a", "b"}, {}, {}}}

	Run(context.Background(), s, &mockPhase{}, rt, beads, &mockGit{}, 0, mockFactory)

	if s.PipelineIndex != 0 {
		t.Errorf("expected PipelineIndex=0 (pause should not advance), got %d", s.PipelineIndex)
	}
	if s.GetStatus() != stream.StatusPaused {
		t.Errorf("expected StatusPaused, got %s", s.GetStatus())
	}
}

func TestRunRuntimeError(t *testing.T) {
	s := newTestStream()
	rt := &mockRuntime{
		results: []mockResult{
			{err: errors.New("connection refused")},
		},
	}
	beads := &mockBeads{openIDs: [][]string{{"a", "b"}}}

	Run(context.Background(), s, &mockPhase{}, rt, beads, &mockGit{}, 0, mockFactory)

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
