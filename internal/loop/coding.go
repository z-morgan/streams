package loop

import (
	"fmt"
	"os/exec"
	"strings"
)

func (p *CodingPhase) ImplementPrompt(ctx PhaseContext) (string, error) {
	return LoadPrompt("coding", "implement", promptDataFromContext(ctx))
}

func (p *CodingPhase) ReviewPrompt(ctx PhaseContext) (string, error) {
	return LoadPrompt("coding", "review", promptDataFromContext(ctx))
}

// CodingPhase implements MacroPhase for the coding iteration cycle.
type CodingPhase struct{}

func (p *CodingPhase) Name() string { return "coding" }

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
