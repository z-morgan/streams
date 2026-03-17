package ui

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/zmorgan/streams/internal/models"
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
	orch         *orchestrator.Orchestrator
	modelFetcher *models.Fetcher
	width        int
	height       int
	view         view
	dashboard    dashboardView
	detail       detailView

	// The currently selected stream ID (set when entering detail view).
	selectedID string

	// Guidance overlay state.
	showGuidance  bool
	guidanceInput textarea.Model

	// Blocker picker overlay state.
	showBlockers     bool
	blockerCursor    int
	blockerChecked   map[string]bool
	blockerStreamIDs []string

	// New stream overlay state.
	showNewStream             bool
	newStreamTitle            textarea.Model
	newStreamInput            textarea.Model
	newStreamStep             int                   // 0 = title, 1 = task, 2 = phases, 3 = models, 4 = breakpoints
	newStreamPhaseCur         int                   // cursor into flattened phase list
	newStreamChecked          map[string]bool       // which phases are checked
	newStreamModelCursor      int                   // cursor into model list
	newStreamModels           stream.ModelConfig    // selected model config
	newStreamPerPhase         bool                  // per-phase model selection mode
	newStreamPhaseModelCursor int                   // which phase row is focused (per-phase mode)
	newStreamFallback         stream.FallbackConfig // fallback model config
	newStreamFBCursor         int                   // cursor into ollama models for fallback picker
	newStreamShowFallback     bool                  // whether fallback picker is visible
	newStreamBreakpoints      map[int]bool          // which pipeline gaps have breakpoints
	newStreamBPCursor         int                   // cursor into breakpoint gaps
	newStreamNotify           stream.NotifySettings // notification toggles
	newStreamOverlayWidth     int                   // dynamic overlay width for task step
	creating                  bool                  // true while orch.Create is running

	// Quit confirmation overlay state.
	showQuitConfirm bool

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
	pendingModels      stream.ModelConfig    // model config stashed while waiting for stealth answer

	// Stream config overlay state (replaces edit breakpoints).
	showStreamConfig     bool
	streamConfigTab      int          // 0=breakpoints, 1=models, 2=fallback
	editBPMap            map[int]bool // which pipeline gaps have breakpoints
	editBPCursor         int          // cursor into breakpoint gaps
	editBPNotify         stream.NotifySettings
	editModelConfig      stream.ModelConfig    // model editing state
	editModelCursor      int                   // cursor into model list
	editPerPhase         bool                  // per-phase mode
	editPhaseModelCursor int                   // which phase row is focused
	editFallback         stream.FallbackConfig // fallback editing state
	editFBCursor         int                   // cursor into ollama models for fallback

	// Help modal overlay state.
	showHelp   bool
	helpScroll int

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
	reviseStep        int  // 0 = phase picker, 1 = feedback input, 2 = enqueue/replace picker
	reviseReplace     bool // true = scrap & replace, false = enqueue

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
		modelFetcher:   &models.Fetcher{},
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
	// Fetch available models from the Anthropic API in the background.
	m.modelFetcher.FetchAsync()

	// Auto-resume streams that were running or created when the app last exited.
	for _, st := range m.orch.List() {
		status := st.GetStatus()
		if status == stream.StatusCreated || status == stream.StatusRunning {
			_ = m.orch.Start(st.ID)
		}
	}
	return tea.Batch(tea.RequestWindowSize, spinnerTick())
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle window resize globally before overlays.
	if msg, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = msg.Width
		m.height = msg.Height
		m.autoSizeNewStreamInput()
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
	if _, ok := msg.(tea.KeyPressMsg); ok {
		m.errorMsg = ""
		m.statusMsg = ""
	}

	// Handle quit confirmation overlay if active.
	if m.showQuitConfirm {
		return m.updateQuitConfirm(msg)
	}

	// Handle help modal overlay if active.
	if m.showHelp {
		return m.updateHelp(msg)
	}

	// Open help on ? keypress (before other overlays so it's always reachable).
	if key, ok := msg.(tea.KeyPressMsg); ok && key.String() == "?" {
		m.showHelp = true
		m.helpScroll = 0
		return m, nil
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
	if m.showStreamConfig {
		return m.updateStreamConfig(msg)
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

	// Handle blocker picker overlay if active.
	if m.showBlockers {
		return m.updateBlockers(msg)
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch m.view {
		case viewDashboard:
			return m.updateDashboard(msg)
		case viewDetail:
			return m.updateDetail(msg)
		}

	case tea.MouseWheelMsg:
		if m.view == viewDetail && m.detail.focusRight {
			return m.handleDetailScroll(msg), nil
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
		modelConfig := m.pendingModels
		orch := m.orch
		return m, func() tea.Msg {
			st, err := orch.Create(title, task, pipeline, breakpoints, notify, modelConfig)
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

func (m Model) updateDashboard(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	streams := m.orch.List()
	m.dashboard.clampCursor(len(streams))

	switch msg.String() {
	case "q", "ctrl+c":
		m.showQuitConfirm = true
		return m, nil

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
			// Skip non-selectable rows (pending revise indicator).
			if m.detail.iterCursor > 0 && m.detail.iterCursor < len(rows) && rows[m.detail.iterCursor].IsPendingRevise {
				m.detail.iterCursor--
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
		m.autoSizeNewStreamInput()
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
			return m.setStatus("Pausing after current step completes…")
		} else {
			return m.setStatus("No stream selected.")
		}

	case "X":
		if st := m.selectedStream(); st != nil {
			m.orch.Kill(st.ID)
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

	case "b":
		if st := m.selectedStream(); st != nil {
			m.selectedID = st.ID
			currentBlockers := st.GetBlockedBy()
			checked := make(map[string]bool)
			for _, bid := range currentBlockers {
				checked[bid] = true
			}
			var otherIDs []string
			for _, s := range streams {
				if s.ID != st.ID {
					otherIDs = append(otherIDs, s.ID)
				}
			}
			if len(otherIDs) == 0 {
				return m.setStatus("No other streams to set as blockers.")
			}
			m.blockerChecked = checked
			m.blockerStreamIDs = otherIDs
			m.blockerCursor = 0
			m.showBlockers = true
		} else {
			return m.setStatus("No stream selected.")
		}

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

func (m Model) updateDetail(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
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
		// Skip non-selectable rows (pending revise indicator).
		if m.detail.iterCursor < len(rows) && rows[m.detail.iterCursor].IsPendingRevise {
			m.detail.iterCursor++
			m.detail.clampCursor(len(rows))
		}
		m.detail.tailScroll = 0
		m.detail.artifactScroll = 0

	case "k", "up":
		m.detail.iterCursor--
		m.detail.clampCursor(len(rows))
		// Skip non-selectable rows (pending revise indicator).
		if m.detail.iterCursor >= 0 && m.detail.iterCursor < len(rows) && rows[m.detail.iterCursor].IsPendingRevise {
			m.detail.iterCursor--
			m.detail.clampCursor(len(rows))
		}
		m.detail.tailScroll = 0
		m.detail.artifactScroll = 0

	case "enter":
		cursor := m.detail.iterCursor
		if cursor >= 0 && cursor < len(rows) && rows[cursor].IsInProgress {
			m.detail.focusRight = true
			m.detail.tailScroll = 0
		} else if m.detail.showArtifact && cursor >= 0 && cursor < len(rows) && !rows[cursor].IsInitialPrompt {
			// Focus right pane for artifact scrolling.
			if idx := rows[cursor].SnapshotIndex; idx >= 0 && idx < len(snaps) && snaps[idx].Artifact != "" {
				m.detail.focusRight = true
				m.detail.artifactScroll = 0
			}
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
			return m.setStatus("Pausing after current step completes…")
		}

	case "X":
		if st != nil {
			m.orch.Kill(st.ID)
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
				mc := st.GetModels()
				m.editModelConfig = mc
				m.editModelCursor = 0
				m.editPerPhase = len(mc.PerPhase) > 0
				m.editPhaseModelCursor = 0
				m.editFallback = st.GetFallback()
				m.editFBCursor = 0
				m.streamConfigTab = 0
				m.showStreamConfig = true
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
		if st != nil && st.GetStatus() != stream.StatusCompleted && st.GetPipelineIndex() > 0 {
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
			if st.GetPendingRevise() != nil {
				m.reviseStep = -1 // pending confirm step
			} else {
				m.reviseStep = 0
			}
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

func (m Model) updateDetailFocusRight(msg tea.KeyPressMsg, st *stream.Stream) (tea.Model, tea.Cmd) {
	if m.detail.showArtifact {
		return m.updateArtifactScroll(msg, st)
	}

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

func (m Model) updateArtifactScroll(msg tea.KeyPressMsg, st *stream.Stream) (tea.Model, tea.Cmd) {
	maxScroll := m.computeArtifactMaxScroll(st)

	switch msg.String() {
	case "esc":
		m.detail.focusRight = false
		return m, nil

	case "f":
		m.detail.showArtifact = false
		m.detail.focusRight = false
		m.detail.artifactScroll = 0

	case "j", "down":
		if m.detail.artifactScroll < maxScroll {
			m.detail.artifactScroll++
		}

	case "k", "up":
		if m.detail.artifactScroll > 0 {
			m.detail.artifactScroll--
		}

	case "G":
		m.detail.artifactScroll = maxScroll
	}

	return m, nil
}

func (m Model) computeArtifactMaxScroll(st *stream.Stream) int {
	if st == nil {
		return 0
	}
	snaps := st.GetSnapshots()
	rows := buildIterationList(st)
	cursor := m.detail.iterCursor
	if cursor < 0 || cursor >= len(rows) {
		return 0
	}
	idx := rows[cursor].SnapshotIndex
	if idx < 0 || idx >= len(snaps) || snaps[idx].Artifact == "" {
		return 0
	}
	leftWidth := 27
	rightWidth := m.width - leftWidth
	if rightWidth < 14 {
		rightWidth = 14
	}
	innerRight := rightWidth - 2
	rendered := renderArtifactDetail(snaps, idx, innerRight)
	totalLines := strings.Count(rendered, "\n") + 1

	paneHeight := m.height - 4
	innerHeight := paneHeight - 2
	visibleLines := innerHeight - 1 // -1 for status marker line
	if visibleLines < 1 {
		visibleLines = 1
	}
	maxScroll := totalLines - visibleLines
	if maxScroll < 0 {
		return 0
	}
	return maxScroll
}

func (m Model) handleDetailScroll(msg tea.MouseWheelMsg) Model {
	scrollAmount := 3
	st := m.orch.Get(m.selectedID)

	if m.detail.showArtifact {
		maxScroll := m.computeArtifactMaxScroll(st)
		if msg.Button == tea.MouseWheelUp {
			m.detail.artifactScroll -= scrollAmount
			if m.detail.artifactScroll < 0 {
				m.detail.artifactScroll = 0
			}
		} else if msg.Button == tea.MouseWheelDown {
			m.detail.artifactScroll += scrollAmount
			if m.detail.artifactScroll > maxScroll {
				m.detail.artifactScroll = maxScroll
			}
		}
	} else {
		lineCount := 0
		if st != nil {
			lineCount = len(st.GetOutputLines())
		}
		maxScroll := lineCount - 5
		if maxScroll < 0 {
			maxScroll = 0
		}
		if msg.Button == tea.MouseWheelUp {
			m.detail.tailScroll += scrollAmount
			if m.detail.tailScroll > maxScroll {
				m.detail.tailScroll = maxScroll
			}
		} else if msg.Button == tea.MouseWheelDown {
			m.detail.tailScroll -= scrollAmount
			if m.detail.tailScroll < 0 {
				m.detail.tailScroll = 0
			}
		}
	}

	return m
}

func (m Model) updateDetailBeadBrowse(msg tea.KeyPressMsg, st *stream.Stream, rows []iterationRow) (tea.Model, tea.Cmd) {
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
	if m.newStreamStep == 4 {
		return m.updateNewStreamBreakpoints(msg)
	}
	if m.newStreamStep == 3 {
		return m.updateNewStreamModels(msg)
	}
	if m.newStreamStep == 2 {
		return m.updateNewStreamPipeline(msg)
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
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

		case "shift+enter":
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
	case tea.KeyPressMsg:
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

		case "space":
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
			m.newStreamStep = 3
			m.newStreamModelCursor = 0
			m.newStreamModels = stream.ModelConfig{}
			m.newStreamPerPhase = false
			return m, nil
		}
	}

	return m, nil
}

func (m Model) updateNewStreamModels(msg tea.Msg) (tea.Model, tea.Cmd) {
	modelOptions := m.modelFetcher.AllOptions()
	pipeline := selectedPipeline(m.newStreamChecked, phaseTree)

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			m.newStreamStep = 2
			return m, nil

		case "tab":
			m.newStreamPerPhase = !m.newStreamPerPhase
			if m.newStreamPerPhase {
				// Initialize per-phase map from current default.
				if m.newStreamModels.PerPhase == nil {
					m.newStreamModels.PerPhase = make(map[string]string)
				}
				m.newStreamPhaseModelCursor = 0
			}
			return m, nil

		case "j", "down":
			if m.newStreamShowFallback {
				ollamaOpts := m.modelFetcher.OllamaOptions()
				if m.newStreamFBCursor < len(ollamaOpts)-1 {
					m.newStreamFBCursor++
				}
			} else if m.newStreamPerPhase {
				if m.newStreamPhaseModelCursor < len(pipeline)-1 {
					m.newStreamPhaseModelCursor++
				}
			} else {
				if m.newStreamModelCursor < len(modelOptions)-1 {
					m.newStreamModelCursor++
				}
			}
			return m, nil

		case "k", "up":
			if m.newStreamShowFallback {
				if m.newStreamFBCursor > 0 {
					m.newStreamFBCursor--
				}
			} else if m.newStreamPerPhase {
				if m.newStreamPhaseModelCursor > 0 {
					m.newStreamPhaseModelCursor--
				}
			} else {
				if m.newStreamModelCursor > 0 {
					m.newStreamModelCursor--
				}
			}
			return m, nil

		case "space":
			if m.newStreamShowFallback {
				ollamaOpts := m.modelFetcher.OllamaOptions()
				if m.newStreamFBCursor >= 0 && m.newStreamFBCursor < len(ollamaOpts) {
					selected := ollamaOpts[m.newStreamFBCursor]
					if m.newStreamFallback.Model == selected {
						m.newStreamFallback.Enabled = false
						m.newStreamFallback.Model = ""
					} else {
						m.newStreamFallback.Enabled = true
						m.newStreamFallback.Model = selected
					}
				}
			} else if !m.newStreamPerPhase {
				if m.newStreamModelCursor >= 0 && m.newStreamModelCursor < len(modelOptions) {
					m.newStreamModels.Default = modelOptions[m.newStreamModelCursor]
				}
			}
			return m, nil

		case "h", "left":
			if m.newStreamPerPhase && len(pipeline) > 0 {
				phaseName := pipeline[m.newStreamPhaseModelCursor]
				current := m.newStreamModels.PerPhase[phaseName]
				idx := modelOptionIndex(modelOptions, current)
				idx--
				if idx < 0 {
					idx = len(modelOptions) - 1
				}
				m.newStreamModels.PerPhase[phaseName] = modelOptions[idx]
			}
			return m, nil

		case "l", "right":
			if m.newStreamPerPhase && len(pipeline) > 0 {
				phaseName := pipeline[m.newStreamPhaseModelCursor]
				current := m.newStreamModels.PerPhase[phaseName]
				idx := modelOptionIndex(modelOptions, current)
				idx++
				if idx >= len(modelOptions) {
					idx = 0
				}
				m.newStreamModels.PerPhase[phaseName] = modelOptions[idx]
			}
			return m, nil

		case "f":
			m.newStreamShowFallback = !m.newStreamShowFallback
			if m.newStreamShowFallback {
				m.newStreamFBCursor = 0
			}
			return m, nil

		case "enter":
			if m.newStreamPerPhase {
				// Clear default when using per-phase config.
				m.newStreamModels.Default = ""
			}
			if len(pipeline) > 1 {
				m.newStreamStep = 4
				m.newStreamBreakpoints = make(map[int]bool)
				m.newStreamBPCursor = 0
				return m, nil
			}
			return m.createStream(pipeline, nil)
		}
	}

	return m, nil
}

// modelOptionIndex returns the index of value in options, or 0 if not found.
func modelOptionIndex(options []string, value string) int {
	for i, opt := range options {
		if opt == value {
			return i
		}
	}
	return 0
}

func (m Model) updateNewStreamBreakpoints(msg tea.Msg) (tea.Model, tea.Cmd) {
	pipeline := selectedPipeline(m.newStreamChecked, phaseTree)
	gapCount := len(pipeline) - 1

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			m.newStreamStep = 3
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

		case "space":
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

func (m Model) updateStreamConfig(msg tea.Msg) (tea.Model, tea.Cmd) {
	st := m.orch.Get(m.selectedID)
	if st == nil {
		m.showStreamConfig = false
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			m.showStreamConfig = false
			return m, nil

		case "tab":
			m.streamConfigTab = (m.streamConfigTab + 1) % 3
			return m, nil

		case "enter":
			// Save breakpoints, models, and fallback.
			pipeline := st.GetPipeline()
			gapCount := len(pipeline) - 1
			var breakpoints []int
			for i := 0; i < gapCount; i++ {
				if m.editBPMap[i] {
					breakpoints = append(breakpoints, i)
				}
			}
			st.SetBreakpoints(breakpoints)
			st.SetNotify(m.editBPNotify)
			if m.editPerPhase {
				m.editModelConfig.Default = ""
			}
			st.SetModels(m.editModelConfig)
			st.SetFallback(m.editFallback)
			m.showStreamConfig = false
			return m, nil

		default:
			switch m.streamConfigTab {
			case 0:
				return m.updateStreamConfigBreakpoints(msg)
			case 1:
				return m.updateStreamConfigModels(msg)
			case 2:
				return m.updateStreamConfigFallback(msg)
			}
		}
	}

	return m, nil
}

func (m Model) updateStreamConfigBreakpoints(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	st := m.orch.Get(m.selectedID)
	if st == nil {
		return m, nil
	}
	pipeline := st.GetPipeline()
	gapCount := len(pipeline) - 1

	switch msg.String() {
	case "j", "down":
		if m.editBPCursor < gapCount-1 {
			m.editBPCursor++
		}
	case "k", "up":
		if m.editBPCursor > 0 {
			m.editBPCursor--
		}
	case "space":
		m.editBPMap[m.editBPCursor] = !m.editBPMap[m.editBPCursor]
	case "1":
		m.editBPNotify.Bell = !m.editBPNotify.Bell
	case "2":
		m.editBPNotify.Flash = !m.editBPNotify.Flash
	case "3":
		m.editBPNotify.System = !m.editBPNotify.System
	}

	return m, nil
}

func (m Model) updateStreamConfigModels(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	st := m.orch.Get(m.selectedID)
	if st == nil {
		return m, nil
	}
	modelOptions := m.modelFetcher.AllOptions()
	pipeline := st.GetPipeline()

	switch msg.String() {
	case "j", "down":
		if m.editPerPhase {
			if m.editPhaseModelCursor < len(pipeline)-1 {
				m.editPhaseModelCursor++
			}
		} else {
			if m.editModelCursor < len(modelOptions)-1 {
				m.editModelCursor++
			}
		}
	case "k", "up":
		if m.editPerPhase {
			if m.editPhaseModelCursor > 0 {
				m.editPhaseModelCursor--
			}
		} else {
			if m.editModelCursor > 0 {
				m.editModelCursor--
			}
		}
	case "space":
		if !m.editPerPhase {
			if m.editModelCursor >= 0 && m.editModelCursor < len(modelOptions) {
				m.editModelConfig.Default = modelOptions[m.editModelCursor]
			}
		} else {
			// Toggle per-phase off
			m.editPerPhase = false
		}
	case "h", "left":
		if m.editPerPhase && len(pipeline) > 0 {
			phaseName := pipeline[m.editPhaseModelCursor]
			if m.editModelConfig.PerPhase == nil {
				m.editModelConfig.PerPhase = make(map[string]string)
			}
			current := m.editModelConfig.PerPhase[phaseName]
			idx := modelOptionIndex(modelOptions, current)
			idx--
			if idx < 0 {
				idx = len(modelOptions) - 1
			}
			m.editModelConfig.PerPhase[phaseName] = modelOptions[idx]
		}
	case "l", "right":
		if m.editPerPhase && len(pipeline) > 0 {
			phaseName := pipeline[m.editPhaseModelCursor]
			if m.editModelConfig.PerPhase == nil {
				m.editModelConfig.PerPhase = make(map[string]string)
			}
			current := m.editModelConfig.PerPhase[phaseName]
			idx := modelOptionIndex(modelOptions, current)
			idx++
			if idx >= len(modelOptions) {
				idx = 0
			}
			m.editModelConfig.PerPhase[phaseName] = modelOptions[idx]
		}
	case "p":
		// Toggle per-phase mode
		m.editPerPhase = !m.editPerPhase
		if m.editPerPhase && m.editModelConfig.PerPhase == nil {
			m.editModelConfig.PerPhase = make(map[string]string)
		}
		m.editPhaseModelCursor = 0
	}

	return m, nil
}

func (m Model) updateStreamConfigFallback(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	ollamaOpts := m.modelFetcher.OllamaOptions()

	switch msg.String() {
	case "space":
		m.editFallback.Enabled = !m.editFallback.Enabled
		if m.editFallback.Enabled && m.editFallback.Model == "" && len(ollamaOpts) > 0 {
			m.editFallback.Model = ollamaOpts[0]
		}
	case "j", "down":
		if m.editFallback.Enabled && m.editFBCursor < len(ollamaOpts)-1 {
			m.editFBCursor++
			m.editFallback.Model = ollamaOpts[m.editFBCursor]
		}
	case "k", "up":
		if m.editFallback.Enabled && m.editFBCursor > 0 {
			m.editFBCursor--
			m.editFallback.Model = ollamaOpts[m.editFBCursor]
		}
	}

	return m, nil
}

func (m Model) createStream(pipeline []string, breakpoints []int) (tea.Model, tea.Cmd) {
	title := m.newStreamTitle.Value()
	task := m.newStreamInput.Value()
	notify := m.newStreamNotify
	modelConfig := m.newStreamModels
	fallbackConfig := m.newStreamFallback
	m.showNewStream = false
	m.newStreamStep = 0
	if m.orch.NeedsBeadsInit() {
		m.pendingTitle = title
		m.pendingTask = task
		m.pendingPipeline = pipeline
		m.pendingBreakpoints = breakpoints
		m.pendingNotify = notify
		m.pendingModels = modelConfig
		m.showBeadsInit = true
		return m, nil
	}
	m.creating = true
	orch := m.orch
	return m, func() tea.Msg {
		st, err := orch.Create(title, task, pipeline, breakpoints, notify, modelConfig)
		if err == nil && (fallbackConfig.Enabled || fallbackConfig.Model != "") {
			st.SetFallback(fallbackConfig)
		}
		return streamCreatedMsg{stream: st, err: err}
	}
}

func (m Model) updateBeadsInit(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
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
			m.pendingModels = stream.ModelConfig{}
			return m, nil
		}
	}
	return m, nil
}

func (m Model) updateGuidance(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			m.showGuidance = false
			return m, nil

		case "super+s", "ctrl+s":
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

func (m Model) updateBlockers(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			m.showBlockers = false
			return m, nil

		case "j", "down":
			if m.blockerCursor < len(m.blockerStreamIDs)-1 {
				m.blockerCursor++
			}
			return m, nil

		case "k", "up":
			if m.blockerCursor > 0 {
				m.blockerCursor--
			}
			return m, nil

		case "space":
			if m.blockerCursor < len(m.blockerStreamIDs) {
				id := m.blockerStreamIDs[m.blockerCursor]
				if m.blockerChecked[id] {
					delete(m.blockerChecked, id)
				} else {
					m.blockerChecked[id] = true
				}
			}
			return m, nil

		case "enter":
			var selected []string
			for _, id := range m.blockerStreamIDs {
				if m.blockerChecked[id] {
					selected = append(selected, id)
				}
			}
			if err := m.orch.SetBlockedBy(m.selectedID, selected); err != nil {
				m.showBlockers = false
				return m.setStatus("Error: " + err.Error())
			}
			m.showBlockers = false
			if len(selected) > 0 {
				return m.setStatus(fmt.Sprintf("Set %d blocker(s).", len(selected)))
			}
			return m.setStatus("Cleared blockers.")
		}
	}
	return m, nil
}

func (m Model) updateRestartPrompt(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
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
	case tea.KeyPressMsg:
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
	case tea.KeyPressMsg:
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

func (m Model) updateHelp(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc", "?":
			m.showHelp = false
			return m, nil
		case "j", "down":
			m.helpScroll++
			return m, nil
		case "k", "up":
			if m.helpScroll > 0 {
				m.helpScroll--
			}
			return m, nil
		case "G":
			m.helpScroll = len(Bindings) // clamped during render
			return m, nil
		case "g":
			m.helpScroll = 0
			return m, nil
		}
	}
	return m, nil
}

func (m Model) updateQuitConfirm(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "y", "enter":
			return m, tea.Quit
		case "n", "esc":
			m.showQuitConfirm = false
			return m, nil
		}
	}
	return m, nil
}

func (m Model) updateDeleteConfirm(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
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
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			m.showComplete = false
			return m, nil

		case "super+s", "ctrl+s":
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

	if m.reviseStep == -1 {
		// Pending revise confirm step: r to replace, esc to cancel.
		switch msg := msg.(type) {
		case tea.KeyPressMsg:
			switch msg.String() {
			case "esc":
				st.SetPendingRevise(nil)
				m.showRevise = false
				return m, nil
			case "r":
				st.SetPendingRevise(nil)
				m.reviseStep = 0
				return m, nil
			}
		}
		return m, nil
	}

	if m.reviseStep == 2 {
		// Enqueue/replace picker step (only shown when stream is running).
		switch msg := msg.(type) {
		case tea.KeyPressMsg:
			switch msg.String() {
			case "esc":
				m.reviseStep = 1
				m.reviseFeedback.Focus()
				return m, textarea.Blink

			case "j", "down", "k", "up", "tab":
				m.reviseReplace = !m.reviseReplace
				return m, nil

			case "enter":
				m.showRevise = false
				feedback := m.reviseFeedback.Value()
				targetIdx := m.revisePhaseCursor
				replace := m.reviseReplace
				id := m.selectedID
				orch := m.orch
				return m, func() tea.Msg {
					return streamRevisedMsg{err: orch.Revise(id, targetIdx, feedback, replace)}
				}
			}
		}
		return m, nil
	}

	if m.reviseStep == 1 {
		// Feedback input step.
		isRunning := st.GetStatus() == stream.StatusRunning
		switch msg := msg.(type) {
		case tea.KeyPressMsg:
			switch msg.String() {
			case "esc":
				m.reviseStep = 0
				return m, nil

			case "super+s", "ctrl+s":
				if isRunning {
					m.reviseReplace = false
					m.reviseStep = 2
					return m, nil
				}
				m.showRevise = false
				feedback := m.reviseFeedback.Value()
				targetIdx := m.revisePhaseCursor
				id := m.selectedID
				orch := m.orch
				return m, func() tea.Msg {
					return streamRevisedMsg{err: orch.Revise(id, targetIdx, feedback, false)}
				}
			}
		}

		var cmd tea.Cmd
		m.reviseFeedback, cmd = m.reviseFeedback.Update(msg)
		return m, cmd
	}

	// Phase picker step.
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
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

func (m Model) View() tea.View {
	v := tea.NewView(m.viewString())
	v.AltScreen = true
	if m.view == viewDetail && m.detail.focusRight {
		v.MouseMode = tea.MouseModeCellMotion
	}
	return v
}

func (m Model) viewString() string {
	if m.showQuitConfirm {
		runningCount := 0
		for _, st := range m.orch.List() {
			if st.GetStatus() == stream.StatusRunning {
				runningCount++
			}
		}
		return renderQuitConfirmOverlay(runningCount, m.width, m.height)
	}

	if m.showHelp {
		return renderHelpOverlay(m.helpScopes(), m.helpTitle(), m.helpScroll, m.width, m.height)
	}

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
		return renderNewStreamOverlay(m.newStreamTitle, m.newStreamInput, m.newStreamStep, m.newStreamPhaseCur, m.newStreamChecked, m.newStreamModelCursor, m.newStreamModels, m.newStreamPerPhase, m.newStreamPhaseModelCursor, m.modelFetcher.Sections(), m.modelFetcher.OllamaRunning(), m.newStreamFallback, m.newStreamShowFallback, m.newStreamFBCursor, m.newStreamBreakpoints, m.newStreamBPCursor, m.newStreamNotify, m.newStreamOverlayWidth, m.width, m.height)
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

	if m.showStreamConfig {
		st := m.orch.Get(m.selectedID)
		if st != nil {
			return renderStreamConfigOverlay(st.GetPipeline(), m.streamConfigTab, m.editBPMap, m.editBPCursor, m.editBPNotify, m.editModelConfig, m.editModelCursor, m.editPerPhase, m.editPhaseModelCursor, m.modelFetcher.Sections(), m.modelFetcher.OllamaRunning(), m.editFallback, m.editFBCursor, m.width, m.height)
		}
	}

	if m.showComplete {
		return renderCompleteOverlay(m.completeInput, m.width, m.height)
	}

	if m.showRevise {
		st := m.orch.Get(m.selectedID)
		var phases []string
		isRunning := false
		var pendingTarget string
		if st != nil {
			pipeline := st.GetPipeline()
			idx := st.GetPipelineIndex()
			if idx <= len(pipeline) {
				phases = pipeline[:idx]
			}
			isRunning = st.GetStatus() == stream.StatusRunning
			if pr := st.GetPendingRevise(); pr != nil && pr.TargetPhaseIndex < len(pipeline) {
				pendingTarget = pipeline[pr.TargetPhaseIndex]
			}
		}
		return renderReviseOverlay(phases, m.revisePhaseCursor, m.reviseStep, isRunning, m.reviseReplace, pendingTarget, m.reviseFeedback, m.width, m.height)
	}

	if m.showGuidance {
		return renderGuidanceOverlay(m.guidanceInput, m.width, m.height)
	}

	if m.showBlockers {
		return renderBlockersOverlay(m.blockerStreamIDs, m.blockerChecked, m.blockerCursor, m.orch, m.width, m.height)
	}

	switch m.view {
	case viewDashboard:
		streams := m.orch.List()
		m.dashboard.clampCursor(len(streams))

		// Build active blocker counts for queued indicator.
		activeBlockers := make(map[string]int)
		for _, st := range streams {
			if n := len(m.orch.ActiveBlockers(st.ID)); n > 0 {
				activeBlockers[st.ID] = n
			}
		}

		var body string
		frame := spinnerFrames[m.spinnerFrame%len(spinnerFrames)]
		switch m.dashboard.mode {
		case modeChannels:
			body = renderChannels(streams, m.dashboard.cursor, m.dashboard.scrollLeft, m.width, m.height, frame, activeBlockers)
		default:
			body = renderDashboardList(streams, m.dashboard.cursor, m.width, m.height, frame, activeBlockers)
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
		if statusLine == "" && !m.orch.EnvironmentConfigured() {
			statusLine = helpStyle.Render("No environment configured. Run `streams init` to set up Docker Compose.")
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

func renderNewStreamOverlay(titleInput, taskInput textarea.Model, step, phaseCursor int, checked map[string]bool, modelCursor int, modelConfig stream.ModelConfig, perPhase bool, phaseModelCursor int, modelSections []models.Section, ollamaRunning bool, fallback stream.FallbackConfig, showFallback bool, fbCursor int, breakpoints map[int]bool, bpCursor int, notify stream.NotifySettings, taskOverlayWidth, width, height int) string {
	var overlay string

	totalSteps := 4
	pipeline := selectedPipeline(checked, phaseTree)
	if len(pipeline) > 1 {
		totalSteps = 5
	}
	stepLabel := helpStyle.Render(fmt.Sprintf("  Step %d of %d", step+1, totalSteps))

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
		overlay += helpStyle.Render("enter: next  shift+enter: new line  esc: back")
	case 3:
		overlay = overlayTitleStyle.Render("New Stream") + stepLabel + "\n\n"
		overlay += helpStyle.Render("Title: "+titleInput.Value()) + "\n"
		overlay += helpStyle.Render("Task: "+taskInput.Value()) + "\n\n"
		if perPhase {
			overlay += "  " + helpStyle.Render("All Phases") + "    " + selectedRowStyle.Render("Per Phase") + "\n\n"
			selectedPipe := selectedPipeline(checked, phaseTree)
			for i, phaseName := range selectedPipe {
				cursor := "  "
				if i == phaseModelCursor {
					cursor = cursorStyle.Render("> ")
				}
				phaseModel := modelConfig.PerPhase[phaseName]
				if phaseModel == "" {
					phaseModel = "sonnet"
				}
				label := fmt.Sprintf("%-12s %s", phaseName, phaseModel)
				if i == phaseModelCursor {
					label = selectedRowStyle.Render(label)
				}
				overlay += cursor + label + "\n"
			}
			overlay += "\n"
			overlay += renderInlineFallback(showFallback, fallback, fbCursor, modelSections, ollamaRunning)
			overlay += "\n" + helpStyle.Render("j/k: navigate phase  h/l: cycle model  tab: all-phases  f: fallback  enter: confirm  esc: back")
		} else {
			overlay += "  " + selectedRowStyle.Render("All Phases") + "    " + helpStyle.Render("Per Phase") + "\n\n"
			selected := modelConfig.Default
			overlay += renderGroupedModelList(modelSections, ollamaRunning, selected, modelCursor)
			overlay += "\n"
			overlay += renderInlineFallback(showFallback, fallback, fbCursor, modelSections, ollamaRunning)
			overlay += "\n" + helpStyle.Render("j/k: navigate  space: select  tab: per-phase  f: fallback  enter: next  esc: back")
		}
	case 4:
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

	ow := overlayWidth(width, 100)
	if step == 1 && taskOverlayWidth > 0 {
		ow = taskOverlayWidth
	}
	box := overlayStyle.Width(ow).Render(overlay)

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

func renderStreamConfigOverlay(pipeline []string, activeTab int, breakpoints map[int]bool, bpCursor int, notify stream.NotifySettings, modelConfig stream.ModelConfig, modelCursor int, perPhase bool, phaseModelCursor int, modelSections []models.Section, ollamaRunning bool, fallback stream.FallbackConfig, fbCursor int, width, height int) string {
	// Tab header.
	tabs := []string{"Breakpoints", "Models", "Fallback"}
	var tabRow string
	for i, t := range tabs {
		if i == activeTab {
			tabRow += selectedRowStyle.Render(t)
		} else {
			tabRow += helpStyle.Render(t)
		}
		if i < len(tabs)-1 {
			tabRow += "    "
		}
	}
	overlay := overlayTitleStyle.Render("Stream Config") + "\n\n"
	overlay += "  " + tabRow + "\n\n"

	switch activeTab {
	case 0:
		// Breakpoints tab content.
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
		overlay += helpStyle.Render("tab: switch section  j/k: navigate  space: toggle  enter: save  esc: cancel")
	case 1:
		// Models tab content.
		if perPhase {
			for i, phaseName := range pipeline {
				cursor := "  "
				if i == phaseModelCursor {
					cursor = cursorStyle.Render("> ")
				}
				phaseModel := modelConfig.PerPhase[phaseName]
				if phaseModel == "" {
					phaseModel = "sonnet"
				}
				label := fmt.Sprintf("%-12s %s", phaseName, phaseModel)
				if i == phaseModelCursor {
					label = selectedRowStyle.Render(label)
				}
				overlay += cursor + label + "\n"
			}
			overlay += "\n  [x] Configure per phase\n"
			overlay += "\n" + helpStyle.Render("tab: switch section  j/k: navigate  h/l: cycle model  p: all-phases  enter: save  esc: cancel")
		} else {
			selected := modelConfig.Default
			overlay += renderGroupedModelList(modelSections, ollamaRunning, selected, modelCursor)
			overlay += "\n  [ ] Configure per phase\n"
			overlay += "\n" + helpStyle.Render("tab: switch section  j/k: navigate  space: select  p: per-phase  enter: save  esc: cancel")
		}
	case 2:
		// Fallback tab content.
		overlay += renderFallbackConfig(fallback, fbCursor, modelSections, ollamaRunning)
		overlay += "\n" + helpStyle.Render("tab: switch section  space: toggle  j/k: select model  enter: save  esc: cancel")
	}

	box := overlayStyle.Width(overlayWidth(width, 100)).Render(overlay)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

// renderGroupedModelList renders model options grouped by section with headers.
// The cursor indexes into the flat selectable list across all sections.
func renderGroupedModelList(sections []models.Section, ollamaRunning bool, selected string, cursor int) string {
	var b strings.Builder
	idx := 0
	for _, sec := range sections {
		header := sec.Header
		if sec.Header == "Local (Ollama)" {
			if ollamaRunning {
				header += " " + greenStyle.Render("● running")
			} else {
				header += " " + helpStyle.Render("○ not running")
			}
		}
		b.WriteString("  " + helpStyle.Render(header) + "\n")
		for _, opt := range sec.Items {
			cur := "  "
			if idx == cursor {
				cur = cursorStyle.Render("> ")
			}
			radio := "○"
			if opt == selected {
				radio = "●"
			}
			label := opt
			if idx == cursor {
				label = selectedRowStyle.Render(radio + " " + label)
			} else {
				label = radio + " " + label
			}
			b.WriteString(cur + label + "\n")
			idx++
		}
	}
	return b.String()
}

// renderFallbackConfig renders the fallback toggle and model picker.
// renderInlineFallback renders the fallback model picker inline within the model selection step.
func renderInlineFallback(show bool, fb stream.FallbackConfig, cursor int, sections []models.Section, ollamaRunning bool) string {
	if !show {
		label := helpStyle.Render("f: fallback")
		if fb.Enabled && fb.Model != "" {
			label = "Fallback → " + fb.Model
		}
		return "  " + label + "\n"
	}

	var b strings.Builder
	b.WriteString("\n  " + helpStyle.Render("Fallback model") + "\n")

	var ollamaItems []string
	for _, sec := range sections {
		if sec.Header == "Local (Ollama)" {
			ollamaItems = sec.Items
		}
	}

	if len(ollamaItems) == 0 {
		b.WriteString("  " + helpStyle.Render("Automatic failover to a local model when the API rate-limits.") + "\n")
		if ollamaRunning {
			b.WriteString("  " + helpStyle.Render("No Ollama models installed. Run: ollama pull <model>") + "\n")
		} else {
			b.WriteString("  " + helpStyle.Render("Ollama not running. Install from ollama.com, then:") + "\n")
			b.WriteString("  " + helpStyle.Render("  ollama serve && ollama pull <model>") + "\n")
		}
		return b.String()
	}

	if !ollamaRunning {
		b.WriteString("  " + helpStyle.Render("Ollama") + " " + helpStyle.Render("○ not running") + "\n")
	}

	for i, opt := range ollamaItems {
		cur := "  "
		if i == cursor {
			cur = cursorStyle.Render("> ")
		}
		radio := "○"
		if opt == fb.Model {
			radio = "●"
		}
		label := opt
		if i == cursor {
			label = selectedRowStyle.Render(radio + " " + label)
		} else {
			label = radio + " " + label
		}
		b.WriteString(cur + label + "\n")
	}

	return b.String()
}

func renderFallbackConfig(fb stream.FallbackConfig, cursor int, sections []models.Section, ollamaRunning bool) string {
	var b strings.Builder

	check := "[ ]"
	if fb.Enabled {
		check = "[x]"
	}
	b.WriteString("  " + check + " Enable rate limit fallback\n\n")

	if ollamaRunning {
		b.WriteString("  " + helpStyle.Render("Ollama") + " " + greenStyle.Render("● running") + "\n")
	} else {
		b.WriteString("  " + helpStyle.Render("Ollama") + " " + helpStyle.Render("○ not running") + "\n")
		b.WriteString("  " + helpStyle.Render("Start with: ollama serve") + "\n")
	}

	if fb.Enabled {
		// Show Ollama models for selection.
		var ollamaItems []string
		for _, sec := range sections {
			if sec.Header == "Local (Ollama)" {
				ollamaItems = sec.Items
			}
		}
		if len(ollamaItems) > 0 {
			b.WriteString("\n  " + helpStyle.Render("Fallback model:") + "\n")
			for i, opt := range ollamaItems {
				cur := "  "
				if i == cursor {
					cur = cursorStyle.Render("> ")
				}
				radio := "○"
				if opt == fb.Model {
					radio = "●"
				}
				label := opt
				if i == cursor {
					label = selectedRowStyle.Render(radio + " " + label)
				} else {
					label = radio + " " + label
				}
				b.WriteString(cur + label + "\n")
			}
		} else {
			b.WriteString("\n  " + helpStyle.Render("No Ollama models available") + "\n")
		}
	}

	return b.String()
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
	overlay += helpStyle.Render("⌘S: send  esc: cancel")

	box := overlayStyle.Width(overlayWidth(width, 100)).Render(overlay)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

func renderBlockersOverlay(streamIDs []string, checked map[string]bool, cursor int, orch *orchestrator.Orchestrator, width, height int) string {
	overlay := overlayTitleStyle.Render("Set Blockers") + "\n\n"
	overlay += helpStyle.Render("This stream will auto-start when all selected blockers stop running.") + "\n\n"

	for i, id := range streamIDs {
		st := orch.Get(id)
		name := id
		if st != nil {
			name = st.Name
		}

		prefix := "  "
		if i == cursor {
			prefix = lipgloss.NewStyle().Foreground(colorPrimary).Render("▸ ")
		}

		check := "[ ] "
		if checked[id] {
			check = "[x] "
		}

		label := name
		if st != nil {
			status := strings.ToLower(st.GetStatus().String())
			label += helpStyle.Render(" (" + status + ")")
		}

		overlay += prefix + check + label + "\n"
	}

	overlay += "\n" + helpStyle.Render("j/k: navigate  space: toggle  enter: confirm  esc: cancel")

	box := overlayStyle.Width(overlayWidth(width, 80)).Render(overlay)
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

func renderQuitConfirmOverlay(runningCount int, width, height int) string {
	overlay := overlayTitleStyle.Render("Quit Streams") + "\n\n"
	if runningCount > 0 {
		label := "stream is"
		if runningCount > 1 {
			label = "streams are"
		}
		overlay += fmt.Sprintf("%d %s currently running.\n", runningCount, label)
		overlay += helpStyle.Render("Running streams will be interrupted and auto-resumed on restart.") + "\n\n"
	}
	overlay += "Are you sure you want to quit?\n\n"
	overlay += helpStyle.Render("y/enter: quit  esc: cancel")

	box := overlayStyle.Width(overlayWidth(width, 70)).Render(overlay)
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
	overlay += helpStyle.Render("⌘S: complete  esc: cancel")

	box := overlayStyle.Width(overlayWidth(width, 100)).Render(overlay)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

func renderReviseOverlay(phases []string, cursor, step int, isRunning, replaceSelected bool, pendingTarget string, feedback textarea.Model, width, height int) string {
	overlay := overlayTitleStyle.Render("Revise Stream") + "\n\n"

	if step == -1 {
		overlay += fmt.Sprintf("Revise pending → %s\n\n", pendingTarget)
		overlay += helpStyle.Render("r: change target  esc: cancel pending revise")
		box := overlayStyle.Width(overlayWidth(width, 80)).Render(overlay)
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
	}

	if step == 2 {
		if cursor >= 0 && cursor < len(phases) {
			overlay += helpStyle.Render("Target phase: "+phases[cursor]) + "\n\n"
		}
		overlay += "The stream is currently running.\n\n"

		options := []struct {
			label string
			desc  string
		}{
			{"Enqueue", "apply after the current iteration completes"},
			{"Replace", "scrap the current iteration and revise now"},
		}
		for i, opt := range options {
			selected := (i == 1) == replaceSelected
			prefix := "  "
			if selected {
				prefix = cursorStyle.Render("> ")
			}
			label := opt.label + " — " + opt.desc
			if selected {
				label = selectedRowStyle.Render(label)
			}
			overlay += prefix + label + "\n"
		}
		overlay += "\n" + helpStyle.Render("j/k: toggle  enter: confirm  esc: back")
		box := overlayStyle.Width(overlayWidth(width, 80)).Render(overlay)
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
	}

	if step == 1 {
		if cursor >= 0 && cursor < len(phases) {
			overlay += helpStyle.Render("Target phase: "+phases[cursor]) + "\n\n"
		}
		overlay += "Feedback (optional):\n"
		overlay += feedback.View() + "\n\n"
		overlay += helpStyle.Render("⌘S: revise  esc: back")
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

// helpScopes returns the scopes relevant to the current view.
func (m Model) helpScopes() []Scope {
	switch m.view {
	case viewDashboard:
		if m.dashboard.mode == modeChannels {
			return []Scope{ScopeDashboardChannels, ScopeGlobal}
		}
		return []Scope{ScopeDashboard, ScopeGlobal}
	case viewDetail:
		return []Scope{ScopeDetail, ScopeDetailOutput, ScopeBeadBrowse, ScopeGlobal}
	default:
		return []Scope{ScopeGlobal}
	}
}

// helpTitle returns the overlay title based on the current view.
func (m Model) helpTitle() string {
	switch m.view {
	case viewDashboard:
		if m.dashboard.mode == modeChannels {
			return "Channels Shortcuts"
		}
		return "Dashboard Shortcuts"
	case viewDetail:
		return "Inspect Shortcuts"
	default:
		return "Keyboard Shortcuts"
	}
}

func renderHelpOverlay(scopes []Scope, title string, scroll, width, height int) string {
	keyStyle := helpKeyStyle
	descStyle := helpActionStyle
	headerStyle := overlayTitleStyle

	// Determine max description width based on overlay width.
	ow := overlayWidth(width, 80)
	// key column (10) + spacing (4) + border/padding (~6) = 20 chars overhead
	descWidth := ow - 20
	if descWidth < 30 {
		descWidth = 30
	}

	var lines []string
	for i, scope := range scopes {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, headerStyle.Render(scope.Label()))

		for _, b := range BindingsForScope(scope) {
			desc := b.Description
			if desc == "" {
				desc = b.Action
			}
			// Word-wrap the description to fit the overlay width.
			wrapped := wrapLines(desc, descWidth)
			for j, wline := range wrapped {
				if j == 0 {
					line := fmt.Sprintf("  %s  %s",
						keyStyle.Width(10).Render(b.Key),
						descStyle.Render(wline))
					lines = append(lines, line)
				} else {
					// Continuation line: indent past the key column.
					line := fmt.Sprintf("  %s  %s",
						keyStyle.Width(10).Render(""),
						descStyle.Render(wline))
					lines = append(lines, line)
				}
			}
		}
	}

	// Apply scroll offset and clamp to available height.
	contentHeight := height - 8 // padding, border, title, footer
	if contentHeight < 5 {
		contentHeight = 5
	}
	maxScroll := len(lines) - contentHeight
	if maxScroll < 0 {
		maxScroll = 0
	}
	if scroll > maxScroll {
		scroll = maxScroll
	}

	end := scroll + contentHeight
	if end > len(lines) {
		end = len(lines)
	}
	visible := lines[scroll:end]

	overlay := overlayTitleStyle.Render(title) + "\n\n"
	overlay += strings.Join(visible, "\n")
	overlay += "\n\n"
	overlay += helpStyle.Render("esc or ? to close  j/k to scroll")

	box := overlayStyle.Width(ow).Render(overlay)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

// wrapLines breaks text into lines of at most maxWidth characters, splitting
// at word boundaries.
func wrapLines(text string, maxWidth int) []string {
	if maxWidth <= 0 {
		return []string{text}
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{""}
	}
	var lines []string
	line := words[0]
	for _, w := range words[1:] {
		if len(line)+1+len(w) > maxWidth {
			lines = append(lines, line)
			line = w
		} else {
			line += " " + w
		}
	}
	lines = append(lines, line)
	return lines
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
// the last pipeline phase with no error — the state where c/r actions are available.
func isPausedAtReview(st *stream.Stream) bool {
	if st.GetStatus() != stream.StatusPaused || !st.Converged || st.GetLastError() != nil {
		return false
	}
	pipeline := st.GetPipeline()
	return st.GetPipelineIndex() >= len(pipeline)-1
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

// autoSizeNewStreamInput adjusts the task textarea width and height to fit its
// content. It first tries the default overlay width (capped at 100), then
// expands horizontally up to the terminal width before allowing vertical
// scrolling.
func (m *Model) autoSizeNewStreamInput() {
	// Overlay chrome for step 1: title+step (1) + blank (1) + "Title: ..." (1) +
	// blank (1) + "Task:" (1) + blank after textarea (1) + help line (1) +
	// border (2) + padding (2) = 11 lines, plus 2 for top/bottom margin.
	const chrome = 13
	maxH := m.height - chrome
	if maxH < 3 {
		maxH = 3
	}

	// Start with the default overlay width (capped at 100).
	ow := overlayWidth(m.width, 100)
	contentW := ow - 6 // subtract border(2) + padding(4)
	if contentW < 20 {
		contentW = 20
	}
	m.newStreamInput.SetWidth(contentW)
	// Add 1 to account for the textarea's cursor/viewport row overhead.
	h := wrappedLineCount(m.newStreamInput.Value(), m.newStreamInput.Width()) + 1

	// If content fills or exceeds available height, expand the overlay
	// horizontally. Using >= provides a buffer for word-wrap overhead that
	// the estimate may miss.
	if h >= maxH {
		maxOW := overlayWidth(m.width, m.width)
		contentW = maxOW - 6
		if contentW < 20 {
			contentW = 20
		}
		m.newStreamInput.SetWidth(contentW)
		h = wrappedLineCount(m.newStreamInput.Value(), m.newStreamInput.Width()) + 1
		ow = maxOW
	}

	if h < 3 {
		h = 3
	}
	if h > maxH {
		h = maxH
	}
	m.newStreamInput.SetHeight(h)
	m.newStreamOverlayWidth = ow
}

// wrappedLineCount estimates the number of visual lines a text occupies when
// word-wrapped at the given width. Mirrors the textarea's wrap behavior where
// each word's trailing space is included in the line width calculation.
func wrappedLineCount(text string, wrapWidth int) int {
	if wrapWidth <= 0 {
		return 1
	}
	total := 0
	for _, line := range strings.Split(text, "\n") {
		words := strings.Fields(line)
		if len(words) == 0 {
			total++
			continue
		}
		// lineW tracks width including a trailing space after each word,
		// matching how the textarea accumulates "word + space" per word.
		lineW := 0
		for _, word := range words {
			w := lipgloss.Width(word)
			if lineW == 0 {
				lineW = w + 1
			} else if lineW+w+1 > wrapWidth {
				total++
				lineW = w + 1
			} else {
				lineW += w + 1
			}
		}
		total++ // count the last line
	}
	return total
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
