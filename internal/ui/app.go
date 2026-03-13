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
	{Name: "review"},
	{Name: "polish"},
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
	showNewStream        bool
	newStreamTitle       textarea.Model
	newStreamInput       textarea.Model
	newStreamStep        int                   // 0 = title input, 1 = task input, 2 = phase picker, 3 = breakpoint picker
	newStreamPhaseCur    int                   // cursor into flattened phase list
	newStreamChecked     map[string]bool       // which phases are checked
	newStreamBreakpoints map[int]bool          // which pipeline gaps have breakpoints
	newStreamBPCursor    int                   // cursor into breakpoint gaps
	newStreamNotify      stream.NotifySettings // notification toggles
	creating             bool                  // true while orch.Create is running

	// Delete confirmation overlay state.
	showDeleteConfirm bool
	deleteTargetID    string

	// Beads init prompt state.
	showBeadsInit      bool
	pendingTitle       string                // title stashed while waiting for stealth answer
	pendingTask        string                // task stashed while waiting for stealth answer
	pendingPipeline    []string              // pipeline stashed while waiting for stealth answer
	pendingBreakpoints []int                 // breakpoints stashed while waiting for stealth answer
	pendingNotify      stream.NotifySettings // notify settings stashed while waiting for stealth answer

	// Edit breakpoints overlay state.
	showEditBreakpoints bool
	editBPMap           map[int]bool // which pipeline gaps have breakpoints
	editBPCursor        int          // cursor into breakpoint gaps
	editBPNotify        stream.NotifySettings

	// Converge confirmation overlay state.
	showConvergeConfirm bool

	// Force-advance confirmation overlay state.
	showForceAdvance bool

	// Complete overlay state (review phase).
	showComplete  bool
	completeInput textarea.Model

	// Revise overlay state (review phase).
	showRevise        bool
	revisePhaseCursor int
	reviseFeedback    textarea.Model
	reviseStep        int // 0 = phase picker, 1 = feedback input

	// Attach state.
	interrupting        bool // true while waiting for Interrupt to finish
	attachedFromRunning bool // true if attach was triggered from a running stream
	showRestartPrompt   bool // true after returning from an auto-paused attach

	// Spinner animation state for in-progress iterations.
	spinnerFrame int

	// Ephemeral status message shown at bottom of dashboard.
	statusMsg string

	// Ephemeral error message, auto-clears after errorTTL ticks.
	errorMsg string
	errorTTL int // spinner ticks remaining until errorMsg clears
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

// streamCompletedMsg is sent when orch.Complete finishes.
type streamCompletedMsg struct {
	err error
}

// streamRevisedMsg is sent when orch.Revise finishes.
type streamRevisedMsg struct {
	err error
}

// forceAdvancedMsg is sent when orch.ForceAdvance finishes.
type forceAdvancedMsg struct {
	err error
}


type beadShowMsg struct {
	output string
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
		if m.errorTTL > 0 {
			m.errorTTL--
			if m.errorTTL == 0 {
				m.errorMsg = ""
			}
		}
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

	// Handle converge confirmation overlay if active.
	if m.showConvergeConfirm {
		return m.updateConvergeConfirm(msg)
	}

	// Handle force-advance confirmation overlay if active.
	if m.showForceAdvance {
		return m.updateForceAdvance(msg)
	}

	// Handle edit breakpoints overlay if active.
	if m.showEditBreakpoints {
		return m.updateEditBreakpoints(msg)
	}

	// Handle complete overlay if active.
	if m.showComplete {
		return m.updateComplete(msg)
	}

	// Handle revise overlay if active.
	if m.showRevise {
		return m.updateRevise(msg)
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
			m = m.withError("Error initializing beads: " + msg.err.Error())
			return m, nil
		}
		title := m.pendingTitle
		task := m.pendingTask
		pipeline := m.pendingPipeline
		breakpoints := m.pendingBreakpoints
		notify := m.pendingNotify
		orch := m.orch
		return m, func() tea.Msg {
			st, err := orch.Create(title, task, pipeline, breakpoints, notify)
			return streamCreatedMsg{stream: st, err: err}
		}

	case streamCreatedMsg:
		m.creating = false
		if msg.err != nil {
			m = m.withError("Error creating stream: " + msg.err.Error())
			return m, nil
		}
		if err := m.orch.Start(msg.stream.ID); err != nil {
			m = m.withError("Stream created but failed to start: " + err.Error())
		}
		return m, nil

	case interruptDoneMsg:
		m.interrupting = false
		if msg.err != nil {
			m = m.withError("Interrupt failed: " + msg.err.Error())
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
			m = m.withError("Attach session exited with error: " + msg.err.Error())
		}
		if m.attachedFromRunning {
			m.showRestartPrompt = true
			m.attachedFromRunning = false
		}
		return m, nil

	case streamCompletedMsg:
		if msg.err != nil {
			m = m.withError("Error completing stream: " + msg.err.Error())
			return m, nil
		}
		m.view = viewDashboard
		return m, nil

	case streamRevisedMsg:
		if msg.err != nil {
			m = m.withError("Error revising stream: " + msg.err.Error())
			return m, nil
		}
		return m, nil

	case forceAdvancedMsg:
		if msg.err != nil {
			m = m.withError("Error advancing phase: " + msg.err.Error())
			return m, nil
		}
		return m, nil


	case beadShowMsg:
		m.detail.beadShowOutput = msg.output
		return m, nil

	case streamDeletedMsg:
		if msg.err != nil {
			m = m.withError("Error deleting stream: " + msg.err.Error())
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
		m.newStreamNotify = stream.NotifySettings{}
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

	case "D":
		if st := m.selectedStream(); st != nil {
			m.selectedID = st.ID
			return m.startDiagnose(st.ID)
		}
		return m.setStatus("No stream selected.")

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
	var snaps []stream.Snapshot
	if st != nil {
		rows = buildIterationList(st)
		snaps = st.GetSnapshots()
	}
	m.detail.clampCursor(len(rows))

	// When focused on the right pane (tail output), handle scroll keys
	if m.detail.focusRight {
		return m.updateDetailFocusRight(msg, st)
	}

	// When in bead browse mode, handle bead navigation
	if m.detail.beadFocused {
		return m.updateDetailBeadBrowse(msg, st, rows)
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
		} else if cursor >= 0 && cursor < len(rows) && !rows[cursor].IsInitialPrompt {
			if idx := rows[cursor].SnapshotIndex; idx >= 0 && idx < len(snaps) {
				snap := snaps[idx]
				if len(snap.BeadsClosed)+len(snap.BeadsOpened) > 0 {
					m.detail.beadFocused = true
					m.detail.beadCursor = 0
					m.detail.beadShowOutput = ""
				}
			}
		}

	case "s":
		if st != nil && !isPausedAtReview(st) {
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

	case "b":
		if st != nil && st.GetStatus() != stream.StatusCompleted {
			pipeline := st.GetPipeline()
			if len(pipeline) > 1 {
				bpMap := make(map[int]bool)
				for _, bp := range st.GetBreakpoints() {
					bpMap[bp] = true
				}
				m.editBPMap = bpMap
				m.editBPCursor = 0
				m.editBPNotify = st.GetNotify()
				m.showEditBreakpoints = true
				return m, nil
			}
		}

	case "d":
		if st != nil && !m.orch.IsRunning(st.ID) {
			m.deleteTargetID = st.ID
			m.showDeleteConfirm = true
		}

	case "c":
		if st != nil && isPausedAtReview(st) {
			ci := textarea.New()
			ci.Placeholder = "feature/my-branch"
			ci.Prompt = ""
			ci.CharLimit = 100
			ci.ShowLineNumbers = false
			ci.SetHeight(1)
			ci.MaxHeight = 0
			ci.SetWidth(m.inputWidth())
			ci.SetValue(slugify(st.Name))
			ci.Focus()
			m.completeInput = ci
			m.showComplete = true
			return m, textarea.Blink
		}

	case "r":
		if st != nil && isPausedAtReview(st) {
			fi := textarea.New()
			fi.Placeholder = "Optional feedback for the target phase..."
			fi.Prompt = ""
			fi.CharLimit = 1000
			fi.ShowLineNumbers = false
			fi.SetHeight(3)
			fi.MaxHeight = 0
			fi.SetWidth(m.inputWidth())
			m.reviseFeedback = fi
			m.revisePhaseCursor = 0
			m.reviseStep = 0
			m.showRevise = true
			return m, nil
		}

	case "w":
		if st != nil && st.GetStatus() == stream.StatusRunning {
			m.showConvergeConfirm = true
			return m, nil
		}

	case ">":
		if st != nil && canForceAdvance(st) {
			m.showForceAdvance = true
			return m, nil
		}

	case "a":
		if st == nil || m.interrupting {
			return m, nil
		}
		if st.GetStatus() == stream.StatusCompleted {
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

	case "D":
		if st == nil {
			return m, nil
		}
		return m.startDiagnose(st.ID)
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

func (m Model) updateDetailBeadBrowse(msg tea.KeyMsg, st *stream.Stream, rows []iterationRow) (tea.Model, tea.Cmd) {
	cursor := m.detail.iterCursor
	var beadCount int
	if cursor >= 0 && cursor < len(rows) {
		if idx := rows[cursor].SnapshotIndex; idx >= 0 {
			snaps := st.GetSnapshots()
			if idx < len(snaps) {
				beadCount = len(snaps[idx].BeadsClosed) + len(snaps[idx].BeadsOpened)
			}
		}
	}

	switch msg.String() {
	case "esc":
		if m.detail.beadShowOutput != "" {
			m.detail.beadShowOutput = ""
		} else {
			m.detail.beadFocused = false
			m.detail.beadCursor = 0
		}
		return m, nil

	case "j", "down":
		if m.detail.beadShowOutput == "" && m.detail.beadCursor < beadCount-1 {
			m.detail.beadCursor++
		}

	case "k", "up":
		if m.detail.beadShowOutput == "" && m.detail.beadCursor > 0 {
			m.detail.beadCursor--
		}

	case "enter":
		if m.detail.beadShowOutput != "" {
			return m, nil
		}
		if cursor >= 0 && cursor < len(rows) {
			if idx := rows[cursor].SnapshotIndex; idx >= 0 {
				snaps := st.GetSnapshots()
				if idx < len(snaps) {
					allIDs := snapshotBeadIDs(snaps[idx])
					if m.detail.beadCursor >= 0 && m.detail.beadCursor < len(allIDs) {
						beadID := allIDs[m.detail.beadCursor]
						workDir := st.WorkTree
						return m, func() tea.Msg {
							cmd := exec.Command("bd", "show", beadID)
							cmd.Dir = workDir
							out, err := cmd.Output()
							if err != nil {
								return beadShowMsg{output: "Error: " + err.Error()}
							}
							return beadShowMsg{output: string(out)}
						}
					}
				}
			}
		}
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

		case "enter":
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

		case "alt+enter":
			if m.newStreamStep == 1 {
				m.newStreamInput.InsertString("\n")
				m.autoSizeNewStreamInput()
				return m, nil
			}
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
	m.autoSizeNewStreamInput()
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

		case "1":
			m.newStreamNotify.Bell = !m.newStreamNotify.Bell
			return m, nil
		case "2":
			m.newStreamNotify.Flash = !m.newStreamNotify.Flash
			return m, nil
		case "3":
			m.newStreamNotify.System = !m.newStreamNotify.System
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

func (m Model) updateEditBreakpoints(msg tea.Msg) (tea.Model, tea.Cmd) {
	st := m.orch.Get(m.selectedID)
	if st == nil {
		m.showEditBreakpoints = false
		return m, nil
	}
	pipeline := st.GetPipeline()
	gapCount := len(pipeline) - 1

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			m.showEditBreakpoints = false
			return m, nil

		case "j", "down":
			if m.editBPCursor < gapCount-1 {
				m.editBPCursor++
			}
			return m, nil

		case "k", "up":
			if m.editBPCursor > 0 {
				m.editBPCursor--
			}
			return m, nil

		case " ":
			m.editBPMap[m.editBPCursor] = !m.editBPMap[m.editBPCursor]
			return m, nil

		case "1":
			m.editBPNotify.Bell = !m.editBPNotify.Bell
			return m, nil
		case "2":
			m.editBPNotify.Flash = !m.editBPNotify.Flash
			return m, nil
		case "3":
			m.editBPNotify.System = !m.editBPNotify.System
			return m, nil

		case "enter":
			var breakpoints []int
			for i := 0; i < gapCount; i++ {
				if m.editBPMap[i] {
					breakpoints = append(breakpoints, i)
				}
			}
			st.SetBreakpoints(breakpoints)
			st.SetNotify(m.editBPNotify)
			m.showEditBreakpoints = false
			return m, nil
		}
	}

	return m, nil
}

func (m Model) createStream(pipeline []string, breakpoints []int) (tea.Model, tea.Cmd) {
	title := m.newStreamTitle.Value()
	task := m.newStreamInput.Value()
	notify := m.newStreamNotify
	m.showNewStream = false
	m.newStreamStep = 0
	if m.orch.NeedsBeadsInit() {
		m.pendingTitle = title
		m.pendingTask = task
		m.pendingPipeline = pipeline
		m.pendingBreakpoints = breakpoints
		m.pendingNotify = notify
		m.showBeadsInit = true
		return m, nil
	}
	m.creating = true
	orch := m.orch
	return m, func() tea.Msg {
		st, err := orch.Create(title, task, pipeline, breakpoints, notify)
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
			m.pendingNotify = stream.NotifySettings{}
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
					m = m.withError("Restart error: " + err.Error())
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

func (m Model) updateConvergeConfirm(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "w":
			m.showConvergeConfirm = false
			if m.selectedID != "" {
				if err := m.orch.Converge(m.selectedID); err != nil {
					m = m.withError("Converge error: " + err.Error())
				} else {
					m.statusMsg = "Wrapping up phase..."
				}
			}
			return m, nil
		case "esc":
			m.showConvergeConfirm = false
			return m, nil
		}
	}
	return m, nil
}

func (m Model) updateForceAdvance(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case ">":
			m.showForceAdvance = false
			id := m.selectedID
			orch := m.orch
			return m, func() tea.Msg {
				return forceAdvancedMsg{err: orch.ForceAdvance(id)}
			}
		case "esc":
			m.showForceAdvance = false
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

func (m Model) updateComplete(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			m.showComplete = false
			return m, nil

		case "ctrl+s":
			branch := m.completeInput.Value()
			if branch == "" {
				return m, nil
			}
			m.showComplete = false
			id := m.selectedID
			orch := m.orch
			return m, func() tea.Msg {
				return streamCompletedMsg{err: orch.Complete(id, branch)}
			}
		}
	}

	var cmd tea.Cmd
	m.completeInput, cmd = m.completeInput.Update(msg)
	return m, cmd
}

func (m Model) updateRevise(msg tea.Msg) (tea.Model, tea.Cmd) {
	st := m.orch.Get(m.selectedID)
	if st == nil {
		m.showRevise = false
		return m, nil
	}

	pipeline := st.GetPipeline()
	pipelineIdx := st.GetPipelineIndex()
	// Phases available for revision: all before the current (review) phase.
	phaseCount := pipelineIdx

	if m.reviseStep == 1 {
		// Feedback input step.
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "esc":
				m.reviseStep = 0
				return m, nil

			case "ctrl+s":
				m.showRevise = false
				feedback := m.reviseFeedback.Value()
				targetIdx := m.revisePhaseCursor
				id := m.selectedID
				orch := m.orch
				return m, func() tea.Msg {
					return streamRevisedMsg{err: orch.Revise(id, targetIdx, feedback)}
				}
			}
		}

		var cmd tea.Cmd
		m.reviseFeedback, cmd = m.reviseFeedback.Update(msg)
		return m, cmd
	}

	// Phase picker step.
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			m.showRevise = false
			return m, nil

		case "j", "down":
			if m.revisePhaseCursor < phaseCount-1 {
				m.revisePhaseCursor++
			}
			return m, nil

		case "k", "up":
			if m.revisePhaseCursor > 0 {
				m.revisePhaseCursor--
			}
			return m, nil

		case "enter":
			if phaseCount > 0 {
				_ = pipeline // used above for phase count
				m.reviseStep = 1
				m.reviseFeedback.Focus()
				return m, textarea.Blink
			}
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
		return renderNewStreamOverlay(m.newStreamTitle, m.newStreamInput, m.newStreamStep, m.newStreamPhaseCur, m.newStreamChecked, m.newStreamBreakpoints, m.newStreamBPCursor, m.newStreamNotify, m.width, m.height)
	}

	if m.showConvergeConfirm {
		return renderConvergeConfirmOverlay(m.width, m.height)
	}

	if m.showForceAdvance {
		st := m.orch.Get(m.selectedID)
		nextPhase := ""
		if st != nil {
			pipeline := st.GetPipeline()
			nextIdx := st.GetPipelineIndex() + 1
			if nextIdx < len(pipeline) {
				nextPhase = pipeline[nextIdx]
			}
		}
		return renderForceAdvanceOverlay(nextPhase, m.width, m.height)
	}

	if m.showEditBreakpoints {
		st := m.orch.Get(m.selectedID)
		if st != nil {
			return renderEditBreakpointsOverlay(st.GetPipeline(), m.editBPMap, m.editBPCursor, m.editBPNotify, m.width, m.height)
		}
	}

	if m.showComplete {
		return renderCompleteOverlay(m.completeInput, m.width, m.height)
	}

	if m.showRevise {
		st := m.orch.Get(m.selectedID)
		var phases []string
		if st != nil {
			pipeline := st.GetPipeline()
			idx := st.GetPipelineIndex()
			if idx <= len(pipeline) {
				phases = pipeline[:idx]
			}
		}
		return renderReviseOverlay(phases, m.revisePhaseCursor, m.reviseStep, m.reviseFeedback, m.width, m.height)
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
			body = renderDashboardList(streams, m.dashboard.cursor, m.width, m.height, frame)
		}

		var statusLine string
		if m.creating {
			statusLine = "Creating stream..."
		}
		if m.errorMsg != "" {
			statusLine = errorBarStyle.Render(m.errorMsg)
		}
		if m.statusMsg != "" {
			statusLine = m.statusMsg
		}

		helpText := dashboardChannelHelp
		if m.dashboard.mode == modeList {
			helpText = dashboardListHelp
		}
		topBar := dashboardTopBar(streams)
		return layoutWithBars(topBar, body, statusLine, renderHelp(helpText), m.width, m.height)

	case viewDetail:
		st := m.orch.Get(m.selectedID)
		layoutWidth := m.detail.contentWidth
		if layoutWidth == 0 {
			layoutWidth = m.width
		}
		frame := spinnerFrames[m.spinnerFrame%len(spinnerFrames)]
		content := renderDetail(st, m.detail, layoutWidth, m.height, frame)

		var statusLine string
		if m.errorMsg != "" {
			statusLine = errorBarStyle.Render(m.errorMsg)
		}
		if m.statusMsg != "" {
			statusLine = m.statusMsg
		}

		rows := buildIterationList(st)
		snaps := st.GetSnapshots()
		helpText := detailHelpText(st, m.detail, rows, snaps)
		topBar := detailTopBar(st, m.width)
		return layoutWithBars(topBar, clipLines(content, m.width), statusLine, renderHelp(helpText), m.width, m.height)

	default:
		return ""
	}
}

func renderNewStreamOverlay(titleInput, taskInput textarea.Model, step, phaseCursor int, checked map[string]bool, breakpoints map[int]bool, bpCursor int, notify stream.NotifySettings, width, height int) string {
	var overlay string

	stepLabel := helpStyle.Render(fmt.Sprintf("  Step %d of 4", step+1))

	switch step {
	case 0:
		overlay = overlayTitleStyle.Render("New Stream") + stepLabel + "\n\n"
		overlay += "Title:\n"
		overlay += titleInput.View() + "\n\n"
		overlay += helpStyle.Render("enter: next  esc: cancel")
	case 1:
		overlay = overlayTitleStyle.Render("New Stream") + stepLabel + "\n\n"
		overlay += helpStyle.Render("Title: "+titleInput.Value()) + "\n\n"
		overlay += "Task:\n"
		overlay += taskInput.View() + "\n\n"
		overlay += helpStyle.Render("enter: next  alt+enter: new line  esc: back")
	case 3:
		pipeline := selectedPipeline(checked, phaseTree)
		overlay = overlayTitleStyle.Render("New Stream") + stepLabel + "\n\n"
		overlay += helpStyle.Render("Title: "+titleInput.Value()) + "\n"
		overlay += helpStyle.Render("Task: "+taskInput.Value()) + "\n\n"
		overlay += "Set breakpoints (pause between phases):\n"
		overlay += helpStyle.Render("  You'll be notified when the stream pauses at each breakpoint.") + "\n\n"
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
		overlay += "\n" + renderNotifyToggles(notify) + "\n"
		overlay += helpStyle.Render("j/k: navigate  space: toggle  enter: create  esc: back")
	default:
		overlay = overlayTitleStyle.Render("New Stream") + stepLabel + "\n\n"
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
		overlay += helpStyle.Render("\nj/k: navigate  space: toggle  enter: next  esc: back")
	}

	box := overlayStyle.Width(overlayWidth(width, 100)).Render(overlay)

	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

func renderNotifyToggles(notify stream.NotifySettings) string {
	dot := func(on bool) string {
		if on {
			return "●"
		}
		return "○"
	}
	return helpStyle.Render(fmt.Sprintf("  Notify: 1 %s bell  2 %s flash  3 %s system", dot(notify.Bell), dot(notify.Flash), dot(notify.System)))
}

func renderEditBreakpointsOverlay(pipeline []string, breakpoints map[int]bool, bpCursor int, notify stream.NotifySettings, width, height int) string {
	overlay := overlayTitleStyle.Render("Edit Breakpoints") + "\n\n"
	overlay += "Set breakpoints (pause between phases):\n"
	overlay += helpStyle.Render("  You'll be notified when the stream pauses at each breakpoint.") + "\n\n"
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
	overlay += "\n" + renderNotifyToggles(notify) + "\n"
	overlay += helpStyle.Render("j/k: navigate  space: toggle  enter: save  esc: cancel")

	box := overlayStyle.Width(overlayWidth(width, 100)).Render(overlay)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

func renderBeadsInitOverlay(width, height int) string {
	overlay := overlayTitleStyle.Render("Initialize Beads") + "\n\n"
	overlay += "This repository doesn't have beads initialized.\n"
	overlay += "Streams uses beads to track issues for each stream.\n\n"
	overlay += "Stealth mode keeps beads files out of git history,\n"
	overlay += "so they won't show up in commits or affect collaborators.\n"
	overlay += "Use this for repos you don't own.\n\n"
	overlay += helpStyle.Render("y: stealth mode  n: normal mode  esc: cancel")

	box := overlayStyle.Width(overlayWidth(width, 80)).Render(overlay)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

func renderGuidanceOverlay(ti textarea.Model, width, height int) string {
	overlay := overlayTitleStyle.Render("Guidance") + "\n\n"
	overlay += ti.View() + "\n\n"
	overlay += helpStyle.Render("ctrl+s: send  esc: cancel")

	box := overlayStyle.Width(overlayWidth(width, 100)).Render(overlay)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

func renderRestartPromptOverlay(width, height int) string {
	overlay := overlayTitleStyle.Render("Restart Stream?") + "\n\n"
	overlay += "The stream was paused for attach.\n"
	overlay += "Would you like to restart it?\n\n"
	overlay += helpStyle.Render("y: restart  n: keep paused")

	box := overlayStyle.Width(overlayWidth(width, 80)).Render(overlay)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

func renderConvergeConfirmOverlay(width, height int) string {
	overlay := overlayTitleStyle.Render("Wrap Up Phase") + "\n\n"
	overlay += "Skip remaining review iterations and converge\n"
	overlay += "the current phase as quickly as possible.\n\n"
	overlay += "The current implement step will finish, but no\n"
	overlay += "further review work will be filed.\n\n"
	overlay += helpStyle.Render("w: confirm  esc: cancel")

	box := overlayStyle.Width(overlayWidth(width, 80)).Render(overlay)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

func renderForceAdvanceOverlay(nextPhase string, width, height int) string {
	overlay := overlayTitleStyle.Render("Skip to Next Phase") + "\n\n"
	overlay += "Force-advance the pipeline, skipping the\n"
	overlay += "current phase without waiting for convergence.\n\n"
	if nextPhase != "" {
		overlay += "Next phase: " + lipgloss.NewStyle().Bold(true).Foreground(colorPrimary).Render(nextPhase) + "\n\n"
	}
	overlay += helpStyle.Render(">: confirm  esc: cancel")

	box := overlayStyle.Width(overlayWidth(width, 80)).Render(overlay)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

func renderDeleteConfirmOverlay(name string, width, height int) string {
	overlay := overlayTitleStyle.Render("Delete Stream") + "\n\n"
	overlay += fmt.Sprintf("Delete %q?\n\n", name)
	overlay += helpStyle.Render("d: delete + clean up branch/beads  k: keep branch/beads  esc: cancel")

	box := overlayStyle.Width(overlayWidth(width, 80)).Render(overlay)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

func renderCompleteOverlay(ti textarea.Model, width, height int) string {
	overlay := overlayTitleStyle.Render("Complete Stream") + "\n\n"
	overlay += helpStyle.Render("Renames the branch, removes the worktree, and marks the stream as done.") + "\n\n"
	overlay += "Branch name:\n"
	overlay += ti.View() + "\n\n"
	overlay += helpStyle.Render("ctrl+s: complete  esc: cancel")

	box := overlayStyle.Width(overlayWidth(width, 100)).Render(overlay)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

func renderReviseOverlay(phases []string, cursor, step int, feedback textarea.Model, width, height int) string {
	overlay := overlayTitleStyle.Render("Revise Stream") + "\n\n"

	if step == 1 {
		if cursor >= 0 && cursor < len(phases) {
			overlay += helpStyle.Render("Target phase: "+phases[cursor]) + "\n\n"
		}
		overlay += "Feedback (optional):\n"
		overlay += feedback.View() + "\n\n"
		overlay += helpStyle.Render("ctrl+s: revise  esc: back")
	} else {
		overlay += "Select a phase to continue from:\n\n"
		for i, name := range phases {
			prefix := "  "
			if i == cursor {
				prefix = cursorStyle.Render("> ")
			}
			label := name
			if i == cursor {
				label = selectedRowStyle.Render(label)
			}
			overlay += prefix + label + "\n"
		}
		overlay += "\n" + helpStyle.Render("j/k: navigate  enter: select  esc: cancel")
	}

	cap := 80
	if step == 1 {
		cap = 100
	}
	box := overlayStyle.Width(overlayWidth(width, cap)).Render(overlay)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

// overlayWidth returns a capped overlay width. Standard overlays cap at 80,
// text-heavy overlays (with textareas) cap at 100.
func overlayWidth(termWidth, maxCap int) int {
	w := termWidth - 6
	if w > maxCap {
		w = maxCap
	}
	if w < 40 {
		w = 40
	}
	return w
}

// isPausedAtReview returns true when the stream is paused and converged at
// the review phase with no error — the state where c/r actions are available.
func isPausedAtReview(st *stream.Stream) bool {
	if st.GetStatus() != stream.StatusPaused || !st.Converged || st.GetLastError() != nil {
		return false
	}
	return currentPhase(st) == "review"
}

// slugify converts a title into a branch-name-friendly slug.
func slugify(s string) string {
	s = strings.ToLower(s)
	s = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' {
			return r
		}
		if r == ' ' || r == '_' {
			return '-'
		}
		return -1
	}, s)
	// Collapse multiple dashes.
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	s = strings.Trim(s, "-")
	return s
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

func (m Model) withError(msg string) Model {
	m.errorMsg = msg
	m.errorTTL = 60 // ~5 seconds at 80ms/tick
	return m
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

// autoSizeNewStreamInput adjusts the task textarea height to fit its content,
// capped so the overlay doesn't exceed the terminal height.
func (m *Model) autoSizeNewStreamInput() {
	// Overlay chrome for step 1: title+step (1) + blank (1) + "Title: ..." (1) +
	// blank (1) + "Task:" (1) + blank after textarea (1) + help line (1) +
	// border (2) + padding (2) = 11 lines, plus 2 for top/bottom margin.
	const chrome = 13
	maxH := m.height - chrome
	if maxH < 3 {
		maxH = 3
	}
	h := m.newStreamInput.LineCount()
	if h < 3 {
		h = 3
	}
	if h > maxH {
		h = maxH
	}
	m.newStreamInput.SetHeight(h)
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

// canForceAdvance returns true when the stream can be force-advanced to the
// next pipeline phase (paused/stopped, not at the last phase).
func canForceAdvance(st *stream.Stream) bool {
	status := st.GetStatus()
	if status != stream.StatusPaused && status != stream.StatusStopped {
		return false
	}
	pipeline := st.GetPipeline()
	return st.GetPipelineIndex() < len(pipeline)-1
}

// canDiagnose returns true when the stream is in a state suitable for diagnosis
// (paused, stopped, errored, or completed — but not running or just created).
func canDiagnose(st *stream.Stream) bool {
	switch st.GetStatus() {
	case stream.StatusPaused, stream.StatusStopped, stream.StatusCompleted:
		return true
	default:
		return false
	}
}

// startDiagnose launches an interactive diagnosis claude session in a new
// terminal tab for the given stream.
func (m Model) startDiagnose(id string) (tea.Model, tea.Cmd) {
	st := m.orch.Get(id)
	if st == nil {
		return m.setStatus("Stream not found.")
	}
	if !canDiagnose(st) {
		return m.setStatus("Stop the stream before diagnosing.")
	}
	if err := m.orch.Diagnose(id); err != nil {
		m = m.withError("Diagnose error: " + err.Error())
		return m, nil
	}
	return m.setStatus("Diagnosis launched in new tab.")
}
