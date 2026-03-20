package loop

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

// BeforeReview runs autosquash to collapse fixup commits before review.
func (p *StepCodingPhase) BeforeReview(ctx PhaseContext) error {
	return autosquash(ctx.WorkDir, ctx.Stream.BaseSHA)
}

func (p *StepCodingPhase) TransitionMode() Transition {
	return TransitionAutoAdvance
}

func (p *StepCodingPhase) ArtifactFile() string { return "" }
