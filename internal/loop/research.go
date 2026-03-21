package loop

// ResearchPhase implements MacroPhase for gathering codebase context before planning.
type ResearchPhase struct{}

func (r *ResearchPhase) Name() string { return "research" }

func (r *ResearchPhase) ImplementPrompt(ctx PhaseContext) (string, error) {
	return LoadPrompt("research", "implement", promptDataFromContext(ctx))
}

func (r *ResearchPhase) ReviewPrompt(ctx PhaseContext) (string, error) {
	return LoadPrompt("research", "review", promptDataFromContext(ctx))
}

func (r *ResearchPhase) ImplementTools() []string {
	return []string{"Bash", "Read", "Edit", "Write", "Glob", "Grep", "WebSearch"}
}

func (r *ResearchPhase) ReviewTools() []string {
	return []string{"Bash", "Read", "Glob", "Grep"}
}

func (r *ResearchPhase) IsConverged(result IterationResult) bool {
	return result.OpenAfterReview <= result.OpenBeforeReview
}

func (r *ResearchPhase) BeforeReview(_ PhaseContext) error {
	return nil
}

func (r *ResearchPhase) TransitionMode() Transition {
	return TransitionAutoAdvance
}

func (r *ResearchPhase) ArtifactFile() string { return "research.md" }
