package loop

import (
	"strings"
	"testing"
)

func TestFormatStepProgress(t *testing.T) {
	steps := []StepWithStatus{
		{Step: Step{ID: "s-1", Title: "Scaffold module", Sequence: 1}, Status: "closed"},
		{Step: Step{ID: "s-2", Title: "Add API endpoint", Sequence: 2}, Status: "open"},
		{Step: Step{ID: "s-3", Title: "Write tests", Sequence: 3}, Status: "open"},
	}

	// Current step is index 1 (s-2).
	result := FormatStepProgress(steps, 1)

	if !strings.Contains(result, "[done] Step 1") {
		t.Error("expected [done] marker for closed step")
	}
	if !strings.Contains(result, "[current] Step 2") {
		t.Error("expected [current] marker for current step")
	}
	if !strings.Contains(result, "[open] Step 3") {
		t.Error("expected [open] marker for future step")
	}
	if !strings.Contains(result, "Scaffold module") {
		t.Error("expected step title in output")
	}
	if !strings.Contains(result, "(s-1)") {
		t.Error("expected step ID in output")
	}
}

func TestFormatStepProgress_NoCurrentStep(t *testing.T) {
	steps := []StepWithStatus{
		{Step: Step{ID: "s-1", Title: "Step one", Sequence: 1}, Status: "closed"},
		{Step: Step{ID: "s-2", Title: "Step two", Sequence: 2}, Status: "open"},
	}

	// Fix mode: currentIdx = -1 (no current step).
	result := FormatStepProgress(steps, -1)

	if !strings.Contains(result, "[done]") {
		t.Error("expected [done] marker for closed step")
	}
	if strings.Contains(result, "[current]") {
		t.Error("expected no [current] marker in fix mode")
	}
	if !strings.Contains(result, "[open]") {
		t.Error("expected [open] marker for open step")
	}
}

func TestFormatStepProgress_AllDone(t *testing.T) {
	steps := []StepWithStatus{
		{Step: Step{ID: "s-1", Title: "Step one", Sequence: 1}, Status: "closed"},
		{Step: Step{ID: "s-2", Title: "Step two", Sequence: 2}, Status: "closed"},
	}

	result := FormatStepProgress(steps, -1)

	if strings.Contains(result, "[current]") || strings.Contains(result, "[open]") {
		t.Error("expected only [done] markers when all steps are closed")
	}
	count := strings.Count(result, "[done]")
	if count != 2 {
		t.Errorf("expected 2 [done] markers, got %d", count)
	}
}

func TestFormatStepProgress_Empty(t *testing.T) {
	result := FormatStepProgress(nil, -1)
	if result != "" {
		t.Errorf("expected empty string for nil steps, got %q", result)
	}
}

func TestHasLabel(t *testing.T) {
	labels := []string{"step", "implementation"}
	if !hasLabel(labels, "step") {
		t.Error("expected hasLabel to find 'step'")
	}
	if hasLabel(labels, "missing") {
		t.Error("expected hasLabel to return false for missing label")
	}
	if hasLabel(nil, "step") {
		t.Error("expected hasLabel to return false for nil labels")
	}
}

func TestFormatSteps(t *testing.T) {
	steps := []Step{
		{ID: "s-1", Title: "First step", Sequence: 1},
		{ID: "s-2", Title: "Second step", Sequence: 2},
	}
	result := FormatSteps(steps)

	if !strings.Contains(result, "1. s-1 — First step") {
		t.Error("expected formatted first step")
	}
	if !strings.Contains(result, "2. s-2 — Second step") {
		t.Error("expected formatted second step")
	}
}

func TestParseSequence(t *testing.T) {
	tests := []struct {
		notes    string
		expected int
	}{
		{"sequence:1", 1},
		{"sequence:5", 5},
		{"some other note\nsequence:3\nmore notes", 3},
		{"no sequence here", 0},
		{"", 0},
		{"sequence:abc", 0},
		{"  sequence:7  ", 7},
	}
	for _, tt := range tests {
		got := parseSequence(tt.notes)
		if got != tt.expected {
			t.Errorf("parseSequence(%q) = %d, want %d", tt.notes, got, tt.expected)
		}
	}
}
