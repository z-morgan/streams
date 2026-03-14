package convergence

import "testing"

func TestParseTier(t *testing.T) {
	tests := []struct {
		title string
		want  Tier
	}{
		{"[T1] hidden_field :id without persisted? guard", Tier1},
		{"[T2] Missing step for database migration", Tier2},
		{"[T3] Extract helper method for readability", Tier3},
		{"[T4] Rename variable for clarity", Tier4},
		{"No tier tag here", TierUnknown},
		{"[T5] Invalid tier", TierUnknown},
		{"[T1] and [T2] both present", Tier1}, // first match wins
	}
	for _, tt := range tests {
		got := ParseTier(tt.title)
		if got != tt.want {
			t.Errorf("ParseTier(%q) = %v, want %v", tt.title, got, tt.want)
		}
	}
}

func TestDefaultTier(t *testing.T) {
	if got := DefaultTier(ModeFast); got != Tier4 {
		t.Errorf("DefaultTier(fast) = %v, want T4", got)
	}
	if got := DefaultTier(ModeBalanced); got != Tier3 {
		t.Errorf("DefaultTier(balanced) = %v, want T3", got)
	}
	if got := DefaultTier(ModeThorough); got != Tier3 {
		t.Errorf("DefaultTier(thorough) = %v, want T3", got)
	}
}

func TestClassifyIssues_Balanced(t *testing.T) {
	cfg := ResolvedConfig{Mode: ModeBalanced, RefinementCap: 6, MaxSectionRevisions: 3}
	issues := []IssueInput{
		{ID: "1", Title: "[T1] Bug found"},
		{ID: "2", Title: "[T2] Missing feature"},
		{ID: "3", Title: "[T3] Design issue"},
		{ID: "4", Title: "[T4] Polish nit"},
	}

	// Before refinement cap.
	results := ClassifyIssues(issues, cfg, 2, nil)
	if !results[0].Blocking {
		t.Error("T1 should block in balanced mode")
	}
	if !results[1].Blocking {
		t.Error("T2 should block in balanced mode")
	}
	if !results[2].Blocking {
		t.Error("T3 should block in balanced mode")
	}
	if results[3].Blocking {
		t.Error("T4 should be advisory in balanced mode")
	}

	// After refinement cap.
	results = ClassifyIssues(issues, cfg, 7, nil)
	if !results[0].Blocking {
		t.Error("T1 should block after refinement cap")
	}
	if !results[1].Blocking {
		t.Error("T2 should block after refinement cap")
	}
	if results[2].Blocking {
		t.Error("T3 should be advisory after refinement cap in balanced mode")
	}
	if results[3].Blocking {
		t.Error("T4 should be advisory after refinement cap")
	}
}

func TestClassifyIssues_Fast(t *testing.T) {
	cfg := ResolvedConfig{Mode: ModeFast, RefinementCap: 6, MaxSectionRevisions: 3}
	issues := []IssueInput{
		{ID: "1", Title: "[T1] Critical bug"},
		{ID: "2", Title: "[T2] Missing feature"},
		{ID: "3", Title: "[T3] Design issue"},
	}

	results := ClassifyIssues(issues, cfg, 1, nil)
	if !results[0].Blocking {
		t.Error("T1 should block in fast mode")
	}
	if results[1].Blocking {
		t.Error("T2 should be advisory in fast mode")
	}
	if results[2].Blocking {
		t.Error("T3 should be advisory in fast mode")
	}
}

func TestClassifyIssues_Thorough(t *testing.T) {
	cfg := ResolvedConfig{Mode: ModeThorough, RefinementCap: 6, MaxSectionRevisions: 3}
	issues := []IssueInput{
		{ID: "1", Title: "[T1] Bug"},
		{ID: "2", Title: "[T4] Polish"},
	}

	// Before refinement cap - all block.
	results := ClassifyIssues(issues, cfg, 2, nil)
	if !results[0].Blocking || !results[1].Blocking {
		t.Error("all tiers should block in thorough mode before refinement cap")
	}

	// After refinement cap - T4 becomes advisory.
	results = ClassifyIssues(issues, cfg, 7, nil)
	if !results[0].Blocking {
		t.Error("T1 should still block after refinement cap in thorough mode")
	}
	if results[1].Blocking {
		t.Error("T4 should be advisory after refinement cap in thorough mode")
	}
}

func TestClassifyIssues_FrozenSection(t *testing.T) {
	cfg := ResolvedConfig{Mode: ModeBalanced, RefinementCap: 6, MaxSectionRevisions: 3}

	tracker := NewTracker()
	frozenIter := 3
	tracker.Sections["step-3-background"] = &SectionState{
		Heading:  "## Step 3: Background",
		FrozenAt: &frozenIter,
	}

	issues := []IssueInput{
		{ID: "1", Title: "[T1] Bug in Step 3: Background color"},
		{ID: "2", Title: "[T2] Missing detail", Description: "The Step 3: Background section needs more detail"},
		{ID: "3", Title: "[T3] Unrelated issue"},
	}

	results := ClassifyIssues(issues, cfg, 4, tracker)

	// T1 on frozen section still blocks.
	if !results[0].Blocking {
		t.Error("T1 on frozen section should still block")
	}
	// T2 on frozen section becomes advisory.
	if results[1].Blocking {
		t.Error("T2 on frozen section should be advisory")
	}
	// T3 on non-frozen section blocks normally.
	if !results[2].Blocking {
		t.Error("T3 on non-frozen section should block in balanced mode")
	}
}

func TestClassifyIssues_UntaggedDefault(t *testing.T) {
	cfg := ResolvedConfig{Mode: ModeBalanced, RefinementCap: 6, MaxSectionRevisions: 3}
	issues := []IssueInput{
		{ID: "1", Title: "No tier tag here"},
	}

	results := ClassifyIssues(issues, cfg, 1, nil)
	if results[0].Tier != Tier3 {
		t.Errorf("untagged issue in balanced mode: tier = %v, want T3", results[0].Tier)
	}
	if !results[0].Blocking {
		t.Error("T3 (default for balanced) should block")
	}
}

func TestConverged(t *testing.T) {
	blocking := []IssueClassification{
		{Blocking: true},
		{Blocking: false},
	}
	if Converged(blocking) {
		t.Error("should not converge with blocking issues")
	}

	advisory := []IssueClassification{
		{Blocking: false},
		{Blocking: false},
	}
	if !Converged(advisory) {
		t.Error("should converge with only advisory issues")
	}

	if !Converged(nil) {
		t.Error("should converge with no issues")
	}
}
