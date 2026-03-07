package ui

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/zmorgan/streams/internal/orchestrator"
	"github.com/zmorgan/streams/internal/stream"
)

// PhaseNode defines a phase that can be selected when creating a stream.
// Children are nested under their parent (e.g., decompose under plan).
type PhaseNode struct {
	Name     string
	Children []PhaseNode
}

// phaseTree defines the available phases and their nesting.
var phaseTree = []PhaseNode{
	{Name: "plan", Children: []PhaseNode{
		{Name: "decompose"},
	}},
	{Name: "coding"},
}

// flatPhase is a flattened view of the phase tree for cursor navigation.
type flatPhase struct {
	Name  string
	Depth int
}

// flattenPhaseTree returns a flat list of phases with depth info for rendering.
func flattenPhaseTree(nodes []PhaseNode, depth int) []flatPhase {
	var result []flatPhase
	for _, node := range nodes {
		result = append(result, flatPhase{Name: node.Name, Depth: depth})
		result = append(result, flattenPhaseTree(node.Children, depth+1)...)
	}
	return result
}

// childPhases returns the names of all children (recursive) of the given phase.
func childPhases(nodes []PhaseNode, parent string) []string {
	for _, node := range nodes {
		if node.Name == parent {
			return collectNames(node.Children)
		}
		if found := childPhases(node.Children, parent); found != nil {
			return found
		}
	}
	return nil
}

func collectNames(nodes []PhaseNode) []string {
	var names []string
	for _, node := range nodes {
		names = append(names, node.Name)
		names = append(names, collectNames(node.Children)...)
	}
	return names
}

// selectedPipeline builds an ordered pipeline from the checked phases.
func selectedPipeline(checked map[string]bool, nodes []PhaseNode) []string {
	var result []string
	for _, node := range nodes {
		if checked[node.Name] {
			result = append(result, node.Name)
		}
		result = append(result, selectedPipeline(checked, node.Children)...)
	}
	return result
}

// defaultPhaseChecks returns a checked map matching the given pipeline.
func defaultPhaseChecks(pipeline []string) map[string]bool {
	checked := make(map[string]bool)
	for _, name := range pipeline {
		checked[name] = true
	}
	return checked
}

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
	showNewStream     bool
	newStreamInput    textinput.Model
	newStreamStep     int             // 0 = task input, 1 = phase picker
	newStreamPhaseCur int             // cursor into flattened phase list
	newStreamChecked  map[string]bool // which phases are checked
	creating          bool            // true while orch.Create is running

	// Delete confirmation overlay state.
	showDeleteConfirm bool
	deleteTargetID    string

	// Beads init prompt state.
	showBeadsInit   bool
	pendingTask     string   // task stashed while waiting for stealth answer
	pendingPipeline []string // pipeline stashed while waiting for stealth answer

	// Attach state.
	interrupting        bool // true while waiting for Interrupt to finish
	attachedFromRunning bool // true if attach was triggered from a running stream
	showRestartPrompt   bool // true after returning from an auto-paused attach

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

// claudeExitMsg is sent when an attached claude --resume session exits.
type claudeExitMsg struct {
	err error
}

// interruptDoneMsg is sent when orch.Interrupt finishes (loop goroutine exited).
type interruptDoneMsg struct {
	sessionID string
	err       error
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
	// Auto-resume streams that were running or created when the app last exited.
	for _, st := range m.orch.List() {
		status := st.GetStatus()
		if status == stream.StatusCreated || status == stream.StatusRunning {
			_ = m.orch.Start(st.ID)
		}
	}
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

	// Clear ephemeral messages on any keypress.
	if _, ok := msg.(tea.KeyMsg); ok {
		m.errorMsg = ""
		m.statusMsg = ""
	}

	// Handle restart prompt overlay if active.
	if m.showRestartPrompt {
		return m.updateRestartPrompt(msg)
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
		}

	case beadsInitDoneMsg:
		if msg.err != nil {
			m.creating = false
			m.errorMsg = "Error initializing beads: " + msg.err.Error()
			return m, nil
		}
		task := m.pendingTask
		pipeline := m.pendingPipeline
		orch := m.orch
		return m, func() tea.Msg {
			st, err := orch.Create(task, pipeline)
			return streamCreatedMsg{stream: st, err: err}
		}

	case streamCreatedMsg:
		m.creating = false
		if msg.err != nil {
			m.errorMsg = "Error creating stream: " + msg.err.Error()
			return m, nil
		}
		if err := m.orch.Start(msg.stream.ID); err != nil {
			m.errorMsg = "Stream created but failed to start: " + err.Error()
		}
		return m, nil

	case interruptDoneMsg:
		m.interrupting = false
		if msg.err != nil {
			m.errorMsg = "Interrupt failed: " + msg.err.Error()
			return m, nil
		}
		st := m.orch.Get(m.selectedID)
		if st == nil {
			return m, nil
		}
		c := exec.Command("claude", "--resume", msg.sessionID)
		c.Dir = st.WorkTree
		return m, tea.ExecProcess(c, func(err error) tea.Msg {
			return claudeExitMsg{err: err}
		})

	case claudeExitMsg:
		if msg.err != nil {
			m.errorMsg = "Attach session exited with error: " + msg.err.Error()
		}
		if m.attachedFromRunning {
			m.showRestartPrompt = true
			m.attachedFromRunning = false
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
			m.detail.contentWidth = m.width
			rows := buildIterationList(st)
			m.detail.iterCursor = len(rows) - 1
			if m.detail.iterCursor < 0 {
				m.detail.iterCursor = 0
			}
		} else {
			return m.setStatus("No stream selected. Press n to create one.")
		}

	case "n":
		m.showNewStream = true
		m.newStreamStep = 0
		m.newStreamInput.Reset()
		m.newStreamInput.Width = m.inputWidth()
		m.newStreamInput.Focus()
		m.newStreamChecked = defaultPhaseChecks(m.orch.DefaultPipeline())
		m.newStreamPhaseCur = 0
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
			m.autoScrollChannels(len(streams))
		}

	case "l", "right":
		if m.dashboard.mode == modeChannels {
			m.dashboard.cursor++
			m.dashboard.clampCursor(len(streams))
			m.autoScrollChannels(len(streams))
		}

	}

	return m, nil
}

func (m Model) updateDetail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	st := m.orch.Get(m.selectedID)
	iterCount := 0
	if st != nil {
		iterCount = len(buildIterationList(st))
	}
	m.detail.clampCursor(iterCount)

	switch msg.String() {
	case "esc", "q":
		m.view = viewDashboard
		return m, nil

	case "j", "down":
		m.detail.iterCursor++
		m.detail.clampCursor(iterCount)

	case "k", "up":
		m.detail.iterCursor--
		m.detail.clampCursor(iterCount)

	case "s":
		if st != nil {
			if err := m.orch.Start(st.ID); err != nil {
				m.statusMsg = "Start error: " + err.Error()
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

	case "a":
		if st == nil || m.interrupting {
			return m, nil
		}
		sessionID := st.GetSessionID()
		if sessionID == "" {
			m.statusMsg = "No session ID yet — run the stream first."
			return m, nil
		}
		if st.GetStatus() == stream.StatusRunning {
			m.interrupting = true
			m.attachedFromRunning = true
			m.statusMsg = "Pausing..."
			id := st.ID
			orch := m.orch
			return m, func() tea.Msg {
				err := orch.Interrupt(id)
				return interruptDoneMsg{sessionID: sessionID, err: err}
			}
		}
		m.attachedFromRunning = false
		c := exec.Command("claude", "--resume", sessionID)
		c.Dir = st.WorkTree
		return m, tea.ExecProcess(c, func(err error) tea.Msg {
			return claudeExitMsg{err: err}
		})
	}

	return m, nil
}

func (m Model) updateNewStream(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.newStreamStep == 1 {
		return m.updateNewStreamPipeline(msg)
	}

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
			m.newStreamStep = 1
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.newStreamInput, cmd = m.newStreamInput.Update(msg)
	return m, cmd
}

func (m Model) updateNewStreamPipeline(msg tea.Msg) (tea.Model, tea.Cmd) {
	flat := flattenPhaseTree(phaseTree, 0)

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			m.newStreamStep = 0
			return m, nil

		case "j", "down":
			if m.newStreamPhaseCur < len(flat)-1 {
				m.newStreamPhaseCur++
			}
			return m, nil

		case "k", "up":
			if m.newStreamPhaseCur > 0 {
				m.newStreamPhaseCur--
			}
			return m, nil

		case " ":
			name := flat[m.newStreamPhaseCur].Name
			if m.newStreamChecked[name] {
				// Unchecking: also uncheck children
				m.newStreamChecked[name] = false
				for _, child := range childPhases(phaseTree, name) {
					m.newStreamChecked[child] = false
				}
			} else {
				// Checking: also check children
				m.newStreamChecked[name] = true
				for _, child := range childPhases(phaseTree, name) {
					m.newStreamChecked[child] = true
				}
			}
			return m, nil

		case "enter":
			pipeline := selectedPipeline(m.newStreamChecked, phaseTree)
			if len(pipeline) == 0 {
				return m, nil
			}
			task := m.newStreamInput.Value()
			m.showNewStream = false
			m.newStreamStep = 0
			if m.orch.NeedsBeadsInit() {
				m.pendingTask = task
				m.pendingPipeline = pipeline
				m.showBeadsInit = true
				return m, nil
			}
			m.creating = true
			orch := m.orch
			return m, func() tea.Msg {
				st, err := orch.Create(task, pipeline)
				return streamCreatedMsg{stream: st, err: err}
			}
		}
	}

	return m, nil
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

func (m Model) updateRestartPrompt(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "y":
			m.showRestartPrompt = false
			if m.selectedID != "" {
				if err := m.orch.Start(m.selectedID); err != nil {
					m.errorMsg = "Restart error: " + err.Error()
				}
			}
			return m, nil
		case "n", "esc":
			m.showRestartPrompt = false
			return m, nil
		}
	}
	return m, nil
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
	if m.showRestartPrompt {
		return renderRestartPromptOverlay(m.width, m.height)
	}

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
		return renderNewStreamOverlay(m.newStreamInput, m.newStreamStep, m.newStreamPhaseCur, m.newStreamChecked, m.width, m.height)
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
		layoutWidth := m.detail.contentWidth
		if layoutWidth == 0 {
			layoutWidth = m.width
		}
		content := renderDetail(st, m.detail, layoutWidth, m.height)
		if m.errorMsg != "" {
			content += "\n" + errorBlockStyle.Render(m.errorMsg)
		}
		if m.statusMsg != "" {
			content += "\n" + helpStyle.Render(m.statusMsg)
		}
		return clipLines(content, m.width)

	default:
		return ""
	}
}

func renderNewStreamOverlay(ti textinput.Model, step, phaseCursor int, checked map[string]bool, width, height int) string {
	var overlay string

	if step == 0 {
		overlay = titleStyle.Render("New Stream") + "\n\n"
		overlay += ti.View() + "\n\n"
		overlay += helpStyle.Render("enter: next  esc: cancel")
	} else {
		overlay = titleStyle.Render("New Stream") + "\n\n"
		overlay += helpStyle.Render("Task: "+ti.Value()) + "\n\n"
		overlay += "Phases:\n"
		flat := flattenPhaseTree(phaseTree, 0)
		for i, fp := range flat {
			cursor := "  "
			if i == phaseCursor {
				cursor = cursorStyle.Render("> ")
			}
			indent := strings.Repeat("  ", fp.Depth)
			check := "[ ] "
			if checked[fp.Name] {
				check = "[x] "
			}
			label := fp.Name
			if i == phaseCursor {
				label = selectedRowStyle.Render(label)
			}
			overlay += cursor + indent + check + label + "\n"
		}
		overlay += "\n" + helpStyle.Render("j/k: navigate  space: toggle  enter: create  esc: back")
	}

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

func renderRestartPromptOverlay(width, height int) string {
	overlay := titleStyle.Render("Restart Stream?") + "\n\n"
	overlay += "The stream was paused for attach.\n"
	overlay += "Would you like to restart it?\n\n"
	overlay += helpStyle.Render("y: restart  n: keep paused")

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

// autoScrollChannels adjusts scrollLeft so the cursor stays in the visible window.
func (m *Model) autoScrollChannels(streamCount int) {
	_, visibleCols := channelLayout(streamCount, m.width)
	if m.dashboard.cursor < m.dashboard.scrollLeft {
		m.dashboard.scrollLeft = m.dashboard.cursor
	}
	if m.dashboard.cursor >= m.dashboard.scrollLeft+visibleCols {
		m.dashboard.scrollLeft = m.dashboard.cursor - visibleCols + 1
	}
	m.dashboard.clampScroll(streamCount, visibleCols)
}

// selectedStream returns the stream at the dashboard cursor, or nil.
func (m Model) selectedStream() *stream.Stream {
	streams := m.orch.List()
	if m.dashboard.cursor >= 0 && m.dashboard.cursor < len(streams) {
		return streams[m.dashboard.cursor]
	}
	return nil
}
