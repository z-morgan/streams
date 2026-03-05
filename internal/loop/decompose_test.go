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
	prompt := d.ImplementPrompt(ctx)

	if !strings.Contains(prompt, "breaking a plan") {
		t.Error("expected first-iteration prompt to contain 'breaking a plan'")
	}
	if !strings.Contains(prompt, "parent-1") {
		t.Error("expected prompt to reference parent ID")
	}
	if !strings.Contains(prompt, `metadata '{"sequence":N}'`) {
		t.Error("expected prompt to include metadata sequence instructions")
	}
}

func TestDecomposePhase_ImplementPromptSubsequentIteration(t *testing.T) {
	d := &DecomposePhase{}
	ctx := PhaseContext{
		Stream:    &stream.Stream{Task: "build a widget", BeadsParentID: "parent-1"},
		Iteration: 1,
	}
	prompt := d.ImplementPrompt(ctx)

	if !strings.Contains(prompt, "revising the decomposition") {
		t.Error("expected subsequent-iteration prompt to contain 'revising the decomposition'")
	}
	if !strings.Contains(prompt, "parent-1") {
		t.Error("expected prompt to reference parent ID")
	}
}

func TestDecomposePhase_ReviewPrompt(t *testing.T) {
	d := &DecomposePhase{}
	ctx := PhaseContext{
		Stream: &stream.Stream{BeadsParentID: "parent-1"},
	}
	prompt := d.ReviewPrompt(ctx)

	if !strings.Contains(prompt, "reviewing the decomposition") {
		t.Error("expected review prompt to contain 'reviewing the decomposition'")
	}
	if !strings.Contains(prompt, "parent-1") {
		t.Error("expected review prompt to reference parent ID")
	}
	if !strings.Contains(prompt, "metadata.sequence") {
		t.Error("expected review prompt to mention metadata.sequence")
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

	if !d.IsConverged(IterationResult{OpenChildrenBefore: 2, OpenChildrenAfter: 1}) {
		t.Error("expected converged when after < before")
	}
	if !d.IsConverged(IterationResult{OpenChildrenBefore: 0, OpenChildrenAfter: 0}) {
		t.Error("expected converged when both zero")
	}
	if d.IsConverged(IterationResult{OpenChildrenBefore: 1, OpenChildrenAfter: 3}) {
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
