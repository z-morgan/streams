package loop

// RefinementPhase implements MacroPhase for holistic post-coding refinement.
// After all plan steps are implemented, this phase catches cross-cutting
// concerns, integration issues, and missed requirements.
type RefinementPhase struct{}

func (p *RefinementPhase) Name() string { return "refine" }

func (p *RefinementPhase) ImplementPrompt(ctx PhaseContext) (string, error) {
	return LoadPrompt("refine", "implement", promptDataFromContext(ctx))
}

func (p *RefinementPhase) ReviewPrompt(ctx PhaseContext) (string, error) {
	return LoadPrompt("refine", "review", promptDataFromContext(ctx))
}

func (p *RefinementPhase) ImplementTools() []string {
	return []string{"Bash", "Read", "Edit", "Write", "Glob", "Grep"}
}

func (p *RefinementPhase) ReviewTools() []string {
	return []string{"Bash", "Read", "Glob", "Grep"}
}

func (p *RefinementPhase) IsConverged(result IterationResult) bool {
	return result.OpenAfterReview <= result.OpenBeforeReview
}

// BeforeReview runs autosquash to collapse fixup commits before review.
func (p *RefinementPhase) BeforeReview(ctx PhaseContext) error {
	return autosquash(ctx.WorkDir, ctx.Stream.BaseSHA)
}

func (p *RefinementPhase) TransitionMode() Transition {
	return TransitionAutoAdvance
}

func (p *RefinementPhase) ArtifactFile() string { return "" }
