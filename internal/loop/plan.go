package loop

// PlanPhase implements MacroPhase for the plan drafting/review cycle.
type PlanPhase struct{}

func (p *PlanPhase) Name() string { return "plan" }

func (p *PlanPhase) ImplementPrompt(ctx PhaseContext) (string, error) {
	return LoadPrompt("plan", "implement", promptDataFromContext(ctx))
}

func (p *PlanPhase) ReviewPrompt(ctx PhaseContext) (string, error) {
	return LoadPrompt("plan", "review", promptDataFromContext(ctx))
}

func (p *PlanPhase) ImplementTools() []string {
	return []string{"Bash", "Read", "Edit", "Write", "Glob", "Grep"}
}

func (p *PlanPhase) ReviewTools() []string {
	return []string{"Bash", "Read", "Glob", "Grep"}
}

func (p *PlanPhase) IsConverged(result IterationResult) bool {
	return result.OpenAfterReview <= result.OpenBeforeReview
}

func (p *PlanPhase) BeforeReview(_ PhaseContext) error {
	return nil
}

func (p *PlanPhase) TransitionMode() Transition {
	return TransitionAutoAdvance
}
