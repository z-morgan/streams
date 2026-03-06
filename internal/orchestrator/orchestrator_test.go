package orchestrator

import (
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
	o := New(&store.Store{Root: root}, Config{})

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

	o := New(s, Config{})
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
	o := New(&store.Store{Root: root}, Config{})

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
	o := New(&store.Store{Root: root}, Config{})

	err := o.SendGuidance("nonexistent", "hello")
	if err == nil {
		t.Fatal("expected error for missing stream")
	}
}

func TestStartMissingStream(t *testing.T) {
	root := t.TempDir()
	o := New(&store.Store{Root: root}, Config{})

	err := o.Start("nonexistent")
	if err == nil {
		t.Fatal("expected error for missing stream")
	}
}

func TestIsRunning(t *testing.T) {
	root := t.TempDir()
	o := New(&store.Store{Root: root}, Config{})

	if o.IsRunning("nope") {
		t.Fatal("expected not running")
	}
}

func TestEventSink(t *testing.T) {
	root := t.TempDir()
	o := New(&store.Store{Root: root}, Config{})

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

func TestEmitWithNilSink(t *testing.T) {
	root := t.TempDir()
	o := New(&store.Store{Root: root}, Config{})

	// Should not panic.
	o.emit(Event{StreamID: "test", Kind: EventStarted})
}
