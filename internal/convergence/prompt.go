package convergence

import (
	"fmt"
	"strings"
)

// PromptContext holds the data for rendering the convergence context block.
type PromptContext struct {
	Iteration           int
	Phase               string
	Mode                Mode
	RefinementCap       int
	MaxSectionRevisions int
	FrozenSections      []FrozenSection
}

// RenderPromptBlock generates the ## Convergence Context block to inject
// into review prompts. Returns empty string if there is no relevant context.
func RenderPromptBlock(ctx PromptContext) string {
	var b strings.Builder

	b.WriteString("\n\n## Convergence Context\n\n")
	fmt.Fprintf(&b, "This is review iteration %d of the %s phase.\n", ctx.Iteration, ctx.Phase)

	if len(ctx.FrozenSections) > 0 {
		fmt.Fprintf(&b, "\n### Frozen Sections\nThe following sections have been revised %d+ times and are now frozen. Do NOT file issues about them unless you identify a correctness bug (Tier 1).\n", ctx.MaxSectionRevisions)
		for _, fs := range ctx.FrozenSections {
			heading := fs.Heading
			if heading == "" {
				heading = fs.ID
			}
			fmt.Fprintf(&b, "- **%s** (revised %d times, frozen at iteration %d)\n", heading, fs.RevisionCount, fs.FrozenAt)
		}
	}

	if ctx.Iteration >= ctx.RefinementCap {
		fmt.Fprintf(&b, "\n### Refinement Cap Reached\nThis phase has exceeded the refinement cap (%d iterations). Only file Tier 1 (correctness) and Tier 2 (completeness) issues. All other issues will be logged as advisory and will not trigger another iteration.\n", ctx.RefinementCap)
	}

	b.WriteString("\n### Issue Tiers\nWhen filing issues, you MUST classify each one by including a tier tag in the issue title:\n\n")
	b.WriteString("  [T1] — Correctness: Would cause a runtime error, data loss, or security issue.\n")
	b.WriteString("  [T2] — Completeness: A requirement from the task is not addressed.\n")
	b.WriteString("  [T3] — Design: The approach works but could be structured better.\n")
	b.WriteString("  [T4] — Polish: Style preferences, minor naming, or formatting choices.\n")

	return b.String()
}
