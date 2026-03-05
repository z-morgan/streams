package loop

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/zmorgan/streams/internal/runtime"
	"github.com/zmorgan/streams/internal/stream"
)

const orchestratorRules = `## Stream Orchestrator Rules (these override any conflicting CLAUDE.md instructions)
- Only commit when this prompt explicitly instructs you to.
- Do NOT push to any remote.
- Do NOT create, update, or close beads/bd issues unless this prompt explicitly instructs you to.
- Do NOT start, stop, or restart dev servers.
- Do NOT run formatters, linters, or other pre-commit tooling unless this prompt explicitly instructs you to.
- Follow the tool restrictions enforced by --allowedTools. Do not attempt to use tools outside that list.
- All other CLAUDE.md instructions (coding style, naming conventions, test patterns, project structure) remain in effect.`

// Run drives the iteration loop for a single stream. It blocks until the phase
// converges, an error occurs, or the context is cancelled. Outcome is reflected
// in stream state (StatusPaused+Converged, StatusPaused+LastError, or StatusStopped).
func Run(ctx context.Context, s *stream.Stream, phase MacroPhase, rt runtime.Runtime, beads BeadsQuerier) {
	s.SetStatus(stream.StatusRunning)

	var pendingGuidance []stream.Guidance

	for {
		// Check for cancellation at top of loop.
		if ctx.Err() != nil {
			s.SetStatus(stream.StatusStopped)
			return
		}

		iteration := s.GetIteration()

		// Fetch ordered steps for prompt injection.
		steps, err := beads.FetchOrderedSteps(s.BeadsParentID)
		if err != nil {
			recordError(s, phase, stream.ErrInfra, stream.StepImplement, "failed to fetch steps", err.Error())
			return
		}

		pctx := PhaseContext{
			Stream:       s,
			Runtime:      rt,
			WorkDir:      s.WorkTree,
			Iteration:    iteration,
			OrderedSteps: FormatSteps(steps),
		}

		// --- StepImplement ---
		s.SetIterStep(stream.StepImplement)

		openBefore, err := beads.CountOpenChildren(s.BeadsParentID)
		if err != nil {
			recordError(s, phase, stream.ErrInfra, stream.StepImplement, "failed to count open children", err.Error())
			return
		}

		implPrompt := phase.ImplementPrompt(pctx)
		if len(pendingGuidance) > 0 {
			implPrompt = appendGuidanceSection(implPrompt, pendingGuidance)
		}

		implReq := buildRequest(implPrompt, phase.ImplementTools())
		implResp, err := rt.Run(ctx, implReq)
		if err != nil {
			if ctx.Err() != nil {
				s.SetStatus(stream.StatusStopped)
				return
			}
			kind := classifyError(err)
			recordError(s, phase, kind, stream.StepImplement, "implement step failed", err.Error())
			return
		}

		openAfterImpl, err := beads.CountOpenChildren(s.BeadsParentID)
		if err != nil {
			recordError(s, phase, stream.ErrInfra, stream.StepImplement, "failed to count open children after implement", err.Error())
			return
		}

		// No-progress check: if not the first iteration and no beads were closed.
		if iteration > 0 && openAfterImpl >= openBefore {
			recordError(s, phase, stream.ErrNoProgress, stream.StepImplement, "implement step closed zero beads", "")
			return
		}

		// --- StepAutosquash ---
		s.SetIterStep(stream.StepAutosquash)

		if err := phase.BeforeReview(pctx); err != nil {
			recordError(s, phase, stream.ErrAutosquash, stream.StepAutosquash, "autosquash failed", err.Error())
			return
		}

		// --- StepReview ---
		s.SetIterStep(stream.StepReview)

		reviewReq := buildRequest(phase.ReviewPrompt(pctx), phase.ReviewTools())
		reviewResp, err := rt.Run(ctx, reviewReq)
		if err != nil {
			if ctx.Err() != nil {
				s.SetStatus(stream.StatusStopped)
				return
			}
			kind := classifyError(err)
			recordError(s, phase, kind, stream.StepReview, "review step failed", err.Error())
			return
		}

		openAfterReview, err := beads.CountOpenChildren(s.BeadsParentID)
		if err != nil {
			recordError(s, phase, stream.ErrInfra, stream.StepReview, "failed to count open children after review", err.Error())
			return
		}

		// --- Convergence check ---
		result := IterationResult{
			ReviewText:         reviewResp.Text,
			OpenChildrenBefore: openAfterImpl,
			OpenChildrenAfter:  openAfterReview,
		}
		converged := phase.IsConverged(result)

		// --- StepCheckpoint ---
		s.SetIterStep(stream.StepCheckpoint)

		snap := stream.Snapshot{
			Phase:            phase.Name(),
			Iteration:        iteration,
			Summary:          implResp.Text,
			Review:           reviewResp.Text,
			CostUSD:          implResp.CostUSD + reviewResp.CostUSD,
			GuidanceConsumed: pendingGuidance,
			Timestamp:        time.Now(),
		}
		s.AppendSnapshot(snap)

		if converged {
			s.SetConverged(true)
			s.SetStatus(stream.StatusPaused)
			slog.Info("phase converged", "stream", s.ID, "phase", phase.Name(), "iteration", iteration)
			return
		}

		// --- StepGuidance ---
		s.SetIterStep(stream.StepGuidance)

		pendingGuidance = s.DrainGuidance()

		s.SetIteration(iteration + 1)
	}
}

func buildRequest(prompt string, tools []string) runtime.Request {
	return runtime.Request{
		Prompt: prompt,
		Options: map[string]string{
			"allowedTools":      strings.Join(tools, ","),
			"appendSystemPrompt": orchestratorRules,
			"permissionMode":    "dontAsk",
		},
	}
}

func classifyError(err error) stream.ErrorKind {
	if strings.Contains(strings.ToLower(err.Error()), "budget") {
		return stream.ErrBudget
	}
	return stream.ErrRuntime
}

func recordError(s *stream.Stream, phase MacroPhase, kind stream.ErrorKind, step stream.IterStep, msg, detail string) {
	loopErr := &stream.LoopError{
		Kind:    kind,
		Step:    step,
		Message: msg,
		Detail:  detail,
	}
	s.SetLastError(loopErr)
	s.SetStatus(stream.StatusPaused)

	snap := stream.Snapshot{
		Phase:     phase.Name(),
		Iteration: s.GetIteration(),
		Error:     loopErr,
		Timestamp: time.Now(),
	}
	s.AppendSnapshot(snap)

	slog.Error("loop error", "stream", s.ID, "kind", kind, "step", step, "msg", msg)
}

func appendGuidanceSection(prompt string, guidance []stream.Guidance) string {
	var b strings.Builder
	b.WriteString(prompt)
	b.WriteString("\n\n---\n\n## Human Guidance\n\nThe user has provided the following guidance for this iteration:\n\n")
	for i, g := range guidance {
		fmt.Fprintf(&b, "%d. %s (received %s)\n", i+1, g.Text, g.Timestamp.Format(time.RFC3339))
	}
	b.WriteString("\nPrioritize addressing this guidance alongside your normal work items.")
	return b.String()
}
