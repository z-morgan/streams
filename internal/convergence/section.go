package convergence

import (
	"regexp"
	"strings"
	"unicode"
)

// Section represents a detected section of an artifact.
type Section struct {
	ID      string // slugified heading or file path
	Heading string // original heading text (empty for file-based sections)
	StartLine int  // 0-indexed start line in the artifact
	EndLine   int  // 0-indexed end line (exclusive)
}

var headingRe = regexp.MustCompile(`^##\s+(.+)`)

// DetectSections identifies sections in an artifact based on the detection mode.
// For headings mode, parses markdown ## headings. Subsections (###, ####) are
// grouped under their parent ## section.
// For files mode, each entry in changedFiles becomes a section.
func DetectSections(content string, mode SectionDetection, phase string, changedFiles []string) []Section {
	effective := mode
	if effective == SectionDetectionAuto {
		switch phase {
		case "research", "plan", "decompose":
			effective = SectionDetectionHeadings
		default:
			effective = SectionDetectionFiles
		}
	}

	if effective == SectionDetectionFiles {
		return detectFileSections(changedFiles)
	}
	return detectHeadingSections(content)
}

func detectFileSections(files []string) []Section {
	sections := make([]Section, len(files))
	for i, f := range files {
		sections[i] = Section{
			ID:      f,
			Heading: f,
		}
	}
	return sections
}

func detectHeadingSections(content string) []Section {
	lines := strings.Split(content, "\n")
	var sections []Section
	var current *Section

	for i, line := range lines {
		if m := headingRe.FindStringSubmatch(line); m != nil {
			// Close previous section.
			if current != nil {
				current.EndLine = i
				sections = append(sections, *current)
			}
			current = &Section{
				ID:        slugify(m[1]),
				Heading:   line,
				StartLine: i,
			}
		}
	}

	// Close last section.
	if current != nil {
		current.EndLine = len(lines)
		sections = append(sections, *current)
	}

	// If no headings found, the entire content is one section.
	if len(sections) == 0 && len(strings.TrimSpace(content)) > 0 {
		sections = append(sections, Section{
			ID:        "document",
			Heading:   "",
			StartLine: 0,
			EndLine:   len(lines),
		})
	}

	return sections
}

// slugify converts heading text to a URL-friendly ID.
func slugify(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			prevDash = false
		} else if !prevDash && b.Len() > 0 {
			b.WriteByte('-')
			prevDash = true
		}
	}
	result := b.String()
	return strings.TrimRight(result, "-")
}
