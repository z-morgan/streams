package ui

import (
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/zmorgan/streams/internal/orchestrator"
	"github.com/zmorgan/streams/internal/stream"
)

type clearStatusMsg struct{}

// view tracks which view is active.
type view int

const (
	viewDashboard view = iota
	viewDetail
)

// Model is the root Bubble Tea model for the streams TUI.
type Model struct {
	orch      *orchestrator.Orchestrator
	width     int
	height    int
	view      view
	dashboard dashboardView
	detail    detailView

	// The currently selected stream ID (set when entering detail view).
	selectedID string

	// Guidance overlay state.
	showGuidance  bool
	guidanceInput textarea.Model

	// New stream overlay state.
	showNewStream  bool
	newStreamInput textinput.Model
	creating       bool // true while orch.Create is running

	// Ephemeral status message shown at bottom of dashboard.
	statusMsg string
}

// streamCreatedMsg is sent when orch.Create finishes.
type streamCreatedMsg struct {
	stream *stream.Stream
	err    error
}

// New creates a new TUI model.
func New(orch *orchestrator.Orchestrator) Model {
	ti := textarea.New()
	ti.Placeholder = "Enter guidance for this stream..."
	ti.CharLimit = 1000

	ni := textinput.New()
	ni.Placeholder = "Describe the task..."
	ni.CharLimit = 200
	ni.Width = 60

	return Model{
		orch:           orch,
		view:           viewDashboard,
		guidanceInput:  ti,
		newStreamInput: ni,
	}
}

// EventSink adapts a tea.Program to the orchestrator.EventSink interface.
type EventSink struct {
	Program *tea.Program
}

func (s *EventSink) Send(event orchestrator.Event) {
	s.Program.Send(event)
}

func (m Model) Init() tea.Cmd {
	return tea.WindowSize()
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle new stream overlay input first if active.
	if m.showNewStream {
		return m.updateNewStream(msg)
	}

	// Handle guidance overlay input first if active.
	if m.showGuidance {
		return m.updateGuidance(msg)
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch m.view {
		case viewDashboard:
			return m.updateDashboard(msg)
		case viewDetail:
			return m.updateDetail(msg)
		}

	case streamCreatedMsg:
		m.creating = false
		if msg.err != nil {
			m.statusMsg = "Error creating stream: " + msg.err.Error()
			return m, clearStatusAfter(3 * time.Second)
		}
		return m, nil

	case clearStatusMsg:
		m.statusMsg = ""
		return m, nil

	case orchestrator.Event:
		// Orchestrator events trigger a re-render (state is in the stream objects).
		return m, nil
	}

	return m, nil
}

func (m Model) updateDashboard(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	streams := m.orch.List()
	m.dashboard.clampCursor(len(streams))

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "j", "down":
		m.dashboard.cursor++
		m.dashboard.clampCursor(len(streams))

	case "k", "up":
		m.dashboard.cursor--
		m.dashboard.clampCursor(len(streams))

	case "enter":
		if st := m.selectedStream(); st != nil {
			m.selectedID = st.ID
			m.view = viewDetail
			m.detail.snapCursor = len(st.Snapshots) - 1
			if m.detail.snapCursor < 0 {
				m.detail.snapCursor = 0
			}
		} else {
			return m.setStatus("No stream selected. Press n to create one.")
		}

	case "n":
		m.showNewStream = true
		m.newStreamInput.Reset()
		m.newStreamInput.Focus()
		return m, textinput.Blink

	case "s":
		if st := m.selectedStream(); st != nil {
			if err := m.orch.Start(st.ID); err != nil {
				return m.setStatus("Start error: " + err.Error())
			}
		} else {
			return m.setStatus("No stream selected. Press n to create one.")
		}

	case "x":
		if st := m.selectedStream(); st != nil {
			m.orch.Stop(st.ID)
		} else {
			return m.setStatus("No stream selected.")
		}

	case "g":
		if st := m.selectedStream(); st != nil {
			m.selectedID = st.ID
			m.showGuidance = true
			m.guidanceInput.Reset()
			m.guidanceInput.Focus()
			return m, textarea.Blink
		}
		return m.setStatus("No stream selected.")

	}

	return m, nil
}

func (m Model) updateDetail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	st := m.orch.Get(m.selectedID)
	if st != nil {
		m.detail.clampCursor(len(st.Snapshots))
	}

	switch msg.String() {
	case "esc", "q":
		m.view = viewDashboard
		return m, nil

	case "j", "down":
		m.detail.snapCursor++
		if st != nil {
			m.detail.clampCursor(len(st.Snapshots))
		}

	case "k", "up":
		m.detail.snapCursor--
		if st != nil {
			m.detail.clampCursor(len(st.Snapshots))
		}

	case "s":
		if st != nil {
			if err := m.orch.Start(st.ID); err != nil {
				return m.setStatus("Start error: " + err.Error())
			}
		}

	case "x":
		if st != nil {
			m.orch.Stop(st.ID)
		}

	case "g":
		if st != nil {
			m.showGuidance = true
			m.guidanceInput.Reset()
			m.guidanceInput.Focus()
			return m, textarea.Blink
		}
	}

	return m, nil
}

func (m Model) updateNewStream(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			m.showNewStream = false
			return m, nil

		case "enter":
			task := m.newStreamInput.Value()
			if task == "" {
				return m, nil
			}
			m.showNewStream = false
			m.creating = true
			orch := m.orch
			return m, func() tea.Msg {
				st, err := orch.Create(task)
				return streamCreatedMsg{stream: st, err: err}
			}
		}
	}

	var cmd tea.Cmd
	m.newStreamInput, cmd = m.newStreamInput.Update(msg)
	return m, cmd
}

func (m Model) updateGuidance(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			m.showGuidance = false
			return m, nil

		case "ctrl+s":
			text := m.guidanceInput.Value()
			if text != "" && m.selectedID != "" {
				m.orch.SendGuidance(m.selectedID, text)
			}
			m.showGuidance = false
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.guidanceInput, cmd = m.guidanceInput.Update(msg)
	return m, cmd
}

func (m Model) View() string {
	if m.showNewStream {
		return renderNewStreamOverlay(m.newStreamInput, m.width, m.height)
	}

	if m.showGuidance {
		return renderGuidanceOverlay(m.guidanceInput, m.width, m.height)
	}

	switch m.view {
	case viewDashboard:
		streams := m.orch.List()
		m.dashboard.clampCursor(len(streams))
		content := renderDashboard(streams, m.dashboard.cursor, m.width, m.height)
		if m.creating {
			content += "\n\n" + helpStyle.Render("Creating stream...")
		}
		if m.statusMsg != "" {
			content += "\n\n" + helpStyle.Render(m.statusMsg)
		}
		return content

	case viewDetail:
		st := m.orch.Get(m.selectedID)
		return renderDetail(st, m.detail.snapCursor, m.width, m.height)

	default:
		return ""
	}
}

func renderNewStreamOverlay(ti textinput.Model, width, height int) string {
	overlay := titleStyle.Render("New Stream") + "\n\n"
	overlay += ti.View() + "\n\n"
	overlay += helpStyle.Render("enter: create  esc: cancel")

	maxWidth := width - 6
	if maxWidth < 40 {
		maxWidth = 40
	}

	box := overlayStyle.Width(maxWidth).Render(overlay)

	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

func renderGuidanceOverlay(ti textarea.Model, width, height int) string {
	overlay := titleStyle.Render("Guidance") + "\n\n"
	overlay += ti.View() + "\n\n"
	overlay += helpStyle.Render("ctrl+s: send  esc: cancel")

	maxWidth := width - 6
	if maxWidth < 40 {
		maxWidth = 40
	}

	box := overlayStyle.Width(maxWidth).Render(overlay)

	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

func clearStatusAfter(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg {
		return clearStatusMsg{}
	})
}

func (m Model) setStatus(msg string) (Model, tea.Cmd) {
	m.statusMsg = msg
	return m, clearStatusAfter(2 * time.Second)
}

// selectedStream returns the stream at the dashboard cursor, or nil.
func (m Model) selectedStream() *stream.Stream {
	streams := m.orch.List()
	if m.dashboard.cursor >= 0 && m.dashboard.cursor < len(streams) {
		return streams[m.dashboard.cursor]
	}
	return nil
}
