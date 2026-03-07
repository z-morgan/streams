package loop

import "fmt"

// ResearchPhase implements MacroPhase for gathering codebase context before planning.
type ResearchPhase struct{}

func (r *ResearchPhase) Name() string { return "research" }

func (r *ResearchPhase) ImplementPrompt(ctx PhaseContext) string {
	if ctx.Iteration == 0 {
		return fmt.Sprintf(`You are researching a codebase to gather context for an upcoming task. Write your findings to research.md in the working directory.

Task: %s

Steps:
1. Explore the codebase structure — key directories, packages, and modules.
2. Identify the files and components most relevant to the task.
3. Note existing patterns, conventions, and architectural decisions.
4. Document any constraints, dependencies, or risks.

Rules:
- Do not write code. Do not commit.
- Do not create beads issues — that's the review step's job.
- Focus on facts about the codebase, not on planning solutions.`,
			ctx.Stream.Task)
	}

	return fmt.Sprintf(`You are revising your research based on feedback. The feedback is tracked as child issues under the parent beads issue.

Task: %s
Parent issue: %s

Steps:
1. Run: bd show %s --children
2. For each open child issue, read it, update research.md accordingly, and close it with bd close.

Rules:
- Do not write code. Do not commit.
- Do not create new beads issues.
- Focus on facts about the codebase, not on planning solutions.`,
		ctx.Stream.Task, ctx.Stream.BeadsParentID, ctx.Stream.BeadsParentID)
}

func (r *ResearchPhase) ReviewPrompt(ctx PhaseContext) string {
	return fmt.Sprintf(`You are reviewing research gathered for a software task. Your job is to file specific, actionable issues for missing or incorrect research — not to make changes yourself.

Task: %s
Parent issue: %s

Steps:
1. Read research.md.
2. Evaluate: Does the research cover the relevant parts of the codebase? Are there blind spots? Is anything inaccurate or superficial?
3. For each improvement, file a child issue:
   bd create --parent %s --title="<specific action>" --type=task --priority=2 --description="<what to research and why>"
4. If the research is sufficient, respond with exactly: "No further improvements needed."

Rules:
- Do NOT edit any files.
- Maximum 5 issues per review.`,
		ctx.Stream.Task, ctx.Stream.BeadsParentID, ctx.Stream.BeadsParentID)
}

func (r *ResearchPhase) ImplementTools() []string {
	return []string{"Bash", "Read", "Edit", "Write", "Glob", "Grep"}
}

func (r *ResearchPhase) ReviewTools() []string {
	return []string{"Bash", "Read", "Glob", "Grep"}
}

func (r *ResearchPhase) IsConverged(result IterationResult) bool {
	return result.OpenChildrenAfter <= result.OpenChildrenBefore
}

func (r *ResearchPhase) BeforeReview(_ PhaseContext) error {
	return nil
}

func (r *ResearchPhase) TransitionMode() Transition {
	return TransitionAutoAdvance
}
