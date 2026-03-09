package diagnosis

import (
	"strings"
	"testing"
	"time"

	"github.com/zmorgan/streams/internal/stream"
)

func TestBuildSystemPrompt_ContainsInstructionsAndContext(t *testing.T) {
	s := &stream.Stream{
		ID:       "diag-1",
		Task:     "Implement user authentication",
		Pipeline: []string{"plan", "coding"},
		Snapshots: []stream.Snapshot{
			{
				Phase:     "plan",
				Iteration: 0,
				Summary:   "Drafted authentication plan with OAuth2",
				CostUSD:   0.10,
				Timestamp: time.Now(),
			},
		},
	}
	s.SetStatus(stream.StatusPaused)

	prompt := BuildSystemPrompt(s, "/tmp/store")

	// Check that system prompt instructions are present.
	for _, expected := range []string{
		"Stream Diagnostician",
		"Diagnose",
		"Scope Selection Rules",
		"Per-stream",
		"Project",
		"Global",
		"Confirmation Protocol",
		"Available Actions",
		"bd close",
		"bd create",
		"Analysis Tips",
	} {
		if !strings.Contains(prompt, expected) {
			t.Errorf("expected system prompt to contain %q", expected)
		}
	}

	// Check that context document is embedded.
	for _, expected := range []string{
		"Stream Diagnosis Context",
		"Implement user authentication",
		"Drafted authentication plan with OAuth2",
		"Override Locations",
	} {
		if !strings.Contains(prompt, expected) {
			t.Errorf("expected context section to contain %q", expected)
		}
	}
}

func TestBuildSystemPrompt_OverridePathsIncludeStreamID(t *testing.T) {
	s := &stream.Stream{
		ID:       "my-stream-42",
		Task:     "test",
		Pipeline: []string{"coding"},
	}
	s.SetStatus(stream.StatusPaused)

	prompt := BuildSystemPrompt(s, "/data/root")

	if !strings.Contains(prompt, "/data/root/streams/my-stream-42/prompts") {
		t.Error("expected per-stream prompts path to include stream ID")
	}
	if !strings.Contains(prompt, "/data/root/prompts") {
		t.Error("expected project prompts path")
	}
}
