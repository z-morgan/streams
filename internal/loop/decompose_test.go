package loop

import (
	"strings"
	"testing"

	"github.com/zmorgan/streams/internal/stream"
)

func TestDecomposePhase_Name(t *testing.T) {
	d := &DecomposePhase{}
	if d.Name() != "decompose" {
		t.Errorf("expected 'decompose', got %q", d.Name())
	}
}

func TestDecomposePhase_ImplementPromptFirstIteration(t *testing.T) {
	d := &DecomposePhase{}
	ctx := PhaseContext{
		Stream:    &stream.Stream{Task: "build a widget", BeadsParentID: "parent-1"},
		Iteration: 0,
	}
	prompt, err := d.ImplementPrompt(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(prompt, "breaking a plan") {
		t.Error("expected first-iteration prompt to contain 'breaking a plan'")
	}
	if !strings.Contains(prompt, "parent-1") {
		t.Error("expected prompt to reference parent ID")
	}
	if !strings.Contains(prompt, `--notes "sequence:N"`) {
		t.Error("expected prompt to include notes sequence instructions")
	}
	if !strings.Contains(prompt, "--label step") {
		t.Error("expected prompt to include --label step")
	}
	if !strings.Contains(prompt, "research.md") {
		t.Error("expected prompt to reference research.md")
	}
}

func TestDecomposePhase_ImplementPromptSubsequentIteration(t *testing.T) {
	d := &DecomposePhase{}
	ctx := PhaseContext{
		Stream:    &stream.Stream{Task: "build a widget", BeadsParentID: "parent-1"},
		Iteration: 1,
	}
	prompt, err := d.ImplementPrompt(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(prompt, "revising the decomposition") {
		t.Error("expected subsequent-iteration prompt to contain 'revising the decomposition'")
	}
	if !strings.Contains(prompt, "parent-1") {
		t.Error("expected prompt to reference parent ID")
	}
	if !strings.Contains(prompt, `labeled "step"`) {
		t.Error("expected subsequent-iteration prompt to explain step label distinction")
	}
	if !strings.Contains(prompt, "--label step") {
		t.Error("expected subsequent-iteration prompt to include --label step for new steps")
	}
}

func TestDecomposePhase_ReviewPrompt(t *testing.T) {
	d := &DecomposePhase{}
	ctx := PhaseContext{
		Stream: &stream.Stream{BeadsParentID: "parent-1"},
	}
	prompt, err := d.ReviewPrompt(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(prompt, "reviewing the decomposition") {
		t.Error("expected review prompt to contain 'reviewing the decomposition'")
	}
	if !strings.Contains(prompt, "parent-1") {
		t.Error("expected review prompt to reference parent ID")
	}
	if !strings.Contains(prompt, "sequence:N") {
		t.Error("expected review prompt to mention sequence:N")
	}
	if !strings.Contains(prompt, `labeled "step"`) {
		t.Error("expected review prompt to explain step label distinction")
	}
}

func TestDecomposePhase_Tools(t *testing.T) {
	d := &DecomposePhase{}

	implTools := d.ImplementTools()
	if len(implTools) != 4 {
		t.Errorf("expected 4 implement tools, got %d", len(implTools))
	}

	reviewTools := d.ReviewTools()
	if len(reviewTools) != 4 {
		t.Errorf("expected 4 review tools, got %d", len(reviewTools))
	}

	// Neither should include Edit or Write.
	for _, tools := range [][]string{implTools, reviewTools} {
		for _, tool := range tools {
			if tool == "Edit" || tool == "Write" {
				t.Errorf("decompose tools should not include %s", tool)
			}
		}
	}
}

func TestDecomposePhase_IsConverged(t *testing.T) {
	d := &DecomposePhase{}

	if !d.IsConverged(IterationResult{OpenBeforeReview: 2, OpenAfterReview: 1}) {
		t.Error("expected converged when after < before")
	}
	if !d.IsConverged(IterationResult{OpenBeforeReview: 0, OpenAfterReview: 0}) {
		t.Error("expected converged when both zero")
	}
	if d.IsConverged(IterationResult{OpenBeforeReview: 1, OpenAfterReview: 3}) {
		t.Error("expected not converged when after > before")
	}
}

func TestDecomposePhase_BeforeReviewIsNoop(t *testing.T) {
	d := &DecomposePhase{}
	if err := d.BeforeReview(PhaseContext{}); err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

func TestDecomposePhase_TransitionMode(t *testing.T) {
	d := &DecomposePhase{}
	if d.TransitionMode() != TransitionAutoAdvance {
		t.Errorf("expected TransitionAutoAdvance, got %s", d.TransitionMode())
	}
}

func TestNewPhase_Decompose(t *testing.T) {
	phase, err := NewPhase("decompose")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if phase.Name() != "decompose" {
		t.Errorf("expected 'decompose', got %q", phase.Name())
	}
}
