package stream

import "testing"

func TestMaxIterHint_NeverConverged(t *testing.T) {
	snaps := []Snapshot{
		{Phase: "coding", BeadsOpened: []string{"b-1"}},
		{Phase: "coding", BeadsOpened: []string{"b-2"}},
		{Phase: "coding", BeadsOpened: []string{"b-3"}},
	}
	hint := MaxIterHint(snaps, "coding")
	if hint == "" {
		t.Fatal("expected non-empty hint")
	}
	want := "Review never converged"
	if !contains(hint, want) {
		t.Errorf("expected hint to contain %q, got %q", want, hint)
	}
}

func TestMaxIterHint_NearlyConverged(t *testing.T) {
	snaps := []Snapshot{
		{Phase: "coding", BeadsClosed: []string{"b-1"}},
		{Phase: "coding", BeadsClosed: []string{"b-2"}},
		{Phase: "coding", BeadsClosed: []string{"b-3"}},
		{Phase: "coding", BeadsClosed: []string{"b-4"}, BeadsOpened: []string{"b-5"}},
	}
	hint := MaxIterHint(snaps, "coding")
	want := "Nearly converged"
	if !contains(hint, want) {
		t.Errorf("expected hint to contain %q, got %q", want, hint)
	}
}

func TestMaxIterHint_NoProgress(t *testing.T) {
	snaps := []Snapshot{
		{Phase: "coding", BeadsClosed: []string{"b-1"}},
		{Phase: "coding"}, // last iteration closed nothing
	}
	hint := MaxIterHint(snaps, "coding")
	want := "couldn't make progress"
	if !contains(hint, want) {
		t.Errorf("expected hint to contain %q, got %q", want, hint)
	}
}

func TestMaxIterHint_FiltersPhase(t *testing.T) {
	snaps := []Snapshot{
		{Phase: "plan", BeadsOpened: []string{"b-1"}},
		{Phase: "coding", BeadsClosed: []string{"b-2"}},
	}
	// For coding phase, only 1 snapshot with no opens — should be empty.
	hint := MaxIterHint(snaps, "coding")
	if hint != "" {
		t.Errorf("expected empty hint for single coding snapshot, got %q", hint)
	}
}

func TestMaxIterHint_IgnoresErrorSnapshots(t *testing.T) {
	snaps := []Snapshot{
		{Phase: "coding", BeadsOpened: []string{"b-1"}},
		{Phase: "coding", Error: &LoopError{Kind: ErrMaxIterations}},
	}
	// Only 1 non-error snapshot with opens → never converged.
	hint := MaxIterHint(snaps, "coding")
	want := "Review never converged"
	if !contains(hint, want) {
		t.Errorf("expected %q in hint, got %q", want, hint)
	}
}

func TestMaxIterHint_EmptySnapshots(t *testing.T) {
	hint := MaxIterHint(nil, "coding")
	if hint != "" {
		t.Errorf("expected empty hint, got %q", hint)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
