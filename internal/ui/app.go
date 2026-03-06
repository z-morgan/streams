package ui

import (
	"fmt"
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
	viewTail
)

// Model is the root Bubble Tea model for the streams TUI.
type Model struct {
	orch      *orchestrator.Orchestrator
	width     int
	height    int
	view      view
	dashboard dashboardView
	detail    detailView
	tail      tailView

	// The currently selected stream ID (set when entering detail view).
	selectedID string

	// Guidance overlay state.
	showGuidance  bool
	guidanceInput textarea.Model

	// New stream overlay state.
	showNewStream  bool
	newStreamInput textinput.Model
	creating       bool // true while orch.Create is running

	// Delete confirmation overlay state.
	showDeleteConfirm bool
	deleteTargetID    string

	// Beads init prompt state.
	showBeadsInit bool
	pendingTask   string // task stashed while waiting for stealth answer

	// Ephemeral status message shown at bottom of dashboard.
	statusMsg string

	// Persistent error message, cleared on next keypress.
	errorMsg string
}

// streamCreatedMsg is sent when orch.Create finishes.
type streamCreatedMsg struct {
	stream *stream.Stream
	err    error
}

// streamDeletedMsg is sent when orch.Delete finishes.
type streamDeletedMsg struct {
	err error
}

// beadsInitDoneMsg is sent when bd init finishes.
type beadsInitDoneMsg struct {
	err error
}

// New creates a new TUI model.
func New(orch *orchestrator.Orchestrator) Model {
	ti := textarea.New()
	ti.Placeholder = "Enter guidance for this stream..."
	ti.CharLimit = 1000

	ni := textinput.New()
	ni.Placeholder = "Describe the task..."
	ni.CharLimit = 200

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
	// Handle window resize globally before overlays.
	if msg, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = msg.Width
		m.height = msg.Height
		m.newStreamInput.Width = m.inputWidth()
		return m, nil
	}

	// Clear persistent error on any keypress.
	if _, ok := msg.(tea.KeyMsg); ok {
		m.errorMsg = ""
	}

	// Handle delete confirmation overlay if active.
	if m.showDeleteConfirm {
		return m.updateDeleteConfirm(msg)
	}

	// Handle beads init prompt if active.
	if m.showBeadsInit {
		return m.updateBeadsInit(msg)
	}

	// Handle new stream overlay input first if active.
	if m.showNewStream {
		return m.updateNewStream(msg)
	}

	// Handle guidance overlay input first if active.
	if m.showGuidance {
		return m.updateGuidance(msg)
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch m.view {
		case viewDashboard:
			return m.updateDashboard(msg)
		case viewDetail:
			return m.updateDetail(msg)
		case viewTail:
			return m.updateTail(msg)
		}

	case beadsInitDoneMsg:
		if msg.err != nil {
			m.creating = false
			m.errorMsg = "Error initializing beads: " + msg.err.Error()
			return m, nil
		}
		task := m.pendingTask
		orch := m.orch
		return m, func() tea.Msg {
			st, err := orch.Create(task)
			return streamCreatedMsg{stream: st, err: err}
		}

	case streamCreatedMsg:
		m.creating = false
		if msg.err != nil {
			m.errorMsg = "Error creating stream: " + msg.err.Error()
			return m, nil
		}
		return m, nil

	case streamDeletedMsg:
		if msg.err != nil {
			m.errorMsg = "Error deleting stream: " + msg.err.Error()
			return m, nil
		}
		streams := m.orch.List()
		m.dashboard.clampCursor(len(streams))
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
		if m.dashboard.mode == modeList {
			m.dashboard.cursor++
			m.dashboard.clampCursor(len(streams))
		}

	case "k", "up":
		if m.dashboard.mode == modeList {
			m.dashboard.cursor--
			m.dashboard.clampCursor(len(streams))
		}

	case "enter":
		if st := m.selectedStream(); st != nil {
			m.selectedID = st.ID
			m.view = viewDetail
			m.detail.snapCursor = len(st.GetSnapshots()) - 1
			if m.detail.snapCursor < 0 {
				m.detail.snapCursor = 0
			}
		} else {
			return m.setStatus("No stream selected. Press n to create one.")
		}

	case "n":
		m.showNewStream = true
		m.newStreamInput.Reset()
		m.newStreamInput.Width = m.inputWidth()
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

	case "d":
		if st := m.selectedStream(); st != nil {
			if m.orch.IsRunning(st.ID) {
				return m.setStatus("Stop the stream first.")
			}
			m.deleteTargetID = st.ID
			m.showDeleteConfirm = true
		} else {
			return m.setStatus("No stream selected.")
		}

	case "v":
		if m.dashboard.mode == modeChannels {
			m.dashboard.mode = modeList
		} else {
			m.dashboard.mode = modeChannels
		}

	case "h", "left":
		if m.dashboard.mode == modeChannels {
			m.dashboard.cursor--
			m.dashboard.clampCursor(len(streams))
		}

	case "l", "right":
		if m.dashboard.mode == modeChannels {
			m.dashboard.cursor++
			m.dashboard.clampCursor(len(streams))
		}

	}

	return m, nil
}

func (m Model) updateDetail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	st := m.orch.Get(m.selectedID)
	snapCount := 0
	if st != nil {
		snapCount = len(st.GetSnapshots())
	}
	m.detail.clampCursor(snapCount)

	switch msg.String() {
	case "esc", "q":
		m.view = viewDashboard
		return m, nil

	case "j", "down":
		m.detail.snapCursor++
		m.detail.clampCursor(snapCount)

	case "k", "up":
		m.detail.snapCursor--
		m.detail.clampCursor(snapCount)

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

	case "t":
		m.view = viewTail
		m.tail.scrollOffset = 0
		return m, nil
	}

	return m, nil
}

func (m Model) updateTail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	st := m.orch.Get(m.selectedID)
	lineCount := 0
	if st != nil {
		lineCount = len(st.GetOutputLines())
	}

	switch msg.String() {
	case "esc", "q":
		m.view = viewDetail
		return m, nil

	case "j", "down":
		if m.tail.scrollOffset > 0 {
			m.tail.scrollOffset--
		}

	case "k", "up":
		maxScroll := lineCount - 5
		if maxScroll < 0 {
			maxScroll = 0
		}
		if m.tail.scrollOffset < maxScroll {
			m.tail.scrollOffset++
		}

	case "G":
		m.tail.scrollOffset = 0
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
			if m.orch.NeedsBeadsInit() {
				m.pendingTask = task
				m.showBeadsInit = true
				return m, nil
			}
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

func (m Model) updateBeadsInit(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "y":
			m.showBeadsInit = false
			m.creating = true
			orch := m.orch
			return m, func() tea.Msg {
				return beadsInitDoneMsg{err: orch.InitBeads(true)}
			}
		case "n":
			m.showBeadsInit = false
			m.creating = true
			orch := m.orch
			return m, func() tea.Msg {
				return beadsInitDoneMsg{err: orch.InitBeads(false)}
			}
		case "esc":
			m.showBeadsInit = false
			m.pendingTask = ""
			return m, nil
		}
	}
	return m, nil
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

func (m Model) updateDeleteConfirm(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "d":
			m.showDeleteConfirm = false
			id := m.deleteTargetID
			orch := m.orch
			return m, func() tea.Msg {
				return streamDeletedMsg{err: orch.Delete(id, true)}
			}
		case "k":
			m.showDeleteConfirm = false
			id := m.deleteTargetID
			orch := m.orch
			return m, func() tea.Msg {
				return streamDeletedMsg{err: orch.Delete(id, false)}
			}
		case "n", "esc":
			m.showDeleteConfirm = false
			return m, nil
		}
	}
	return m, nil
}

func (m Model) View() string {
	if m.showDeleteConfirm {
		st := m.orch.Get(m.deleteTargetID)
		name := m.deleteTargetID
		if st != nil {
			name = st.Name
		}
		return renderDeleteConfirmOverlay(name, m.width, m.height)
	}

	if m.showBeadsInit {
		return renderBeadsInitOverlay(m.width, m.height)
	}

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
		var body string
		switch m.dashboard.mode {
		case modeChannels:
			body = renderChannels(streams, m.dashboard.cursor, m.dashboard.scrollLeft, m.width, m.height)
		default:
			body = renderDashboardList(streams, m.dashboard.cursor)
		}
		if m.creating {
			body += "\n" + helpStyle.Render("Creating stream...")
		}
		if m.errorMsg != "" {
			body += "\n" + errorBlockStyle.Render(m.errorMsg)
		}
		if m.statusMsg != "" {
			body += "\n" + helpStyle.Render(m.statusMsg)
		}
		help := dashboardChannelHelp
		if m.dashboard.mode == modeList {
			help = dashboardListHelp
		}
		footer := helpStyle.Render(help)
		return layoutWithFooter(body, footer, m.height)

	case viewDetail:
		st := m.orch.Get(m.selectedID)
		return renderDetail(st, m.detail.snapCursor, m.width, m.height)

	case viewTail:
		st := m.orch.Get(m.selectedID)
		return renderTail(st, m.tail, m.width, m.height)

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

func renderBeadsInitOverlay(width, height int) string {
	overlay := titleStyle.Render("Initialize Beads") + "\n\n"
	overlay += "This repository doesn't have beads initialized.\n"
	overlay += "Streams uses beads to track issues for each stream.\n\n"
	overlay += "Stealth mode keeps beads files out of git history,\n"
	overlay += "so they won't show up in commits or affect collaborators.\n"
	overlay += "Use this for repos you don't own.\n\n"
	overlay += helpStyle.Render("y: stealth mode  n: normal mode  esc: cancel")

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

func renderDeleteConfirmOverlay(name string, width, height int) string {
	overlay := titleStyle.Render("Delete Stream") + "\n\n"
	overlay += fmt.Sprintf("Delete %q?\n\n", name)
	overlay += helpStyle.Render("d: delete + clean up branch/beads  k: keep branch/beads  esc: cancel")

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

// inputWidth returns the usable width for text inputs inside overlay boxes.
// Accounts for the outer margin (6), overlay border (2), and overlay padding (4).
func (m Model) inputWidth() int {
	w := m.width - 12
	if w < 20 {
		w = 20
	}
	return w
}

// selectedStream returns the stream at the dashboard cursor, or nil.
func (m Model) selectedStream() *stream.Stream {
	streams := m.orch.List()
	if m.dashboard.cursor >= 0 && m.dashboard.cursor < len(streams) {
		return streams[m.dashboard.cursor]
	}
	return nil
}
