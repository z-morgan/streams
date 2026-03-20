package loop

import (
	"fmt"
	"strings"

	"github.com/zmorgan/streams/internal/convergence"
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
	Stream             *stream.Stream
	Runtime            runtime.Runtime
	WorkDir            string
	Iteration          int
	OrderedSteps       string           // formatted step list injected into implement prompt
	PromptOverrideDirs []string         // per-stream and project prompt override directories
	MCPConfigPath      string           // absolute path to mcp.json (empty = no MCP)
	MCPToolPatterns    []string         // e.g. ["mcp__chrome-devtools__*"]
	ConvergenceConfig  *convergence.ResolvedConfig // nil = use legacy convergence
	PlanContent        string           // contents of plan.md (empty if no plan phase)
	StepBeads          []StepWithStatus // all step-labeled children with status
	OpenReviewBeads    []string         // IDs of open non-step children
}

// IterationResult captures the outcome of a single iteration for convergence detection.
// The Go loop populates this — agents don't produce it directly.
type IterationResult struct {
	ReviewText       string
	OpenBeforeReview int
	OpenAfterReview  int
	BeadsClosed      []string
	BeadsOpened      []string
}

// MacroPhase defines the behavior for one phase of the stream pipeline.
type MacroPhase interface {
	Name() string
	ImplementPrompt(ctx PhaseContext) (string, error)
	ReviewPrompt(ctx PhaseContext) (string, error)
	ImplementTools() []string
	ReviewTools() []string
	IsConverged(result IterationResult) bool
	BeforeReview(ctx PhaseContext) error
	TransitionMode() Transition
	ArtifactFile() string // relative path to the phase's artifact file (empty if none)
}

func promptDataFromContext(ctx PhaseContext) PromptData {
	// Determine step-coding mode and current step.
	isFixMode := len(ctx.OpenReviewBeads) > 0
	var currentStep string
	var currentStepID string
	currentIdx := -1
	if !isFixMode {
		for i, s := range ctx.StepBeads {
			if s.Status != "closed" {
				currentIdx = i
				currentStep = s.Title
				currentStepID = s.ID
				break
			}
		}
	}

	return PromptData{
		Task:              ctx.Stream.Task,
		ParentID:          ctx.Stream.BeadsParentID,
		Iteration:         ctx.Iteration,
		OrderedSteps:      ctx.OrderedSteps,
		PlanContent:       ctx.PlanContent,
		AllStepsFormatted: FormatStepProgress(ctx.StepBeads, currentIdx),
		CurrentStep:       currentStep,
		CurrentStepID:     currentStepID,
		IsFixMode:         isFixMode,
		ReviewBeadsList:   formatReviewBeads(ctx.OpenReviewBeads),
		OverrideDirs:      ctx.PromptOverrideDirs,
	}
}

func formatReviewBeads(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	var b strings.Builder
	for _, id := range ids {
		fmt.Fprintf(&b, "- %s\n", id)
	}
	return b.String()
}

// NewPhase returns a MacroPhase for the given pipeline phase name.
func NewPhase(name string) (MacroPhase, error) {
	switch name {
	case "research":
		return &ResearchPhase{}, nil
	case "plan":
		return &PlanPhase{}, nil
	case "decompose":
		return &DecomposePhase{}, nil
	case "coding":
		return &CodingPhase{}, nil
	case "step-coding":
		return &StepCodingPhase{}, nil
	case "review":
		return &ReviewPhase{}, nil
	case "polish":
		return NewPolishPhase(nil), nil
	default:
		return nil, fmt.Errorf("unknown pipeline phase: %q", name)
	}
}
