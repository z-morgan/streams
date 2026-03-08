package loop

import (
	"fmt"
	"os/exec"
	"strings"
)

// PolishPhase implements MacroPhase (and SlottedPhase) for post-coding polish.
// It runs a series of slots serially instead of the normal implement→review cycle.
type PolishPhase struct {
	slots []Slot
}

// NewPolishPhase creates a PolishPhase that runs only the named slots.
// If slotNames is nil, all default slots are used.
func NewPolishPhase(slotNames []string) *PolishPhase {
	if slotNames == nil {
		return &PolishPhase{slots: DefaultSlots()}
	}
	defaults := DefaultSlots()
	byName := make(map[string]Slot, len(defaults))
	for _, s := range defaults {
		byName[s.Name] = s
	}
	var slots []Slot
	for _, name := range slotNames {
		if s, ok := byName[name]; ok {
			slots = append(slots, s)
		}
	}
	return &PolishPhase{slots: slots}
}

func (p *PolishPhase) Name() string  { return "polish" }
func (p *PolishPhase) Slots() []Slot { return p.slots }

// ImplementPrompt builds the prompt for a given slot. This is called by
// runSlots, not by the normal Run loop.
func (p *PolishPhase) ImplementPrompt(ctx PhaseContext) (string, error) {
	// Not used directly — runSlots builds prompts per-slot.
	return "", nil
}

func (p *PolishPhase) ReviewPrompt(_ PhaseContext) (string, error) { return "", nil }

func (p *PolishPhase) ImplementTools() []string {
	return []string{"Bash", "Read", "Glob", "Grep", "Edit", "Write"}
}

func (p *PolishPhase) ReviewTools() []string { return nil }

func (p *PolishPhase) IsConverged(_ IterationResult) bool { return true }

func (p *PolishPhase) BeforeReview(_ PhaseContext) error { return nil }

func (p *PolishPhase) TransitionMode() Transition { return TransitionPause }

func (p *PolishPhase) ArtifactFile() string { return "" }

// buildSlotPrompt builds PromptData and renders the template for a single slot.
func buildSlotPrompt(slot Slot, ctx PhaseContext) (string, error) {
	data := promptDataFromContext(ctx)
	data.BaseSHA = ctx.Stream.BaseSHA

	workDir := ctx.WorkDir
	baseSHA := ctx.Stream.BaseSHA

	if workDir != "" && baseSHA != "" {
		switch slot.Scope {
		case ScopeDiff:
			cmd := exec.Command("git", "log", "--oneline", baseSHA+"..HEAD")
			cmd.Dir = workDir
			if out, err := cmd.Output(); err == nil {
				data.CommitLog = strings.TrimSpace(string(out))
			}

			cmd = exec.Command("git", "diff", "--stat", baseSHA+"..HEAD")
			cmd.Dir = workDir
			if out, err := cmd.Output(); err == nil {
				data.DiffStat = strings.TrimSpace(string(out))
			}
		case ScopeCommit:
			commits, err := gatherCommitData(workDir, baseSHA)
			if err == nil {
				data.Commits = commits
			}
		}
	}

	return LoadPrompt("polish", slot.Name, data)
}

// gatherCommitData formats per-commit sections (SHA, message, diff) for
// commit-scoped polish slots.
func gatherCommitData(workDir, baseSHA string) (string, error) {
	cmd := exec.Command("git", "log", "--reverse", "--format=COMMIT %H%n%s%n%b", "-p", baseSHA+"..HEAD")
	cmd.Dir = workDir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git log failed: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}
