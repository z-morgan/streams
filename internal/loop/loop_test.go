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

// mockBeads returns configurable open counts per call and a fixed step list.
type mockBeads struct {
	openCounts []int
	countCalls int
	steps      []Step
}

func (m *mockBeads) CountOpenChildren(_ string) (int, error) {
	i := m.countCalls
	m.countCalls++
	if i < len(m.openCounts) {
		return m.openCounts[i], nil
	}
	return 0, nil
}

func (m *mockBeads) FetchOrderedSteps(_ string) ([]Step, error) {
	return m.steps, nil
}

// mockPhase is a minimal MacroPhase for testing.
type mockPhase struct{}

func (p *mockPhase) Name() string                         { return "test" }
func (p *mockPhase) ImplementPrompt(_ PhaseContext) string { return "implement" }
func (p *mockPhase) ReviewPrompt(_ PhaseContext) string    { return "review" }
func (p *mockPhase) ImplementTools() []string              { return []string{"Bash"} }
func (p *mockPhase) ReviewTools() []string                 { return []string{"Bash"} }
func (p *mockPhase) IsConverged(r IterationResult) bool {
	return r.OpenChildrenAfter <= r.OpenChildrenBefore
}
func (p *mockPhase) BeforeReview(_ PhaseContext) error { return nil }
func (p *mockPhase) TransitionMode() Transition        { return TransitionPause }

func newTestStream() *stream.Stream {
	return &stream.Stream{
		ID:            "test-1",
		BeadsParentID: "parent-1",
	}
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
	beads := &mockBeads{openCounts: []int{2, 0, 0}}

	Run(context.Background(), s, &mockPhase{}, rt, beads)

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
	beads := &mockBeads{openCounts: []int{3, 1, 3, 3, 0, 0}}

	Run(context.Background(), s, &mockPhase{}, rt, beads)

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

	Run(ctx, s, &mockPhase{}, &mockRuntime{}, &mockBeads{})

	if s.GetStatus() != stream.StatusStopped {
		t.Errorf("expected StatusStopped, got %s", s.GetStatus())
	}
}

func TestRunRuntimeError(t *testing.T) {
	s := newTestStream()
	rt := &mockRuntime{
		results: []mockResult{
			{err: errors.New("connection refused")},
		},
	}
	beads := &mockBeads{openCounts: []int{2}}

	Run(context.Background(), s, &mockPhase{}, rt, beads)

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
