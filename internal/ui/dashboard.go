package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/zmorgan/streams/internal/stream"
)

type dashboardMode int

const (
	modeChannels dashboardMode = iota
	modeList
)

type dashboardView struct {
	cursor     int
	mode       dashboardMode
	scrollLeft int // horizontal scroll offset for channel view
}

func (d *dashboardView) clampCursor(count int) {
	if count == 0 {
		d.cursor = 0
		return
	}
	if d.cursor >= count {
		d.cursor = count - 1
	}
	if d.cursor < 0 {
		d.cursor = 0
	}
}

func (d *dashboardView) clampScroll(streamCount, visibleCols int) {
	maxScroll := streamCount - visibleCols
	if maxScroll < 0 {
		maxScroll = 0
	}
	if d.scrollLeft > maxScroll {
		d.scrollLeft = maxScroll
	}
	if d.scrollLeft < 0 {
		d.scrollLeft = 0
	}
}

func renderDashboardList(streams []*stream.Stream, cursor int, width, height int, spinnerFrame string) string {
	if len(streams) == 0 {
		return renderEmptyState(width, height-4)
	}

	var b strings.Builder

	// Column widths
	const statusCol = 14
	const phaseCol = 12
	const iterCol = 10

	// Dynamic name width: find longest name, cap at 40
	nameCol := 10
	for _, st := range streams {
		if n := len(st.Name); n > nameCol {
			nameCol = n
		}
	}
	if nameCol > 40 {
		nameCol = 40
	}

	// Header
	header := fmt.Sprintf("  %-*s  %-*s  %-*s  %s",
		nameCol, "Name",
		statusCol, "Status",
		phaseCol, "Phase",
		"Iter",
	)
	b.WriteString(helpStyle.Render(header))
	b.WriteString("\n")
	b.WriteString(helpStyle.Render(strings.Repeat("─", nameCol+statusCol+phaseCol+iterCol+8)))
	b.WriteString("\n")

	accentStyle := lipgloss.NewStyle().Foreground(colorPrimary)

	for i, st := range streams {
		selected := i == cursor
		accent := "  "
		style := normalRowStyle
		if selected {
			accent = accentStyle.Render("▎ ")
			style = selectedRowStyle
		}

		status := statusIndicator(st)
		phase := currentPhase(st)
		iter := fmt.Sprintf("iter %d", st.GetIteration())

		spinner := "  "
		if st.GetStatus() == stream.StatusRunning {
			spinner = spinnerFrame + " "
		}

		name := truncate(st.Name, nameCol)
		// Pad status to visual width (ANSI codes inflate byte count)
		statusPad := statusCol - lipgloss.Width(status)
		if statusPad < 0 {
			statusPad = 0
		}
		paddedStatus := status + strings.Repeat(" ", statusPad)

		row := fmt.Sprintf("%s%-*s  %s  %-*s  %s%s",
			accent,
			nameCol, name,
			paddedStatus,
			phaseCol, phase,
			spinner, iter,
		)

		if gc := st.GetGuidanceCount(); gc > 0 {
			row += fmt.Sprintf("  [%d queued]", gc)
		}

		b.WriteString(style.Render(row))
		b.WriteString("\n")

		if selected && st.WorkTree != "" {
			worktree := "    " + metadataStyle.Render("worktree: "+st.Branch)
			b.WriteString(worktree)
			b.WriteString("\n")
		}
	}

	return b.String()
}

const dashboardListHelp = "j/k: navigate  enter: inspect  n: new  s: start  x: stop  d: delete  D: diagnose  g: guidance  v: channels  q: quit"
const dashboardChannelHelp = "h/l: navigate  enter: inspect  n: new  s: start  x: stop  d: delete  D: diagnose  g: guidance  v: list  q: quit"

// renderHelp formats a help string with styled keys and actions.
// Input format: "key: action  key: action  key: action"
// Groups are separated by double spaces; key/action split on first ":".
func renderHelp(help string) string {
	groups := strings.Split(help, "  ")
	var parts []string
	sep := helpSepStyle.Render(" │ ")
	for _, g := range groups {
		g = strings.TrimSpace(g)
		if g == "" {
			continue
		}
		if idx := strings.Index(g, ": "); idx >= 0 {
			key := g[:idx]
			action := g[idx+2:]
			parts = append(parts, helpKeyStyle.Render(key)+helpActionStyle.Render(" "+action))
		} else {
			parts = append(parts, helpActionStyle.Render(g))
		}
	}
	return strings.Join(parts, sep)
}

// channelLayout computes column width and visible column count for the
// terminal width. Columns are between 25 and 40 characters wide.
func channelLayout(streamCount, termWidth int) (colWidth, visibleCols int) {
	const minCol = 25
	const maxCol = 40

	if streamCount == 0 {
		return 0, 0
	}

	if termWidth < minCol {
		if termWidth < 1 {
			return 1, 1
		}
		return termWidth, 1
	}

	visibleCols = termWidth / minCol
	if visibleCols > streamCount {
		visibleCols = streamCount
	}
	if visibleCols < 1 {
		visibleCols = 1
	}

	colWidth = termWidth / visibleCols
	if colWidth > maxCol {
		colWidth = maxCol
	}

	return colWidth, visibleCols
}

// renderChannel renders a single stream as a vertical column.
func renderChannel(st *stream.Stream, colWidth, availHeight int, selected bool, spinnerFrame string) string {
	innerWidth := colWidth - 6 // border (2) + padding (2) + margin (2)
	if innerWidth < 1 {
		innerWidth = 1
	}

	// Header: status dot + name, subtitle: status text + phase
	name := truncate(st.Name, innerWidth-2) // leave room for dot prefix
	dot := statusDot(st)
	phase := currentPhase(st)
	label := statusLabel(st)

	worktree := ""
	if st.WorkTree != "" {
		worktree = helpStyle.Render(truncate("worktree: "+st.Branch, innerWidth)) + "\n"
	}

	subtitle := label + channelHeaderMutedStyle.Render(" · "+phase)

	header := dot + " " + channelHeaderStyle.Render(name) + "\n" +
		worktree +
		subtitle

	// Iteration rows from snapshots — number sequentially per phase
	snapshots := st.GetSnapshots()
	var rows []string
	phaseCount := make(map[string]int)
	for _, snap := range snapshots {
		phaseCount[snap.Phase]++
		label := fmt.Sprintf("%s %d", snap.Phase, phaseCount[snap.Phase])

		style := iterRowStyle
		if snap.Error != nil {
			label += " !"
			style = iterRowErrorStyle
		}

		rows = append(rows, style.Render(truncate(label, innerWidth)))
	}

	// In-progress indicator for running streams
	if st.GetStatus() == stream.StatusRunning {
		step := st.GetIterStep()
		phase := currentPhase(st)
		displayNum := phaseCount[phase] + 1
		indicator := fmt.Sprintf("%s %s %d...", spinnerFrame, phase, displayNum)
		if step != stream.StepImplement {
			indicator = fmt.Sprintf("%s %s %d (%s)...", spinnerFrame, phase, displayNum, step)
		}
		rows = append(rows, inProgressStyle.Render(truncate(indicator, innerWidth)))
	}

	// Guidance count
	if gc := st.GetGuidanceCount(); gc > 0 {
		rows = append(rows, helpStyle.Render(fmt.Sprintf("[%d queued]", gc)))
	}

	// Vertical auto-scroll: show only last N rows that fit
	// availHeight accounts for header (2 lines) + separator (1 line) + gap (1 line)
	maxRows := availHeight - 4
	if maxRows < 1 {
		maxRows = 1
	}
	if len(rows) > maxRows {
		rows = rows[len(rows)-maxRows:]
	}

	sep := channelSepStyle.Render(strings.Repeat("─", innerWidth))
	content := header + "\n" + sep + "\n" + strings.Join(rows, "\n")

	borderStyle := channelBorderStyle
	if selected {
		borderStyle = channelSelectedBorderStyle
	}

	// Width includes padding (1+1), so add 2 to keep text area = innerWidth
	return borderStyle.Width(innerWidth + 2).Height(availHeight).Render(content)
}

func renderChannels(streams []*stream.Stream, cursor, scrollLeft, width, height int, spinnerFrame string) string {
	var b strings.Builder

	if len(streams) == 0 {
		return renderEmptyState(width, height-4)
	}

	colWidth, visibleCols := channelLayout(len(streams), width)

	// Height available for columns: total minus title (2 lines), footer gap, help line
	availHeight := height - 5
	if availHeight < 5 {
		availHeight = 5
	}

	// Render visible columns
	var columns []string
	endIdx := scrollLeft + visibleCols
	if endIdx > len(streams) {
		endIdx = len(streams)
	}

	for i := scrollLeft; i < endIdx; i++ {
		col := renderChannel(streams[i], colWidth, availHeight, i == cursor, spinnerFrame)
		columns = append(columns, col)
	}

	joined := lipgloss.JoinHorizontal(lipgloss.Top, columns...)

	// Edge-mounted scroll arrows
	leftArrow := " "
	rightArrow := " "
	if scrollLeft > 0 {
		leftArrow = helpStyle.Render("◀")
	}
	if endIdx < len(streams) {
		rightArrow = helpStyle.Render("▶")
	}
	if scrollLeft > 0 || endIdx < len(streams) {
		// Mount arrows vertically centered on the channel area
		joinedLines := strings.Split(joined, "\n")
		midLine := len(joinedLines) / 2
		var result strings.Builder
		for i, line := range joinedLines {
			if i == midLine {
				result.WriteString(leftArrow + line + rightArrow)
			} else {
				result.WriteString(" " + line + " ")
			}
			if i < len(joinedLines)-1 {
				result.WriteString("\n")
			}
		}
		joined = result.String()
	}

	b.WriteString(joined)
	b.WriteString("\n")
	return b.String()
}

func statusDot(st *stream.Stream) string {
	status := st.GetStatus()
	color, ok := statusColors[status.String()]
	if !ok {
		color = colorMuted
	}
	if status == stream.StatusPaused && st.GetLastError() != nil {
		color = colorError
	}
	return lipgloss.NewStyle().Foreground(color).Render("●")
}

// statusLabel returns a short colored status text for the channel subtitle.
func statusLabel(st *stream.Stream) string {
	status := st.GetStatus()
	name := status.String()
	color, ok := statusColors[name]
	if !ok {
		color = colorMuted
	}
	style := lipgloss.NewStyle().Foreground(color)

	switch {
	case status == stream.StatusCompleted:
		return lipgloss.NewStyle().Foreground(colorSuccess).Bold(true).Render("completed")
	case status == stream.StatusPaused && st.GetLastError() != nil:
		return lipgloss.NewStyle().Foreground(colorError).Bold(true).Render("error")
	case isPausedAtBreakpoint(st):
		return lipgloss.NewStyle().Foreground(colorWarning).Render("breakpoint")
	case isPausedAtReview(st):
		return lipgloss.NewStyle().Foreground(colorWarning).Render("review")
	default:
		return style.Render(strings.ToLower(name))
	}
}

func statusIndicator(st *stream.Stream) string {
	status := st.GetStatus()
	name := status.String()
	color, ok := statusColors[name]
	if !ok {
		color = colorMuted
	}
	style := lipgloss.NewStyle().Foreground(color)

	if status == stream.StatusCompleted {
		return lipgloss.NewStyle().Foreground(colorSuccess).Bold(true).Render("✓ Completed")
	}

	if status == stream.StatusPaused && st.GetLastError() != nil {
		return lipgloss.NewStyle().Foreground(colorError).Bold(true).Render("! Error")
	}

	if isPausedAtBreakpoint(st) {
		return lipgloss.NewStyle().Foreground(colorWarning).Render("⏸ Breakpoint")
	}

	if isPausedAtReview(st) {
		return lipgloss.NewStyle().Foreground(colorWarning).Render("⏸ Review")
	}

	return style.Render(name)
}

func currentPhase(st *stream.Stream) string {
	idx := st.GetPipelineIndex()
	pipeline := st.GetPipeline()
	if idx < len(pipeline) {
		return pipeline[idx]
	}
	return "done"
}

func truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if len(s) <= max {
		return s
	}
	if max == 1 {
		return "…"
	}
	return s[:max-1] + "…"
}

// layoutWithBars renders the full screen layout: top bar, body, bottom bar.
// The top bar shows the title and context info. The bottom bar has two rows:
// a status line and the help key legend.
func layoutWithBars(topBar, body, statusLine, helpLine string, width, height int) string {
	top := topBarStyle.Width(width).Render(topBar)

	var bottomParts []string
	if statusLine != "" {
		bottomParts = append(bottomParts, bottomBarStyle.Width(width).Render(statusLine))
	}
	bottomParts = append(bottomParts, bottomBarStyle.Width(width).Render(helpLine))
	bottom := strings.Join(bottomParts, "\n")

	if height <= 0 {
		return top + "\n" + body + "\n" + bottom
	}

	topLines := strings.Count(top, "\n") + 1
	bodyLines := strings.Count(body, "\n") + 1
	bottomLines := strings.Count(bottom, "\n") + 1
	gap := height - topLines - bodyLines - bottomLines
	if gap < 1 {
		gap = 1
	}

	return top + "\n" + body + strings.Repeat("\n", gap) + bottom
}

// dashboardTopBar returns the top bar content for the dashboard view.
func dashboardTopBar(streams []*stream.Stream) string {
	if len(streams) == 0 {
		return "Streams"
	}

	counts := make(map[string]int)
	for _, st := range streams {
		counts[st.GetStatus().String()]++
	}

	var parts []string
	if n := counts["Running"]; n > 0 {
		parts = append(parts, fmt.Sprintf("%d running", n))
	}
	if n := counts["Paused"]; n > 0 {
		parts = append(parts, fmt.Sprintf("%d paused", n))
	}
	if n := counts["Completed"]; n > 0 {
		parts = append(parts, fmt.Sprintf("%d completed", n))
	}
	if n := counts["Stopped"]; n > 0 {
		parts = append(parts, fmt.Sprintf("%d stopped", n))
	}

	if len(parts) == 0 {
		return fmt.Sprintf("Streams (%d)", len(streams))
	}
	return "Streams  " + helpStyle.Render(strings.Join(parts, " · "))
}

// detailTopBar returns the top bar content for the detail view.
func detailTopBar(st *stream.Stream, width int) string {
	if st == nil {
		return "Streams › ?"
	}
	phase := currentPhase(st)
	iter := fmt.Sprintf("iter %d", st.GetIteration())
	suffix := phase + " · " + iter
	// "Streams › " = 10 visual, suffix + spacing = len+4, padding = 2
	nameMax := width - 10 - len(suffix) - 4 - 2
	if nameMax < 10 {
		nameMax = 10
	}
	return "Streams › " + truncate(st.Name, nameMax) + "  " + helpStyle.Render(suffix)
}

func renderEmptyState(width, height int) string {
	msg := helpStyle.Render("No streams yet. Press ") +
		helpKeyStyle.Render("n") +
		helpStyle.Render(" to create one.")
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, msg)
}
