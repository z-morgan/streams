package ui

import (
	"fmt"
	"strings"

	"github.com/zmorgan/streams/internal/stream"
)

type tailView struct {
	scrollOffset int // lines scrolled up from bottom (0 = auto-follow)
}

func renderTail(st *stream.Stream, tv tailView, width, height int) string {
	if st == nil {
		return "No stream selected."
	}

	var b strings.Builder

	// Header
	header := fmt.Sprintf("%s  [%s]  %s  iter %d",
		st.Name, st.GetStatus(), currentPhase(st), st.GetIteration())
	b.WriteString(headerStyle.Render(header))
	b.WriteString("\n")

	lines := st.GetOutputLines()

	// Calculate available height for output (header + footer use ~3 lines)
	availableHeight := height - 4
	if availableHeight < 5 {
		availableHeight = 5
	}

	if len(lines) == 0 {
		b.WriteString(helpStyle.Render("No output yet."))
		b.WriteString("\n")
	} else {
		// Determine visible window
		totalLines := len(lines)
		endIdx := totalLines - tv.scrollOffset
		if endIdx < 0 {
			endIdx = 0
		}
		if endIdx > totalLines {
			endIdx = totalLines
		}
		startIdx := endIdx - availableHeight
		if startIdx < 0 {
			startIdx = 0
		}

		for i := startIdx; i < endIdx; i++ {
			line := lines[i]
			if strings.HasPrefix(line, "> ") {
				b.WriteString(toolLineStyle.Render(line))
			} else {
				b.WriteString(wrapText(line, width-2))
			}
			b.WriteString("\n")
		}

		// Scroll indicator
		if tv.scrollOffset > 0 {
			indicator := fmt.Sprintf("-- %d lines below --", tv.scrollOffset)
			b.WriteString(helpStyle.Render(indicator))
			b.WriteString("\n")
		}
	}

	footer := helpStyle.Render("j/k: scroll  G: bottom  q/esc: back")
	return layoutWithFooter(b.String(), footer, height)
}
