package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaults(t *testing.T) {
	d := Defaults()
	if d.MaxBudgetPerStep == nil || *d.MaxBudgetPerStep != "" {
		t.Errorf("expected default budget empty (disabled), got %v", d.MaxBudgetPerStep)
	}
	if d.MaxIterations == nil || *d.MaxIterations != 10 {
		t.Errorf("expected default iterations 10, got %v", d.MaxIterations)
	}
	if d.Pipeline == nil || *d.Pipeline != "coding" {
		t.Errorf("expected default pipeline coding, got %v", d.Pipeline)
	}
}

func TestParseLine(t *testing.T) {
	tests := []struct {
		line    string
		key     string
		value   string
		wantOK  bool
	}{
		{`max-budget-per-step = "5.00"`, "max-budget-per-step", "5.00", true},
		{`max-iterations = 10`, "max-iterations", "10", true},
		{`pipeline = 'coding'`, "pipeline", "coding", true},
		{`key=value`, "key", "value", true},
		{`key = `, "key", "", true},
		{`no-equals-sign`, "", "", false},
		{`# comment`, "", "", false},
	}
	for _, tt := range tests {
		key, value, ok := parseLine(tt.line)
		if ok != tt.wantOK {
			t.Errorf("parseLine(%q) ok = %v, want %v", tt.line, ok, tt.wantOK)
			continue
		}
		if !ok {
			continue
		}
		if key != tt.key || value != tt.value {
			t.Errorf("parseLine(%q) = (%q, %q), want (%q, %q)", tt.line, key, value, tt.key, tt.value)
		}
	}
}

func TestLoadFileMissing(t *testing.T) {
	cfg := LoadFile("/nonexistent/path/config.toml")
	if cfg.MaxBudgetPerStep != nil || cfg.MaxIterations != nil || cfg.Pipeline != nil {
		t.Error("expected all nil fields for missing file")
	}
}

func TestLoadFileEmptyPath(t *testing.T) {
	cfg := LoadFile("")
	if cfg.MaxBudgetPerStep != nil || cfg.MaxIterations != nil || cfg.Pipeline != nil {
		t.Error("expected all nil fields for empty path")
	}
}

func TestLoadFilePartial(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	os.WriteFile(path, []byte("max-budget-per-step = \"10.00\"\n"), 0o644)

	cfg := LoadFile(path)
	if cfg.MaxBudgetPerStep == nil || *cfg.MaxBudgetPerStep != "10.00" {
		t.Errorf("expected budget 10.00, got %v", cfg.MaxBudgetPerStep)
	}
	if cfg.MaxIterations != nil {
		t.Error("expected nil MaxIterations for partial config")
	}
	if cfg.Pipeline != nil {
		t.Error("expected nil Pipeline for partial config")
	}
}

func TestMergePrecedence(t *testing.T) {
	budget5 := "5.00"
	budget10 := "10.00"
	iter10 := 10
	iter20 := 20
	pipeCoding := "coding"
	pipeResearch := "research,plan,coding"

	defaults := Config{
		MaxBudgetPerStep: &budget5,
		MaxIterations:    &iter10,
		Pipeline:         &pipeCoding,
	}
	user := Config{
		MaxBudgetPerStep: &budget10,
		// MaxIterations not set — should keep default
	}
	project := Config{
		MaxIterations: &iter20,
		Pipeline:      &pipeResearch,
		// MaxBudgetPerStep not set — should keep user override
	}

	result := Merge(defaults, user, project)

	if result.MaxBudgetPerStep == nil || *result.MaxBudgetPerStep != "10.00" {
		t.Errorf("expected budget from user layer (10.00), got %v", result.MaxBudgetPerStep)
	}
	if result.MaxIterations == nil || *result.MaxIterations != 20 {
		t.Errorf("expected iterations from project layer (20), got %v", result.MaxIterations)
	}
	if result.Pipeline == nil || *result.Pipeline != "research,plan,coding" {
		t.Errorf("expected pipeline from project layer, got %v", result.Pipeline)
	}
}

func TestMergeCLIOverridesAll(t *testing.T) {
	budget5 := "5.00"
	budgetZero := "0"
	iter10 := 10
	pipeCoding := "coding"

	base := Config{
		MaxBudgetPerStep: &budget5,
		MaxIterations:    &iter10,
		Pipeline:         &pipeCoding,
	}
	cliOverride := Config{
		MaxBudgetPerStep: &budgetZero,
	}

	result := Merge(base, cliOverride)
	if result.MaxBudgetPerStep == nil || *result.MaxBudgetPerStep != "0" {
		t.Errorf("expected CLI override to win, got %v", result.MaxBudgetPerStep)
	}
	// Other fields should be preserved from base.
	if result.MaxIterations == nil || *result.MaxIterations != 10 {
		t.Errorf("expected base iterations preserved, got %v", result.MaxIterations)
	}
}

func TestBudgetEnabled(t *testing.T) {
	tests := []struct {
		name    string
		budget  *string
		enabled bool
	}{
		{"nil", nil, false},
		{"empty", strPtr(""), false},
		{"zero", strPtr("0"), false},
		{"zero-decimal", strPtr("0.00"), false},
		{"positive", strPtr("5.00"), true},
		{"invalid", strPtr("abc"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{MaxBudgetPerStep: tt.budget}
			if got := cfg.BudgetEnabled(); got != tt.enabled {
				t.Errorf("BudgetEnabled() = %v, want %v", got, tt.enabled)
			}
		})
	}
}

func TestLoadIntegration(t *testing.T) {
	// Set up temp dirs for user and project config.
	userDir := t.TempDir()
	projectDir := t.TempDir()

	// Override UserConfigDir for this test.
	original := UserConfigDir
	UserConfigDir = func() string { return userDir }
	defer func() { UserConfigDir = original }()

	// Write user config with custom budget.
	os.WriteFile(filepath.Join(userDir, "config.toml"), []byte(
		"max-budget-per-step = \"8.00\"\nmax-iterations = 15\n",
	), 0o644)

	// Write project config overriding iterations only.
	os.WriteFile(filepath.Join(projectDir, "config.toml"), []byte(
		"max-iterations = 25\n",
	), 0o644)

	cfg := Load(projectDir)

	// Budget: user config (8.00) wins over default (5.00).
	if cfg.MaxBudgetPerStep == nil || *cfg.MaxBudgetPerStep != "8.00" {
		t.Errorf("expected budget 8.00 from user config, got %v", cfg.MaxBudgetPerStep)
	}
	// Iterations: project config (25) wins over user (15).
	if cfg.MaxIterations == nil || *cfg.MaxIterations != 25 {
		t.Errorf("expected iterations 25 from project config, got %v", cfg.MaxIterations)
	}
	// Pipeline: default (coding) since neither file sets it.
	if cfg.Pipeline == nil || *cfg.Pipeline != "coding" {
		t.Errorf("expected default pipeline, got %v", cfg.Pipeline)
	}
}

func TestWriteAndLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	budget := "3.50"
	iterations := 7
	pipeline := "research,coding"
	cfg := Config{
		MaxBudgetPerStep: &budget,
		MaxIterations:    &iterations,
		Pipeline:         &pipeline,
	}

	if err := WriteFile(path, cfg); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	loaded := LoadFile(path)
	if loaded.MaxBudgetPerStep == nil || *loaded.MaxBudgetPerStep != "3.50" {
		t.Errorf("round-trip budget: got %v", loaded.MaxBudgetPerStep)
	}
	if loaded.MaxIterations == nil || *loaded.MaxIterations != 7 {
		t.Errorf("round-trip iterations: got %v", loaded.MaxIterations)
	}
	if loaded.Pipeline == nil || *loaded.Pipeline != "research,coding" {
		t.Errorf("round-trip pipeline: got %v", loaded.Pipeline)
	}
}

func TestSetInFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	// Set budget on a new file.
	if err := SetInFile(path, "max-budget-per-step", "12.00"); err != nil {
		t.Fatalf("SetInFile budget: %v", err)
	}

	// Set iterations in the same file.
	if err := SetInFile(path, "max-iterations", "30"); err != nil {
		t.Fatalf("SetInFile iterations: %v", err)
	}

	cfg := LoadFile(path)
	if cfg.MaxBudgetPerStep == nil || *cfg.MaxBudgetPerStep != "12.00" {
		t.Errorf("expected budget 12.00, got %v", cfg.MaxBudgetPerStep)
	}
	if cfg.MaxIterations == nil || *cfg.MaxIterations != 30 {
		t.Errorf("expected iterations 30, got %v", cfg.MaxIterations)
	}
}

func TestSetInFileValidation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	if err := SetInFile(path, "max-iterations", "abc"); err == nil {
		t.Error("expected error for non-integer max-iterations")
	}
	if err := SetInFile(path, "unknown-key", "value"); err == nil {
		t.Error("expected error for unknown key")
	}
}

func TestWriteFilePartial(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	budget := "2.00"
	cfg := Config{MaxBudgetPerStep: &budget}

	if err := WriteFile(path, cfg); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	content, _ := os.ReadFile(path)
	s := string(content)
	if !contains(s, "max-budget-per-step") {
		t.Error("expected budget key in output")
	}
	if contains(s, "max-iterations") {
		t.Error("expected no iterations key for nil field")
	}
	if contains(s, "pipeline") {
		t.Error("expected no pipeline key for nil field")
	}
}

func TestFormat(t *testing.T) {
	budget := "5.00"
	iterations := 10
	pipeline := "coding"
	cfg := Config{
		MaxBudgetPerStep: &budget,
		MaxIterations:    &iterations,
		Pipeline:         &pipeline,
	}
	out := Format(cfg)
	if !contains(out, `"5.00"`) || !contains(out, "10") || !contains(out, `"coding"`) {
		t.Errorf("Format output missing expected values: %s", out)
	}
}

func TestFormatDisabledBudget(t *testing.T) {
	empty := ""
	cfg := Config{MaxBudgetPerStep: &empty}
	out := Format(cfg)
	if !contains(out, "(disabled)") {
		t.Errorf("expected (disabled) annotation, got: %s", out)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func strPtr(s string) *string { return &s }
