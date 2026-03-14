package loop

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/zmorgan/streams/internal/convergence"
	"github.com/zmorgan/streams/internal/runtime"
	"github.com/zmorgan/streams/internal/stream"
)

func pipelineFactory(name string) (MacroPhase, error) {
	switch name {
	case "research", "plan", "decompose":
		return &mockAutoAdvancePhase{}, nil
	case "coding":
		return &mockPhase{}, nil
	default:
		return nil, fmt.Errorf("unknown phase: %s", name)
	}
}

func TestFullPipelineThreePhases(t *testing.T) {
	s := newTestStream()
	s.Pipeline = []string{"plan", "decompose", "coding"}
	s.PipelineIndex = 0

	// 2 runtime calls per phase (implement + review) x 3 phases = 6 calls
	rt := &mockRuntime{
		results: []mockResult{
			{resp: &runtime.Response{Text: "plan-impl"}},
			{resp: &runtime.Response{Text: "plan-review"}},
			{resp: &runtime.Response{Text: "decompose-impl"}},
			{resp: &runtime.Response{Text: "decompose-review"}},
			{resp: &runtime.Response{Text: "coding-impl"}},
			{resp: &runtime.Response{Text: "coding-review"}},
		},
	}

	// 3 ListOpenChildren calls per phase (idsBefore, idsAfterImpl, idsAfterReview)
	// Each phase converges in 1 iteration: has beads before, none after.
	beads := &mockBeads{openIDs: [][]string{
		// plan phase
		ids("b-1"), nil, nil,
		// decompose phase
		ids("b-2"), nil, nil,
		// coding phase
		ids("b-3"), nil, nil,
	}}

	Run(context.Background(), s, &mockAutoAdvancePhase{}, rt, beads, &mockGit{}, 0, pipelineFactory, nil, convergence.Config{})

	if s.GetStatus() != stream.StatusPaused {
		t.Errorf("expected StatusPaused, got %s", s.GetStatus())
	}
	if !s.Converged {
		t.Error("expected Converged=true")
	}
	if s.PipelineIndex != 2 {
		t.Errorf("expected PipelineIndex=2, got %d", s.PipelineIndex)
	}
	if len(s.Snapshots) != 3 {
		t.Errorf("expected 3 snapshots, got %d", len(s.Snapshots))
	}
	if s.LastError != nil {
		t.Errorf("expected no error, got %v", s.LastError)
	}
}

func TestFullPipelineErrorPausesStream(t *testing.T) {
	s := newTestStream()
	s.Pipeline = []string{"plan", "decompose", "coding"}
	s.PipelineIndex = 0

	// Plan succeeds (2 calls), decompose implement errors (1 call)
	rt := &mockRuntime{
		results: []mockResult{
			{resp: &runtime.Response{Text: "plan-impl"}},
			{resp: &runtime.Response{Text: "plan-review"}},
			{err: errors.New("connection refused")},
		},
	}

	// Plan phase: converges (beads before, none after)
	// Decompose phase: idsBefore succeeds, then runtime errors
	beads := &mockBeads{openIDs: [][]string{
		// plan phase
		ids("b-1"), nil, nil,
		// decompose phase (only idsBefore is reached before error)
		ids("b-2"),
	}}

	Run(context.Background(), s, &mockAutoAdvancePhase{}, rt, beads, &mockGit{}, 0, pipelineFactory, nil, convergence.Config{})

	if s.GetStatus() != stream.StatusPaused {
		t.Errorf("expected StatusPaused, got %s", s.GetStatus())
	}
	if s.LastError == nil {
		t.Fatal("expected LastError to be set")
	}
	if s.LastError.Kind != stream.ErrRuntime {
		t.Errorf("expected ErrRuntime, got %s", s.LastError.Kind)
	}
	if s.PipelineIndex != 1 {
		t.Errorf("expected PipelineIndex=1, got %d", s.PipelineIndex)
	}
	if len(s.Snapshots) != 2 {
		t.Errorf("expected 2 snapshots (1 plan + 1 error), got %d", len(s.Snapshots))
	}
}

func TestFullPipelineCheckpointCallback(t *testing.T) {
	s := newTestStream()
	s.Pipeline = []string{"plan", "coding"}
	s.PipelineIndex = 0

	rt := &mockRuntime{
		results: []mockResult{
			{resp: &runtime.Response{Text: "plan-impl"}},
			{resp: &runtime.Response{Text: "plan-review"}},
			{resp: &runtime.Response{Text: "coding-impl"}},
			{resp: &runtime.Response{Text: "coding-review"}},
		},
	}

	beads := &mockBeads{openIDs: [][]string{
		// plan phase
		ids("b-1"), nil, nil,
		// coding phase
		ids("b-2"), nil, nil,
	}}

	checkpoints := 0
	onCheckpoint := func(_ *stream.Stream) {
		checkpoints++
	}

	Run(context.Background(), s, &mockAutoAdvancePhase{}, rt, beads, &mockGit{}, 0, pipelineFactory, onCheckpoint, convergence.Config{})

	if checkpoints != 2 {
		t.Errorf("expected 2 checkpoint callbacks, got %d", checkpoints)
	}
}

