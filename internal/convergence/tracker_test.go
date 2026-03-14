package convergence

import "testing"

func TestClassifyEdit(t *testing.T) {
	tests := []struct {
		adds, removes int
		want          EditType
	}{
		{5, 0, EditAdditive},     // pure addition
		{3, 3, EditSubstitutive}, // equal adds/removes
		{4, 3, EditSubstitutive}, // within 30% threshold
		{10, 0, EditAdditive},    // net growth
		{10, 2, EditAdditive},    // mostly additions
		{0, 0, EditAdditive},     // no change (shouldn't be called, but safe)
	}
	for _, tt := range tests {
		got := classifyEdit(tt.adds, tt.removes)
		if got != tt.want {
			t.Errorf("classifyEdit(%d, %d) = %v, want %v", tt.adds, tt.removes, got, tt.want)
		}
	}
}

func TestTrackerRecordChanges(t *testing.T) {
	cfg := ResolvedConfig{
		MaxSectionRevisions: 3,
		SectionDetection:    SectionDetectionHeadings,
	}

	tracker := NewTracker()

	prev := "## Section A\n\nOriginal content.\n\n## Section B\n\nOther content.\n"
	cur := "## Section A\n\nModified content.\n\n## Section B\n\nOther content.\n"

	frozen := tracker.RecordChanges(prev, cur, 1, cfg, "plan")
	if len(frozen) != 0 {
		t.Errorf("iteration 1: got %d frozen, want 0", len(frozen))
	}

	stateA := tracker.Sections["section-a"]
	if stateA == nil {
		t.Fatal("section-a not tracked")
	}
	if len(stateA.Revisions) != 1 {
		t.Errorf("section-a has %d revisions, want 1", len(stateA.Revisions))
	}

	// Section B should not have revisions (no change).
	stateB := tracker.Sections["section-b"]
	if stateB != nil && len(stateB.Revisions) > 0 {
		t.Errorf("section-b has %d revisions, want 0", len(stateB.Revisions))
	}
}

func TestTrackerFreezing(t *testing.T) {
	cfg := ResolvedConfig{
		MaxSectionRevisions: 2,
		SectionDetection:    SectionDetectionHeadings,
	}

	tracker := NewTracker()

	base := "## Section A\n\nContent v1.\n"
	v2 := "## Section A\n\nContent v2.\n"
	v3 := "## Section A\n\nContent v3.\n"

	tracker.RecordChanges(base, v2, 1, cfg, "plan")
	frozen := tracker.RecordChanges(v2, v3, 2, cfg, "plan")

	if len(frozen) != 1 || frozen[0] != "section-a" {
		t.Errorf("expected section-a frozen, got %v", frozen)
	}

	if !tracker.IsFrozen("section-a") {
		t.Error("section-a should be frozen")
	}

	// Further changes should be ignored since the section is frozen.
	v4 := "## Section A\n\nContent v4.\n"
	frozen = tracker.RecordChanges(v3, v4, 3, cfg, "plan")
	if len(frozen) != 0 {
		t.Errorf("expected no new freezes, got %v", frozen)
	}
}

func TestTrackerFrozenSections(t *testing.T) {
	cfg := ResolvedConfig{
		MaxSectionRevisions: 1,
		SectionDetection:    SectionDetectionHeadings,
	}

	tracker := NewTracker()

	prev := "## My Section\n\nOriginal.\n"
	cur := "## My Section\n\nChanged.\n"
	tracker.RecordChanges(prev, cur, 1, cfg, "plan")

	frozenList := tracker.FrozenSections()
	if len(frozenList) != 1 {
		t.Fatalf("got %d frozen sections, want 1", len(frozenList))
	}
	if frozenList[0].ID != "my-section" {
		t.Errorf("frozen ID = %q, want %q", frozenList[0].ID, "my-section")
	}
	if frozenList[0].FrozenAt != 1 {
		t.Errorf("frozen at = %d, want 1", frozenList[0].FrozenAt)
	}
}

func TestRecordFileChanges(t *testing.T) {
	cfg := ResolvedConfig{
		MaxSectionRevisions: 2,
	}

	tracker := NewTracker()

	tracker.RecordFileChanges([]string{"main.go", "utils.go"}, 1, cfg)
	tracker.RecordFileChanges([]string{"main.go"}, 2, cfg)

	if !tracker.IsFrozen("main.go") {
		t.Error("main.go should be frozen after 2 revisions")
	}
	if tracker.IsFrozen("utils.go") {
		t.Error("utils.go should not be frozen after 1 revision")
	}
}

func TestDiffLineCount(t *testing.T) {
	prev := []string{"a", "b", "c"}
	cur := []string{"a", "d", "c", "e"}

	adds, removes := diffLineCount(prev, cur)
	if adds != 2 { // "d" and "e" are new
		t.Errorf("adds = %d, want 2", adds)
	}
	if removes != 1 { // "b" was removed
		t.Errorf("removes = %d, want 1", removes)
	}
}
