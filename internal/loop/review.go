package loop

import (
	"fmt"
	"os/exec"
	"strings"
)

// ReviewPhase implements MacroPhase for the post-coding review summary.
// It produces a structured summary of the stream's work, then pauses for
// human action (revise or complete).
type ReviewPhase struct{}

func (p *ReviewPhase) Name() string { return "review" }

func (p *ReviewPhase) ImplementPrompt(ctx PhaseContext) (string, error) {
	data := promptDataFromContext(ctx)

	// Populate review-specific fields from git state and snapshots.
	baseSHA := ctx.Stream.BaseSHA
	workDir := ctx.WorkDir

	if workDir != "" && baseSHA != "" {
		cmd := exec.Command("git", "log", "--oneline", baseSHA+"..HEAD")
		cmd.Dir = workDir
		if out, err := cmd.Output(); err == nil {
			data.CommitLog = strings.TrimSpace(string(out))
		}

		cmd = exec.Command("git", "diff", "--stat", baseSHA+"..HEAD")
		cmd.Dir = workDir
		if out, err := cmd.Output(); err == nil {
			data.DiffStat = strings.TrimSpace(string(out))
		}
	}

	// Build snapshot summaries and total cost.
	snaps := ctx.Stream.GetSnapshots()
	var totalCost float64
	var summaryLines []string
	for _, snap := range snaps {
		totalCost += snap.CostUSD
		if snap.Summary != "" {
			line := fmt.Sprintf("[%s iter %d] %s", snap.Phase, snap.Iteration+1, snap.Summary)
			summaryLines = append(summaryLines, line)
		}
		if snap.Review != "" {
			line := fmt.Sprintf("[%s iter %d review] %s", snap.Phase, snap.Iteration+1, snap.Review)
			summaryLines = append(summaryLines, line)
		}
	}
	data.TotalCost = fmt.Sprintf("$%.2f", totalCost)
	data.SnapshotSummaries = strings.Join(summaryLines, "\n\n")

	return LoadPrompt("review", "implement", data)
}

func (p *ReviewPhase) ReviewPrompt(_ PhaseContext) (string, error) {
	return "", nil
}

func (p *ReviewPhase) ImplementTools() []string {
	return []string{"Bash", "Read", "Glob", "Grep"}
}

func (p *ReviewPhase) ReviewTools() []string {
	return nil
}

func (p *ReviewPhase) IsConverged(_ IterationResult) bool {
	return true
}

func (p *ReviewPhase) BeforeReview(_ PhaseContext) error {
	return nil
}

func (p *ReviewPhase) TransitionMode() Transition {
	return TransitionPause
}

func (p *ReviewPhase) ArtifactFile() string { return "" }
