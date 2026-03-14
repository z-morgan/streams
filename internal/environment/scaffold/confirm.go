package scaffold

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// ConfirmResult holds the user's confirmed (possibly edited) profile.
type ConfirmResult struct {
	Profile   ProjectProfile
	Confirmed bool // false if the user cancelled
}

// field identifies an editable row in the form.
type field int

const (
	fieldPort field = iota
	fieldHealthPath
	fieldDevCommand
	fieldDBAdapter
	fieldRedis
	fieldElasticsearch
	fieldMemcached
	fieldGenerate
	fieldCancel
	fieldCount // sentinel
)

// ConfirmModel is the Bubble Tea model for the scaffold confirmation form.
type ConfirmModel struct {
	profile ProjectProfile
	result  ConfirmResult

	cursor   field
	editing  bool   // true when editing a text field
	editBuf  string // buffer for the field being edited

	width  int
	height int
	done   bool
}

// NewConfirmModel creates a confirmation form pre-populated with detected values.
func NewConfirmModel(p ProjectProfile) ConfirmModel {
	return ConfirmModel{
		profile: p,
	}
}

// Result returns the confirmation result after the program exits.
func (m ConfirmModel) Result() ConfirmResult {
	return m.result
}

func (m ConfirmModel) Init() tea.Cmd {
	return nil
}

func (m ConfirmModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyPressMsg:
		if m.editing {
			return m.updateEditing(msg)
		}
		return m.updateNavigating(msg)
	}
	return m, nil
}

func (m ConfirmModel) updateNavigating(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc", "ctrl+c":
		m.result = ConfirmResult{Confirmed: false}
		m.done = true
		return m, tea.Quit

	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < fieldCount-1 {
			m.cursor++
		}
	case "tab":
		m.cursor = (m.cursor + 1) % fieldCount
	case "shift+tab":
		if m.cursor > 0 {
			m.cursor--
		} else {
			m.cursor = fieldCount - 1
		}

	case "space":
		// Toggle checkboxes
		switch m.cursor {
		case fieldRedis:
			m.profile.Redis = !m.profile.Redis
		case fieldElasticsearch:
			m.profile.Elasticsearch = !m.profile.Elasticsearch
		case fieldMemcached:
			m.profile.Memcached = !m.profile.Memcached
		}

	case "enter":
		switch m.cursor {
		case fieldPort, fieldHealthPath, fieldDevCommand, fieldDBAdapter:
			m.editing = true
			m.editBuf = m.fieldValue(m.cursor)
		case fieldGenerate:
			m.result = ConfirmResult{Profile: m.profile, Confirmed: true}
			m.done = true
			return m, tea.Quit
		case fieldCancel:
			m.result = ConfirmResult{Confirmed: false}
			m.done = true
			return m, tea.Quit
		case fieldRedis:
			m.profile.Redis = !m.profile.Redis
		case fieldElasticsearch:
			m.profile.Elasticsearch = !m.profile.Elasticsearch
		case fieldMemcached:
			m.profile.Memcached = !m.profile.Memcached
		}
	}
	return m, nil
}

func (m ConfirmModel) updateEditing(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.applyEdit()
		m.editing = false
	case "esc":
		m.editing = false
	case "backspace":
		if len(m.editBuf) > 0 {
			m.editBuf = m.editBuf[:len(m.editBuf)-1]
		}
	default:
		if len(msg.String()) == 1 {
			m.editBuf += msg.String()
		}
	}
	return m, nil
}

func (m *ConfirmModel) applyEdit() {
	switch m.cursor {
	case fieldPort:
		if port, err := strconv.Atoi(m.editBuf); err == nil && port > 0 && port <= 65535 {
			m.profile.DevPort = port
		}
	case fieldHealthPath:
		m.profile.HealthPath = m.editBuf
	case fieldDevCommand:
		m.profile.DevCommand = m.editBuf
	case fieldDBAdapter:
		m.profile.DatabaseAdapter = m.editBuf
	}
}

func (m ConfirmModel) fieldValue(f field) string {
	switch f {
	case fieldPort:
		return strconv.Itoa(m.profile.DevPort)
	case fieldHealthPath:
		return m.profile.HealthPath
	case fieldDevCommand:
		return m.profile.DevCommand
	case fieldDBAdapter:
		return m.profile.DatabaseAdapter
	default:
		return ""
	}
}

func (m ConfirmModel) View() tea.View {
	v := tea.NewView(m.viewString())
	v.AltScreen = true
	return v
}

func (m ConfirmModel) viewString() string {
	if m.done {
		return ""
	}

	var b strings.Builder

	// Title
	b.WriteString(titleStyle.Render("Environment Setup"))
	b.WriteString("\n\n")

	// Detected summary
	b.WriteString(labelStyle.Render("Detected: "))
	b.WriteString(frameworkSummary(m.profile))
	b.WriteString("\n\n")

	// Editable fields
	b.WriteString(sectionStyle.Render("Configuration"))
	b.WriteString("\n")
	b.WriteString(m.renderTextField(fieldPort, "App service port", strconv.Itoa(m.profile.DevPort)))
	b.WriteString(m.renderTextField(fieldHealthPath, "Health check path", m.profile.HealthPath))
	b.WriteString(m.renderTextField(fieldDevCommand, "Dev command", m.profile.DevCommand))
	b.WriteString(m.renderTextField(fieldDBAdapter, "Database", m.profile.DatabaseAdapter))
	b.WriteString("\n")

	// Service checkboxes
	b.WriteString(sectionStyle.Render("Additional services"))
	b.WriteString("\n")
	b.WriteString(m.renderCheckbox(fieldRedis, "Redis", m.profile.Redis))
	b.WriteString(m.renderCheckbox(fieldElasticsearch, "Elasticsearch", m.profile.Elasticsearch))
	b.WriteString(m.renderCheckbox(fieldMemcached, "Memcached", m.profile.Memcached))
	b.WriteString("\n")

	// Buttons
	b.WriteString(m.renderButton(fieldGenerate, "Generate"))
	b.WriteString("  ")
	b.WriteString(m.renderButton(fieldCancel, "Cancel"))
	b.WriteString("\n\n")

	// Help
	b.WriteString(hintStyle.Render("↑/↓: navigate  enter: edit/select  space: toggle  esc: cancel"))

	content := formBoxStyle.Render(b.String())
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
}

func (m ConfirmModel) renderTextField(f field, label, value string) string {
	cursor := "  "
	if m.cursor == f {
		cursor = cursorGlyph
	}

	if m.editing && m.cursor == f {
		return fmt.Sprintf("%s  %-22s %s▌\n", cursor, labelStyle.Render(label+":"), editingStyle.Render(m.editBuf))
	}
	return fmt.Sprintf("%s  %-22s %s\n", cursor, labelStyle.Render(label+":"), value)
}

func (m ConfirmModel) renderCheckbox(f field, label string, checked bool) string {
	cursor := "  "
	if m.cursor == f {
		cursor = cursorGlyph
	}

	check := "[ ]"
	if checked {
		check = "[x]"
	}

	row := fmt.Sprintf("%s  %s %s", cursor, check, label)
	if m.cursor == f {
		return selectedStyle.Render(row) + "\n"
	}
	return row + "\n"
}

func (m ConfirmModel) renderButton(f field, label string) string {
	if m.cursor == f {
		return activeButtonStyle.Render("[ " + label + " ]")
	}
	return buttonStyle.Render("[ " + label + " ]")
}

func frameworkSummary(p ProjectProfile) string {
	parts := []string{}

	if p.Framework != "unknown" {
		parts = append(parts, capitalize(p.Framework))
	} else if p.Language != "unknown" {
		parts = append(parts, capitalize(p.Language))
	} else {
		parts = append(parts, "Unknown stack")
	}

	if p.DatabaseAdapter != "unknown" && p.DatabaseAdapter != "none" {
		parts = append(parts, capitalize(p.DatabaseAdapter))
	}

	if p.Redis {
		parts = append(parts, "Redis")
	}
	if p.Sidekiq {
		parts = append(parts, "Sidekiq")
	}

	return strings.Join(parts, " + ")
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// Styles scoped to the confirmation form.
var (
	formTitleColor = lipgloss.Color("33")
	formMutedColor = lipgloss.Color("240")
	formAccentColor = lipgloss.Color("39")

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(formTitleColor)

	sectionStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(formMutedColor).
			MarginTop(1)

	labelStyle = lipgloss.NewStyle().
			Foreground(formMutedColor)

	selectedStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(formTitleColor)

	editingStyle = lipgloss.NewStyle().
			Underline(true)

	hintStyle = lipgloss.NewStyle().
			Foreground(formMutedColor)

	activeButtonStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(formAccentColor)

	buttonStyle = lipgloss.NewStyle().
			Foreground(formMutedColor)

	formBoxStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(formTitleColor).
			Padding(1, 2).
			Width(60)

	cursorGlyph = lipgloss.NewStyle().
			Foreground(formAccentColor).
			Bold(true).
			Render("▸ ")
)
