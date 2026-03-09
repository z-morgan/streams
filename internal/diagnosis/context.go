package diagnosis

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zmorgan/streams/internal/loop"
	"github.com/zmorgan/streams/internal/stream"
)

// BuildContext produces a structured markdown document summarizing a stream's
// full history for the diagnosis agent. It includes the task, pipeline config,
// iteration history, current state, artifacts, prompt templates, and override
// locations.
func BuildContext(s *stream.Stream, storeRoot string) string {
	var b strings.Builder

	b.WriteString("# Stream Diagnosis Context\n\n")

	writeTask(&b, s)
	writePipeline(&b, s)
	writeIterationHistory(&b, s)
	writeCurrentState(&b, s)
	writeArtifacts(&b, s)
	writePromptTemplates(&b, s, storeRoot)
	writeOverrideLocations(&b, s, storeRoot)

	return b.String()
}

func writeTask(b *strings.Builder, s *stream.Stream) {
	b.WriteString("## Task\n\n")
	b.WriteString(s.Task)
	b.WriteString("\n\n")
}

func writePipeline(b *strings.Builder, s *stream.Stream) {
	b.WriteString("## Pipeline\n\n")

	breakpoints := make(map[int]bool)
	for _, bp := range s.GetBreakpoints() {
		breakpoints[bp] = true
	}

	for i, phase := range s.GetPipeline() {
		marker := "  "
		if i == s.GetPipelineIndex() {
			marker = "→ "
		}
		suffix := ""
		if breakpoints[i] {
			suffix = " (breakpoint)"
		}
		fmt.Fprintf(b, "%s%d. %s%s\n", marker, i+1, phase, suffix)
	}
	b.WriteString("\n")
}

func writeIterationHistory(b *strings.Builder, s *stream.Stream) {
	b.WriteString("## Iteration History\n\n")

	snapshots := s.GetSnapshots()
	if len(snapshots) == 0 {
		b.WriteString("No iterations completed yet.\n\n")
		return
	}

	// Group snapshots by phase.
	var currentPhase string
	for _, snap := range snapshots {
		if snap.Phase != currentPhase {
			currentPhase = snap.Phase
			fmt.Fprintf(b, "### %s Phase\n\n", capitalize(currentPhase))
		}

		iterLabel := fmt.Sprintf("Iteration %d", snap.Iteration+1)
		if snap.SlotName != "" {
			iterLabel = fmt.Sprintf("Slot: %s", snap.SlotName)
		}
		fmt.Fprintf(b, "#### %s\n\n", iterLabel)

		if snap.Error != nil {
			fmt.Fprintf(b, "- **Error**: %s — %s\n", snap.Error.Kind, snap.Error.Message)
			if snap.Error.Detail != "" {
				fmt.Fprintf(b, "  - Detail: %s\n", truncate(snap.Error.Detail, 500))
			}
		}

		if snap.Summary != "" {
			fmt.Fprintf(b, "- **Summary**: %s\n", truncate(snap.Summary, 1000))
		}
		if snap.Review != "" {
			fmt.Fprintf(b, "- **Review**: %s\n", truncate(snap.Review, 1000))
		}

		if len(snap.BeadsClosed) > 0 {
			fmt.Fprintf(b, "- **Beads closed**: %d (%s)\n", len(snap.BeadsClosed), strings.Join(snap.BeadsClosed, ", "))
		}
		if len(snap.BeadsOpened) > 0 {
			fmt.Fprintf(b, "- **Beads opened**: %d (%s)\n", len(snap.BeadsOpened), strings.Join(snap.BeadsOpened, ", "))
		}

		if snap.DiffStat != "" {
			fmt.Fprintf(b, "- **Diff stat**: %s\n", snap.DiffStat)
		}

		if snap.CostUSD > 0 {
			fmt.Fprintf(b, "- **Cost**: $%.2f\n", snap.CostUSD)
		}

		if snap.AutosquashErr != "" {
			fmt.Fprintf(b, "- **Autosquash error**: %s\n", snap.AutosquashErr)
		}

		if len(snap.GuidanceConsumed) > 0 {
			b.WriteString("- **Guidance consumed**:\n")
			for _, g := range snap.GuidanceConsumed {
				fmt.Fprintf(b, "  - %s\n", g.Text)
			}
		}

		b.WriteString("\n")
	}
}

func writeCurrentState(b *strings.Builder, s *stream.Stream) {
	b.WriteString("## Current State\n\n")

	status := s.GetStatus()
	fmt.Fprintf(b, "- **Status**: %s\n", status)

	pipeline := s.GetPipeline()
	idx := s.GetPipelineIndex()
	if idx < len(pipeline) {
		fmt.Fprintf(b, "- **Current phase**: %s (index %d of %d)\n", pipeline[idx], idx+1, len(pipeline))
	}

	fmt.Fprintf(b, "- **Iteration**: %d\n", s.GetIteration())
	fmt.Fprintf(b, "- **Converged**: %v\n", s.Converged)

	if s.Branch != "" {
		fmt.Fprintf(b, "- **Branch**: %s\n", s.Branch)
	}
	if s.WorkTree != "" {
		fmt.Fprintf(b, "- **Worktree**: %s\n", s.WorkTree)
	}

	lastErr := s.GetLastError()
	if lastErr != nil {
		fmt.Fprintf(b, "- **Last error**: [%s at %s] %s\n", lastErr.Kind, lastErr.Step, lastErr.Message)
		if lastErr.Detail != "" {
			fmt.Fprintf(b, "  - Detail: %s\n", truncate(lastErr.Detail, 500))
		}
	}

	// Pending guidance.
	guidance := s.GetGuidance()
	if len(guidance) > 0 {
		fmt.Fprintf(b, "- **Pending guidance**: %d items\n", len(guidance))
		for _, g := range guidance {
			fmt.Fprintf(b, "  - %s\n", g.Text)
		}
	}

	// Total cost across all snapshots.
	var totalCost float64
	for _, snap := range s.GetSnapshots() {
		totalCost += snap.CostUSD
	}
	if totalCost > 0 {
		fmt.Fprintf(b, "- **Total cost**: $%.2f\n", totalCost)
	}

	b.WriteString("\n")
}

func writeArtifacts(b *strings.Builder, s *stream.Stream) {
	snapshots := s.GetSnapshots()

	// Collect the latest artifact per phase.
	artifacts := make(map[string]string)
	for _, snap := range snapshots {
		if snap.Artifact != "" {
			artifacts[snap.Phase] = snap.Artifact
		}
	}

	if len(artifacts) == 0 {
		return
	}

	b.WriteString("## Artifacts\n\n")
	for phase, content := range artifacts {
		fmt.Fprintf(b, "### %s artifact\n\n", phase)
		b.WriteString("```\n")
		b.WriteString(truncate(content, 5000))
		b.WriteString("\n```\n\n")
	}
}

func writePromptTemplates(b *strings.Builder, s *stream.Stream, storeRoot string) {
	b.WriteString("## Prompt Templates in Use\n\n")

	// Show the template names relevant to this stream's pipeline.
	pipeline := s.GetPipeline()
	shownPhases := make(map[string]bool)

	for _, phaseName := range pipeline {
		if shownPhases[phaseName] {
			continue
		}
		shownPhases[phaseName] = true

		names := promptNamesForPhase(phaseName)
		for _, name := range names {
			fmt.Fprintf(b, "### %s\n\n", name)

			// Show where this template would be loaded from.
			streamDir := filepath.Join(storeRoot, "streams", s.ID, "prompts")
			projectDir := filepath.Join(storeRoot, "prompts")
			globalDir := loop.GlobalPromptsDir()

			source := "embedded default"
			for _, check := range []struct {
				dir, label string
			}{
				{streamDir, "per-stream override"},
				{projectDir, "project override"},
				{globalDir, "global override"},
			} {
				if check.dir != "" {
					content, err := readPromptFile(check.dir, name)
					if err == nil {
						source = check.label + " (" + filepath.Join(check.dir, name+".tmpl") + ")"
						fmt.Fprintf(b, "Source: %s\n\n```\n%s\n```\n\n", source, truncate(content, 3000))
						break
					}
				}
			}

			if source == "embedded default" {
				content, err := loop.ExportPrompt(name)
				if err == nil {
					fmt.Fprintf(b, "Source: %s\n\n```\n%s\n```\n\n", source, truncate(content, 3000))
				} else {
					fmt.Fprintf(b, "Source: %s (template not found)\n\n", source)
				}
			}
		}
	}
}

func writeOverrideLocations(b *strings.Builder, s *stream.Stream, storeRoot string) {
	b.WriteString("## Override Locations\n\n")
	b.WriteString("Prompts are checked in this order (first match wins):\n\n")

	streamDir := filepath.Join(storeRoot, "streams", s.ID, "prompts")
	projectDir := filepath.Join(storeRoot, "prompts")
	globalDir := loop.GlobalPromptsDir()

	fmt.Fprintf(b, "1. **Per-stream**: `%s`\n", streamDir)
	fmt.Fprintf(b, "2. **Project**: `%s`\n", projectDir)
	fmt.Fprintf(b, "3. **Global**: `%s`\n", globalDir)
	b.WriteString("4. **Embedded defaults** (built into the binary)\n\n")
}

// promptNamesForPhase returns the template names relevant to a pipeline phase.
func promptNamesForPhase(phase string) []string {
	switch phase {
	case "research":
		return []string{"research-implement", "research-review"}
	case "plan":
		return []string{"plan-implement", "plan-review"}
	case "decompose":
		return []string{"decompose-implement", "decompose-review"}
	case "coding":
		return []string{"coding-implement", "coding-review"}
	case "review":
		return []string{"review-implement"}
	case "polish":
		return []string{"polish-lint", "polish-rubocop", "polish-commits"}
	default:
		return nil
	}
}

func readPromptFile(dir, name string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dir, name+".tmpl"))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "\n... (truncated)"
}
