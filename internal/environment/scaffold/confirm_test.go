package scaffold

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func keyPress(code rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code, Text: string(code)}
}

func specialKey(code rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code}
}

func TestConfirmModel_Navigate(t *testing.T) {
	m := NewConfirmModel(ProjectProfile{
		Framework:       "rails",
		DevPort:         3000,
		HealthPath:      "/up",
		DevCommand:      "bin/rails server",
		DatabaseAdapter: "postgresql",
	})

	assertEqual(t, "initial cursor", int(m.cursor), int(fieldPort))

	// Move down with j
	result, _ := m.Update(keyPress('j'))
	m = result.(ConfirmModel)
	assertEqual(t, "after j", int(m.cursor), int(fieldHealthPath))

	// Move up with k
	result, _ = m.Update(keyPress('k'))
	m = result.(ConfirmModel)
	assertEqual(t, "after k", int(m.cursor), int(fieldPort))

	// Can't go above first field
	result, _ = m.Update(keyPress('k'))
	m = result.(ConfirmModel)
	assertEqual(t, "at top", int(m.cursor), int(fieldPort))
}

func TestConfirmModel_ToggleCheckbox(t *testing.T) {
	m := NewConfirmModel(ProjectProfile{
		Framework: "rails",
		Redis:     false,
		DevPort:   3000,
	})

	m.cursor = fieldRedis
	assertFalse(t, "redis initially", m.profile.Redis)

	// Toggle with space
	result, _ := m.Update(keyPress(' '))
	m = result.(ConfirmModel)
	assertTrue(t, "redis after space", m.profile.Redis)

	// Toggle again
	result, _ = m.Update(keyPress(' '))
	m = result.(ConfirmModel)
	assertFalse(t, "redis after second space", m.profile.Redis)
}

func TestConfirmModel_EditField(t *testing.T) {
	m := NewConfirmModel(ProjectProfile{
		Framework:  "rails",
		DevPort:    3000,
		HealthPath: "/up",
	})

	m.cursor = fieldHealthPath
	result, _ := m.Update(specialKey(tea.KeyEnter))
	m = result.(ConfirmModel)
	assertTrue(t, "editing", m.editing)
	assertEqual(t, "editBuf", m.editBuf, "/up")

	// Backspace three times to clear
	for range 3 {
		result, _ = m.Update(specialKey(tea.KeyBackspace))
		m = result.(ConfirmModel)
	}

	// Type "/"
	result, _ = m.Update(keyPress('/'))
	m = result.(ConfirmModel)

	// Confirm
	result, _ = m.Update(specialKey(tea.KeyEnter))
	m = result.(ConfirmModel)
	assertFalse(t, "editing after enter", m.editing)
	assertEqual(t, "health path", m.profile.HealthPath, "/")
}

func TestConfirmModel_EditPort(t *testing.T) {
	m := NewConfirmModel(ProjectProfile{
		Framework: "rails",
		DevPort:   3000,
	})

	m.cursor = fieldPort
	result, _ := m.Update(specialKey(tea.KeyEnter))
	m = result.(ConfirmModel)

	// Clear
	for range 4 {
		result, _ = m.Update(specialKey(tea.KeyBackspace))
		m = result.(ConfirmModel)
	}
	// Type 4000
	for _, ch := range "4000" {
		result, _ = m.Update(keyPress(ch))
		m = result.(ConfirmModel)
	}

	result, _ = m.Update(specialKey(tea.KeyEnter))
	m = result.(ConfirmModel)
	assertEqual(t, "port", m.profile.DevPort, 4000)
}

func TestConfirmModel_Generate(t *testing.T) {
	m := NewConfirmModel(ProjectProfile{
		Framework: "rails",
		DevPort:   3000,
	})

	m.cursor = fieldGenerate
	result, cmd := m.Update(specialKey(tea.KeyEnter))
	m = result.(ConfirmModel)

	assertTrue(t, "confirmed", m.Result().Confirmed)
	assertEqual(t, "framework", m.Result().Profile.Framework, "rails")
	if cmd == nil {
		t.Error("expected tea.Quit command")
	}
}

func TestConfirmModel_Cancel(t *testing.T) {
	m := NewConfirmModel(ProjectProfile{
		Framework: "rails",
		DevPort:   3000,
	})

	m.cursor = fieldCancel
	result, cmd := m.Update(specialKey(tea.KeyEnter))
	m = result.(ConfirmModel)

	assertFalse(t, "confirmed", m.Result().Confirmed)
	if cmd == nil {
		t.Error("expected tea.Quit command")
	}
}

func TestConfirmModel_EscCancels(t *testing.T) {
	m := NewConfirmModel(ProjectProfile{Framework: "rails", DevPort: 3000})

	result, cmd := m.Update(specialKey(tea.KeyEscape))
	m = result.(ConfirmModel)

	assertFalse(t, "confirmed", m.Result().Confirmed)
	if cmd == nil {
		t.Error("expected tea.Quit command")
	}
}

func TestConfirmModel_EscCancelsEdit(t *testing.T) {
	m := NewConfirmModel(ProjectProfile{
		Framework:  "rails",
		DevPort:    3000,
		HealthPath: "/up",
	})

	// Enter edit mode
	m.cursor = fieldHealthPath
	result, _ := m.Update(specialKey(tea.KeyEnter))
	m = result.(ConfirmModel)
	assertTrue(t, "editing", m.editing)

	// Type something
	result, _ = m.Update(keyPress('x'))
	m = result.(ConfirmModel)

	// Esc cancels edit, preserves original value
	result, _ = m.Update(specialKey(tea.KeyEscape))
	m = result.(ConfirmModel)
	assertFalse(t, "editing after esc", m.editing)
	assertEqual(t, "health path unchanged", m.profile.HealthPath, "/up")
}

func TestFrameworkSummary(t *testing.T) {
	tests := []struct {
		name    string
		profile ProjectProfile
		want    string
	}{
		{
			name:    "rails with pg and redis",
			profile: ProjectProfile{Framework: "rails", DatabaseAdapter: "postgresql", Redis: true},
			want:    "Rails + Postgresql + Redis",
		},
		{
			name:    "unknown framework known language",
			profile: ProjectProfile{Framework: "unknown", Language: "ruby", DatabaseAdapter: "unknown"},
			want:    "Ruby",
		},
		{
			name:    "fully unknown",
			profile: ProjectProfile{Framework: "unknown", Language: "unknown", DatabaseAdapter: "unknown"},
			want:    "Unknown stack",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := frameworkSummary(tt.profile)
			assertEqual(t, "summary", got, tt.want)
		})
	}
}

func TestConfirmModel_View(t *testing.T) {
	m := NewConfirmModel(ProjectProfile{
		Framework:       "rails",
		Language:        "ruby",
		DatabaseAdapter: "postgresql",
		Redis:           true,
		DevPort:         3000,
		DevCommand:      "bin/rails server",
		HealthPath:      "/up",
	})

	result, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	m = result.(ConfirmModel)

	view := m.viewString()
	assertContains(t, view, "Environment Setup")
	assertContains(t, view, "Rails + Postgresql + Redis")
	assertContains(t, view, "3000")
	assertContains(t, view, "/up")
	assertContains(t, view, "bin/rails server")
	assertContains(t, view, "[x] Redis")
	assertContains(t, view, "[ ] Elasticsearch")
	assertContains(t, view, "Generate")
	assertContains(t, view, "Cancel")
}

func assertFalse(t *testing.T, field string, got bool) {
	t.Helper()
	if got {
		t.Errorf("%s: got true, want false", field)
	}
}
