package convergence

import (
	"regexp"
	"strings"
)

// Tier represents an issue severity classification.
type Tier int

const (
	TierUnknown Tier = iota
	Tier1            // Correctness
	Tier2            // Completeness
	Tier3            // Design
	Tier4            // Polish
)

var tierNames = map[Tier]string{
	TierUnknown: "unknown",
	Tier1:       "T1",
	Tier2:       "T2",
	Tier3:       "T3",
	Tier4:       "T4",
}

func (t Tier) String() string {
	if s, ok := tierNames[t]; ok {
		return s
	}
	return "unknown"
}

var tierTagRe = regexp.MustCompile(`\[T([1-4])\]`)

// ParseTier extracts a tier tag from an issue title. Returns TierUnknown if
// no tag is found.
func ParseTier(title string) Tier {
	m := tierTagRe.FindStringSubmatch(title)
	if m == nil {
		return TierUnknown
	}
	switch m[1] {
	case "1":
		return Tier1
	case "2":
		return Tier2
	case "3":
		return Tier3
	case "4":
		return Tier4
	}
	return TierUnknown
}

// DefaultTier returns the default tier for untagged issues based on mode.
func DefaultTier(mode Mode) Tier {
	switch mode {
	case ModeFast:
		return Tier4 // assume polish in fast mode
	default:
		return Tier3 // assume design-level in balanced/thorough
	}
}

// IssueClassification holds the result of classifying a single issue.
type IssueClassification struct {
	ID       string
	Title    string
	Tier     Tier
	Blocking bool
	Reason   string // why it was classified this way
}

// ClassifyIssues determines which issues are blocking and which are advisory
// based on the convergence mode, refinement cap, and section tracker state.
func ClassifyIssues(issues []IssueInput, cfg ResolvedConfig, iteration int, tracker *Tracker) []IssueClassification {
	refinementCapReached := iteration >= cfg.RefinementCap

	results := make([]IssueClassification, len(issues))
	for i, issue := range issues {
		tier := ParseTier(issue.Title)
		if tier == TierUnknown {
			tier = DefaultTier(cfg.Mode)
		}

		blocking, reason := isBlocking(tier, cfg.Mode, refinementCapReached, issue, tracker)

		results[i] = IssueClassification{
			ID:       issue.ID,
			Title:    issue.Title,
			Tier:     tier,
			Blocking: blocking,
			Reason:   reason,
		}
	}
	return results
}

// IssueInput is the minimal issue data needed for classification.
type IssueInput struct {
	ID          string
	Title       string
	Description string // used for section matching
}

// isBlocking determines if an issue blocks convergence.
func isBlocking(tier Tier, mode Mode, refinementCapReached bool, issue IssueInput, tracker *Tracker) (bool, string) {
	// Check if the issue targets a frozen section.
	if tracker != nil {
		sectionID := matchIssueToSection(issue, tracker)
		if sectionID != "" && tracker.IsFrozen(sectionID) {
			if tier == Tier1 {
				return true, "T1 on frozen section (correctness always blocks)"
			}
			return false, "section " + sectionID + " is frozen"
		}
	}

	switch mode {
	case ModeThorough:
		if refinementCapReached {
			// After refinement cap, thorough drops to balanced behavior.
			if tier == Tier4 {
				return false, "T4 advisory after refinement cap (thorough → balanced)"
			}
			return true, "blocking in thorough mode (post-refinement-cap)"
		}
		return true, "blocking in thorough mode"

	case ModeFast:
		if tier == Tier1 {
			return true, "T1 always blocks in fast mode"
		}
		return false, "advisory in fast mode (only T1 blocks)"

	default: // ModeBalanced
		if refinementCapReached {
			if tier <= Tier2 {
				return true, "T1/T2 block after refinement cap"
			}
			return false, tierNames[tier] + " advisory after refinement cap"
		}
		if tier <= Tier3 {
			return true, tierNames[tier] + " blocks in balanced mode"
		}
		return false, "T4 advisory in balanced mode"
	}
}

// matchIssueToSection attempts to match an issue to a tracked section by
// looking for section headings or IDs referenced in the issue title or description.
func matchIssueToSection(issue IssueInput, tracker *Tracker) string {
	text := strings.ToLower(issue.Title + " " + issue.Description)
	for id, state := range tracker.Sections {
		// Match by section ID.
		if strings.Contains(text, strings.ToLower(id)) {
			return id
		}
		// Match by heading text (without the ## prefix).
		if state.Heading != "" {
			heading := strings.TrimSpace(strings.TrimLeft(state.Heading, "#"))
			if heading != "" && strings.Contains(text, strings.ToLower(heading)) {
				return id
			}
		}
	}
	return ""
}

// Converged returns true if there are no blocking issues.
func Converged(classifications []IssueClassification) bool {
	for _, c := range classifications {
		if c.Blocking {
			return false
		}
	}
	return true
}

// BlockingIssues returns only the blocking issues from a classification result.
func BlockingIssues(classifications []IssueClassification) []IssueClassification {
	var blocking []IssueClassification
	for _, c := range classifications {
		if c.Blocking {
			blocking = append(blocking, c)
		}
	}
	return blocking
}

// AdvisoryIssues returns only the advisory (non-blocking) issues.
func AdvisoryIssues(classifications []IssueClassification) []IssueClassification {
	var advisory []IssueClassification
	for _, c := range classifications {
		if !c.Blocking {
			advisory = append(advisory, c)
		}
	}
	return advisory
}
