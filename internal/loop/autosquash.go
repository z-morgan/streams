package loop

import (
	"fmt"
	"os/exec"
	"strings"
)

// autosquash runs git rebase --autosquash, handling stash/pop of dirty state.
// Returns nil on success, or an error with the rebase output on failure (after aborting).
func autosquash(workDir, baseSHA string) error {
	// Check if HEAD == BaseSHA (nothing to squash).
	headCmd := exec.Command("git", "rev-parse", "HEAD")
	headCmd.Dir = workDir
	headOut, err := headCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to get HEAD: %w", err)
	}
	if strings.TrimSpace(string(headOut)) == baseSHA {
		return nil
	}

	// Check for uncommitted changes.
	statusCmd := exec.Command("git", "status", "--porcelain")
	statusCmd.Dir = workDir
	statusOut, err := statusCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to check git status: %w", err)
	}
	dirty := len(strings.TrimSpace(string(statusOut))) > 0

	if dirty {
		stash := exec.Command("git", "stash", "--include-untracked")
		stash.Dir = workDir
		if out, err := stash.CombinedOutput(); err != nil {
			return fmt.Errorf("git stash failed: %s", out)
		}
	}

	cmd := exec.Command("git", "rebase", "--autosquash", baseSHA)
	cmd.Dir = workDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Abort the failed rebase and restore stash.
		abort := exec.Command("git", "rebase", "--abort")
		abort.Dir = workDir
		_ = abort.Run()

		if dirty {
			pop := exec.Command("git", "stash", "pop")
			pop.Dir = workDir
			_ = pop.Run()
		}
		return fmt.Errorf("autosquash rebase failed: %s", out)
	}

	if dirty {
		pop := exec.Command("git", "stash", "pop")
		pop.Dir = workDir
		if popOut, err := pop.CombinedOutput(); err != nil {
			return fmt.Errorf("autosquash succeeded but stash pop failed: %s", popOut)
		}
	}

	return nil
}
