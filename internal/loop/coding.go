package loop

import (
	"fmt"
	"os/exec"
	"strings"
)

// CodingPhase implements MacroPhase for the coding iteration cycle.
type CodingPhase struct{}

func (p *CodingPhase) Name() string { return "coding" }

func (p *CodingPhase) ImplementPrompt(ctx PhaseContext) string {
	return fmt.Sprintf(`You are implementing a software task. Work items are tracked as child beads issues under the parent.

Task: %s
Parent issue: %s

Work through these steps in order:
%s
For each step:
1. Implement the change described in the issue.
2. Run tests.
3. Commit with a descriptive message.
4. Close the issue: bd close {step_id}

If there are also open review feedback issues (description references a commit SHA), address those with fixup commits: git commit --fixup=<sha>

Rules:
- One commit per issue.
- Run tests before committing.
- Do not create new beads issues.`,
		ctx.Stream.Task, ctx.Stream.BeadsParentID, ctx.OrderedSteps)
}

func (p *CodingPhase) ReviewPrompt(ctx PhaseContext) string {
	return fmt.Sprintf(`You are reviewing code that was just written or refined. Your job is to file specific, actionable improvement issues — not to make changes yourself.

Task: %s
Parent issue: %s

Steps:
1. Read the relevant code (use Glob/Grep/Read to find what was changed).
2. Review the git log to understand the commit structure.
3. Evaluate against these criteria:
   - Pattern conformance: Does this match how the codebase already does things?
   - Simplicity: Can anything be removed or consolidated?
   - Readability: Would a new developer understand this without comments?
   - Correctness: Are there bugs, edge cases, or missing error handling?
4. For each improvement, file a child issue referencing the relevant commit SHA in the description:
   bd create --parent %s --title="<specific action>" --type=task --priority=2 --description="Commit <sha>: <what to change and why>"
5. If no improvements needed, respond with exactly: "No further improvements needed."

Rules:
- Do NOT edit or write any files.
- Each issue must be a single, actionable change.
- Do not file issues about style/formatting that a linter would catch.
- Do not file issues for missing features outside the task scope.
- Maximum 5 issues per review.`,
		ctx.Stream.Task, ctx.Stream.BeadsParentID, ctx.Stream.BeadsParentID)
}

func (p *CodingPhase) ImplementTools() []string {
	return []string{"Bash", "Read", "Edit", "Write", "Glob", "Grep"}
}

func (p *CodingPhase) ReviewTools() []string {
	return []string{"Bash", "Read", "Glob", "Grep"}
}

func (p *CodingPhase) IsConverged(result IterationResult) bool {
	return result.OpenChildrenAfter <= result.OpenChildrenBefore
}

// BeforeReview runs git rebase --autosquash to collapse fixup commits
// so the review step sees clean history.
func (p *CodingPhase) BeforeReview(ctx PhaseContext) error {
	// Get current HEAD to compare with BaseSHA.
	headCmd := exec.Command("git", "rev-parse", "HEAD")
	headCmd.Dir = ctx.WorkDir
	headOut, err := headCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to get HEAD: %w", err)
	}
	headSHA := strings.TrimSpace(string(headOut))

	// If no commits were made since BaseSHA, there's nothing to squash.
	if headSHA == ctx.Stream.BaseSHA {
		return nil
	}

	// Check for uncommitted changes that would block the rebase.
	statusCmd := exec.Command("git", "status", "--porcelain")
	statusCmd.Dir = ctx.WorkDir
	statusOut, err := statusCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to check git status: %w", err)
	}
	dirty := len(strings.TrimSpace(string(statusOut))) > 0

	// Stash uncommitted changes before rebasing.
	if dirty {
		stash := exec.Command("git", "stash", "--include-untracked")
		stash.Dir = ctx.WorkDir
		if out, err := stash.CombinedOutput(); err != nil {
			return fmt.Errorf("git stash failed: %s", out)
		}
	}

	cmd := exec.Command("git", "rebase", "--autosquash", ctx.Stream.BaseSHA)
	cmd.Dir = ctx.WorkDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Abort the failed rebase to leave the worktree in a clean state.
		abort := exec.Command("git", "rebase", "--abort")
		abort.Dir = ctx.WorkDir
		_ = abort.Run()

		// Restore stashed changes even on rebase failure.
		if dirty {
			pop := exec.Command("git", "stash", "pop")
			pop.Dir = ctx.WorkDir
			_ = pop.Run()
		}
		return fmt.Errorf("autosquash rebase failed: %s", out)
	}

	// Restore stashed changes after successful rebase.
	if dirty {
		pop := exec.Command("git", "stash", "pop")
		pop.Dir = ctx.WorkDir
		if popOut, err := pop.CombinedOutput(); err != nil {
			return fmt.Errorf("autosquash succeeded but stash pop failed: %s", popOut)
		}
	}

	return nil
}

func (p *CodingPhase) TransitionMode() Transition {
	return TransitionPause
}
