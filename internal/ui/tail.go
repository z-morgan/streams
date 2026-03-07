package ui

import (
	"fmt"
	"strings"

	"github.com/zmorgan/streams/internal/stream"
)

func renderTailContent(st *stream.Stream, width, availableHeight, scrollOffset int) string {
	lines := st.GetOutputLines()
	if len(lines) == 0 {
		return helpStyle.Render("No output yet.")
	}

	if availableHeight < 5 {
		availableHeight = 5
	}

	endIdx := len(lines) - scrollOffset
	if endIdx < 0 {
		endIdx = 0
	}
	if endIdx > len(lines) {
		endIdx = len(lines)
	}
	startIdx := endIdx - availableHeight
	if startIdx < 0 {
		startIdx = 0
	}

	var b strings.Builder
	for i := startIdx; i < endIdx; i++ {
		line := lines[i]
		if strings.HasPrefix(line, "> ") {
			b.WriteString(toolLineStyle.Render(line))
		} else {
			b.WriteString(wrapText(line, width-2))
		}
		b.WriteString("\n")
	}

	if scrollOffset > 0 {
		indicator := fmt.Sprintf("-- %d lines below --", scrollOffset)
		b.WriteString(helpStyle.Render(indicator))
		b.WriteString("\n")
	}

	return b.String()
}
