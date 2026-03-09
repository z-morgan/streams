package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/zmorgan/streams/internal/stream"
)

func renderTailContent(st *stream.Stream, width, availableHeight, scrollOffset int) string {
	lines := st.GetOutputLines()
	if len(lines) == 0 {
		return helpStyle.Render("No output yet.")
	}

	// Collapse consecutive empty lines
	lines = collapseEmptyLines(lines)

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

	errorStyle := lipgloss.NewStyle().Foreground(colorError)

	var b strings.Builder
	for i := startIdx; i < endIdx; i++ {
		line := lines[i]
		if strings.HasPrefix(line, "> ") {
			b.WriteString(toolLineStyle.Render(line))
		} else if isErrorLine(line) {
			b.WriteString(errorStyle.Render(wrapText(line, width-2)))
		} else {
			b.WriteString(wrapText(line, width-2))
		}
		b.WriteString("\n")
	}

	return b.String()
}

// collapseEmptyLines replaces runs of consecutive empty lines with a single empty line.
func collapseEmptyLines(lines []string) []string {
	var result []string
	prevEmpty := false
	for _, line := range lines {
		empty := strings.TrimSpace(line) == ""
		if empty && prevEmpty {
			continue
		}
		result = append(result, line)
		prevEmpty = empty
	}
	return result
}

// isErrorLine returns true if a line appears to contain an error message.
func isErrorLine(line string) bool {
	lower := strings.ToLower(line)
	return strings.Contains(lower, "error") ||
		strings.Contains(lower, "failed") ||
		strings.Contains(lower, "panic:") ||
		strings.HasPrefix(lower, "fatal")
}
