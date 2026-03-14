package convergence

import "strings"

// EditType classifies a section revision.
type EditType int

const (
	EditAdditive    EditType = iota // net new content
	EditSubstitutive                // lateral change, roughly equal adds/removes
)

var editTypeNames = map[EditType]string{
	EditAdditive:    "additive",
	EditSubstitutive: "substitutive",
}

func (e EditType) String() string {
	if s, ok := editTypeNames[e]; ok {
		return s
	}
	return "additive"
}

// ParseEditType converts a string to an EditType.
func ParseEditType(s string) EditType {
	if s == "substitutive" {
		return EditSubstitutive
	}
	return EditAdditive
}

// Revision records a single section edit.
type Revision struct {
	Iteration    int      `json:"iteration"`
	Type         EditType `json:"type"`
	LinesChanged int      `json:"lines_changed"`
}

// SectionState tracks the revision history and freeze state of one section.
type SectionState struct {
	Heading   string     `json:"heading"`
	FirstSeen int        `json:"first_seen"`
	Revisions []Revision `json:"revisions"`
	FrozenAt  *int       `json:"frozen_at,omitempty"`
}

// Tracker maintains per-section state across iterations.
type Tracker struct {
	Sections map[string]*SectionState `json:"sections"`
}

// NewTracker creates an empty section tracker.
func NewTracker() *Tracker {
	return &Tracker{Sections: make(map[string]*SectionState)}
}

// RecordChanges compares previous and current artifact content, detects which
// sections changed, classifies the edits, and updates the tracker state.
// Returns the list of section IDs that were newly frozen this iteration.
func (t *Tracker) RecordChanges(prevContent, curContent string, iteration int, cfg ResolvedConfig, phase string) []string {
	mode := cfg.SectionDetection
	prevSections := DetectSections(prevContent, mode, phase, nil)
	curSections := DetectSections(curContent, mode, phase, nil)

	prevLines := strings.Split(prevContent, "\n")
	curLines := strings.Split(curContent, "\n")

	// Build lookup for previous sections by ID.
	prevByID := make(map[string]Section, len(prevSections))
	for _, s := range prevSections {
		prevByID[s.ID] = s
	}

	var newlyFrozen []string

	for _, sec := range curSections {
		// Initialize section state if needed.
		state, ok := t.Sections[sec.ID]
		if !ok {
			state = &SectionState{
				Heading:   sec.Heading,
				FirstSeen: iteration,
			}
			t.Sections[sec.ID] = state
		} else {
			state.Heading = sec.Heading
		}

		// Skip if already frozen.
		if state.FrozenAt != nil {
			continue
		}

		// Get section content for comparison.
		curSecLines := extractLines(curLines, sec.StartLine, sec.EndLine)

		var prevSecLines []string
		if prev, ok := prevByID[sec.ID]; ok {
			prevSecLines = extractLines(prevLines, prev.StartLine, prev.EndLine)
		}

		// Compute diff.
		adds, removes := diffLineCount(prevSecLines, curSecLines)
		if adds == 0 && removes == 0 {
			continue // no change to this section
		}

		editType := classifyEdit(adds, removes)

		// Check for significant change reset: if >30% of section lines changed,
		// reset revision count.
		totalChanged := adds + removes
		sectionSize := max(len(curSecLines), len(prevSecLines))
		if sectionSize > 0 && float64(totalChanged)/float64(sectionSize) > 0.3 {
			// Significant structural change — only reset if this looks like
			// new content, not a lateral rewrite.
			if editType == EditAdditive && totalChanged > 5 {
				state.Revisions = nil
			}
		}

		state.Revisions = append(state.Revisions, Revision{
			Iteration:    iteration,
			Type:         editType,
			LinesChanged: adds + removes,
		})

		// Check freeze threshold.
		if len(state.Revisions) >= cfg.MaxSectionRevisions {
			frozen := iteration
			state.FrozenAt = &frozen
			newlyFrozen = append(newlyFrozen, sec.ID)
		}
	}

	return newlyFrozen
}

// RecordFileChanges is like RecordChanges but for file-based section tracking.
// Each changed file is a section.
func (t *Tracker) RecordFileChanges(changedFiles []string, iteration int, cfg ResolvedConfig) []string {
	var newlyFrozen []string

	for _, file := range changedFiles {
		state, ok := t.Sections[file]
		if !ok {
			state = &SectionState{
				Heading:   file,
				FirstSeen: iteration,
			}
			t.Sections[file] = state
		}

		if state.FrozenAt != nil {
			continue
		}

		state.Revisions = append(state.Revisions, Revision{
			Iteration:    iteration,
			Type:         EditAdditive, // for file-based tracking, all changes are additive
			LinesChanged: 0,            // we don't have per-file line counts here
		})

		if len(state.Revisions) >= cfg.MaxSectionRevisions {
			frozen := iteration
			state.FrozenAt = &frozen
			newlyFrozen = append(newlyFrozen, file)
		}
	}

	return newlyFrozen
}

// FrozenSections returns the IDs and headings of all frozen sections.
func (t *Tracker) FrozenSections() []FrozenSection {
	var frozen []FrozenSection
	for id, state := range t.Sections {
		if state.FrozenAt != nil {
			frozen = append(frozen, FrozenSection{
				ID:            id,
				Heading:       state.Heading,
				RevisionCount: len(state.Revisions),
				FrozenAt:      *state.FrozenAt,
			})
		}
	}
	return frozen
}

// IsFrozen returns true if the given section ID is frozen.
func (t *Tracker) IsFrozen(sectionID string) bool {
	state, ok := t.Sections[sectionID]
	return ok && state.FrozenAt != nil
}

// FrozenSection holds display information about a frozen section.
type FrozenSection struct {
	ID            string
	Heading       string
	RevisionCount int
	FrozenAt      int
}

// classifyEdit determines edit type from diff stats.
func classifyEdit(adds, removes int) EditType {
	if adds > 0 && removes == 0 {
		return EditAdditive
	}
	total := adds + removes
	if total == 0 {
		return EditAdditive
	}
	threshold := max(1, int(0.3*float64(total)))
	if abs(adds-removes) <= threshold {
		return EditSubstitutive
	}
	return EditAdditive
}

// diffLineCount computes a simple line-based diff between two slices of lines.
// Returns the count of added and removed lines.
func diffLineCount(prev, cur []string) (adds, removes int) {
	prevSet := make(map[string]int, len(prev))
	for _, line := range prev {
		prevSet[line]++
	}

	curSet := make(map[string]int, len(cur))
	for _, line := range cur {
		curSet[line]++
	}

	for line, count := range curSet {
		prevCount := prevSet[line]
		if count > prevCount {
			adds += count - prevCount
		}
	}

	for line, count := range prevSet {
		curCount := curSet[line]
		if count > curCount {
			removes += count - curCount
		}
	}

	return adds, removes
}

func extractLines(lines []string, start, end int) []string {
	if start < 0 {
		start = 0
	}
	if end > len(lines) {
		end = len(lines)
	}
	if start >= end {
		return nil
	}
	return lines[start:end]
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
