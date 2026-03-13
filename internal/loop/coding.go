package loop

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"

	"github.com/zmorgan/streams/internal/runtime"
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
	return result.OpenAfterReview <= result.OpenBeforeReview
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
		rebaseOutput := string(out)
		slog.Info("autosquash failed, attempting agent resolution", "stream", ctx.Stream.ID)

		// Try agent-based conflict resolution before aborting.
		agentErr := p.runRebaseAgent(context.Background(), ctx, rebaseOutput)
		if agentErr == nil {
			// Agent resolved conflicts — check that rebase actually completed.
			if !isRebaseInProgress(ctx.WorkDir) {
				if dirty {
					pop := exec.Command("git", "stash", "pop")
					pop.Dir = ctx.WorkDir
					if popOut, err := pop.CombinedOutput(); err != nil {
						return fmt.Errorf("autosquash succeeded (agent) but stash pop failed: %s", popOut)
					}
				}
				slog.Info("rebase agent resolved conflicts", "stream", ctx.Stream.ID)
				return nil
			}
			agentErr = fmt.Errorf("agent finished but rebase still in progress")
		}

		slog.Warn("rebase agent failed, aborting", "stream", ctx.Stream.ID, "err", agentErr)

		// Fall back: abort and restore.
		abort := exec.Command("git", "rebase", "--abort")
		abort.Dir = ctx.WorkDir
		_ = abort.Run()

		if dirty {
			pop := exec.Command("git", "stash", "pop")
			pop.Dir = ctx.WorkDir
			_ = pop.Run()
		}
		return fmt.Errorf("autosquash rebase failed (agent could not resolve): %s", rebaseOutput)
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

// runRebaseAgent invokes a Claude agent to resolve autosquash rebase conflicts.
func (p *CodingPhase) runRebaseAgent(ctx context.Context, pctx PhaseContext, rebaseOutput string) error {
	data := promptDataFromContext(pctx)
	data.RebaseOutput = rebaseOutput

	prompt, err := LoadPrompt("coding", "rebase", data)
	if err != nil {
		return fmt.Errorf("failed to load rebase prompt: %w", err)
	}

	rt := &runtime.BudgetRuntime{Inner: pctx.Runtime, MaxBudget: "0.50"}

	req := buildRequest(prompt, p.ImplementTools())
	req.OnOutput = func(line string) { pctx.Stream.AppendOutput(line) }

	_, err = rt.Run(ctx, req)
	return err
}

// isRebaseInProgress checks whether a git rebase is still in progress.
func isRebaseInProgress(workDir string) bool {
	cmd := exec.Command("git", "rev-parse", "--git-path", "rebase-merge")
	cmd.Dir = workDir
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	// git rev-parse --git-path returns the path; check if the directory exists.
	path := strings.TrimSpace(string(out))
	check := exec.Command("test", "-d", path)
	check.Dir = workDir
	return check.Run() == nil
}

func (p *CodingPhase) TransitionMode() Transition {
	return TransitionAutoAdvance
}

func (p *CodingPhase) ArtifactFile() string { return "" }
