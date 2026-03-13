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

	// Bead browse mode: activated by pressing enter on a completed snapshot with beads.
	beadFocused    bool   // true when bead-browse mode is active
	beadCursor     int    // index into combined bead list (closed then opened)
	beadShowOutput string // cached bd show output for the selected bead
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
	Phase              string
	Iteration          int
	IsInProgress       bool
	IsPaused           bool
	IsBreakpoint       bool
	HasError           bool
	IsInitialPrompt    bool
	IsPendingRevise    bool   // informational row: a revise is queued
	PendingRevisePhase string // target phase name for the pending revise
	Step               stream.IterStep // current step (only meaningful for in-progress rows)
	SnapshotIndex      int             // -1 for non-snapshot rows
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
		if pr := st.GetPendingRevise(); pr != nil {
			pipeline := st.GetPipeline()
			targetName := "?"
			if pr.TargetPhaseIndex < len(pipeline) {
				targetName = pipeline[pr.TargetPhaseIndex]
			}
			rows = append(rows, iterationRow{
				IsPendingRevise:    true,
				PendingRevisePhase: targetName,
				SnapshotIndex:      -1,
			})
		}
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
		// Account for border chars: 2 per pane (left+right border) = 4 total
		leftWidth := 27 // 25 content + 2 border
		rightWidth := width - leftWidth
		if rightWidth < 14 {
			rightWidth = 14
		}
		innerRight := rightWidth - 2 // content width inside right border

		dimLeft := dv.focusRight || dv.beadFocused
		left := renderIterationList(rows, dv.iterCursor, leftWidth-2, dimLeft, spinnerFrame)

		var right string
		rightTitle := "Snapshot"
		cursorIdx := dv.iterCursor
		if cursorIdx >= 0 && cursorIdx < len(rows) {
			row := rows[cursorIdx]
			if dv.beadFocused && row.SnapshotIndex >= 0 && row.SnapshotIndex < len(snaps) {
				rightTitle = "Beads"
				right = renderBeadBrowse(snaps[row.SnapshotIndex], dv, innerRight)
			} else if row.IsInitialPrompt {
				rightTitle = "Prompt"
				right = wrapText(st.Task, innerRight)
			} else if row.IsInProgress {
				rightTitle = "Live Output"
				right = renderTailContent(st, innerRight, paneHeight, dv.tailScroll)
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
				rightTitle = "Artifact"
				right = renderArtifactDetail(snaps, row.SnapshotIndex, innerRight)
			} else {
				right = renderSnapshotDetail(snaps, row.SnapshotIndex, innerRight)
			}
		}

		right = detailStatusMarker(st) + "\n" + right

		// Subtract 2 from paneHeight for top+bottom border lines
		innerHeight := paneHeight - 2
		if innerHeight < 3 {
			innerHeight = 3
		}

		// Scroll position footer for live output
		var rightFooter string
		if cursorIdx >= 0 && cursorIdx < len(rows) && rows[cursorIdx].IsInProgress {
			outputLines := st.GetOutputLines()
			totalLines := len(outputLines)
			if totalLines > 0 && dv.tailScroll > 0 {
				endLine := totalLines - dv.tailScroll
				if endLine < 0 {
					endLine = 0
				}
				rightFooter = fmt.Sprintf("line %d/%d", endLine, totalLines)
			}
		}
		b.WriteString(joinPanes(left, right, "Iterations", rightTitle, rightFooter, leftWidth, rightWidth, innerHeight, dv.focusRight))
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
	if dv.beadFocused {
		if dv.beadShowOutput != "" {
			return "esc: back to bead list"
		}
		return "j/k: select bead  enter: show details  esc: back to snapshot"
	}

	if dv.focusRight {
		return "j/k: scroll  G: bottom  esc: back to list"
	}

	status := st.GetStatus()

	if status == stream.StatusCompleted {
		return "D: diagnose  d: delete  q/esc: back"
	}

	if isPausedAtReview(st) {
		return "j/k: iterations  c: complete  r: revise  D: diagnose  g: guidance  b: breakpoints  d: delete  q/esc: back"
	}

	canRevise := st.GetPipelineIndex() > 0

	var help string
	if status == stream.StatusRunning {
		help = "j/k: iterations  enter: focus output  a: attach  w: wrap up  x: stop"
		if canRevise {
			help += "  r: revise"
		}
		help += "  g: guidance  b: breakpoints  q/esc: back"
	} else if canForceAdvance(st) {
		help = "j/k: iterations  enter: focus output  a: attach  s: start  >: skip phase  D: diagnose"
		if canRevise {
			help += "  r: revise"
		}
		help += "  g: guidance  b: breakpoints  q/esc: back"
	} else {
		help = "j/k: iterations  enter: focus output  a: attach  s: start  D: diagnose  x: stop"
		if canRevise {
			help += "  r: revise"
		}
		help += "  g: guidance  b: breakpoints  q/esc: back"
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
		label := ""
		icon := ""
		var iconColor lipgloss.Color

		if row.IsPendingRevise {
			reviseLabel := "↩ revise → " + row.PendingRevisePhase
			iconStr := lipgloss.NewStyle().Foreground(colorWarning).Render("↩")
			textStr := lipgloss.NewStyle().Foreground(colorMuted).Render(" revise → " + row.PendingRevisePhase)
			maxLabel := width - 2 // prefix
			full := iconStr + textStr
			if lipgloss.Width(full) > maxLabel {
				full = ansi.Truncate(reviseLabel, maxLabel, "…")
				full = lipgloss.NewStyle().Foreground(colorMuted).Render(full)
			}
			b.WriteString("  " + full + "\n")
			continue
		}

		if row.IsInitialPrompt {
			label = "Initial Prompt"
		} else {
			label = fmt.Sprintf("%s %d", row.Phase, row.Iteration+1)
			if row.IsInProgress {
				if row.IsBreakpoint || row.IsPaused {
					icon = "⏸"
					iconColor = colorWarning
				} else {
					if row.Step == stream.StepImplement || row.Step == stream.StepReview {
						label += " · " + row.Step.String()
					}
					icon = spinnerFrame
					iconColor = colorPrimary
				}
			} else if row.HasError {
				icon = "✗"
				iconColor = colorError
			} else {
				icon = "✓"
				iconColor = colorSuccess
			}
		}

		isSelected := !dimmed && i == cursor

		// Layout: [▌/ ][space][label...][padding][space+icon]
		prefixWidth := 2
		iconSpace := 0
		if icon != "" {
			iconSpace = 2 // space + icon char
		}
		maxLabel := width - prefixWidth - iconSpace
		if maxLabel < 0 {
			maxLabel = 0
		}

		if lipgloss.Width(label) > maxLabel {
			label = ansi.Truncate(label, maxLabel, "…")
		}
		pad := maxLabel - lipgloss.Width(label)
		if pad < 0 {
			pad = 0
		}

		if isSelected {
			bg := colorSubtle
			accent := lipgloss.NewStyle().Foreground(colorPrimary).Background(bg).Bold(true).Render("▌")
			labelColor := colorPrimary
			if row.IsInitialPrompt {
				labelColor = colorMuted
			}
			labelStr := lipgloss.NewStyle().Foreground(labelColor).Background(bg).Bold(true).Render(" " + label)
			padStr := lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", pad))
			iconStr := ""
			if icon != "" {
				iconStr = lipgloss.NewStyle().Foreground(iconColor).Background(bg).Render(" " + icon)
			}
			b.WriteString(accent + labelStr + padStr + iconStr)
		} else {
			labelColor := colorSecondary
			if dimmed || row.IsInitialPrompt {
				labelColor = colorMuted
			}
			labelStr := lipgloss.NewStyle().Foreground(labelColor).Render("  " + label)
			padStr := strings.Repeat(" ", pad)
			iconStr := ""
			if icon != "" {
				ic := iconColor
				if dimmed {
					ic = colorMuted
				}
				iconStr = lipgloss.NewStyle().Foreground(ic).Render(" " + icon)
			}
			b.WriteString(labelStr + padStr + iconStr)
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
	hr := helpStyle.Render(strings.Repeat("─", width))

	// Header with right-aligned cost
	headerRendered := labelStyle.Render("Iteration Snapshot")
	if snap.CostUSD > 0 {
		costStr := helpStyle.Render(fmt.Sprintf("$%.2f", snap.CostUSD))
		pad := width - lipgloss.Width(headerRendered) - lipgloss.Width(costStr)
		if pad < 1 {
			pad = 1
		}
		b.WriteString(headerRendered + strings.Repeat(" ", pad) + costStr)
	} else {
		b.WriteString(headerRendered)
	}
	b.WriteString("\n" + hr + "\n")

	b.WriteString(labelStyle.Render("Implementation Agent's Report"))
	b.WriteString("\n")
	b.WriteString(wrapText(snap.Summary, width))
	b.WriteString("\n")

	reviewText := snap.Review
	if reviewText == "" {
		reviewText = reviewFallback(snap)
	}
	if reviewText != "" {
		b.WriteString("\n" + hr + "\n")
		b.WriteString(labelStyle.Render("Review Agent's Report"))
		b.WriteString("\n")
		b.WriteString(wrapText(reviewText, width))
		b.WriteString("\n")
	}

	if len(snap.BeadsClosed) > 0 || len(snap.BeadsOpened) > 0 {
		b.WriteString("\n" + hr + "\n")
	}

	successIcon := lipgloss.NewStyle().Foreground(colorSuccess).Render("✓")
	openedIcon := lipgloss.NewStyle().Foreground(colorWarning).Render("+")

	if len(snap.BeadsClosed) > 0 {
		b.WriteString(labelStyle.Render(fmt.Sprintf("Closed (%d)", len(snap.BeadsClosed))))
		b.WriteString("\n")
		for _, id := range snap.BeadsClosed {
			b.WriteString("  " + successIcon + " " + beadLabel(id, snap.BeadTitles) + "\n")
		}
	}

	if len(snap.BeadsOpened) > 0 {
		if len(snap.BeadsClosed) > 0 {
			b.WriteString("\n")
		}
		b.WriteString(labelStyle.Render(fmt.Sprintf("Opened (%d)", len(snap.BeadsOpened))))
		b.WriteString("\n")
		for _, id := range snap.BeadsOpened {
			b.WriteString("  " + openedIcon + " " + beadLabel(id, snap.BeadTitles) + "\n")
		}
	}

	if snap.DiffStat != "" {
		b.WriteString("\n" + hr + "\n")
		b.WriteString(labelStyle.Render("Diff"))
		b.WriteString("\n")
		b.WriteString(colorizeDiffStat(snap.DiffStat))
		b.WriteString("\n")
	}

	if len(snap.GuidanceConsumed) > 0 {
		b.WriteString("\n" + hr + "\n")
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

// colorizeDiffStat renders +/- characters in diff stat lines with green/red.
func colorizeDiffStat(stat string) string {
	greenStyle := lipgloss.NewStyle().Foreground(colorSuccess)
	redStyle := lipgloss.NewStyle().Foreground(colorError)
	lines := strings.Split(stat, "\n")
	for i, line := range lines {
		if idx := strings.LastIndex(line, "|"); idx >= 0 {
			prefix := line[:idx+1]
			rest := line[idx+1:]
			var colored strings.Builder
			colored.WriteString(prefix)
			for _, ch := range rest {
				switch ch {
				case '+':
					colored.WriteString(greenStyle.Render("+"))
				case '-':
					colored.WriteString(redStyle.Render("-"))
				default:
					colored.WriteRune(ch)
				}
			}
			lines[i] = colored.String()
		}
	}
	return strings.Join(lines, "\n")
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
	fieldStyle := lipgloss.NewStyle().Foreground(colorSecondary)
	var b strings.Builder
	b.WriteString(fieldStyle.Render("Kind:") + " " + err.Kind.String() + "\n")
	b.WriteString(fieldStyle.Render("Step:") + " " + err.Step.String() + "\n")
	b.WriteString(fieldStyle.Render("Message:") + " " + err.Message)
	if err.Detail != "" {
		b.WriteString("\n" + fieldStyle.Render("Detail:") + "\n" + err.Detail)
	}
	return errorBlockStyle.Render(b.String())
}

// borderedPane renders content inside a labeled bordered box.
// title appears in the top border. footer (if non-empty) appears in the bottom border.
func borderedPane(content, title, footer string, width, height int, borderColor lipgloss.Color) string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(borderColor)
	borderStyle := lipgloss.NewStyle().Foreground(borderColor)

	innerWidth := width - 2 // left and right border chars

	// Top border: ╭─ Title ──────╮
	titleRendered := titleStyle.Render(title)
	titleVisWidth := lipgloss.Width(titleRendered)
	fillLen := innerWidth - titleVisWidth - 3 // 3 for "─ " before and " " after title
	if fillLen < 0 {
		fillLen = 0
	}
	topLine := borderStyle.Render("╭─") + " " + titleRendered + " " + borderStyle.Render(strings.Repeat("─", fillLen)+"╮")

	// Content lines, padded and clipped to fit
	lines := strings.Split(content, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}

	var b strings.Builder
	b.WriteString(topLine + "\n")
	for i := 0; i < height; i++ {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		// Pad to inner width
		visWidth := lipgloss.Width(line)
		pad := innerWidth - visWidth
		if pad < 0 {
			// Truncate if too wide
			line = ansi.Truncate(line, innerWidth, "…")
			pad = 0
		}
		b.WriteString(borderStyle.Render("│") + line + strings.Repeat(" ", pad) + borderStyle.Render("│") + "\n")
	}
	// Bottom border with optional footer label
	if footer != "" {
		footerRendered := helpStyle.Render(footer)
		footerVisWidth := lipgloss.Width(footerRendered)
		footerFill := innerWidth - footerVisWidth - 3
		if footerFill < 0 {
			footerFill = 0
		}
		b.WriteString(borderStyle.Render("╰"+strings.Repeat("─", footerFill)+" ") + footerRendered + " " + borderStyle.Render("╯"))
	} else {
		b.WriteString(borderStyle.Render("╰" + strings.Repeat("─", innerWidth) + "╯"))
	}

	return b.String()
}

func joinPanes(left, right string, leftTitle, rightTitle, rightFooter string, leftWidth, rightWidth, maxLines int, focusRight bool) string {
	leftColor := colorMuted
	rightColor := colorMuted
	if focusRight {
		rightColor = colorPrimary
	} else {
		leftColor = colorPrimary
	}

	leftBox := borderedPane(left, leftTitle, "", leftWidth, maxLines, leftColor)
	rightBox := borderedPane(right, rightTitle, rightFooter, rightWidth, maxLines, rightColor)

	return lipgloss.JoinHorizontal(lipgloss.Top, leftBox, rightBox)
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
	labels := make([]string, n)
	for i, id := range snap.BeadsOpened {
		labels[i] = beadLabel(id, snap.BeadTitles)
	}
	if n == 1 {
		return fmt.Sprintf("Filed 1 issue: %s", labels[0])
	}
	return fmt.Sprintf("Filed %d issues: %s", n, strings.Join(labels, ", "))
}

// beadLabel returns "id — title" when a title is available, otherwise just the id.
func beadLabel(id string, titles map[string]string) string {
	if title, ok := titles[id]; ok && title != "" {
		return id + " — " + title
	}
	return id
}

// snapshotBeadIDs returns the combined list of bead IDs (closed then opened) for a snapshot.
func snapshotBeadIDs(snap stream.Snapshot) []string {
	ids := make([]string, 0, len(snap.BeadsClosed)+len(snap.BeadsOpened))
	ids = append(ids, snap.BeadsClosed...)
	ids = append(ids, snap.BeadsOpened...)
	return ids
}

// renderBeadBrowse renders the bead browse mode for a snapshot.
// If beadShowOutput is set, it displays the full bd show output.
// Otherwise it shows the navigable bead list.
func renderBeadBrowse(snap stream.Snapshot, dv detailView, width int) string {
	if dv.beadShowOutput != "" {
		return wrapText(dv.beadShowOutput, width)
	}

	var b strings.Builder
	closedIcon := lipgloss.NewStyle().Foreground(colorSuccess).Render("✓")
	openedIcon := lipgloss.NewStyle().Foreground(colorWarning).Render("+")

	allIDs := snapshotBeadIDs(snap)
	closedCount := len(snap.BeadsClosed)

	for i, id := range allIDs {
		icon := closedIcon
		if i >= closedCount {
			icon = openedIcon
		}

		label := beadLabel(id, snap.BeadTitles)

		if i == dv.beadCursor {
			bg := colorSubtle
			accent := lipgloss.NewStyle().Foreground(colorPrimary).Background(bg).Bold(true).Render("▌")
			text := lipgloss.NewStyle().Foreground(colorPrimary).Background(bg).Bold(true).Render(" " + label)

			maxLabel := width - 4 // accent + icon + spaces
			if lipgloss.Width(label) > maxLabel {
				label = ansi.Truncate(label, maxLabel, "…")
				text = lipgloss.NewStyle().Foreground(colorPrimary).Background(bg).Bold(true).Render(" " + label)
			}

			pad := width - lipgloss.Width(accent) - lipgloss.Width(text) - lipgloss.Width(icon) - 1
			if pad < 0 {
				pad = 0
			}
			padStr := lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", pad))
			iconStr := lipgloss.NewStyle().Background(bg).Render(icon)
			b.WriteString(accent + iconStr + text + padStr + "\n")
		} else {
			b.WriteString("  " + icon + " " + label + "\n")
		}
	}

	return b.String()
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
