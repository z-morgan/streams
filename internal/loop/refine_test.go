package loop

import (
	"strings"
	"testing"

	"github.com/zmorgan/streams/internal/stream"
)

func TestRefinementPhase_Name(t *testing.T) {
	p := &RefinementPhase{}
	if p.Name() != "refine" {
		t.Errorf("expected 'refine', got %q", p.Name())
	}
}

func TestRefinementPhase_ImplementPrompt(t *testing.T) {
	p := &RefinementPhase{}
	ctx := PhaseContext{
		Stream:    &stream.Stream{Task: "build a feature", BeadsParentID: "parent-1"},
		Iteration: 0,
	}
	prompt, err := p.ImplementPrompt(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(prompt, "refining a completed implementation") {
		t.Error("expected refine implement prompt")
	}
	if !strings.Contains(prompt, "cross-cutting concerns") {
		t.Error("expected prompt to mention cross-cutting concerns")
	}
	if !strings.Contains(prompt, "proactive pass") {
		t.Error("expected prompt to mention proactive pass for first iteration")
	}
}

func TestRefinementPhase_ReviewPrompt(t *testing.T) {
	p := &RefinementPhase{}
	ctx := PhaseContext{
		Stream:    &stream.Stream{Task: "build a feature", BeadsParentID: "parent-1"},
		Iteration: 0,
	}
	prompt, err := p.ReviewPrompt(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(prompt, "integration quality") {
		t.Error("expected review prompt to evaluate integration quality")
	}
	if !strings.Contains(prompt, "Dead code") {
		t.Error("expected review prompt to check for dead code")
	}
}

func TestRefinementPhase_ReviewPromptIterationGating(t *testing.T) {
	p := &RefinementPhase{}

	// At iteration 3, should gate to T1/T2 only.
	ctx := PhaseContext{
		Stream:    &stream.Stream{Task: "test", BeadsParentID: "p-1"},
		Iteration: 3,
	}
	prompt, err := p.ReviewPrompt(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(prompt, "Only file [T1] or [T2] issues") {
		t.Error("expected iteration 3+ to gate to T1/T2")
	}

	// At iteration 5, should gate to T1 only.
	ctx.Iteration = 5
	prompt, err = p.ReviewPrompt(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(prompt, "only file [T1] issues") {
		t.Error("expected iteration 5+ to gate to T1 only")
	}
}

func TestRefinementPhase_IsConverged(t *testing.T) {
	p := &RefinementPhase{}

	if !p.IsConverged(IterationResult{OpenBeforeReview: 2, OpenAfterReview: 1}) {
		t.Error("expected converged when after < before")
	}
	if !p.IsConverged(IterationResult{OpenBeforeReview: 0, OpenAfterReview: 0}) {
		t.Error("expected converged when both zero")
	}
	if !p.IsConverged(IterationResult{OpenBeforeReview: 3, OpenAfterReview: 3}) {
		t.Error("expected converged when after == before")
	}
	if p.IsConverged(IterationResult{OpenBeforeReview: 1, OpenAfterReview: 3}) {
		t.Error("expected not converged when after > before")
	}
}

func TestRefinementPhase_TransitionMode(t *testing.T) {
	p := &RefinementPhase{}
	if p.TransitionMode() != TransitionAutoAdvance {
		t.Errorf("expected TransitionAutoAdvance, got %s", p.TransitionMode())
	}
}

func TestNewPhase_Refine(t *testing.T) {
	phase, err := NewPhase("refine")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if phase.Name() != "refine" {
		t.Errorf("expected 'refine', got %q", phase.Name())
	}
}
