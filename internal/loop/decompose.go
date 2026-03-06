package loop

import "fmt"

// DecomposePhase implements MacroPhase for breaking a plan into sequenced step beads.
type DecomposePhase struct{}

func (d *DecomposePhase) Name() string { return "decompose" }

func (d *DecomposePhase) ImplementPrompt(ctx PhaseContext) string {
	if ctx.Iteration == 0 {
		return fmt.Sprintf(`You are breaking a plan into implementation steps. Each step will become one commit.

Read plan.md and create one beads issue per logical step, using the notes field to encode execution order:
  bd create --parent %s --title="<descriptive action>" --type=task --priority=2 --notes "sequence:N" --description="<what to do in this step>"

Example:
  bd create --parent %s --title="Scaffold Go module and directory layout" --type=task --priority=2 --notes "sequence:1" --description="..."
  bd create --parent %s --title="Define runtime interface" --type=task --priority=2 --notes "sequence:2" --description="..."

Rules:
- Steps should be small and independently meaningful.
- Order matters — assign sequential sequence values starting at 1.
- Each step should be completable in a single commit.
- Titles should be descriptive actions, not numbered prefixes.
- Do not write code. Do not commit.`,
			ctx.Stream.BeadsParentID, ctx.Stream.BeadsParentID, ctx.Stream.BeadsParentID)
	}

	return fmt.Sprintf(`You are revising the decomposition of a plan into implementation steps based on feedback.

Task: %s
Parent issue: %s

Steps:
1. Run: bd show %s --children
2. For each open child issue that is feedback (not a step bead), read it, adjust the step beads accordingly (create, update notes, or close), and close the feedback issue with bd close.

Rules:
- Do not write code. Do not commit.
- Do not create new feedback issues — only adjust step beads and close feedback.`,
		ctx.Stream.Task, ctx.Stream.BeadsParentID, ctx.Stream.BeadsParentID)
}

func (d *DecomposePhase) ReviewPrompt(ctx PhaseContext) string {
	return fmt.Sprintf(`You are reviewing the decomposition of a plan into implementation steps. Check the child issues under the parent.

Parent issue: %s

Run: bd show %s --children

Evaluate: Are steps well-scoped? Well-ordered (check notes sequence values)? Missing steps? Steps that should be split or merged?

If steps need reordering, renumbering, insertion, or removal, file a child issue describing the change. The next implement iteration will update the sequence notes accordingly.

File child issues for any adjustments needed. If the decomposition is ready, respond with exactly: "No further improvements needed."`,
		ctx.Stream.BeadsParentID, ctx.Stream.BeadsParentID)
}

func (d *DecomposePhase) ImplementTools() []string {
	return []string{"Bash", "Read", "Glob", "Grep"}
}

func (d *DecomposePhase) ReviewTools() []string {
	return []string{"Bash", "Read", "Glob", "Grep"}
}

func (d *DecomposePhase) IsConverged(result IterationResult) bool {
	return result.OpenChildrenAfter <= result.OpenChildrenBefore
}

func (d *DecomposePhase) BeforeReview(_ PhaseContext) error {
	return nil
}

func (d *DecomposePhase) TransitionMode() Transition {
	return TransitionAutoAdvance
}
