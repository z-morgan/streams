package convergence

import "testing"

func TestParseMode(t *testing.T) {
	tests := []struct {
		input string
		want  Mode
	}{
		{"balanced", ModeBalanced},
		{"thorough", ModeThorough},
		{"fast", ModeFast},
		{"unknown", ModeBalanced},
		{"", ModeBalanced},
	}
	for _, tt := range tests {
		if got := ParseMode(tt.input); got != tt.want {
			t.Errorf("ParseMode(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestParseSectionDetection(t *testing.T) {
	tests := []struct {
		input string
		want  SectionDetection
	}{
		{"auto", SectionDetectionAuto},
		{"headings", SectionDetectionHeadings},
		{"files", SectionDetectionFiles},
		{"unknown", SectionDetectionAuto},
	}
	for _, tt := range tests {
		if got := ParseSectionDetection(tt.input); got != tt.want {
			t.Errorf("ParseSectionDetection(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestMerge(t *testing.T) {
	mode1 := ModeBalanced
	mode2 := ModeFast
	rev3 := 3
	rev5 := 5

	base := Config{Mode: &mode1, MaxSectionRevisions: &rev3}
	override := Config{Mode: &mode2, MaxSectionRevisions: &rev5}

	result := Merge(base, override)
	if *result.Mode != ModeFast {
		t.Errorf("Mode = %v, want fast", *result.Mode)
	}
	if *result.MaxSectionRevisions != 5 {
		t.Errorf("MaxSectionRevisions = %d, want 5", *result.MaxSectionRevisions)
	}
}

func TestMergeNilFields(t *testing.T) {
	mode := ModeThorough
	rev := 3

	base := Config{Mode: &mode, MaxSectionRevisions: &rev}
	override := Config{} // all nil

	result := Merge(base, override)
	if *result.Mode != ModeThorough {
		t.Errorf("Mode = %v, want thorough", *result.Mode)
	}
	if *result.MaxSectionRevisions != 3 {
		t.Errorf("MaxSectionRevisions = %d, want 3", *result.MaxSectionRevisions)
	}
}

func TestResolved(t *testing.T) {
	mode := ModeBalanced
	rev := 3
	cap := 6
	fastMode := ModeFast
	rev2 := 2

	cfg := Config{
		Mode:                &mode,
		MaxSectionRevisions: &rev,
		RefinementCap:       &cap,
		Phases: map[string]Config{
			"research": {Mode: &fastMode, MaxSectionRevisions: &rev2},
		},
	}

	// Default phase resolution.
	rc := cfg.Resolved("coding")
	if rc.Mode != ModeBalanced {
		t.Errorf("coding Mode = %v, want balanced", rc.Mode)
	}
	if rc.MaxSectionRevisions != 3 {
		t.Errorf("coding MaxSectionRevisions = %d, want 3", rc.MaxSectionRevisions)
	}

	// Per-phase override.
	rc = cfg.Resolved("research")
	if rc.Mode != ModeFast {
		t.Errorf("research Mode = %v, want fast", rc.Mode)
	}
	if rc.MaxSectionRevisions != 2 {
		t.Errorf("research MaxSectionRevisions = %d, want 2", rc.MaxSectionRevisions)
	}
	if rc.RefinementCap != 6 {
		t.Errorf("research RefinementCap = %d, want 6 (inherited)", rc.RefinementCap)
	}
}

func TestDefaults(t *testing.T) {
	d := Defaults()
	if *d.Mode != ModeBalanced {
		t.Errorf("default Mode = %v, want balanced", *d.Mode)
	}
	if *d.MaxSectionRevisions != 3 {
		t.Errorf("default MaxSectionRevisions = %d, want 3", *d.MaxSectionRevisions)
	}
	if *d.RefinementCap != 6 {
		t.Errorf("default RefinementCap = %d, want 6", *d.RefinementCap)
	}
	if *d.SectionDetection != SectionDetectionAuto {
		t.Errorf("default SectionDetection = %v, want auto", *d.SectionDetection)
	}
}
