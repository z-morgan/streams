package loop

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
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

// PhaseFactory creates a MacroPhase by pipeline phase name.
type PhaseFactory func(name string) (MacroPhase, error)

// Run drives the iteration loop for a single stream. It blocks until the phase
// converges, an error occurs, the context is cancelled, or maxIterations is
// reached (0 means unlimited). Outcome is reflected in stream state
// (StatusPaused+Converged, StatusPaused+LastError, or StatusStopped).
func Run(ctx context.Context, s *stream.Stream, phase MacroPhase, rt runtime.Runtime, beads BeadsQuerier, git GitQuerier, maxIterations int, factory PhaseFactory, onCheckpoint func(*stream.Stream)) {
	s.SetStatus(stream.StatusRunning)
	s.ClearOutput()

	// If converged (e.g. resuming from a breakpoint) with no pending guidance,
	// advance to the next pipeline phase before entering the iteration loop.
	if s.Converged {
		pipeline := s.GetPipeline()
		nextIdx := s.GetPipelineIndex() + 1
		if nextIdx < len(pipeline) {
			nextPhase, err := factory(pipeline[nextIdx])
			if err != nil {
				recordError(s, phase, stream.ErrInfra, stream.StepCheckpoint, "failed to instantiate next phase", err.Error())
				return
			}
			s.SetPipelineIndex(nextIdx)
			s.SetConverged(false)
			s.SetIteration(0)
			phase = nextPhase
			slog.Info("advancing pipeline on resume", "stream", s.ID, "phase", nextPhase.Name(), "pipelineIndex", nextIdx)
		} else {
			// Already at last phase and converged — nothing to do.
			s.SetStatus(stream.StatusPaused)
			return
		}
	}

	var pendingGuidance []stream.Guidance

	for {
		// Check for cancellation at top of loop.
		if ctx.Err() != nil {
			s.SetStatus(stream.StatusStopped)
			return
		}

		// Check max iterations.
		if maxIterations > 0 && s.GetIteration() >= maxIterations {
			s.SetStatus(stream.StatusPaused)
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

		idsBefore, err := beads.ListOpenChildren(s.BeadsParentID)
		if err != nil {
			recordError(s, phase, stream.ErrInfra, stream.StepImplement, "failed to list open children", err.Error())
			return
		}

		headBefore, err := git.HeadSHA(s.WorkTree)
		if err != nil {
			recordError(s, phase, stream.ErrInfra, stream.StepImplement, "failed to get HEAD SHA", err.Error())
			return
		}

		implPrompt, err := phase.ImplementPrompt(pctx)
		if err != nil {
			recordError(s, phase, stream.ErrInfra, stream.StepImplement, "failed to load implement prompt", err.Error())
			return
		}
		if len(pendingGuidance) > 0 {
			implPrompt = appendGuidanceSection(implPrompt, pendingGuidance)
		}

		implReq := buildRequest(implPrompt, phase.ImplementTools())
		implReq.OnOutput = func(line string) { s.AppendOutput(line) }
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

		if implResp.SessionID != "" {
			s.SetSessionID(implResp.SessionID)
		}

		idsAfterImpl, err := beads.ListOpenChildren(s.BeadsParentID)
		if err != nil {
			recordError(s, phase, stream.ErrInfra, stream.StepImplement, "failed to list open children after implement", err.Error())
			return
		}

		headAfterImpl, err := git.HeadSHA(s.WorkTree)
		if err != nil {
			recordError(s, phase, stream.ErrInfra, stream.StepImplement, "failed to get HEAD SHA after implement", err.Error())
			return
		}

		beadsClosed := setDiff(idsBefore, idsAfterImpl)

		// No-progress check: if not the first iteration, there were open beads
		// to work on, but none were closed. When idsBefore is empty there's
		// nothing to make progress on — let the review step handle convergence.
		if iteration > 0 && len(idsBefore) > 0 && len(beadsClosed) == 0 {
			recordError(s, phase, stream.ErrNoProgress, stream.StepImplement, "implement step closed zero beads", "")
			return
		}

		// --- StepAutosquash ---
		s.SetIterStep(stream.StepAutosquash)

		var autosquashErr string
		if err := phase.BeforeReview(pctx); err != nil {
			// Autosquash failure is non-terminal — the code is fine, only the
			// commit history has unsquashed fixups. Log and continue to review.
			autosquashErr = err.Error()
			slog.Warn("autosquash failed, continuing to review", "stream", s.ID, "err", err)
		}

		// --- StepReview ---
		s.SetIterStep(stream.StepReview)

		reviewPrompt, err := phase.ReviewPrompt(pctx)
		if err != nil {
			recordError(s, phase, stream.ErrInfra, stream.StepReview, "failed to load review prompt", err.Error())
			return
		}
		reviewReq := buildRequest(reviewPrompt, phase.ReviewTools())
		reviewReq.OnOutput = func(line string) { s.AppendOutput(line) }
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

		idsAfterReview, err := beads.ListOpenChildren(s.BeadsParentID)
		if err != nil {
			recordError(s, phase, stream.ErrInfra, stream.StepReview, "failed to list open children after review", err.Error())
			return
		}

		beadsOpened := setDiff(idsAfterReview, idsAfterImpl)

		// --- Convergence check ---
		result := IterationResult{
			ReviewText:         reviewResp.Text,
			OpenBeforeReview: len(idsAfterImpl),
			OpenAfterReview:  len(idsAfterReview),
			BeadsClosed:        beadsClosed,
			BeadsOpened:        beadsOpened,
		}
		converged := phase.IsConverged(result)

		// --- StepCheckpoint ---
		s.SetIterStep(stream.StepCheckpoint)

		diffStat, _ := git.DiffStat(s.WorkTree, headBefore)
		commitSHAs, _ := git.CommitsBetween(s.WorkTree, headBefore, headAfterImpl)

		var artifact string
		if af := phase.ArtifactFile(); af != "" {
			data, err := os.ReadFile(filepath.Join(s.WorkTree, af))
			if err == nil {
				artifact = string(data)
			}
		}

		snap := stream.Snapshot{
			Phase:            phase.Name(),
			Iteration:        iteration,
			Summary:          implResp.Text,
			Review:           reviewResp.Text,
			Artifact:         artifact,
			CostUSD:          implResp.CostUSD + reviewResp.CostUSD,
			DiffStat:         diffStat,
			CommitSHAs:       commitSHAs,
			BeadsClosed:      beadsClosed,
			BeadsOpened:      beadsOpened,
			AutosquashErr:    autosquashErr,
			GuidanceConsumed: pendingGuidance,
			Timestamp:        time.Now(),
		}
		s.AppendSnapshot(snap)
		if onCheckpoint != nil {
			onCheckpoint(s)
		}

		if converged {
			s.SetConverged(true)
			slog.Info("phase converged", "stream", s.ID, "phase", phase.Name(), "iteration", iteration)

			pipeline := s.GetPipeline()
			nextIdx := s.GetPipelineIndex() + 1

			hasBreakpoint := false
			for _, bp := range s.GetBreakpoints() {
				if bp == s.GetPipelineIndex() {
					hasBreakpoint = true
					break
				}
			}

			if !hasBreakpoint && phase.TransitionMode() == TransitionAutoAdvance && nextIdx < len(pipeline) {
				nextPhase, err := factory(pipeline[nextIdx])
				if err != nil {
					recordError(s, phase, stream.ErrInfra, stream.StepCheckpoint, "failed to instantiate next phase", err.Error())
					return
				}
				s.SetPipelineIndex(nextIdx)
				s.SetConverged(false)
				s.SetIteration(0)
				phase = nextPhase
				pendingGuidance = nil
				slog.Info("auto-advancing pipeline", "stream", s.ID, "phase", nextPhase.Name(), "pipelineIndex", nextIdx)
				continue
			}

			s.SetStatus(stream.StatusPaused)
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
			"allowedTools":       strings.Join(tools, ","),
			"appendSystemPrompt": orchestratorRules,
			"permissionMode":     "dontAsk",
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

// setDiff returns elements in a that are not in b.
func setDiff(a, b []string) []string {
	set := make(map[string]struct{}, len(b))
	for _, id := range b {
		set[id] = struct{}{}
	}
	var diff []string
	for _, id := range a {
		if _, ok := set[id]; !ok {
			diff = append(diff, id)
		}
	}
	return diff
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
