package stream

import "testing"

func TestBlockedByRoundTrip(t *testing.T) {
	st := &Stream{ID: "test-1"}

	// Initially empty.
	if got := st.GetBlockedBy(); len(got) != 0 {
		t.Fatalf("expected empty BlockedBy, got %v", got)
	}

	// Set some blockers.
	st.SetBlockedBy([]string{"a", "b", "c"})
	got := st.GetBlockedBy()
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("expected [a b c], got %v", got)
	}

	// Returned slice is a copy — mutation doesn't affect the stream.
	got[0] = "x"
	if st.GetBlockedBy()[0] != "a" {
		t.Fatal("GetBlockedBy should return a copy")
	}

	// Clear blockers.
	st.SetBlockedBy(nil)
	if got := st.GetBlockedBy(); len(got) != 0 {
		t.Fatalf("expected empty after clear, got %v", got)
	}
}
