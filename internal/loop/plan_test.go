package loop

import (
	"strings"
	"testing"

	"github.com/zmorgan/streams/internal/stream"
)

func TestPlanPhase_Name(t *testing.T) {
	p := &PlanPhase{}
	if p.Name() != "plan" {
		t.Errorf("expected 'plan', got %q", p.Name())
	}
}

func TestPlanPhase_ImplementPromptFirstIteration(t *testing.T) {
	p := &PlanPhase{}
	ctx := PhaseContext{
		Stream:    &stream.Stream{Task: "build a widget", BeadsParentID: "parent-1"},
		Iteration: 0,
	}
	prompt, err := p.ImplementPrompt(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(prompt, "drafting a plan") {
		t.Error("expected first-iteration prompt to contain 'drafting a plan'")
	}
	if !strings.Contains(prompt, "build a widget") {
		t.Error("expected prompt to contain task description")
	}
	if !strings.Contains(prompt, "research.md") {
		t.Error("expected first-iteration prompt to reference research.md")
	}
	if strings.Contains(prompt, "parent-1") {
		t.Error("first-iteration prompt should not reference parent ID")
	}
}

func TestPlanPhase_ImplementPromptSubsequentIteration(t *testing.T) {
	p := &PlanPhase{}
	ctx := PhaseContext{
		Stream:    &stream.Stream{Task: "build a widget", BeadsParentID: "parent-1"},
		Iteration: 1,
	}
	prompt, err := p.ImplementPrompt(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(prompt, "revising a plan") {
		t.Error("expected subsequent-iteration prompt to contain 'revising a plan'")
	}
	if !strings.Contains(prompt, "parent-1") {
		t.Error("expected prompt to reference parent ID")
	}
}

func TestPlanPhase_ReviewPrompt(t *testing.T) {
	p := &PlanPhase{}
	ctx := PhaseContext{
		Stream: &stream.Stream{Task: "build a widget", BeadsParentID: "parent-1"},
	}
	prompt, err := p.ReviewPrompt(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(prompt, "reviewing a plan") {
		t.Error("expected review prompt to contain 'reviewing a plan'")
	}
	if !strings.Contains(prompt, "bd create --parent parent-1") {
		t.Error("expected review prompt to contain bd create with parent ID")
	}
}

func TestPlanPhase_Tools(t *testing.T) {
	p := &PlanPhase{}

	implTools := p.ImplementTools()
	if len(implTools) != 6 {
		t.Errorf("expected 6 implement tools, got %d", len(implTools))
	}

	reviewTools := p.ReviewTools()
	if len(reviewTools) != 4 {
		t.Errorf("expected 4 review tools, got %d", len(reviewTools))
	}

	// Review should not include Edit or Write.
	for _, tool := range reviewTools {
		if tool == "Edit" || tool == "Write" {
			t.Errorf("review tools should not include %s", tool)
		}
	}
}

func TestPlanPhase_IsConverged(t *testing.T) {
	p := &PlanPhase{}

	if !p.IsConverged(IterationResult{OpenBeforeReview: 2, OpenAfterReview: 1}) {
		t.Error("expected converged when after < before")
	}
	if !p.IsConverged(IterationResult{OpenBeforeReview: 0, OpenAfterReview: 0}) {
		t.Error("expected converged when both zero")
	}
	if p.IsConverged(IterationResult{OpenBeforeReview: 1, OpenAfterReview: 3}) {
		t.Error("expected not converged when after > before")
	}
}

func TestPlanPhase_BeforeReviewIsNoop(t *testing.T) {
	p := &PlanPhase{}
	if err := p.BeforeReview(PhaseContext{}); err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

func TestPlanPhase_TransitionMode(t *testing.T) {
	p := &PlanPhase{}
	if p.TransitionMode() != TransitionAutoAdvance {
		t.Errorf("expected TransitionAutoAdvance, got %s", p.TransitionMode())
	}
}

func TestNewPhase_Plan(t *testing.T) {
	phase, err := NewPhase("plan")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if phase.Name() != "plan" {
		t.Errorf("expected 'plan', got %q", phase.Name())
	}
}
