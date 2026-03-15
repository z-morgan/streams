package orchestrator

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zmorgan/streams/internal/store"
	"github.com/zmorgan/streams/internal/stream"
)

type testSink struct {
	mu     sync.Mutex
	events []Event
}

func (s *testSink) Send(e Event) {
	s.mu.Lock()
	s.events = append(s.events, e)
	s.mu.Unlock()
}

func (s *testSink) Events() []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]Event, len(s.events))
	copy(cp, s.events)
	return cp
}

func TestListAndGet(t *testing.T) {
	root := t.TempDir()
	o := New(&store.Store{Root: root}, Config{}, nil, nil)

	if len(o.List()) != 0 {
		t.Fatal("expected empty list")
	}
	if o.Get("nope") != nil {
		t.Fatal("expected nil for missing stream")
	}
}

func TestLoadExisting(t *testing.T) {
	root := t.TempDir()
	s := &store.Store{Root: root}

	// Pre-persist a stream.
	st := &stream.Stream{
		ID:        "existing-1",
		Name:      "Existing",
		Task:      "test",
		Pipeline:  []string{"coding"},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if _, err := s.Save(st, 0); err != nil {
		t.Fatalf("Save: %v", err)
	}

	o := New(s, Config{}, nil, nil)
	if err := o.LoadExisting(); err != nil {
		t.Fatalf("LoadExisting: %v", err)
	}

	if len(o.List()) != 1 {
		t.Fatalf("expected 1 stream, got %d", len(o.List()))
	}
	if o.Get("existing-1") == nil {
		t.Fatal("expected to find existing-1")
	}
}

func TestSendGuidance(t *testing.T) {
	root := t.TempDir()
	o := New(&store.Store{Root: root}, Config{}, nil, nil)

	// Add a stream directly for testing.
	st := &stream.Stream{
		ID:        "g-1",
		Name:      "Guidance test",
		Task:      "test",
		Pipeline:  []string{"coding"},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	o.mu.Lock()
	o.streams["g-1"] = st
	o.mu.Unlock()

	if err := o.SendGuidance("g-1", "focus on tests"); err != nil {
		t.Fatalf("SendGuidance: %v", err)
	}

	drained := st.DrainGuidance()
	if len(drained) != 1 {
		t.Fatalf("expected 1 guidance item, got %d", len(drained))
	}
	if drained[0].Text != "focus on tests" {
		t.Errorf("guidance text: got %q, want %q", drained[0].Text, "focus on tests")
	}
}

func TestSendGuidanceMissingStream(t *testing.T) {
	root := t.TempDir()
	o := New(&store.Store{Root: root}, Config{}, nil, nil)

	err := o.SendGuidance("nonexistent", "hello")
	if err == nil {
		t.Fatal("expected error for missing stream")
	}
}

func TestStartMissingStream(t *testing.T) {
	root := t.TempDir()
	o := New(&store.Store{Root: root}, Config{}, nil, nil)

	err := o.Start("nonexistent")
	if err == nil {
		t.Fatal("expected error for missing stream")
	}
}

func TestIsRunning(t *testing.T) {
	root := t.TempDir()
	o := New(&store.Store{Root: root}, Config{}, nil, nil)

	if o.IsRunning("nope") {
		t.Fatal("expected not running")
	}
}

func TestEventSink(t *testing.T) {
	root := t.TempDir()
	o := New(&store.Store{Root: root}, Config{}, nil, nil)

	sink := &testSink{}
	o.SetSink(sink)

	o.emit(Event{StreamID: "test", Kind: EventStarted})

	// emit is async (goroutine), give it a moment to deliver.
	time.Sleep(50 * time.Millisecond)

	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Kind != EventStarted {
		t.Errorf("event kind: got %v, want EventStarted", events[0].Kind)
	}
}

func TestValidatePipeline(t *testing.T) {
	if err := ValidatePipeline([]string{"plan", "decompose", "coding"}); err != nil {
		t.Fatalf("expected valid pipeline: %v", err)
	}

	if err := ValidatePipeline([]string{"coding", "bogus"}); err == nil {
		t.Fatal("expected error for invalid pipeline phase")
	}
}

func TestEmitWithNilSink(t *testing.T) {
	root := t.TempDir()
	o := New(&store.Store{Root: root}, Config{}, nil, nil)

	// Should not panic.
	o.emit(Event{StreamID: "test", Kind: EventStarted})
}

// initTestRepo creates a git repo with one commit and returns the repo path
// and the SHA of that initial commit.
func initTestRepo(t *testing.T) (repoDir, baseSHA string) {
	t.Helper()
	repoDir = t.TempDir()
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "test@test.com"},
		{"config", "user.name", "Test"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	// Create an initial commit so we have a base SHA.
	readme := filepath.Join(repoDir, "README.md")
	if err := os.WriteFile(readme, []byte("init\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"add", "README.md"},
		{"commit", "-m", "initial"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	return repoDir, strings.TrimSpace(string(out))
}

func TestCompleteSucceedsWithDirtyArtifacts(t *testing.T) {
	repoDir, baseSHA := initTestRepo(t)

	// Create a worktree branch and worktree directory.
	branch := "streams/test-1"
	wtPath := filepath.Join(repoDir, ".streams", "worktrees", "test-1")
	cmd := exec.Command("git", "worktree", "add", "-b", branch, wtPath)
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("worktree add: %s", out)
	}

	// Make a commit in the worktree beyond the base SHA.
	appFile := filepath.Join(wtPath, "app.go")
	if err := os.WriteFile(appFile, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"add", "app.go"},
		{"commit", "-m", "add app"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = wtPath
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}

	// Write artifact files (research.md untracked, plan.md untracked).
	os.WriteFile(filepath.Join(wtPath, "research.md"), []byte("# Research\n"), 0644)
	os.WriteFile(filepath.Join(wtPath, "plan.md"), []byte("# Plan\n"), 0644)

	// Set up orchestrator with the stream.
	storeRoot := t.TempDir()
	o := New(&store.Store{Root: storeRoot}, Config{RepoDir: repoDir}, nil, nil)
	st := &stream.Stream{
		ID:        "test-1",
		Name:      "Test",
		Task:      "test task",
		Pipeline:  []string{"research", "plan", "coding"},
		Branch:    branch,
		WorkTree:  wtPath,
		BaseSHA:   baseSHA,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	o.mu.Lock()
	o.streams["test-1"] = st
	o.mu.Unlock()

	// Complete should succeed — artifact files should be cleaned up.
	if err := o.Complete("test-1", branch); err != nil {
		t.Fatalf("Complete() failed: %v", err)
	}

	if st.Status != stream.StatusCompleted {
		t.Errorf("expected StatusCompleted, got %v", st.Status)
	}
}

func TestCompleteRejectsNonArtifactDirtyFiles(t *testing.T) {
	repoDir, baseSHA := initTestRepo(t)

	branch := "streams/test-2"
	wtPath := filepath.Join(repoDir, ".streams", "worktrees", "test-2")
	cmd := exec.Command("git", "worktree", "add", "-b", branch, wtPath)
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("worktree add: %s", out)
	}

	// Make a commit beyond base.
	appFile := filepath.Join(wtPath, "app.go")
	if err := os.WriteFile(appFile, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"add", "app.go"},
		{"commit", "-m", "add app"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = wtPath
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}

	// Write a non-artifact dirty file.
	os.WriteFile(filepath.Join(wtPath, "dirty.txt"), []byte("uncommitted\n"), 0644)

	storeRoot := t.TempDir()
	o := New(&store.Store{Root: storeRoot}, Config{RepoDir: repoDir}, nil, nil)
	st := &stream.Stream{
		ID:        "test-2",
		Name:      "Test",
		Task:      "test task",
		Pipeline:  []string{"research", "plan", "coding"},
		Branch:    branch,
		WorkTree:  wtPath,
		BaseSHA:   baseSHA,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	o.mu.Lock()
	o.streams["test-2"] = st
	o.mu.Unlock()

	err := o.Complete("test-2", branch)
	if err == nil {
		t.Fatal("expected Complete() to fail with dirty non-artifact file")
	}
	if !strings.Contains(err.Error(), "uncommitted changes") {
		t.Errorf("expected 'uncommitted changes' error, got: %v", err)
	}
}
