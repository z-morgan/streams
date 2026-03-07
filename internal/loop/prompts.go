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
// It checks for a user override at ~/.config/streams/prompts/<phase>-<step>.tmpl,
// falling back to the embedded default.
func LoadPrompt(phase, step string, data PromptData) (string, error) {
	name := phase + "-" + step + ".tmpl"

	// Check for user override.
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
