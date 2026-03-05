package loop

import "fmt"

// PlanPhase implements MacroPhase for the plan drafting/review cycle.
type PlanPhase struct{}

func (p *PlanPhase) Name() string { return "plan" }

func (p *PlanPhase) ImplementPrompt(ctx PhaseContext) string {
	if ctx.Iteration == 0 {
		return fmt.Sprintf(`You are drafting a plan for a software task. Write or revise plan.md in the working directory.

Task: %s

Rules:
- Write a clear, step-by-step implementation plan.
- Do not write code. Do not commit.
- Do not create beads issues — that's the review step's job.`,
			ctx.Stream.Task)
	}

	return fmt.Sprintf(`You are revising a plan based on feedback. The feedback is tracked as child issues under the parent beads issue.

Task: %s
Parent issue: %s

Steps:
1. Run: bd show %s --children
2. For each open child issue, read it, update plan.md accordingly, and close it with bd close.

Rules:
- Do not write code. Do not commit.
- Do not create new beads issues.`,
		ctx.Stream.Task, ctx.Stream.BeadsParentID, ctx.Stream.BeadsParentID)
}

func (p *PlanPhase) ReviewPrompt(ctx PhaseContext) string {
	return fmt.Sprintf(`You are reviewing a plan for a software task. Your job is to file specific, actionable improvement issues — not to make changes yourself.

Task: %s
Parent issue: %s

Steps:
1. Read plan.md.
2. Evaluate: Is the plan complete? Are steps well-ordered? Are there gaps, ambiguities, or unnecessary complexity?
3. For each improvement, file a child issue:
   bd create --parent %s --title="<specific action>" --type=task --priority=2 --description="<what to change and why>"
4. If the plan is ready, respond with exactly: "No further improvements needed."

Rules:
- Do NOT edit any files.
- Maximum 5 issues per review.`,
		ctx.Stream.Task, ctx.Stream.BeadsParentID, ctx.Stream.BeadsParentID)
}

func (p *PlanPhase) ImplementTools() []string {
	return []string{"Bash", "Read", "Edit", "Write", "Glob", "Grep"}
}

func (p *PlanPhase) ReviewTools() []string {
	return []string{"Bash", "Read", "Glob", "Grep"}
}

func (p *PlanPhase) IsConverged(result IterationResult) bool {
	return result.OpenChildrenAfter <= result.OpenChildrenBefore
}

func (p *PlanPhase) BeforeReview(_ PhaseContext) error {
	return nil
}

func (p *PlanPhase) TransitionMode() Transition {
	return TransitionAutoAdvance
}
