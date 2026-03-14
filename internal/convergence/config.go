// Package convergence implements phase convergence detection, section
// tracking, and tier-based issue classification for the review loop.
package convergence

// Mode controls the quality/speed tradeoff for phase convergence.
type Mode int

const (
	ModeBalanced Mode = iota // T1-T3 block; T4 advisory; relaxes after refinement cap
	ModeThorough             // all tiers block; relaxes to balanced after refinement cap
	ModeFast                 // only T1 blocks; everything else advisory
)

var modeNames = map[Mode]string{
	ModeBalanced: "balanced",
	ModeThorough: "thorough",
	ModeFast:     "fast",
}

func (m Mode) String() string {
	if s, ok := modeNames[m]; ok {
		return s
	}
	return "balanced"
}

// ParseMode converts a string to a Mode. Returns ModeBalanced for unrecognized values.
func ParseMode(s string) Mode {
	switch s {
	case "thorough":
		return ModeThorough
	case "fast":
		return ModeFast
	default:
		return ModeBalanced
	}
}

// SectionDetection controls how artifact sections are identified.
type SectionDetection int

const (
	SectionDetectionAuto     SectionDetection = iota // headings for plan/research, files for coding
	SectionDetectionHeadings                         // parse markdown ## headings
	SectionDetectionFiles                            // each file path is a section
)

var sectionDetectionNames = map[SectionDetection]string{
	SectionDetectionAuto:     "auto",
	SectionDetectionHeadings: "headings",
	SectionDetectionFiles:    "files",
}

func (sd SectionDetection) String() string {
	if s, ok := sectionDetectionNames[sd]; ok {
		return s
	}
	return "auto"
}

// ParseSectionDetection converts a string to a SectionDetection.
func ParseSectionDetection(s string) SectionDetection {
	switch s {
	case "headings":
		return SectionDetectionHeadings
	case "files":
		return SectionDetectionFiles
	default:
		return SectionDetectionAuto
	}
}

// Config holds convergence settings. Used at global, per-stream, and
// per-phase levels. Pointer fields indicate "not set" (nil) so that
// layered merging can distinguish absence from explicit zero values.
type Config struct {
	Mode                 *Mode             `json:"mode,omitempty"`
	MaxSectionRevisions  *int              `json:"max_section_revisions,omitempty"`
	RefinementCap        *int              `json:"refinement_cap,omitempty"`
	SectionDetection     *SectionDetection `json:"section_detection,omitempty"`
	Phases               map[string]Config `json:"phases,omitempty"` // per-phase overrides (stream.json only)
}

// Defaults returns the built-in default convergence configuration.
func Defaults() Config {
	mode := ModeBalanced
	maxRevisions := 3
	refinementCap := 6
	detection := SectionDetectionAuto
	return Config{
		Mode:                &mode,
		MaxSectionRevisions: &maxRevisions,
		RefinementCap:       &refinementCap,
		SectionDetection:    &detection,
	}
}

// Merge returns a new Config with non-nil fields from override taking
// precedence over base. Per-phase overrides are merged key-by-key.
func Merge(base, override Config) Config {
	result := base
	if override.Mode != nil {
		result.Mode = override.Mode
	}
	if override.MaxSectionRevisions != nil {
		result.MaxSectionRevisions = override.MaxSectionRevisions
	}
	if override.RefinementCap != nil {
		result.RefinementCap = override.RefinementCap
	}
	if override.SectionDetection != nil {
		result.SectionDetection = override.SectionDetection
	}
	if override.Phases != nil {
		if result.Phases == nil {
			result.Phases = make(map[string]Config)
		}
		for k, v := range override.Phases {
			if existing, ok := result.Phases[k]; ok {
				result.Phases[k] = Merge(existing, v)
			} else {
				result.Phases[k] = v
			}
		}
	}
	return result
}

// Resolved returns a fully resolved config with no nil fields. Applies
// per-phase override for the given phase name if one exists.
func (c Config) Resolved(phase string) ResolvedConfig {
	effective := c
	if phaseOverride, ok := c.Phases[phase]; ok {
		effective = Merge(effective, phaseOverride)
	}

	defaults := Defaults()
	rc := ResolvedConfig{
		Mode:                *defaults.Mode,
		MaxSectionRevisions: *defaults.MaxSectionRevisions,
		RefinementCap:       *defaults.RefinementCap,
		SectionDetection:    *defaults.SectionDetection,
	}
	if effective.Mode != nil {
		rc.Mode = *effective.Mode
	}
	if effective.MaxSectionRevisions != nil {
		rc.MaxSectionRevisions = *effective.MaxSectionRevisions
	}
	if effective.RefinementCap != nil {
		rc.RefinementCap = *effective.RefinementCap
	}
	if effective.SectionDetection != nil {
		rc.SectionDetection = *effective.SectionDetection
	}
	return rc
}

// ResolvedConfig holds fully resolved convergence settings with no nil fields.
type ResolvedConfig struct {
	Mode                Mode
	MaxSectionRevisions int
	RefinementCap       int
	SectionDetection    SectionDetection
}
