package diagnosis

import (
	"strings"
	"testing"
	"time"

	"github.com/zmorgan/streams/internal/stream"
)

func TestBuildContext_BasicStream(t *testing.T) {
	s := &stream.Stream{
		ID:            "test-123",
		Name:          "Fix login bug",
		Task:          "Fix the login page timeout issue",
		Pipeline:      []string{"research", "coding"},
		PipelineIndex: 1,
		Breakpoints:   []int{0},
		Branch:        "streams/test-123",
		WorkTree:      "/tmp/worktrees/test-123",
		BeadsParentID: "test-123",
		Converged:     false,
		Snapshots: []stream.Snapshot{
			{
				Phase:       "research",
				Iteration:   0,
				Summary:     "Investigated the login flow and found the timeout is set to 5s",
				Review:      "Research is thorough, proceed to coding",
				Artifact:    "# Research\nThe timeout is in auth.go line 42",
				CostUSD:     0.15,
				BeadsClosed: []string{"test-123-a"},
				BeadsOpened: []string{"test-123-b", "test-123-c"},
				Timestamp:   time.Now(),
			},
			{
				Phase:       "coding",
				Iteration:   0,
				Summary:     "Updated timeout to 30s and added retry logic",
				Review:      "Code changes look good, tests pass",
				CostUSD:     0.25,
				DiffStat:    " auth.go | 5 ++---\n 1 file changed, 2 insertions(+), 3 deletions(-)",
				CommitSHAs:  []string{"abc123"},
				BeadsClosed: []string{"test-123-b"},
				Timestamp:   time.Now(),
			},
			{
				Phase:     "coding",
				Iteration: 1,
				Error: &stream.LoopError{
					Kind:    stream.ErrNoProgress,
					Step:    stream.StepImplement,
					Message: "implement step closed zero beads",
				},
				Timestamp: time.Now(),
			},
		},
	}
	s.SetStatus(stream.StatusPaused)
	s.SetIteration(1)

	s.LastError = &stream.LoopError{
		Kind:    stream.ErrNoProgress,
		Step:    stream.StepImplement,
		Message: "implement step closed zero beads",
	}

	ctx := BuildContext(s, "/tmp/streams-data")

	// Check major sections are present.
	for _, expected := range []string{
		"# Stream Diagnosis Context",
		"## Task",
		"Fix the login page timeout issue",
		"## Pipeline",
		"research",
		"coding",
		"(breakpoint)",
		"## Iteration History",
		"### Research Phase",
		"### Coding Phase",
		"Investigated the login flow",
		"Updated timeout to 30s",
		"implement step closed zero beads",
		"## Current State",
		"Paused",
		"## Artifacts",
		"research artifact",
		"## Prompt Templates in Use",
		"## Override Locations",
		"Per-stream",
		"Project",
		"Global",
	} {
		if !strings.Contains(ctx, expected) {
			t.Errorf("expected context to contain %q", expected)
		}
	}
}

func TestBuildContext_EmptyStream(t *testing.T) {
	s := &stream.Stream{
		ID:       "empty-1",
		Task:     "Do something",
		Pipeline: []string{"coding"},
	}
	s.SetStatus(stream.StatusCreated)

	ctx := BuildContext(s, "/tmp/data")

	if !strings.Contains(ctx, "No iterations completed yet") {
		t.Error("expected empty iteration history message")
	}
	if !strings.Contains(ctx, "Do something") {
		t.Error("expected task description")
	}
}

func TestBuildContext_CostTotals(t *testing.T) {
	s := &stream.Stream{
		ID:       "cost-1",
		Task:     "Cost test",
		Pipeline: []string{"coding"},
		Snapshots: []stream.Snapshot{
			{Phase: "coding", Iteration: 0, CostUSD: 0.50, Timestamp: time.Now()},
			{Phase: "coding", Iteration: 1, CostUSD: 0.75, Timestamp: time.Now()},
		},
	}
	s.SetStatus(stream.StatusPaused)

	ctx := BuildContext(s, "/tmp/data")

	if !strings.Contains(ctx, "$1.25") {
		t.Error("expected total cost of $1.25")
	}
}
