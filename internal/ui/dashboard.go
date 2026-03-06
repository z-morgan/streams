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

func renderDashboardList(streams []*stream.Stream, cursor int) string {
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
			iter := fmt.Sprintf("iter %d", st.GetIteration())
			guidanceCount := st.GetGuidanceCount()

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

const dashboardListHelp = "j/k: navigate  enter: inspect  n: new  s: start  x: stop  d: delete  g: guidance  v: channels  q: quit"
const dashboardChannelHelp = "h/l: navigate  enter: inspect  n: new  s: start  x: stop  d: delete  g: guidance  v: list  q: quit"

func renderChannels(streams []*stream.Stream, cursor, scrollLeft, width, height int) string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("Streams"))
	b.WriteString("\n\n")

	if len(streams) == 0 {
		b.WriteString(helpStyle.Render("No streams yet. Press n to create one."))
		b.WriteString("\n")
		return b.String()
	}

	// placeholder — will be replaced in step 4
	b.WriteString(helpStyle.Render("[channel view]"))
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

	if status == stream.StatusPaused && st.GetLastError() != nil {
		return lipgloss.NewStyle().Foreground(colorError).Bold(true).Render("! Error")
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
