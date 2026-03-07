package ui

import (
	"strings"

	"github.com/zmorgan/streams/internal/stream"
)

func renderTailContent(st *stream.Stream, width, availableHeight int) string {
	lines := st.GetOutputLines()
	if len(lines) == 0 {
		return helpStyle.Render("No output yet.")
	}

	if availableHeight < 5 {
		availableHeight = 5
	}

	startIdx := len(lines) - availableHeight
	if startIdx < 0 {
		startIdx = 0
	}

	var b strings.Builder
	for i := startIdx; i < len(lines); i++ {
		line := lines[i]
		if strings.HasPrefix(line, "> ") {
			b.WriteString(toolLineStyle.Render(line))
		} else {
			b.WriteString(wrapText(line, width-2))
		}
		b.WriteString("\n")
	}
	return b.String()
}
