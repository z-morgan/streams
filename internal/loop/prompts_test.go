package loop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadPrompt_EmbeddedDefault(t *testing.T) {
	// Ensure no user override directory interferes.
	original := userPromptsDir
	userPromptsDir = func() string { return "" }
	defer func() { userPromptsDir = original }()

	data := PromptData{
		Task:      "build a widget",
		ParentID:  "parent-1",
		Iteration: 0,
	}

	prompt, err := LoadPrompt("plan", "implement", data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(prompt, "drafting a plan") {
		t.Error("expected embedded default to contain 'drafting a plan'")
	}
	if !strings.Contains(prompt, "build a widget") {
		t.Error("expected prompt to contain task description")
	}
}

func TestLoadPrompt_EmbeddedDefaultSubsequentIteration(t *testing.T) {
	original := userPromptsDir
	userPromptsDir = func() string { return "" }
	defer func() { userPromptsDir = original }()

	data := PromptData{
		Task:      "build a widget",
		ParentID:  "parent-1",
		Iteration: 1,
	}

	prompt, err := LoadPrompt("plan", "implement", data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(prompt, "revising a plan") {
		t.Error("expected subsequent iteration to contain 'revising a plan'")
	}
	if !strings.Contains(prompt, "parent-1") {
		t.Error("expected prompt to reference parent ID")
	}
}

func TestLoadPrompt_UserOverride(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(dir, "plan-implement.tmpl"),
		[]byte("Custom prompt for {{.Task}}"),
		0o644,
	); err != nil {
		t.Fatalf("failed to write override: %v", err)
	}

	original := userPromptsDir
	userPromptsDir = func() string { return dir }
	defer func() { userPromptsDir = original }()

	data := PromptData{Task: "build a widget"}

	prompt, err := LoadPrompt("plan", "implement", data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if prompt != "Custom prompt for build a widget" {
		t.Errorf("expected user override, got %q", prompt)
	}
}

func TestLoadPrompt_MissingTemplate(t *testing.T) {
	original := userPromptsDir
	userPromptsDir = func() string { return "" }
	defer func() { userPromptsDir = original }()

	_, err := LoadPrompt("nonexistent", "phase", PromptData{})
	if err == nil {
		t.Fatal("expected error for missing template")
	}
	if !strings.Contains(err.Error(), "no prompt template found") {
		t.Errorf("expected 'no prompt template found' error, got: %v", err)
	}
}

func TestLoadPrompt_RebaseTemplate(t *testing.T) {
	original := userPromptsDir
	userPromptsDir = func() string { return "" }
	defer func() { userPromptsDir = original }()

	data := PromptData{
		RebaseOutput: "CONFLICT (content): Merge conflict in main.go",
	}

	prompt, err := LoadPrompt("coding", "rebase", data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(prompt, "CONFLICT (content): Merge conflict in main.go") {
		t.Error("expected prompt to contain rebase output")
	}
	if !strings.Contains(prompt, "rebase --continue") {
		t.Error("expected prompt to instruct continuing the rebase")
	}
}

func TestLoadPrompt_OverrideDirsPrecedence(t *testing.T) {
	// Set up three override dirs: per-stream, project, and global.
	streamDir := t.TempDir()
	projectDir := t.TempDir()
	globalDir := t.TempDir()

	original := userPromptsDir
	userPromptsDir = func() string { return globalDir }
	defer func() { userPromptsDir = original }()

	// Write overrides at all three levels.
	os.WriteFile(filepath.Join(globalDir, "plan-implement.tmpl"), []byte("global: {{.Task}}"), 0o644)
	os.WriteFile(filepath.Join(projectDir, "plan-implement.tmpl"), []byte("project: {{.Task}}"), 0o644)
	os.WriteFile(filepath.Join(streamDir, "plan-implement.tmpl"), []byte("stream: {{.Task}}"), 0o644)

	data := PromptData{
		Task:         "test task",
		OverrideDirs: []string{streamDir, projectDir},
	}

	// Per-stream should win.
	prompt, err := LoadPrompt("plan", "implement", data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prompt != "stream: test task" {
		t.Errorf("expected per-stream override, got %q", prompt)
	}

	// Remove per-stream override — project should win.
	os.Remove(filepath.Join(streamDir, "plan-implement.tmpl"))
	prompt, err = LoadPrompt("plan", "implement", data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prompt != "project: test task" {
		t.Errorf("expected project override, got %q", prompt)
	}

	// Remove project override — global should win.
	os.Remove(filepath.Join(projectDir, "plan-implement.tmpl"))
	prompt, err = LoadPrompt("plan", "implement", data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prompt != "global: test task" {
		t.Errorf("expected global override, got %q", prompt)
	}

	// Remove global override — embedded default should be used.
	os.Remove(filepath.Join(globalDir, "plan-implement.tmpl"))
	prompt, err = LoadPrompt("plan", "implement", data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(prompt, "drafting a plan") {
		t.Error("expected embedded default when no overrides present")
	}
}

func TestLoadPrompt_ResearchImplementMethodologyFirst(t *testing.T) {
	original := userPromptsDir
	userPromptsDir = func() string { return "" }
	defer func() { userPromptsDir = original }()

	data := PromptData{
		Task:      "Run the app with `ruby app.rb` and probe it with chrome-devtools-mcp",
		Iteration: 0,
	}

	prompt, err := LoadPrompt("research", "implement", data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The prompt should instruct the agent to follow task-specified methodology first.
	if !strings.Contains(prompt, "follow that methodology") {
		t.Error("expected research-implement to instruct following task-specified methodology")
	}

	// The task content should appear in the rendered prompt.
	if !strings.Contains(prompt, "chrome-devtools-mcp") {
		t.Error("expected prompt to contain the task description")
	}
}

func TestLoadPrompt_ResearchReviewMethodologyCompliance(t *testing.T) {
	original := userPromptsDir
	userPromptsDir = func() string { return "" }
	defer func() { userPromptsDir = original }()

	data := PromptData{
		Task:     "Run the app and test all endpoints",
		ParentID: "test-parent",
	}

	prompt, err := LoadPrompt("research", "review", data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The review prompt should check methodology compliance.
	if !strings.Contains(prompt, "verify that the research shows evidence") {
		t.Error("expected research-review to check methodology compliance")
	}
}

func TestLoadPrompt_MalformedUserTemplate(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(dir, "plan-implement.tmpl"),
		[]byte("Bad template {{.Unknown | badFunc}}"),
		0o644,
	); err != nil {
		t.Fatalf("failed to write override: %v", err)
	}

	original := userPromptsDir
	userPromptsDir = func() string { return dir }
	defer func() { userPromptsDir = original }()

	_, err := LoadPrompt("plan", "implement", PromptData{})
	if err == nil {
		t.Fatal("expected error for malformed template")
	}
	if !strings.Contains(err.Error(), "failed to parse template") {
		t.Errorf("expected parse error, got: %v", err)
	}
}
