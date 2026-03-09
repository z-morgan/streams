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

func renderDashboardList(streams []*stream.Stream, cursor int, spinnerFrame string) string {
	var b strings.Builder

	if len(streams) == 0 {
		b.WriteString(helpStyle.Render("No streams yet. Press n to create one."))
		b.WriteString("\n")
	} else {
		for i, st := range streams {
			prefix := "  "
			style := normalRowStyle
			if i == cursor {
				prefix = cursorStyle.Render("> ")
				style = selectedRowStyle
			}

			status := statusIndicator(st)
			phase := currentPhase(st)
			iter := fmt.Sprintf("iter %d", st.GetIteration())
			guidanceCount := st.GetGuidanceCount()

			spinner := "  "
			if st.GetStatus() == stream.StatusRunning {
				spinner = spinnerFrame + " "
			}

			worktree := ""
			if st.WorkTree != "" {
				worktree = helpStyle.Render("worktree: "+st.Branch) + "  "
			}

			row := fmt.Sprintf("%s%-20s %s%s  %-10s  %s%s",
				prefix,
				truncate(st.Name, 20),
				worktree,
				status,
				phase,
				spinner,
				iter,
			)

			if guidanceCount > 0 {
				row += fmt.Sprintf("  [%d queued]", guidanceCount)
			}

			b.WriteString(style.Render(row))
			b.WriteString("\n")
		}
	}

	return b.String()
}

const dashboardListHelp = "j/k: navigate  enter: inspect  n: new  s: start  x: stop  d: delete  D: diagnose  g: guidance  v: channels  q: quit"
const dashboardChannelHelp = "h/l: navigate  enter: inspect  n: new  s: start  x: stop  d: delete  D: diagnose  g: guidance  v: list  q: quit"

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
	innerWidth := colWidth - 4 // border + padding takes ~4 chars
	if innerWidth < 1 {
		innerWidth = 1
	}

	// Header: name + status/phase
	name := truncate(st.Name, innerWidth)
	status := statusIndicator(st)
	phase := currentPhase(st)

	worktree := ""
	if st.WorkTree != "" {
		worktree = helpStyle.Render(truncate("worktree: "+st.Branch, innerWidth)) + "\n"
	}

	header := channelHeaderStyle.Render(name) + "\n" +
		worktree +
		status + "  " + channelHeaderMutedStyle.Render(phase)

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
		b.WriteString(helpStyle.Render("No streams yet. Press n to create one."))
		b.WriteString("\n")
		return b.String()
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

	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, columns...))

	// Scroll indicators
	var indicators []string
	if scrollLeft > 0 {
		indicators = append(indicators, "<")
	}
	if endIdx < len(streams) {
		indicators = append(indicators, ">")
	}
	if len(indicators) > 0 {
		b.WriteString("\n")
		b.WriteString(helpStyle.Render(strings.Join(indicators, "  ")))
	}

	b.WriteString("\n")
	return b.String()
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
