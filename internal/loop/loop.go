package loop

import (
	"context"
	"encoding/json"
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
// promptOverrideDirs are checked in order before the global user prompts dir
// (typically [per-stream dir, project dir]).
func Run(ctx context.Context, s *stream.Stream, phase MacroPhase, rt runtime.Runtime, beads BeadsQuerier, git GitQuerier, maxIterations int, factory PhaseFactory, onCheckpoint func(*stream.Stream), promptOverrideDirs ...string) {
	s.SetStatus(stream.StatusRunning)
	s.ClearOutput()

	// Derive MCP tool patterns from the stream's MCP config file.
	mcpConfigPath, mcpToolPatterns := loadMCPToolPatterns(s.GetMCPConfigPath())

	// Slotted phases (e.g. polish) bypass the normal implement→review cycle.
	if slotted, ok := phase.(SlottedPhase); ok {
		runSlots(ctx, s, slotted, rt, git, onCheckpoint, promptOverrideDirs, mcpConfigPath, mcpToolPatterns)
		return
	}

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

			// If the next phase is slotted, run its slots and return.
			if slotted, ok := nextPhase.(SlottedPhase); ok {
				runSlots(ctx, s, slotted, rt, git, onCheckpoint, promptOverrideDirs, mcpConfigPath, mcpToolPatterns)
				return
			}
		} else {
			// Already at last phase and converged — nothing to do.
			s.SetStatus(stream.StatusPaused)
			return
		}
	}

	var pendingGuidance []stream.Guidance
	startIteration := s.GetIteration()

	for {
		// Check for graceful pause request before starting a new iteration.
		if s.DrainPauseRequested() {
			s.SetStatus(stream.StatusPaused)
			return
		}

		// Check for hard cancellation at top of loop.
		if ctx.Err() != nil {
			s.SetStatus(stream.StatusStopped)
			return
		}

		// Check max iterations relative to the start of this session so that
		// resuming a paused stream gets a fresh budget rather than immediately
		// re-pausing at the same limit.
		if maxIterations > 0 && s.GetIteration()-startIteration >= maxIterations {
			detail := buildMaxIterDiagnostic(s, phase, beads)
			recordError(s, phase, stream.ErrMaxIterations, stream.StepImplement,
				fmt.Sprintf("iteration limit (%d) reached", maxIterations), detail)
			return
		}

		iteration := s.GetIteration()
		usedFallback := false

		// Fetch ordered steps for prompt injection.
		steps, err := beads.FetchOrderedSteps(s.BeadsParentID)
		if err != nil {
			recordError(s, phase, stream.ErrInfra, stream.StepImplement, "failed to fetch steps", err.Error())
			return
		}

		pctx := PhaseContext{
			Stream:             s,
			Runtime:            rt,
			WorkDir:            s.WorkTree,
			Iteration:          iteration,
			OrderedSteps:       FormatSteps(steps),
			PromptOverrideDirs: promptOverrideDirs,
			MCPConfigPath:      mcpConfigPath,
			MCPToolPatterns:    mcpToolPatterns,
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

		phaseModel := s.GetModels().ModelForPhase(phase.Name())
		implReq := buildRequest(implPrompt, phase.ImplementTools(), s.GetEnvironmentPort(), phaseModel, mcpConfigPath, mcpToolPatterns)
		implReq.OnOutput = func(line string) { s.AppendOutput(line) }
		implResp, err := rt.Run(ctx, implReq)
		if err != nil {
			if ctx.Err() != nil {
				s.SetStatus(stream.StatusStopped)
				return
			}
			kind := classifyError(err)
			if kind == stream.ErrRateLimit {
				fb := s.GetFallback()
				if fb.Enabled && fb.Model != "" {
					s.AppendOutput(fmt.Sprintf("[streams] Rate limit hit — falling back to %s", fb.Model))
					slog.Info("rate limit fallback", "stream", s.ID, "fallbackModel", fb.Model)
					fbReq := buildRequest(implPrompt, phase.ImplementTools(), s.GetEnvironmentPort(), fb.Model, mcpConfigPath, mcpToolPatterns)
					fbReq.OnOutput = func(line string) { s.AppendOutput(line) }
					implResp, err = rt.Run(ctx, fbReq)
					if err != nil {
						if ctx.Err() != nil {
							s.SetStatus(stream.StatusStopped)
							return
						}
						recordError(s, phase, classifyError(err), stream.StepImplement, "fallback model also failed", err.Error())
						return
					}
					usedFallback = true
					goto implSucceeded
				}
			}
			recordError(s, phase, kind, stream.StepImplement, "implement step failed", err.Error())
			return
		}
	implSucceeded:

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

		// Check for graceful pause between implement and review.
		if s.DrainPauseRequested() {
			diffStat, _ := git.DiffStat(s.WorkTree, headBefore)
			commitSHAs, _ := git.CommitsBetween(s.WorkTree, headBefore, headAfterImpl)
			snap := stream.Snapshot{
				Phase:       phase.Name(),
				Iteration:   iteration,
				Summary:     implResp.Text,
				CostUSD:     implResp.CostUSD,
				DiffStat:    diffStat,
				CommitSHAs:  commitSHAs,
				BeadsClosed: beadsClosed,
				Timestamp:   time.Now(),
			}
			s.AppendSnapshot(snap)
			if onCheckpoint != nil {
				onCheckpoint(s)
			}
			s.SetIteration(iteration + 1)
			s.SetStatus(stream.StatusPaused)
			return
		}

		// --- StepReview ---
		s.SetIterStep(stream.StepReview)

		var reviewResp *runtime.Response
		var idsAfterReview []string
		var beadsOpened []string

		if s.DrainConvergeASAP() {
			// ConvergeASAP: skip review so open count stays the same → IsConverged() returns true.
			slog.Info("converge ASAP: skipping review", "stream", s.ID, "phase", phase.Name(), "iteration", iteration)
			s.AppendOutput("[streams] Wrapping up — skipping review to converge phase")
			reviewResp = &runtime.Response{}
			idsAfterReview = idsAfterImpl
		} else {
			reviewPrompt, err := phase.ReviewPrompt(pctx)
			if err != nil {
				recordError(s, phase, stream.ErrInfra, stream.StepReview, "failed to load review prompt", err.Error())
				return
			}

			if reviewPrompt == "" {
				// Phase has no review step (e.g. ReviewPhase). Skip the runtime call.
				reviewResp = &runtime.Response{}
			} else {
				reviewReq := buildRequest(reviewPrompt, phase.ReviewTools(), s.GetEnvironmentPort(), phaseModel, mcpConfigPath, mcpToolPatterns)
				reviewReq.OnOutput = func(line string) { s.AppendOutput(line) }
				reviewResp, err = rt.Run(ctx, reviewReq)
				if err != nil {
					if ctx.Err() != nil {
						s.SetStatus(stream.StatusStopped)
						return
					}
					kind := classifyError(err)
					if kind == stream.ErrRateLimit {
						fb := s.GetFallback()
						if fb.Enabled && fb.Model != "" {
							s.AppendOutput(fmt.Sprintf("[streams] Rate limit hit — falling back to %s", fb.Model))
							slog.Info("rate limit fallback (review)", "stream", s.ID, "fallbackModel", fb.Model)
							fbReq := buildRequest(reviewPrompt, phase.ReviewTools(), s.GetEnvironmentPort(), fb.Model, mcpConfigPath, mcpToolPatterns)
							fbReq.OnOutput = func(line string) { s.AppendOutput(line) }
							reviewResp, err = rt.Run(ctx, fbReq)
							if err != nil {
								if ctx.Err() != nil {
									s.SetStatus(stream.StatusStopped)
									return
								}
								recordError(s, phase, classifyError(err), stream.StepReview, "fallback model also failed", err.Error())
								return
							}
							usedFallback = true
							goto reviewSucceeded
						}
					}
					recordError(s, phase, kind, stream.StepReview, "review step failed", err.Error())
					return
				}
			reviewSucceeded:
			}

			idsAfterReview, err = beads.ListOpenChildren(s.BeadsParentID)
			if err != nil {
				recordError(s, phase, stream.ErrInfra, stream.StepReview, "failed to list open children after review", err.Error())
				return
			}

			beadsOpened = setDiff(idsAfterReview, idsAfterImpl)
		}

		// --- Convergence check ---
		result := IterationResult{
			ReviewText:       reviewResp.Text,
			OpenBeforeReview: len(idsAfterImpl),
			OpenAfterReview:  len(idsAfterReview),
			BeadsClosed:      beadsClosed,
			BeadsOpened:      beadsOpened,
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

		// Build title map for beads referenced in this snapshot.
		var beadTitles map[string]string
		if len(beadsClosed) > 0 || len(beadsOpened) > 0 {
			beadTitles, _ = beads.FetchAllChildTitles(s.BeadsParentID)
		}

		reviseFrom, reviseFeedback := s.DrainReviseContext()

		var fallbackModel string
		if usedFallback {
			fallbackModel = s.GetFallback().Model
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
			BeadTitles:       beadTitles,
			ReviseFrom:       reviseFrom,
			ReviseFeedback:   reviseFeedback,
			AutosquashErr:    autosquashErr,
			GuidanceConsumed: pendingGuidance,
			UsedFallback:     usedFallback,
			FallbackModel:    fallbackModel,
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

				// If the next phase is slotted (e.g. polish), run its slots
				// and return instead of continuing the normal iteration loop.
				if slotted, ok := nextPhase.(SlottedPhase); ok {
					runSlots(ctx, s, slotted, rt, git, onCheckpoint, promptOverrideDirs, mcpConfigPath, mcpToolPatterns)
					return
				}
				continue
			}

			s.SetStatus(stream.StatusPaused)
			return
		}

		// --- StepGuidance ---
		s.SetIterStep(stream.StepGuidance)

		pendingGuidance = s.DrainGuidance()

		// Check for a queued revise request between iterations.
		if pr := s.DrainPendingRevise(); pr != nil {
			fromPhase := phase.Name()
			s.SetReviseContext(fromPhase, pr.Feedback)
			s.SetPipelineIndex(pr.TargetPhaseIndex)
			s.SetConverged(false)
			s.SetIteration(0)
			if pr.Feedback != "" {
				s.AddGuidance(stream.Guidance{
					Text:      pr.Feedback,
					Timestamp: time.Now(),
				})
			}
			newPhase, err := factory(s.GetPipeline()[pr.TargetPhaseIndex])
			if err != nil {
				recordError(s, phase, stream.ErrInfra, stream.StepGuidance, "failed to instantiate revise target phase", err.Error())
				return
			}
			phase = newPhase
			pendingGuidance = s.DrainGuidance()
			startIteration = 0
			slog.Info("applying queued revise", "stream", s.ID, "fromPhase", fromPhase, "targetPhase", newPhase.Name())
			continue
		}

		s.SetIteration(iteration + 1)
	}
}

func buildRequest(prompt string, tools []string, envPort int, model string, mcpConfigPath string, mcpToolPatterns []string) runtime.Request {
	systemPrompt := orchestratorRules

	hasBrowserMCP := false
	for _, p := range mcpToolPatterns {
		if strings.Contains(p, "chrome-devtools") || strings.Contains(p, "playwright") {
			hasBrowserMCP = true
			break
		}
	}

	if envPort > 0 && hasBrowserMCP {
		systemPrompt += fmt.Sprintf(`

## Application Server

A live application server is running at http://localhost:%d.
Use the chrome-devtools MCP tool to open pages, inspect elements, and verify your UI changes in the browser.
After making code changes, the server will automatically reload — just refresh the page.`, envPort)
	} else if envPort > 0 {
		systemPrompt += fmt.Sprintf(`

## Application Server

A live application server is running at http://localhost:%d.
After making code changes, the server will automatically reload.`, envPort)
	} else if hasBrowserMCP {
		systemPrompt += `

## Browser Tools

Use the chrome-devtools MCP tool to open pages, inspect elements, and verify your UI changes in the browser.`
	}

	allTools := append(tools, mcpToolPatterns...)
	opts := map[string]string{
		"allowedTools":       strings.Join(allTools, ","),
		"appendSystemPrompt": systemPrompt,
		"permissionMode":     "dontAsk",
	}
	if model != "" {
		opts["model"] = model
	}
	if mcpConfigPath != "" {
		opts["mcpConfig"] = mcpConfigPath
	}
	return runtime.Request{
		Prompt:  prompt,
		Options: opts,
	}
}

func classifyError(err error) stream.ErrorKind {
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "budget") {
		return stream.ErrBudget
	}
	for _, s := range []string{"rate limit", "rate_limit", "429", "overloaded", "too many requests", "usage limit", "hit your limit"} {
		if strings.Contains(msg, s) {
			return stream.ErrRateLimit
		}
	}
	return stream.ErrRuntime
}

// buildMaxIterDiagnostic generates a detail string for MaxIterations errors
// by analysing the stream's snapshots for the current phase.
func buildMaxIterDiagnostic(s *stream.Stream, phase MacroPhase, beads BeadsQuerier) string {
	phaseName := phase.Name()
	snaps := s.GetSnapshots()

	var phaseSnaps []stream.Snapshot
	for _, snap := range snaps {
		if snap.Phase == phaseName && snap.Error == nil {
			phaseSnaps = append(phaseSnaps, snap)
		}
	}
	if len(phaseSnaps) == 0 {
		return ""
	}

	reviewFiledCount := 0
	totalOpened := 0
	totalClosed := 0
	for _, snap := range phaseSnaps {
		if len(snap.BeadsOpened) > 0 {
			reviewFiledCount++
		}
		totalOpened += len(snap.BeadsOpened)
		totalClosed += len(snap.BeadsClosed)
	}

	var b strings.Builder
	total := len(phaseSnaps)
	if reviewFiledCount == total {
		fmt.Fprintf(&b, "Review filed new issues on %d of %d iterations (never converged).", reviewFiledCount, total)
	} else {
		fmt.Fprintf(&b, "Review filed new issues on %d of %d iterations.", reviewFiledCount, total)
	}
	fmt.Fprintf(&b, " %d issues opened, %d closed across the phase.", totalOpened, totalClosed)

	openIDs, err := beads.ListOpenChildren(s.BeadsParentID)
	if err == nil && len(openIDs) > 0 {
		titles, _ := beads.FetchAllChildTitles(s.BeadsParentID)
		labels := make([]string, len(openIDs))
		for i, id := range openIDs {
			if title, ok := titles[id]; ok && title != "" {
				labels[i] = fmt.Sprintf("%s (%q)", id, title)
			} else {
				labels[i] = id
			}
		}
		fmt.Fprintf(&b, " %d issues still open: %s.", len(openIDs), strings.Join(labels, ", "))
	}

	return b.String()
}

func recordError(s *stream.Stream, phase MacroPhase, kind stream.ErrorKind, step stream.IterStep, msg, detail string) {
	loopErr := &stream.LoopError{
		Kind:    kind,
		Phase:   phase.Name(),
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

// runSlots drives a SlottedPhase by iterating over its slots serially.
// Each slot gets its own prompt, agent invocation, and snapshot.
func runSlots(ctx context.Context, s *stream.Stream, phase SlottedPhase, rt runtime.Runtime, git GitQuerier, onCheckpoint func(*stream.Stream), promptOverrideDirs []string, mcpConfigPath string, mcpToolPatterns []string) {
	slots := phase.Slots()
	var firstErr *stream.LoopError

	for i, slot := range slots {
		// Check for graceful pause between slots.
		if s.DrainPauseRequested() {
			s.SetStatus(stream.StatusPaused)
			return
		}

		if ctx.Err() != nil {
			s.SetStatus(stream.StatusStopped)
			return
		}

		s.SetIteration(i)
		s.SetIterStep(stream.StepImplement)

		pctx := PhaseContext{
			Stream:             s,
			Runtime:            rt,
			WorkDir:            s.WorkTree,
			Iteration:          i,
			PromptOverrideDirs: promptOverrideDirs,
			MCPConfigPath:      mcpConfigPath,
			MCPToolPatterns:    mcpToolPatterns,
		}

		headBefore, _ := git.HeadSHA(s.WorkTree)

		prompt, err := buildSlotPrompt(slot, pctx)
		if err != nil {
			slotErr := &stream.LoopError{
				Kind:    stream.ErrInfra,
				Step:    stream.StepImplement,
				Message: fmt.Sprintf("slot %q: failed to build prompt", slot.Name),
				Detail:  err.Error(),
			}
			if firstErr == nil {
				firstErr = slotErr
			}
			snap := stream.Snapshot{
				Phase:     phase.Name(),
				Iteration: i,
				SlotName:  slot.Name,
				Error:     slotErr,
				Timestamp: time.Now(),
			}
			s.AppendSnapshot(snap)
			if onCheckpoint != nil {
				onCheckpoint(s)
			}
			continue
		}

		slotModel := s.GetModels().ModelForPhase(phase.Name())
		req := buildRequest(prompt, slot.Tools, s.GetEnvironmentPort(), slotModel, mcpConfigPath, mcpToolPatterns)
		req.OnOutput = func(line string) { s.AppendOutput(line) }
		resp, err := rt.Run(ctx, req)
		if err != nil {
			if ctx.Err() != nil {
				s.SetStatus(stream.StatusStopped)
				return
			}
			kind := classifyError(err)
			slotErr := &stream.LoopError{
				Kind:    kind,
				Step:    stream.StepImplement,
				Message: fmt.Sprintf("slot %q failed", slot.Name),
				Detail:  err.Error(),
			}
			if firstErr == nil {
				firstErr = slotErr
			}
			snap := stream.Snapshot{
				Phase:     phase.Name(),
				Iteration: i,
				SlotName:  slot.Name,
				Error:     slotErr,
				Timestamp: time.Now(),
			}
			s.AppendSnapshot(snap)
			if onCheckpoint != nil {
				onCheckpoint(s)
			}
			continue
		}

		if resp.SessionID != "" {
			s.SetSessionID(resp.SessionID)
		}

		// Run autosquash after commit-scoped slots to collapse fixup commits
		// before the next slot sees history.
		var autosquashErr string
		if slot.Scope == ScopeCommit && s.WorkTree != "" && s.BaseSHA != "" {
			if err := autosquash(s.WorkTree, s.BaseSHA); err != nil {
				autosquashErr = err.Error()
				slog.Warn("autosquash failed after polish slot", "stream", s.ID, "slot", slot.Name, "err", err)
			}
		}

		headAfter, _ := git.HeadSHA(s.WorkTree)
		diffStat, _ := git.DiffStat(s.WorkTree, headBefore)
		commitSHAs, _ := git.CommitsBetween(s.WorkTree, headBefore, headAfter)

		snap := stream.Snapshot{
			Phase:         phase.Name(),
			Iteration:     i,
			SlotName:      slot.Name,
			Summary:       resp.Text,
			CostUSD:       resp.CostUSD,
			DiffStat:      diffStat,
			CommitSHAs:    commitSHAs,
			AutosquashErr: autosquashErr,
			Timestamp:     time.Now(),
		}
		s.AppendSnapshot(snap)
		if onCheckpoint != nil {
			onCheckpoint(s)
		}
	}

	if firstErr != nil {
		s.SetLastError(firstErr)
	}
	s.SetConverged(true)
	s.SetStatus(stream.StatusPaused)
	slog.Info("polish phase converged", "stream", s.ID, "slots", len(slots))
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

// loadMCPToolPatterns reads an MCP config file and returns the path and
// derived tool patterns. Returns empty values if the path is empty or
// the file cannot be read.
func loadMCPToolPatterns(path string) (string, []string) {
	if path == "" {
		return "", nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		slog.Warn("failed to read MCP config, continuing without MCP", "path", path, "err", err)
		return "", nil
	}

	var f struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &f); err != nil {
		slog.Warn("failed to parse MCP config, continuing without MCP", "path", path, "err", err)
		return "", nil
	}

	if len(f.MCPServers) == 0 {
		return "", nil
	}

	patterns := make([]string, 0, len(f.MCPServers))
	for name := range f.MCPServers {
		patterns = append(patterns, "mcp__"+name+"__*")
	}
	return path, patterns
}
