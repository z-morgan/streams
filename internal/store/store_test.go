package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zmorgan/streams/internal/stream"
)

func TestSaveAndLoad(t *testing.T) {
	root := t.TempDir()
	s := &Store{Root: root}

	st := &stream.Stream{
		ID:            "test-001",
		Name:          "Test stream",
		Task:          "Do the thing",
		Mode:          stream.ModeAutonomous,
		Status:        stream.StatusRunning,
		Pipeline:      []string{"plan", "coding"},
		PipelineIndex: 1,
		IterStep:      stream.StepReview,
		Converged:     false,
		BeadsParentID: "beads-abc",
		BaseSHA:       "deadbeef",
		Branch:        "streams/test-001",
		WorkTree:      "/tmp/wt/test-001",
		CreatedAt:     time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:     time.Date(2025, 1, 1, 1, 0, 0, 0, time.UTC),
	}
	st.SetIteration(3)
	st.Snapshots = []stream.Snapshot{
		{Phase: "plan", Iteration: 0, Summary: "planned", Timestamp: time.Now()},
		{Phase: "coding", Iteration: 1, Summary: "coded", CostUSD: 0.50, Timestamp: time.Now()},
	}

	persisted, err := s.Save(st, 0)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if persisted != 2 {
		t.Fatalf("expected 2 persisted snapshots, got %d", persisted)
	}

	loaded, err := s.Load("test-001")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if loaded.ID != st.ID {
		t.Errorf("ID: got %q, want %q", loaded.ID, st.ID)
	}
	if loaded.Task != st.Task {
		t.Errorf("Task: got %q, want %q", loaded.Task, st.Task)
	}
	if loaded.GetStatus() != stream.StatusRunning {
		t.Errorf("Status: got %v, want Running", loaded.GetStatus())
	}
	if loaded.GetIteration() != 3 {
		t.Errorf("Iteration: got %d, want 3", loaded.GetIteration())
	}
	if loaded.PipelineIndex != 1 {
		t.Errorf("PipelineIndex: got %d, want 1", loaded.PipelineIndex)
	}
	if len(loaded.Snapshots) != 2 {
		t.Fatalf("Snapshots: got %d, want 2", len(loaded.Snapshots))
	}
	if loaded.Snapshots[0].Phase != "plan" {
		t.Errorf("Snapshot[0].Phase: got %q, want %q", loaded.Snapshots[0].Phase, "plan")
	}
	if loaded.Snapshots[1].CostUSD != 0.50 {
		t.Errorf("Snapshot[1].CostUSD: got %f, want 0.50", loaded.Snapshots[1].CostUSD)
	}
}

func TestSaveAppendsSnapshots(t *testing.T) {
	root := t.TempDir()
	s := &Store{Root: root}

	st := &stream.Stream{
		ID:        "test-002",
		Name:      "Append test",
		Task:      "test",
		Pipeline:  []string{"coding"},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	st.Snapshots = []stream.Snapshot{
		{Phase: "coding", Iteration: 0, Summary: "first", Timestamp: time.Now()},
	}

	persisted, err := s.Save(st, 0)
	if err != nil {
		t.Fatalf("Save 1: %v", err)
	}

	// Add another snapshot and save again.
	st.Snapshots = append(st.Snapshots, stream.Snapshot{
		Phase: "coding", Iteration: 1, Summary: "second", Timestamp: time.Now(),
	})
	persisted, err = s.Save(st, persisted)
	if err != nil {
		t.Fatalf("Save 2: %v", err)
	}
	if persisted != 2 {
		t.Fatalf("expected 2 persisted, got %d", persisted)
	}

	loaded, err := s.Load("test-002")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded.Snapshots) != 2 {
		t.Fatalf("Snapshots: got %d, want 2", len(loaded.Snapshots))
	}
	if loaded.Snapshots[1].Summary != "second" {
		t.Errorf("Snapshot[1].Summary: got %q, want %q", loaded.Snapshots[1].Summary, "second")
	}
}

func TestSaveWithError(t *testing.T) {
	root := t.TempDir()
	s := &Store{Root: root}

	st := &stream.Stream{
		ID:        "test-003",
		Name:      "Error test",
		Task:      "test",
		Pipeline:  []string{"coding"},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		LastError: &stream.LoopError{
			Kind:    stream.ErrBudget,
			Step:    stream.StepImplement,
			Message: "budget exceeded",
			Detail:  "max $2.00",
		},
	}
	st.SetStatus(stream.StatusPaused)

	_, err := s.Save(st, 0)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := s.Load("test-003")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.LastError == nil {
		t.Fatal("expected LastError to be set")
	}
	if loaded.LastError.Kind != stream.ErrBudget {
		t.Errorf("ErrorKind: got %v, want Budget", loaded.LastError.Kind)
	}
	if loaded.GetStatus() != stream.StatusPaused {
		t.Errorf("Status: got %v, want Paused", loaded.GetStatus())
	}
}

func TestLoadAll(t *testing.T) {
	root := t.TempDir()
	s := &Store{Root: root}

	for _, id := range []string{"s1", "s2"} {
		st := &stream.Stream{
			ID:        id,
			Name:      id,
			Task:      id,
			Pipeline:  []string{"coding"},
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		if _, err := s.Save(st, 0); err != nil {
			t.Fatalf("Save %s: %v", id, err)
		}
	}

	streams, err := s.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(streams) != 2 {
		t.Fatalf("LoadAll: got %d streams, want 2", len(streams))
	}
}

func TestLoadAllEmpty(t *testing.T) {
	root := t.TempDir()
	s := &Store{Root: root}

	streams, err := s.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(streams) != 0 {
		t.Fatalf("expected 0 streams, got %d", len(streams))
	}
}

func TestLoadAllIgnoresFiles(t *testing.T) {
	root := t.TempDir()
	s := &Store{Root: root}

	// Create a file (not a dir) in the streams directory — should be ignored.
	streamsDir := filepath.Join(root, "streams")
	os.MkdirAll(streamsDir, 0o755)
	os.WriteFile(filepath.Join(streamsDir, "not-a-stream.txt"), []byte("hi"), 0o644)

	streams, err := s.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(streams) != 0 {
		t.Fatalf("expected 0 streams, got %d", len(streams))
	}
}
