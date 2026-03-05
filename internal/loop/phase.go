package loop

import (
	"fmt"

	"github.com/zmorgan/streams/internal/runtime"
	"github.com/zmorgan/streams/internal/stream"
)

// Transition controls what happens after a macro-phase converges.
type Transition int

const (
	TransitionPause       Transition = iota // pause for human review before next phase
	TransitionAutoAdvance                   // automatically advance to next pipeline phase
)

var transitionNames = [...]string{
	"Pause",
	"AutoAdvance",
}

func (t Transition) String() string {
	if int(t) < len(transitionNames) {
		return transitionNames[t]
	}
	return fmt.Sprintf("Transition(%d)", int(t))
}

// PhaseContext provides the loop context to a macro-phase for a single iteration.
type PhaseContext struct {
	Stream       *stream.Stream
	Runtime      runtime.Runtime
	WorkDir      string
	Iteration    int
	OrderedSteps string // formatted step list injected into implement prompt
}

// IterationResult captures the outcome of a single iteration for convergence detection.
// The Go loop populates this — agents don't produce it directly.
type IterationResult struct {
	ReviewText         string
	OpenChildrenBefore int
	OpenChildrenAfter  int
	BeadsClosed        []string
	BeadsOpened        []string
}

// MacroPhase defines the behavior for one phase of the stream pipeline.
type MacroPhase interface {
	Name() string
	ImplementPrompt(ctx PhaseContext) string
	ReviewPrompt(ctx PhaseContext) string
	ImplementTools() []string
	ReviewTools() []string
	IsConverged(result IterationResult) bool
	BeforeReview(ctx PhaseContext) error
	TransitionMode() Transition
}

// NewPhase returns a MacroPhase for the given pipeline phase name.
func NewPhase(name string) (MacroPhase, error) {
	switch name {
	case "coding":
		return &CodingPhase{}, nil
	default:
		return nil, fmt.Errorf("unknown pipeline phase: %q", name)
	}
}
