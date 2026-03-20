package loop

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

//go:embed prompts/*.tmpl
var embeddedPrompts embed.FS

// PromptData holds the template variables available to all prompt templates.
type PromptData struct {
	Task         string
	ParentID     string
	Iteration    int
	OrderedSteps string
	RebaseOutput string // stderr/stdout from a failed autosquash rebase; only set for the rebase prompt.

	// Review phase fields (populated only by ReviewPhase.ImplementPrompt).
	CommitLog         string
	DiffStat          string
	TotalCost         string
	SnapshotSummaries string

	// Polish phase fields.
	BaseSHA string // stream's rebase target, used by polish templates for git commands
	Commits string // pre-formatted per-commit sections for commit-scoped slots

	// Step-coding phase fields.
	PlanContent       string // full plan.md content
	AllStepsFormatted string // "[done] Step 1 — Title\n[current] Step 2 — Title\n..."
	CurrentStep       string // title of current step (step mode)
	CurrentStepID     string // bead ID of current step
	IsFixMode         bool
	ReviewBeadsList   string // formatted list of open review beads

	// OverrideDirs are checked in order before the global user prompts dir.
	// Typically: [per-stream dir, project dir].
	OverrideDirs []string
}

// userPromptsDir returns the directory to check for user prompt overrides.
// Tests override this to use a temp directory.
var userPromptsDir = defaultUserPromptsDir

func defaultUserPromptsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "streams", "prompts")
}

// LoadPrompt loads and renders a prompt template for the given phase and step.
// It checks override directories in order (per-stream, project), then the global
// user override at ~/.config/streams/prompts/, falling back to the embedded default.
func LoadPrompt(phase, step string, data PromptData) (string, error) {
	name := phase + "-" + step + ".tmpl"

	// Check override dirs in order (per-stream → project).
	for _, dir := range data.OverrideDirs {
		if dir == "" {
			continue
		}
		overridePath := filepath.Join(dir, name)
		if content, err := os.ReadFile(overridePath); err == nil {
			return renderTemplate(name, string(content), data)
		}
	}

	// Check global user override.
	dir := userPromptsDir()
	if dir != "" {
		userPath := filepath.Join(dir, name)
		if content, err := os.ReadFile(userPath); err == nil {
			return renderTemplate(name, string(content), data)
		}
	}

	// Fall back to embedded default.
	content, err := embeddedPrompts.ReadFile("prompts/" + name)
	if err != nil {
		return "", fmt.Errorf("no prompt template found for %s-%s: %w", phase, step, err)
	}
	return renderTemplate(name, string(content), data)
}

// ListPromptNames returns the names of all embedded prompt templates (without .tmpl extension).
func ListPromptNames() []string {
	entries, _ := embeddedPrompts.ReadDir("prompts")
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, ".tmpl") {
			names = append(names, strings.TrimSuffix(name, ".tmpl"))
		}
	}
	return names
}

// ExportPrompt returns the raw content of an embedded prompt template.
func ExportPrompt(name string) (string, error) {
	content, err := embeddedPrompts.ReadFile("prompts/" + name + ".tmpl")
	if err != nil {
		return "", fmt.Errorf("unknown prompt template: %s", name)
	}
	return string(content), nil
}

// GlobalPromptsDir returns the global user prompt override directory path.
func GlobalPromptsDir() string {
	return userPromptsDir()
}

func renderTemplate(name, content string, data PromptData) (string, error) {
	tmpl, err := template.New(name).Parse(content)
	if err != nil {
		return "", fmt.Errorf("failed to parse template %s: %w", name, err)
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute template %s: %w", name, err)
	}
	return buf.String(), nil
}
