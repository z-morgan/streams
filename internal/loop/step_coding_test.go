package loop

import (
	"strings"
	"testing"

	"github.com/zmorgan/streams/internal/stream"
)

func TestStepCodingPhase_Name(t *testing.T) {
	p := &StepCodingPhase{}
	if p.Name() != "step-coding" {
		t.Errorf("expected 'step-coding', got %q", p.Name())
	}
}

func TestStepCodingPhase_ImplementPromptStepMode(t *testing.T) {
	p := &StepCodingPhase{}
	ctx := PhaseContext{
		Stream: &stream.Stream{Task: "build a feature", BeadsParentID: "parent-1"},
		StepBeads: []StepWithStatus{
			{Step: Step{ID: "s-1", Title: "Scaffold module", Sequence: 1}, Status: "closed"},
			{Step: Step{ID: "s-2", Title: "Add API endpoint", Sequence: 2}, Status: "open"},
			{Step: Step{ID: "s-3", Title: "Write tests", Sequence: 3}, Status: "open"},
		},
		PlanContent: "This is the plan content.",
	}
	prompt, err := p.ImplementPrompt(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(prompt, "one step of a multi-step plan") {
		t.Error("expected step mode prompt")
	}
	if !strings.Contains(prompt, "s-2") {
		t.Error("expected prompt to reference current step ID")
	}
	if !strings.Contains(prompt, "Add API endpoint") {
		t.Error("expected prompt to reference current step title")
	}
	if !strings.Contains(prompt, "This is the plan content.") {
		t.Error("expected prompt to include plan content")
	}
	if !strings.Contains(prompt, "[done]") {
		t.Error("expected prompt to include [done] marker for completed steps")
	}
	if !strings.Contains(prompt, "[current]") {
		t.Error("expected prompt to include [current] marker")
	}
}

func TestStepCodingPhase_ImplementPromptFixMode(t *testing.T) {
	p := &StepCodingPhase{}
	ctx := PhaseContext{
		Stream:          &stream.Stream{Task: "build a feature", BeadsParentID: "parent-1"},
		OpenReviewBeads: []string{"review-1", "review-2"},
		StepBeads: []StepWithStatus{
			{Step: Step{ID: "s-1", Title: "Scaffold module", Sequence: 1}, Status: "closed"},
			{Step: Step{ID: "s-2", Title: "Add API endpoint", Sequence: 2}, Status: "open"},
		},
		PlanContent: "This is the plan content.",
	}
	prompt, err := p.ImplementPrompt(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(prompt, "fixing issues") {
		t.Error("expected fix mode prompt")
	}
	if !strings.Contains(prompt, "review-1") {
		t.Error("expected prompt to list review beads")
	}
	if !strings.Contains(prompt, "fixup commit") {
		t.Error("expected prompt to mention fixup commits")
	}
}

func TestStepCodingPhase_ReviewPromptIncludesPlanAndSteps(t *testing.T) {
	p := &StepCodingPhase{}
	ctx := PhaseContext{
		Stream: &stream.Stream{Task: "build a feature", BeadsParentID: "parent-1"},
		StepBeads: []StepWithStatus{
			{Step: Step{ID: "s-1", Title: "Step one", Sequence: 1}, Status: "closed"},
			{Step: Step{ID: "s-2", Title: "Step two", Sequence: 2}, Status: "open"},
		},
		PlanContent: "Detailed plan here.",
	}
	prompt, err := p.ReviewPrompt(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(prompt, "Detailed plan here.") {
		t.Error("expected review prompt to include plan content")
	}
	if !strings.Contains(prompt, "[done]") {
		t.Error("expected review prompt to include step status markers")
	}
	if !strings.Contains(prompt, "Foundation") {
		t.Error("expected review prompt to include forward-looking Foundation criterion")
	}
}

func TestStepCodingPhase_Tools(t *testing.T) {
	p := &StepCodingPhase{}

	implTools := p.ImplementTools()
	hasEdit := false
	for _, tool := range implTools {
		if tool == "Edit" {
			hasEdit = true
		}
	}
	if !hasEdit {
		t.Error("expected implement tools to include Edit")
	}

	reviewTools := p.ReviewTools()
	for _, tool := range reviewTools {
		if tool == "Edit" || tool == "Write" {
			t.Errorf("review tools should not include %s", tool)
		}
	}
}

func TestStepCodingPhase_IsConverged(t *testing.T) {
	p := &StepCodingPhase{}

	if !p.IsConverged(IterationResult{OpenAfterReview: 0}) {
		t.Error("expected converged when OpenAfterReview is 0")
	}
	if p.IsConverged(IterationResult{OpenAfterReview: 1}) {
		t.Error("expected not converged when OpenAfterReview > 0")
	}
	if p.IsConverged(IterationResult{OpenAfterReview: 3, OpenBeforeReview: 5}) {
		t.Error("expected not converged even when after < before (step-coding needs 0)")
	}
}

func TestStepCodingPhase_TransitionMode(t *testing.T) {
	p := &StepCodingPhase{}
	if p.TransitionMode() != TransitionAutoAdvance {
		t.Errorf("expected TransitionAutoAdvance, got %s", p.TransitionMode())
	}
}

func TestNewPhase_StepCoding(t *testing.T) {
	phase, err := NewPhase("step-coding")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if phase.Name() != "step-coding" {
		t.Errorf("expected 'step-coding', got %q", phase.Name())
	}
}
