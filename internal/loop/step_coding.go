package loop

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
)

// StepCodingPhase implements MacroPhase for step-per-iteration coding.
// Each iteration either implements the next unclosed step bead (step mode)
// or addresses open review issues (fix mode).
type StepCodingPhase struct{}

func (p *StepCodingPhase) Name() string { return "step-coding" }

func (p *StepCodingPhase) ImplementPrompt(ctx PhaseContext) (string, error) {
	data := promptDataFromContext(ctx)

	if data.IsFixMode {
		return LoadPrompt("step-coding", "implement-fix", data)
	}
	return LoadPrompt("step-coding", "implement-step", data)
}

func (p *StepCodingPhase) ReviewPrompt(ctx PhaseContext) (string, error) {
	return LoadPrompt("step-coding", "review", promptDataFromContext(ctx))
}

func (p *StepCodingPhase) ImplementTools() []string {
	return []string{"Bash", "Read", "Edit", "Write", "Glob", "Grep"}
}

func (p *StepCodingPhase) ReviewTools() []string {
	return []string{"Bash", "Read", "Glob", "Grep"}
}

// IsConverged returns true when all step beads are closed and no review
// issues remain (OpenAfterReview == 0).
func (p *StepCodingPhase) IsConverged(result IterationResult) bool {
	return result.OpenAfterReview == 0
}

// BeforeReview runs autosquash with agent-based conflict resolution.
func (p *StepCodingPhase) BeforeReview(ctx PhaseContext) error {
	headCmd := exec.Command("git", "rev-parse", "HEAD")
	headCmd.Dir = ctx.WorkDir
	headOut, err := headCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to get HEAD: %w", err)
	}
	headSHA := strings.TrimSpace(string(headOut))

	if headSHA == ctx.Stream.BaseSHA {
		return nil
	}

	statusCmd := exec.Command("git", "status", "--porcelain")
	statusCmd.Dir = ctx.WorkDir
	statusOut, err := statusCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to check git status: %w", err)
	}
	dirty := len(strings.TrimSpace(string(statusOut))) > 0

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

		if strings.Contains(rebaseOutput, "would make it empty") ||
			strings.Contains(rebaseOutput, "would make\nit empty") {
			abort := exec.Command("git", "rebase", "--abort")
			abort.Dir = ctx.WorkDir
			_ = abort.Run()
			if dirty {
				pop := exec.Command("git", "stash", "pop")
				pop.Dir = ctx.WorkDir
				_ = pop.Run()
			}
			return fmt.Errorf("autosquash produced empty commit (fixup fully reverted its target)")
		}

		slog.Info("autosquash failed, attempting agent resolution", "stream", ctx.Stream.ID)

		agentErr := runRebaseAgent(context.Background(), ctx, rebaseOutput, p.ImplementTools())
		if agentErr == nil {
			if !isRebaseInProgress(ctx.WorkDir) {
				postHead := exec.Command("git", "rev-parse", "HEAD")
				postHead.Dir = ctx.WorkDir
				postOut, postErr := postHead.Output()
				if postErr != nil || strings.TrimSpace(string(postOut)) == ctx.Stream.BaseSHA {
					return fmt.Errorf("rebase agent dropped all commits — HEAD is at baseSHA")
				}
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

	if dirty {
		pop := exec.Command("git", "stash", "pop")
		pop.Dir = ctx.WorkDir
		if popOut, err := pop.CombinedOutput(); err != nil {
			return fmt.Errorf("autosquash succeeded but stash pop failed: %s", popOut)
		}
	}

	return nil
}

func (p *StepCodingPhase) TransitionMode() Transition {
	return TransitionAutoAdvance
}

func (p *StepCodingPhase) ArtifactFile() string { return "" }
