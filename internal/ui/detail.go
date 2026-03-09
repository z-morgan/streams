package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/zmorgan/streams/internal/stream"
)

type detailView struct {
	iterCursor   int
	tailScroll   int
	focusRight   bool
	showArtifact bool // toggle between snapshot summary and artifact file
	contentWidth int  // layout width captured on entry; 0 = not yet set
}

func (d *detailView) clampCursor(count int) {
	if count == 0 {
		d.iterCursor = 0
		return
	}
	if d.iterCursor >= count {
		d.iterCursor = count - 1
	}
	if d.iterCursor < 0 {
		d.iterCursor = 0
	}
}

type iterationRow struct {
	Phase           string
	Iteration       int
	IsInProgress    bool
	IsPaused        bool
	IsBreakpoint    bool
	HasError        bool
	IsInitialPrompt bool
	Step            stream.IterStep // current step (only meaningful for in-progress rows)
	SnapshotIndex   int             // -1 for non-snapshot rows
}

func buildIterationList(st *stream.Stream) []iterationRow {
	snaps := st.GetSnapshots()
	var rows []iterationRow

	// Initial prompt row always comes first.
	rows = append(rows, iterationRow{
		IsInitialPrompt: true,
		SnapshotIndex:   -1,
	})

	// Number snapshots sequentially per phase for display purposes.
	phaseCount := make(map[string]int)
	for i, snap := range snaps {
		phaseCount[snap.Phase]++
		rows = append(rows, iterationRow{
			Phase:         snap.Phase,
			Iteration:     phaseCount[snap.Phase] - 1, // 0-based; rendered as +1
			HasError:      snap.Error != nil,
			SnapshotIndex: i,
		})
	}

	status := st.GetStatus()
	phase := currentPhase(st)

	if status == stream.StatusRunning {
		rows = append(rows, iterationRow{
			Phase:         phase,
			Iteration:     phaseCount[phase], // next sequential number (0-based)
			IsInProgress:  true,
			Step:          st.GetIterStep(),
			SnapshotIndex: -1,
		})
	} else if status == stream.StatusPaused {
		// Append a paused row if the last snapshot for this phase already
		// covers the current state (e.g. an error snapshot).
		lastPhaseSnap := -1
		for i := len(snaps) - 1; i >= 0; i-- {
			if snaps[i].Phase == phase {
				lastPhaseSnap = i
				break
			}
		}
		hasSnapshot := lastPhaseSnap >= 0 && snaps[lastPhaseSnap].Iteration == st.GetIteration()
		if !hasSnapshot {
			rows = append(rows, iterationRow{
				Phase:         phase,
				Iteration:     phaseCount[phase],
				IsInProgress:  true,
				IsPaused:      true,
				IsBreakpoint:  isPausedAtBreakpoint(st),
				SnapshotIndex: -1,
			})
		}
	}

	return rows
}

func renderDetail(st *stream.Stream, dv detailView, width, height int, spinnerFrame string) string {
	if st == nil {
		return "No stream selected."
	}

	var b strings.Builder

	// Available height for the two-pane area:
	// top bar = 1 line, bottom bar = 2 lines, gap = 1 line
	paneHeight := height - 4
	if paneHeight < 5 {
		paneHeight = 5
	}

	rows := buildIterationList(st)
	snaps := st.GetSnapshots()

	if len(rows) == 0 {
		b.WriteString(helpStyle.Render("Waiting for output..."))
		b.WriteString("\n")
	} else {
		// Two-pane: left = iteration list, right = selected iteration details
		leftWidth := 25
		rightWidth := width - leftWidth - 3 // 3 for separator
		if rightWidth < 10 {
			rightWidth = 10
		}

		left := renderIterationList(rows, dv.iterCursor, leftWidth, dv.focusRight, spinnerFrame)

		var right string
		cursor := dv.iterCursor
		if cursor >= 0 && cursor < len(rows) {
			row := rows[cursor]
			if row.IsInitialPrompt {
				right = labelStyle.Render("Initial Prompt") + "\n" + wrapText(st.Task, rightWidth)
			} else if row.IsInProgress {
				right = renderTailContent(st, rightWidth, paneHeight, dv.tailScroll)
				if row.IsBreakpoint {
					right += "\n" + helpStyle.Render("(breakpoint — press s to continue, g to add guidance)")
				} else if row.IsPaused {
					if err := st.GetLastError(); err != nil {
						right += "\n" + renderErrorBlock(err)
					} else {
						right += "\n" + helpStyle.Render("(paused)")
					}
				}
			} else if dv.showArtifact && row.SnapshotIndex >= 0 && row.SnapshotIndex < len(snaps) && snaps[row.SnapshotIndex].Artifact != "" {
				right = renderArtifactDetail(snaps, row.SnapshotIndex, rightWidth)
			} else {
				right = renderSnapshotDetail(snaps, row.SnapshotIndex, rightWidth)
			}
		}

		right = detailStatusMarker(st) + "\n" + right

		b.WriteString(joinPanes(left, right, leftWidth, paneHeight))
	}

	return b.String()
}

func isPausedAtBreakpoint(st *stream.Stream) bool {
	if st.GetStatus() != stream.StatusPaused || !st.Converged || st.GetLastError() != nil {
		return false
	}
	idx := st.GetPipelineIndex()
	for _, bp := range st.GetBreakpoints() {
		if bp == idx {
			return true
		}
	}
	return false
}

func detailHelpText(st *stream.Stream, dv detailView, rows []iterationRow, snaps []stream.Snapshot) string {
	if dv.focusRight {
		return "j/k: scroll  G: bottom  esc: back to list"
	}

	status := st.GetStatus()

	if status == stream.StatusCompleted {
		return "D: diagnose  d: delete  q/esc: back"
	}

	if isPausedAtReview(st) {
		return "j/k: iterations  c: complete  r: revise  D: diagnose  g: guidance  d: delete  q/esc: back"
	}

	var help string
	if status == stream.StatusRunning {
		help = "j/k: iterations  enter: focus output  a: attach  w: wrap up  x: stop  g: guidance  q/esc: back"
	} else {
		help = "j/k: iterations  enter: focus output  a: attach  s: start  D: diagnose  x: stop  g: guidance  q/esc: back"
	}

	// Show artifact toggle hint when the selected snapshot has an artifact.
	cursor := dv.iterCursor
	if cursor >= 0 && cursor < len(rows) {
		row := rows[cursor]
		if row.SnapshotIndex >= 0 && row.SnapshotIndex < len(snaps) && snaps[row.SnapshotIndex].Artifact != "" {
			if dv.showArtifact {
				help += "  f: show summary"
			} else {
				help += "  f: show " + snaps[row.SnapshotIndex].Phase + ".md"
			}
		}
	}

	return help
}

func detailStatusMarker(st *stream.Stream) string {
	status := st.GetStatus()
	name := status.String()

	if status == stream.StatusCompleted {
		marker := lipgloss.NewStyle().Foreground(colorSuccess).Bold(true).Render("[Completed]")
		marker += "  " + helpStyle.Render("branch: "+st.Branch)
		return marker
	}

	if status == stream.StatusPaused && st.GetLastError() != nil {
		marker := lipgloss.NewStyle().Foreground(colorError).Bold(true).Render("[! Error]")
		if st.WorkTree != "" {
			marker += "  " + helpStyle.Render("worktree: "+st.Branch)
		}
		return marker
	}

	label := name
	if status == stream.StatusRunning {
		step := st.GetIterStep()
		if step == stream.StepImplement || step == stream.StepReview {
			label += " · " + step.String()
		}
	} else if isPausedAtBreakpoint(st) {
		label += " · breakpoint"
	}

	color, ok := statusColors[name]
	if !ok {
		color = colorMuted
	}
	marker := lipgloss.NewStyle().Foreground(color).Render("[" + label + "]")

	if st.WorkTree != "" {
		marker += "  " + helpStyle.Render("worktree: "+st.Branch)
	}

	return marker
}

func renderIterationList(rows []iterationRow, cursor int, width int, dimmed bool, spinnerFrame string) string {
	var b strings.Builder

	for i, row := range rows {
		if row.IsInitialPrompt {
			label := "Initial Prompt"
			if dimmed {
				b.WriteString(snapshotNormalStyle.Render("  " + label))
			} else if i == cursor {
				b.WriteString(snapshotSelectedStyle.Render("> " + label))
			} else {
				b.WriteString(snapshotNormalStyle.Render("  " + label))
			}
			b.WriteString("\n")
			b.WriteString(labelStyle.Render("Iterations"))
			b.WriteString("\n")
			continue
		}

		label := fmt.Sprintf("%s %d", row.Phase, row.Iteration+1)
		if row.IsInProgress {
			if row.IsBreakpoint {
				label += " (breakpoint)"
			} else if row.IsPaused {
				label += " (paused)"
			} else {
				if row.Step == stream.StepImplement || row.Step == stream.StepReview {
					label += " · " + row.Step.String()
				}
				label = spinnerFrame + " " + label
			}
		}
		if row.HasError {
			label += " !"
		}

		if dimmed {
			b.WriteString(snapshotNormalStyle.Render("  " + label))
		} else if i == cursor {
			b.WriteString(snapshotSelectedStyle.Render("> " + label))
		} else {
			b.WriteString(snapshotNormalStyle.Render("  " + label))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func renderSnapshotDetail(snaps []stream.Snapshot, cursor int, width int) string {
	if cursor < 0 || cursor >= len(snaps) {
		return ""
	}
	snap := snaps[cursor]

	var b strings.Builder

	b.WriteString(labelStyle.Render("Iteration Snapshot"))
	b.WriteString("\n\n")

	b.WriteString(labelStyle.Render("Implementation Agent's Report"))
	b.WriteString("\n")
	b.WriteString(wrapText(snap.Summary, width))
	b.WriteString("\n\n")

	reviewText := snap.Review
	if reviewText == "" {
		reviewText = reviewFallback(snap)
	}
	if reviewText != "" {
		b.WriteString(labelStyle.Render("Review Agent's Report"))
		b.WriteString("\n")
		b.WriteString(wrapText(reviewText, width))
		b.WriteString("\n\n")
	}

	if len(snap.BeadsClosed) > 0 {
		b.WriteString(labelStyle.Render(fmt.Sprintf("Closed (%d)", len(snap.BeadsClosed))))
		b.WriteString("\n")
		b.WriteString("  " + strings.Join(snap.BeadsClosed, "  "))
		b.WriteString("\n\n")
	}

	if len(snap.BeadsOpened) > 0 {
		b.WriteString(labelStyle.Render(fmt.Sprintf("Opened (%d)", len(snap.BeadsOpened))))
		b.WriteString("\n")
		b.WriteString("  " + strings.Join(snap.BeadsOpened, "  "))
		b.WriteString("\n\n")
	}

	if snap.DiffStat != "" {
		b.WriteString(labelStyle.Render("Diff"))
		b.WriteString("\n")
		b.WriteString(snap.DiffStat)
		b.WriteString("\n\n")
	}

	if snap.CostUSD > 0 {
		b.WriteString(fmt.Sprintf("Cost: $%.2f\n", snap.CostUSD))
	}

	if len(snap.GuidanceConsumed) > 0 {
		b.WriteString(labelStyle.Render("Guidance Applied"))
		b.WriteString("\n")
		for _, g := range snap.GuidanceConsumed {
			b.WriteString(fmt.Sprintf("  - %s\n", g.Text))
		}
	}

	if snap.Error != nil {
		b.WriteString("\n")
		b.WriteString(renderErrorBlock(snap.Error))
	}

	return b.String()
}

func renderArtifactDetail(snaps []stream.Snapshot, cursor int, width int) string {
	if cursor < 0 || cursor >= len(snaps) {
		return ""
	}
	snap := snaps[cursor]

	var b strings.Builder
	b.WriteString(labelStyle.Render(snap.Phase + ".md"))
	b.WriteString("\n")
	b.WriteString(wrapText(snap.Artifact, width))
	return b.String()
}

func renderErrorBlock(err *stream.LoopError) string {
	msg := fmt.Sprintf("Error [%s at %s]: %s", err.Kind, err.Step, err.Message)
	if err.Detail != "" {
		msg += "\n" + err.Detail
	}
	return errorBlockStyle.Render(msg)
}

func joinPanes(left, right string, leftWidth, maxLines int) string {
	leftLines := strings.Split(left, "\n")
	rightLines := strings.Split(right, "\n")

	lineCount := len(leftLines)
	if len(rightLines) > lineCount {
		lineCount = len(rightLines)
	}
	if maxLines > 0 && lineCount > maxLines {
		lineCount = maxLines
	}

	var b strings.Builder
	for i := 0; i < lineCount; i++ {
		l := ""
		if i < len(leftLines) {
			l = leftLines[i]
		}
		r := ""
		if i < len(rightLines) {
			r = rightLines[i]
		}
		// Pad left using visible width (ANSI-aware)
		visWidth := lipgloss.Width(l)
		pad := leftWidth - visWidth
		if pad < 0 {
			pad = 0
		}
		b.WriteString(l + strings.Repeat(" ", pad) + " │ " + r + "\n")
	}
	return b.String()
}

// clipLines truncates each line to maxWidth visible characters so that
// content rendered at a wider layout width doesn't wrap at the terminal edge.
func clipLines(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if lipgloss.Width(line) > maxWidth {
			lines[i] = truncateAnsi(line, maxWidth)
		}
	}
	return strings.Join(lines, "\n")
}

// truncateAnsi truncates a string that may contain ANSI escape sequences
// to the given visible width.
func truncateAnsi(s string, maxWidth int) string {
	return ansi.Truncate(s, maxWidth, "")
}

// reviewFallback generates a summary line when the review agent's text output
// was empty but it did produce observable side effects (filed issues).
func reviewFallback(snap stream.Snapshot) string {
	n := len(snap.BeadsOpened)
	if n == 0 {
		return ""
	}
	if n == 1 {
		return fmt.Sprintf("Filed 1 issue: %s", snap.BeadsOpened[0])
	}
	return fmt.Sprintf("Filed %d issues: %s", n, strings.Join(snap.BeadsOpened, ", "))
}

func wrapText(s string, width int) string {
	if width <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	var result []string
	for _, line := range lines {
		// Don't wrap table rows; clipLines will truncate at the terminal edge
		if len(line) <= width || strings.HasPrefix(strings.TrimSpace(line), "|") {
			result = append(result, line)
			continue
		}
		// Wrap long prose lines at word boundaries
		for len(line) > width {
			breakAt := strings.LastIndex(line[:width], " ")
			if breakAt <= 0 {
				breakAt = width
			}
			result = append(result, line[:breakAt])
			line = strings.TrimLeft(line[breakAt:], " ")
		}
		if line != "" {
			result = append(result, line)
		}
	}
	return strings.Join(result, "\n")
}
