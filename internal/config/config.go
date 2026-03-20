// Package config handles persistent configuration for streams.
//
// Config files use a simple key = value format (one per line). Lines starting
// with # are comments. Blank lines are ignored. Keys use kebab-case to match
// CLI flag names.
//
// Precedence (highest to lowest):
//  1. CLI flags (applied by the caller after Load)
//  2. Project config — <dataDir>/config.toml
//  3. User config   — ~/.config/streams/config.toml
//  4. Built-in defaults
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/zmorgan/streams/internal/convergence"
)

// TemplateConfig defines a stream template from config files.
type TemplateConfig struct {
	Name        string
	Description string
	Phases      string // compact spec: "research,plan>decompose,coding,review,polish"
}

// Config holds all persistent settings. Zero values mean "not set" so that
// merging can distinguish between "absent" and "explicitly set to the zero
// value" (e.g. max-budget-per-step = "" to disable budget).
type Config struct {
	MaxBudgetPerStep *string // nil = not set, "" or "0" = disabled
	MaxIterations    *int    // nil = not set
	Pipeline         *string // nil = not set
	PolishSlots      *string // nil = use built-in defaults; comma-separated slot names
	Templates        []TemplateConfig
	Convergence      convergence.Config
}

// Defaults returns the built-in default configuration.
func Defaults() Config {
	budget := ""
	iterations := 10
	pipeline := "coding"
	return Config{
		MaxBudgetPerStep: &budget,
		MaxIterations:    &iterations,
		Pipeline:         &pipeline,
	}
}

// UserConfigDir returns the directory for user-level config files.
// Tests can override this via the package-level variable.
var UserConfigDir = defaultUserConfigDir

func defaultUserConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "streams")
}

// UserConfigPath returns the path to the user config file.
func UserConfigPath() string {
	dir := UserConfigDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "config.toml")
}

// ProjectConfigPath returns the path to the project config file.
func ProjectConfigPath(dataDir string) string {
	return filepath.Join(dataDir, "config.toml")
}

// Load merges user config, project config, and built-in defaults.
// The result has no nil fields — every field is resolved to a value.
func Load(dataDir string) Config {
	user := loadFile(UserConfigPath())
	project := loadFile(ProjectConfigPath(dataDir))
	return merge(Defaults(), user, project)
}

// merge applies layers in order: defaults ← user ← project.
// Each non-nil field in a later layer overrides the earlier value.
func merge(layers ...Config) Config {
	result := Config{}
	for _, layer := range layers {
		if layer.MaxBudgetPerStep != nil {
			result.MaxBudgetPerStep = layer.MaxBudgetPerStep
		}
		if layer.MaxIterations != nil {
			result.MaxIterations = layer.MaxIterations
		}
		if layer.Pipeline != nil {
			result.Pipeline = layer.Pipeline
		}
		if layer.PolishSlots != nil {
			result.PolishSlots = layer.PolishSlots
		}
		if len(layer.Templates) > 0 {
			result.Templates = append(result.Templates, layer.Templates...)
		}
		result.Convergence = convergence.Merge(result.Convergence, layer.Convergence)
	}
	return result
}

// Merge is the exported wrapper around merge for use by callers that need
// to apply additional layers (e.g. CLI flag overrides).
func Merge(base Config, overrides ...Config) Config {
	layers := make([]Config, 0, 1+len(overrides))
	layers = append(layers, base)
	layers = append(layers, overrides...)
	return merge(layers...)
}

// BudgetEnabled returns true if the resolved budget is a positive dollar
// amount (not empty, not "0", not "0.00").
func (c Config) BudgetEnabled() bool {
	if c.MaxBudgetPerStep == nil || *c.MaxBudgetPerStep == "" {
		return false
	}
	f, err := strconv.ParseFloat(*c.MaxBudgetPerStep, 64)
	if err != nil {
		return false
	}
	return f > 0
}

// loadFile reads a config file and returns a Config with only the keys
// present in the file set (non-nil). Returns a zero Config if the file
// doesn't exist or can't be read.
func loadFile(path string) Config {
	if path == "" {
		return Config{}
	}
	f, err := os.Open(path)
	if err != nil {
		return Config{}
	}
	defer f.Close()
	return parse(f)
}

// LoadFile is the exported version of loadFile for use by the config
// subcommand and tests.
func LoadFile(path string) Config {
	return loadFile(path)
}

// parse reads key = value lines from a reader. It handles:
//   - # comments
//   - blank lines
//   - quoted and unquoted values
//   - kebab-case keys matching CLI flag names
//   - [[template]] array-of-tables sections
func parse(r *os.File) Config {
	cfg := Config{}
	scanner := bufio.NewScanner(r)
	section := ""                       // current TOML table, e.g. "convergence"
	var currentTemplate *TemplateConfig // non-nil when inside a [[template]] block
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Handle TOML table headers like [convergence] and [[template]].
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			// Flush pending template.
			if currentTemplate != nil {
				cfg.Templates = append(cfg.Templates, *currentTemplate)
				currentTemplate = nil
			}
			inner := strings.TrimSpace(line[1 : len(line)-1])
			if strings.HasPrefix(inner, "[") && strings.HasSuffix(inner, "]") {
				// Array-of-tables: [[template]]
				arrayName := strings.TrimSpace(inner[1 : len(inner)-1])
				if arrayName == "template" {
					currentTemplate = &TemplateConfig{}
					section = ""
				}
			} else {
				section = inner
			}
			continue
		}

		key, value, ok := parseLine(line)
		if !ok {
			continue
		}

		// Inside a [[template]] block.
		if currentTemplate != nil {
			switch key {
			case "name":
				currentTemplate.Name = value
			case "description":
				currentTemplate.Description = value
			case "phases":
				currentTemplate.Phases = value
			}
			continue
		}

		// Prefix key with section if inside a table.
		if section != "" {
			key = section + "." + key
		}

		switch key {
		case "max-budget-per-step":
			cfg.MaxBudgetPerStep = &value
		case "max-iterations":
			if n, err := strconv.Atoi(value); err == nil {
				cfg.MaxIterations = &n
			}
		case "pipeline":
			cfg.Pipeline = &value
		case "polish-slots":
			cfg.PolishSlots = &value
		case "convergence.mode":
			m := convergence.ParseMode(value)
			cfg.Convergence.Mode = &m
		case "convergence.max-section-revisions":
			if n, err := strconv.Atoi(value); err == nil {
				cfg.Convergence.MaxSectionRevisions = &n
			}
		case "convergence.refinement-cap":
			if n, err := strconv.Atoi(value); err == nil {
				cfg.Convergence.RefinementCap = &n
			}
		case "convergence.section-detection":
			sd := convergence.ParseSectionDetection(value)
			cfg.Convergence.SectionDetection = &sd
		}
	}
	// Flush last template if file ends inside a [[template]] block.
	if currentTemplate != nil {
		cfg.Templates = append(cfg.Templates, *currentTemplate)
	}
	return cfg
}

// parseLine splits "key = value" and strips optional quotes from value.
func parseLine(line string) (key, value string, ok bool) {
	idx := strings.IndexByte(line, '=')
	if idx < 0 {
		return "", "", false
	}
	key = strings.TrimSpace(line[:idx])
	value = strings.TrimSpace(line[idx+1:])

	// Strip matching quotes.
	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') ||
			(value[0] == '\'' && value[len(value)-1] == '\'') {
			value = value[1 : len(value)-1]
		}
	}
	return key, value, true
}

// WriteFile writes a Config to disk in the key = value format. Only non-nil
// fields are written. The file is created (with parent dirs) if it doesn't
// exist, or overwritten if it does.
func WriteFile(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	var buf strings.Builder
	if cfg.MaxBudgetPerStep != nil {
		fmt.Fprintf(&buf, "max-budget-per-step = %q\n", *cfg.MaxBudgetPerStep)
	}
	if cfg.MaxIterations != nil {
		fmt.Fprintf(&buf, "max-iterations = %d\n", *cfg.MaxIterations)
	}
	if cfg.Pipeline != nil {
		fmt.Fprintf(&buf, "pipeline = %q\n", *cfg.Pipeline)
	}
	if cfg.PolishSlots != nil {
		fmt.Fprintf(&buf, "polish-slots = %q\n", *cfg.PolishSlots)
	}

	c := cfg.Convergence
	if c.Mode != nil || c.MaxSectionRevisions != nil || c.RefinementCap != nil || c.SectionDetection != nil {
		buf.WriteString("\n[convergence]\n")
		if c.Mode != nil {
			fmt.Fprintf(&buf, "mode = %q\n", c.Mode.String())
		}
		if c.MaxSectionRevisions != nil {
			fmt.Fprintf(&buf, "max-section-revisions = %d\n", *c.MaxSectionRevisions)
		}
		if c.RefinementCap != nil {
			fmt.Fprintf(&buf, "refinement-cap = %d\n", *c.RefinementCap)
		}
		if c.SectionDetection != nil {
			fmt.Fprintf(&buf, "section-detection = %q\n", c.SectionDetection.String())
		}
	}
	return os.WriteFile(path, []byte(buf.String()), 0o644)
}

// SetInFile reads an existing config file, sets or updates a single key, and
// writes the file back. Creates the file if it doesn't exist.
func SetInFile(path, key, value string) error {
	cfg := loadFile(path)
	switch key {
	case "max-budget-per-step":
		cfg.MaxBudgetPerStep = &value
	case "max-iterations":
		if _, err := strconv.Atoi(value); err != nil {
			return fmt.Errorf("max-iterations must be an integer, got %q", value)
		}
		n, _ := strconv.Atoi(value)
		cfg.MaxIterations = &n
	case "pipeline":
		cfg.Pipeline = &value
	case "polish-slots":
		cfg.PolishSlots = &value
	case "convergence.mode":
		m := convergence.ParseMode(value)
		cfg.Convergence.Mode = &m
	case "convergence.max-section-revisions":
		if _, err := strconv.Atoi(value); err != nil {
			return fmt.Errorf("convergence.max-section-revisions must be an integer, got %q", value)
		}
		n, _ := strconv.Atoi(value)
		cfg.Convergence.MaxSectionRevisions = &n
	case "convergence.refinement-cap":
		if _, err := strconv.Atoi(value); err != nil {
			return fmt.Errorf("convergence.refinement-cap must be an integer, got %q", value)
		}
		n, _ := strconv.Atoi(value)
		cfg.Convergence.RefinementCap = &n
	case "convergence.section-detection":
		sd := convergence.ParseSectionDetection(value)
		cfg.Convergence.SectionDetection = &sd
	default:
		return fmt.Errorf("unknown config key: %q", key)
	}
	return WriteFile(path, cfg)
}

// Format returns a human-readable representation of a resolved config.
func Format(cfg Config) string {
	var buf strings.Builder
	if cfg.MaxBudgetPerStep != nil {
		if *cfg.MaxBudgetPerStep == "" || *cfg.MaxBudgetPerStep == "0" {
			fmt.Fprintf(&buf, "max-budget-per-step = \"\" (disabled)\n")
		} else {
			fmt.Fprintf(&buf, "max-budget-per-step = %q\n", *cfg.MaxBudgetPerStep)
		}
	}
	if cfg.MaxIterations != nil {
		fmt.Fprintf(&buf, "max-iterations = %d\n", *cfg.MaxIterations)
	}
	if cfg.Pipeline != nil {
		fmt.Fprintf(&buf, "pipeline = %q\n", *cfg.Pipeline)
	}
	if cfg.PolishSlots != nil {
		fmt.Fprintf(&buf, "polish-slots = %q\n", *cfg.PolishSlots)
	}

	c := cfg.Convergence
	if c.Mode != nil || c.MaxSectionRevisions != nil || c.RefinementCap != nil || c.SectionDetection != nil {
		buf.WriteString("\n[convergence]\n")
		if c.Mode != nil {
			fmt.Fprintf(&buf, "mode = %q\n", c.Mode.String())
		}
		if c.MaxSectionRevisions != nil {
			fmt.Fprintf(&buf, "max-section-revisions = %d\n", *c.MaxSectionRevisions)
		}
		if c.RefinementCap != nil {
			fmt.Fprintf(&buf, "refinement-cap = %d\n", *c.RefinementCap)
		}
		if c.SectionDetection != nil {
			fmt.Fprintf(&buf, "section-detection = %q\n", c.SectionDetection.String())
		}
	}
	return buf.String()
}

// ValidKeys returns the list of recognized config keys.
func ValidKeys() []string {
	return []string{
		"max-budget-per-step", "max-iterations", "pipeline", "polish-slots",
		"convergence.mode", "convergence.max-section-revisions",
		"convergence.refinement-cap", "convergence.section-detection",
	}
}
