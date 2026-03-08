package ui

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
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
	{Name: "research"},
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

// spinnerTickMsg drives the animated spinner for in-progress iterations.
type spinnerTickMsg struct{}

// tailTickMsg drives periodic re-renders while the detail view is open,
// so that streaming output appears near-live.
type tailTickMsg struct{}

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
	newStreamTitle    textarea.Model
	newStreamInput    textarea.Model
	newStreamStep        int             // 0 = title input, 1 = task input, 2 = phase picker, 3 = breakpoint picker
	newStreamPhaseCur    int             // cursor into flattened phase list
	newStreamChecked     map[string]bool // which phases are checked
	newStreamBreakpoints map[int]bool    // which pipeline gaps have breakpoints
	newStreamBPCursor    int             // cursor into breakpoint gaps
	creating             bool            // true while orch.Create is running

	// Delete confirmation overlay state.
	showDeleteConfirm bool
	deleteTargetID    string

	// Beads init prompt state.
	showBeadsInit   bool
	pendingTitle       string   // title stashed while waiting for stealth answer
	pendingTask        string   // task stashed while waiting for stealth answer
	pendingPipeline    []string // pipeline stashed while waiting for stealth answer
	pendingBreakpoints []int   // breakpoints stashed while waiting for stealth answer

	// Attach state.
	interrupting        bool // true while waiting for Interrupt to finish
	attachedFromRunning bool // true if attach was triggered from a running stream
	showRestartPrompt   bool // true after returning from an auto-paused attach

	// Spinner animation state for in-progress iterations.
	spinnerFrame int

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

	titleInput := textarea.New()
	titleInput.Placeholder = "Enter a title..."
	titleInput.Prompt = ""
	titleInput.CharLimit = 100
	titleInput.ShowLineNumbers = false
	titleInput.SetHeight(1)
	titleInput.MaxHeight = 0

	ni := textarea.New()
	ni.Placeholder = "Describe the task..."
	ni.Prompt = ""
	ni.CharLimit = 0
	ni.ShowLineNumbers = false
	ni.SetHeight(3)
	ni.MaxHeight = 0

	return Model{
		orch:           orch,
		view:           viewDashboard,
		guidanceInput:  ti,
		newStreamTitle: titleInput,
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
	return tea.Batch(tea.WindowSize(), spinnerTick())
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle window resize globally before overlays.
	if msg, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = msg.Width
		m.height = msg.Height
		m.newStreamInput.SetWidth(m.inputWidth())
		return m, nil
	}

	// Handle spinner/tail ticks globally so overlay handlers can't break the chain.
	if _, ok := msg.(spinnerTickMsg); ok {
		m.spinnerFrame++
		return m, spinnerTick()
	}
	if _, ok := msg.(tailTickMsg); ok {
		if m.view == viewDetail {
			return m, tailTick()
		}
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
		title := m.pendingTitle
		task := m.pendingTask
		pipeline := m.pendingPipeline
		breakpoints := m.pendingBreakpoints
		orch := m.orch
		return m, func() tea.Msg {
			st, err := orch.Create(title, task, pipeline, breakpoints)
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
			return m, tailTick()
		} else {
			return m.setStatus("No stream selected. Press n to create one.")
		}

	case "n":
		m.showNewStream = true
		m.newStreamStep = 0
		m.newStreamTitle.Reset()
		m.newStreamTitle.SetWidth(m.inputWidth())
		m.newStreamTitle.Focus()
		m.newStreamInput.Reset()
		m.newStreamInput.SetWidth(m.inputWidth())
		m.newStreamChecked = defaultPhaseChecks(m.orch.DefaultPipeline())
		m.newStreamPhaseCur = 0
		return m, textarea.Blink

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
	var rows []iterationRow
	if st != nil {
		rows = buildIterationList(st)
	}
	m.detail.clampCursor(len(rows))

	// When focused on the right pane (tail output), handle scroll keys
	if m.detail.focusRight {
		return m.updateDetailFocusRight(msg, st)
	}

	switch msg.String() {
	case "esc", "q":
		m.view = viewDashboard
		return m, nil

	case "j", "down":
		m.detail.iterCursor++
		m.detail.clampCursor(len(rows))
		m.detail.tailScroll = 0

	case "k", "up":
		m.detail.iterCursor--
		m.detail.clampCursor(len(rows))
		m.detail.tailScroll = 0

	case "enter":
		cursor := m.detail.iterCursor
		if cursor >= 0 && cursor < len(rows) && rows[cursor].IsInProgress {
			m.detail.focusRight = true
			m.detail.tailScroll = 0
		}

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

	case "f":
		m.detail.showArtifact = !m.detail.showArtifact

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

func (m Model) updateDetailFocusRight(msg tea.KeyMsg, st *stream.Stream) (tea.Model, tea.Cmd) {
	lineCount := 0
	if st != nil {
		lineCount = len(st.GetOutputLines())
	}

	switch msg.String() {
	case "esc":
		m.detail.focusRight = false
		return m, nil

	case "j", "down":
		if m.detail.tailScroll > 0 {
			m.detail.tailScroll--
		}

	case "k", "up":
		maxScroll := lineCount - 5
		if maxScroll < 0 {
			maxScroll = 0
		}
		if m.detail.tailScroll < maxScroll {
			m.detail.tailScroll++
		}

	case "G":
		m.detail.tailScroll = 0
	}

	return m, nil
}

func (m Model) updateNewStream(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.newStreamStep == 3 {
		return m.updateNewStreamBreakpoints(msg)
	}
	if m.newStreamStep == 2 {
		return m.updateNewStreamPipeline(msg)
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			if m.newStreamStep == 1 {
				m.newStreamStep = 0
				m.newStreamTitle.Focus()
				return m, textarea.Blink
			}
			m.showNewStream = false
			return m, nil

		case "ctrl+n":
			if m.newStreamStep == 0 {
				title := m.newStreamTitle.Value()
				if title == "" {
					return m, nil
				}
				m.newStreamStep = 1
				m.newStreamInput.Focus()
				return m, textarea.Blink
			}
			task := m.newStreamInput.Value()
			if task == "" {
				return m, nil
			}
			m.newStreamStep = 2
			return m, nil
		}
	}

	if m.newStreamStep == 0 {
		var cmd tea.Cmd
		m.newStreamTitle, cmd = m.newStreamTitle.Update(msg)
		return m, cmd
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
			m.newStreamStep = 1
			m.newStreamInput.Focus()
			return m, textarea.Blink

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
			if len(pipeline) > 1 {
				m.newStreamStep = 3
				m.newStreamBreakpoints = make(map[int]bool)
				m.newStreamBPCursor = 0
				return m, nil
			}
			return m.createStream(pipeline, nil)
		}
	}

	return m, nil
}

func (m Model) updateNewStreamBreakpoints(msg tea.Msg) (tea.Model, tea.Cmd) {
	pipeline := selectedPipeline(m.newStreamChecked, phaseTree)
	gapCount := len(pipeline) - 1

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			m.newStreamStep = 2
			return m, nil

		case "j", "down":
			if m.newStreamBPCursor < gapCount-1 {
				m.newStreamBPCursor++
			}
			return m, nil

		case "k", "up":
			if m.newStreamBPCursor > 0 {
				m.newStreamBPCursor--
			}
			return m, nil

		case " ":
			m.newStreamBreakpoints[m.newStreamBPCursor] = !m.newStreamBreakpoints[m.newStreamBPCursor]
			return m, nil

		case "enter":
			var breakpoints []int
			for i := 0; i < gapCount; i++ {
				if m.newStreamBreakpoints[i] {
					breakpoints = append(breakpoints, i)
				}
			}
			return m.createStream(pipeline, breakpoints)
		}
	}

	return m, nil
}

func (m Model) createStream(pipeline []string, breakpoints []int) (tea.Model, tea.Cmd) {
	title := m.newStreamTitle.Value()
	task := m.newStreamInput.Value()
	m.showNewStream = false
	m.newStreamStep = 0
	if m.orch.NeedsBeadsInit() {
		m.pendingTitle = title
		m.pendingTask = task
		m.pendingPipeline = pipeline
		m.pendingBreakpoints = breakpoints
		m.showBeadsInit = true
		return m, nil
	}
	m.creating = true
	orch := m.orch
	return m, func() tea.Msg {
		st, err := orch.Create(title, task, pipeline, breakpoints)
		return streamCreatedMsg{stream: st, err: err}
	}
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
			m.pendingTitle = ""
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
		return renderNewStreamOverlay(m.newStreamTitle, m.newStreamInput, m.newStreamStep, m.newStreamPhaseCur, m.newStreamChecked, m.newStreamBreakpoints, m.newStreamBPCursor, m.width, m.height)
	}

	if m.showGuidance {
		return renderGuidanceOverlay(m.guidanceInput, m.width, m.height)
	}

	switch m.view {
	case viewDashboard:
		streams := m.orch.List()
		m.dashboard.clampCursor(len(streams))
		var body string
		frame := spinnerFrames[m.spinnerFrame%len(spinnerFrames)]
		switch m.dashboard.mode {
		case modeChannels:
			body = renderChannels(streams, m.dashboard.cursor, m.dashboard.scrollLeft, m.width, m.height, frame)
		default:
			body = renderDashboardList(streams, m.dashboard.cursor, frame)
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
		frame := spinnerFrames[m.spinnerFrame%len(spinnerFrames)]
		content := renderDetail(st, m.detail, layoutWidth, m.height, frame)
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

func renderNewStreamOverlay(titleInput, taskInput textarea.Model, step, phaseCursor int, checked map[string]bool, breakpoints map[int]bool, bpCursor int, width, height int) string {
	var overlay string

	switch step {
	case 0:
		overlay = titleStyle.Render("New Stream") + "\n\n"
		overlay += "Title:\n"
		overlay += titleInput.View() + "\n\n"
		overlay += helpStyle.Render("ctrl+n: next  esc: cancel")
	case 1:
		overlay = titleStyle.Render("New Stream") + "\n\n"
		overlay += helpStyle.Render("Title: "+titleInput.Value()) + "\n\n"
		overlay += "Task:\n"
		overlay += taskInput.View() + "\n\n"
		overlay += helpStyle.Render("ctrl+n: next  esc: back")
	case 3:
		pipeline := selectedPipeline(checked, phaseTree)
		overlay = titleStyle.Render("New Stream") + "\n\n"
		overlay += helpStyle.Render("Title: "+titleInput.Value()) + "\n"
		overlay += helpStyle.Render("Task: "+taskInput.Value()) + "\n\n"
		overlay += "Set breakpoints (pause between phases):\n\n"
		for i, name := range pipeline {
			overlay += "  " + name + "\n"
			if i < len(pipeline)-1 {
				cursor := "  "
				if i == bpCursor {
					cursor = cursorStyle.Render("> ")
				}
				check := "[ ]"
				if breakpoints[i] {
					check = "[x]"
				}
				label := fmt.Sprintf("── %s pause after %s ──", check, name)
				if i == bpCursor {
					label = selectedRowStyle.Render(label)
				}
				overlay += cursor + label + "\n"
			}
		}
		overlay += "\n" + helpStyle.Render("j/k: navigate  space: toggle  enter: create  esc: back")
	default:
		overlay = titleStyle.Render("New Stream") + "\n\n"
		overlay += helpStyle.Render("Title: "+titleInput.Value()) + "\n"
		overlay += helpStyle.Render("Task: "+taskInput.Value()) + "\n\n"
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
		overlay += "\n" + helpStyle.Render("j/k: navigate  space: toggle  enter: next  esc: back")
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

func tailTick() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(time.Time) tea.Msg {
		return tailTickMsg{}
	})
}

func spinnerTick() tea.Cmd {
	return tea.Tick(80*time.Millisecond, func(time.Time) tea.Msg {
		return spinnerTickMsg{}
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
