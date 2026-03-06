package ui

import (
	"fmt"
	"strings"

	"github.com/zmorgan/streams/internal/stream"
)

type detailView struct {
	snapCursor int
}

func (d *detailView) clampCursor(count int) {
	if count == 0 {
		d.snapCursor = 0
		return
	}
	if d.snapCursor >= count {
		d.snapCursor = count - 1
	}
	if d.snapCursor < 0 {
		d.snapCursor = 0
	}
}

func renderDetail(st *stream.Stream, snapCursor int, width, height int) string {
	if st == nil {
		return "No stream selected."
	}

	var b strings.Builder

	// Header
	header := fmt.Sprintf("%s  [%s]  %s  iter %d",
		st.Name, st.GetStatus(), currentPhase(st), st.GetIteration())
	b.WriteString(headerStyle.Render(header))
	b.WriteString("\n")

	snaps := st.GetSnapshots()

	if len(snaps) == 0 {
		b.WriteString(helpStyle.Render("No snapshots yet."))
		b.WriteString("\n")
	} else {
		// Two-pane: left = snapshot list, right = selected snapshot details
		leftWidth := 25
		rightWidth := width - leftWidth - 3 // 3 for separator
		if rightWidth < 40 {
			rightWidth = 40
		}

		left := renderSnapshotList(snaps, snapCursor, leftWidth)
		right := renderSnapshotDetail(snaps, snapCursor, rightWidth)

		b.WriteString(lipglossJoinHorizontal(left, right))
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("j/k: snapshots  s: start  x: stop  g: guidance  q/esc: back"))

	return b.String()
}

func renderSnapshotList(snaps []stream.Snapshot, cursor int, width int) string {
	var b strings.Builder
	b.WriteString(labelStyle.Render("Snapshots"))
	b.WriteString("\n")

	for i, snap := range snaps {
		label := fmt.Sprintf("%s %d", snap.Phase, snap.Iteration+1)
		if snap.Error != nil {
			label += " !"
		}

		if i == cursor {
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

func renderErrorBlock(err *stream.LoopError) string {
	msg := fmt.Sprintf("Error [%s at %s]: %s", err.Kind, err.Step, err.Message)
	if err.Detail != "" {
		msg += "\n" + err.Detail
	}
	return errorBlockStyle.Render(msg)
}

func lipglossJoinHorizontal(left, right string) string {
	leftLines := strings.Split(left, "\n")
	rightLines := strings.Split(right, "\n")

	maxLeft := 0
	for _, l := range leftLines {
		if len(l) > maxLeft {
			maxLeft = len(l)
		}
	}

	maxLines := len(leftLines)
	if len(rightLines) > maxLines {
		maxLines = len(rightLines)
	}

	var b strings.Builder
	for i := 0; i < maxLines; i++ {
		l := ""
		if i < len(leftLines) {
			l = leftLines[i]
		}
		r := ""
		if i < len(rightLines) {
			r = rightLines[i]
		}
		b.WriteString(fmt.Sprintf("%-*s | %s\n", maxLeft, l, r))
	}
	return b.String()
}

func wrapText(s string, width int) string {
	if width <= 0 || len(s) <= width {
		return s
	}
	var b strings.Builder
	for len(s) > width {
		b.WriteString(s[:width])
		b.WriteString("\n")
		s = s[width:]
	}
	b.WriteString(s)
	return b.String()
}
