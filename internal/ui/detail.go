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
	Phase         string
	Iteration     int
	IsInProgress  bool
	IsPaused      bool
	HasError      bool
	Step          stream.IterStep // current step (only meaningful for in-progress rows)
	SnapshotIndex int             // -1 for in-progress rows
}

func buildIterationList(st *stream.Stream) []iterationRow {
	snaps := st.GetSnapshots()
	var rows []iterationRow

	for i, snap := range snaps {
		rows = append(rows, iterationRow{
			Phase:         snap.Phase,
			Iteration:     snap.Iteration,
			HasError:      snap.Error != nil,
			SnapshotIndex: i,
		})
	}

	status := st.GetStatus()
	currentIter := st.GetIteration()

	if status == stream.StatusRunning {
		rows = append(rows, iterationRow{
			Phase:         currentPhase(st),
			Iteration:     currentIter,
			IsInProgress:  true,
			Step:          st.GetIterStep(),
			SnapshotIndex: -1,
		})
	} else if status == stream.StatusPaused {
		// Append a paused row if no snapshot matches the current iteration
		hasSnapshot := false
		for _, snap := range snaps {
			if snap.Iteration == currentIter && snap.Phase == currentPhase(st) {
				hasSnapshot = true
				break
			}
		}
		if !hasSnapshot {
			rows = append(rows, iterationRow{
				Phase:         currentPhase(st),
				Iteration:     currentIter,
				IsInProgress:  true,
				IsPaused:      true,
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

	// Header
	header := fmt.Sprintf("%s  [%s]  %s  iter %d",
		st.Name, st.GetStatus(), currentPhase(st), st.GetIteration())
	if st.GetStatus() == stream.StatusRunning {
		step := st.GetIterStep()
		if step == stream.StepImplement || step == stream.StepReview {
			header += " · " + step.String()
		}
	}
	b.WriteString(headerStyle.Render(header))
	b.WriteString("\n")

	// Available height for the two-pane area:
	// header (text + border + margin) = 3 lines, plus explicit \n = 4 lines
	// footer (\n + help line) = 2 lines
	paneHeight := height - 6
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
			if row.IsInProgress {
				right = renderTailContent(st, rightWidth, paneHeight, dv.tailScroll)
				if row.IsPaused {
					right += "\n" + helpStyle.Render("(paused)")
				}
			} else if dv.showArtifact && row.SnapshotIndex >= 0 && row.SnapshotIndex < len(snaps) && snaps[row.SnapshotIndex].Artifact != "" {
				right = renderArtifactDetail(snaps, row.SnapshotIndex, rightWidth)
			} else {
				right = renderSnapshotDetail(snaps, row.SnapshotIndex, rightWidth)
			}
		}

		b.WriteString(joinPanes(left, right, leftWidth, paneHeight))
	}

	b.WriteString("\n")
	help := "j/k: iterations  enter: focus output  a: attach  s: start  x: stop  g: guidance  q/esc: back"
	if dv.focusRight {
		help = "j/k: scroll  G: bottom  esc: back to list"
	}
	// Show artifact toggle hint when the selected snapshot has an artifact
	if !dv.focusRight {
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
	}
	b.WriteString(helpStyle.Render(help))

	return b.String()
}

func renderIterationList(rows []iterationRow, cursor int, width int, dimmed bool, spinnerFrame string) string {
	var b strings.Builder
	b.WriteString(labelStyle.Render("Iterations"))
	b.WriteString("\n")

	for i, row := range rows {
		label := fmt.Sprintf("%s %d", row.Phase, row.Iteration+1)
		if row.IsInProgress {
			if row.IsPaused {
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

	b.WriteString(labelStyle.Render("Summary"))
	b.WriteString("\n")
	b.WriteString(wrapText(snap.Summary, width))
	b.WriteString("\n\n")

	if snap.Review != "" {
		b.WriteString(labelStyle.Render("Review"))
		b.WriteString("\n")
		b.WriteString(wrapText(snap.Review, width))
		b.WriteString("\n\n")
	}

	if snap.DiffStat != "" {
		b.WriteString(labelStyle.Render("Diff"))
		b.WriteString("\n")
		b.WriteString(snap.DiffStat)
		b.WriteString("\n\n")
	}

	if len(snap.GateResults) > 0 {
		b.WriteString(labelStyle.Render("Gates"))
		b.WriteString("\n")
		for _, g := range snap.GateResults {
			mark := "+"
			if !g.Passed {
				mark = "-"
			}
			b.WriteString(fmt.Sprintf("  [%s] %s\n", mark, g.Gate))
		}
		b.WriteString("\n")
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
