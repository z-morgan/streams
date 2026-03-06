package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/zmorgan/streams/internal/stream"
)

type dashboardView struct {
	cursor int
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

func renderDashboard(streams []*stream.Stream, cursor int) string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("Streams"))
	b.WriteString("\n\n")

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
			iter := fmt.Sprintf("iter %d", st.Iteration)
			guidanceCount := len(st.Guidance)

			row := fmt.Sprintf("%s%-20s %s  %-10s  %s",
				prefix,
				truncate(st.Name, 20),
				status,
				phase,
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

const dashboardHelp = "j/k: navigate  enter: inspect  n: new  s: start  x: stop  g: guidance  q: quit"

func statusIndicator(st *stream.Stream) string {
	status := st.GetStatus()
	name := status.String()
	color, ok := statusColors[name]
	if !ok {
		color = colorMuted
	}
	style := lipgloss.NewStyle().Foreground(color)

	if status == stream.StatusPaused && st.LastError != nil {
		return lipgloss.NewStyle().Foreground(colorError).Bold(true).Render("! Error")
	}

	return style.Render(name)
}

func currentPhase(st *stream.Stream) string {
	if st.PipelineIndex < len(st.Pipeline) {
		return st.Pipeline[st.PipelineIndex]
	}
	return "done"
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

// layoutWithFooter places body at the top and footer at the bottom of the
// terminal, filling the gap between them with blank lines. This ensures the
// footer (help text) is always visible regardless of body length.
func layoutWithFooter(body, footer string, height int) string {
	if height <= 0 {
		return body + "\n" + footer
	}

	bodyLines := strings.Count(body, "\n") + 1
	footerLines := strings.Count(footer, "\n") + 1
	gap := height - bodyLines - footerLines
	if gap < 1 {
		gap = 1
	}

	return body + strings.Repeat("\n", gap) + footer
}
