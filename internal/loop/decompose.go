package loop

// DecomposePhase implements MacroPhase for breaking a plan into sequenced step beads.
type DecomposePhase struct{}

func (d *DecomposePhase) Name() string { return "decompose" }

func (d *DecomposePhase) ImplementPrompt(ctx PhaseContext) (string, error) {
	return LoadPrompt("decompose", "implement", promptDataFromContext(ctx))
}

func (d *DecomposePhase) ReviewPrompt(ctx PhaseContext) (string, error) {
	return LoadPrompt("decompose", "review", promptDataFromContext(ctx))
}

func (d *DecomposePhase) ImplementTools() []string {
	return []string{"Bash", "Read", "Glob", "Grep"}
}

func (d *DecomposePhase) ReviewTools() []string {
	return []string{"Bash", "Read", "Glob", "Grep"}
}

func (d *DecomposePhase) IsConverged(result IterationResult) bool {
	return result.OpenChildrenAfter <= result.OpenChildrenBefore
}

func (d *DecomposePhase) BeforeReview(_ PhaseContext) error {
	return nil
}

func (d *DecomposePhase) TransitionMode() Transition {
	return TransitionAutoAdvance
}
