package loop

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

// Step represents a single work item fetched from beads.
type Step struct {
	ID       string
	Title    string
	Sequence int
}

// StepWithStatus extends Step with status information for progress tracking.
type StepWithStatus struct {
	Step
	Status string // "open", "closed", "in_progress"
}

// BeadsQuerier abstracts the beads CLI for testability.
type BeadsQuerier interface {
	ListOpenChildren(parentID string) ([]string, error)
	FetchOrderedSteps(parentID string) ([]Step, error)
	FetchAllChildTitles(parentID string) (map[string]string, error)
	FetchStepBeads(parentID string) ([]StepWithStatus, error)
	FetchOpenNonStepChildren(parentID string) ([]string, error)
	LabelIssue(id, label string) error
}

// CLIBeadsQuerier shells out to the bd CLI to query beads issues.
type CLIBeadsQuerier struct {
	WorkDir string
}

// beadsChild represents the JSON structure returned by bd show --children --json.
type beadsChild struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
	Notes  string `json:"notes"`
}

// beadsListChild represents the JSON structure returned by bd list --json,
// which includes labels but not notes.
type beadsListChild struct {
	ID     string   `json:"id"`
	Title  string   `json:"title"`
	Status string   `json:"status"`
	Labels []string `json:"labels"`
}

func (q *CLIBeadsQuerier) ListOpenChildren(parentID string) ([]string, error) {
	children, err := q.fetchChildren(parentID)
	if err != nil {
		return nil, err
	}

	var ids []string
	for _, c := range children {
		if c.Status == "open" || c.Status == "in_progress" {
			ids = append(ids, c.ID)
		}
	}
	return ids, nil
}

func (q *CLIBeadsQuerier) FetchOrderedSteps(parentID string) ([]Step, error) {
	children, err := q.fetchChildren(parentID)
	if err != nil {
		return nil, err
	}

	var steps []Step
	for _, c := range children {
		if c.Status != "open" && c.Status != "in_progress" {
			continue
		}
		seq := parseSequence(c.Notes)
		steps = append(steps, Step{
			ID:       c.ID,
			Title:    c.Title,
			Sequence: seq,
		})
	}

	sort.Slice(steps, func(i, j int) bool {
		return steps[i].Sequence < steps[j].Sequence
	})

	return steps, nil
}

func (q *CLIBeadsQuerier) FetchAllChildTitles(parentID string) (map[string]string, error) {
	children, err := q.fetchChildren(parentID)
	if err != nil {
		return nil, err
	}

	titles := make(map[string]string, len(children))
	for _, c := range children {
		titles[c.ID] = c.Title
	}
	return titles, nil
}

func (q *CLIBeadsQuerier) LabelIssue(id, label string) error {
	cmd := exec.Command("bd", "label", "add", id, label)
	cmd.Dir = q.WorkDir
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("bd label add %s %s: %w", id, label, err)
	}
	return nil
}

func (q *CLIBeadsQuerier) FetchStepBeads(parentID string) ([]StepWithStatus, error) {
	// Get all children with notes (for sequence parsing).
	children, err := q.fetchChildren(parentID)
	if err != nil {
		return nil, err
	}

	// Get the set of IDs that have the "step" label.
	stepIDs, err := q.fetchStepLabeledIDs(parentID)
	if err != nil {
		return nil, err
	}

	var steps []StepWithStatus
	for _, c := range children {
		if !stepIDs[c.ID] {
			continue
		}
		seq := parseSequence(c.Notes)
		steps = append(steps, StepWithStatus{
			Step: Step{
				ID:       c.ID,
				Title:    c.Title,
				Sequence: seq,
			},
			Status: c.Status,
		})
	}

	sort.Slice(steps, func(i, j int) bool {
		return steps[i].Sequence < steps[j].Sequence
	})

	return steps, nil
}

func (q *CLIBeadsQuerier) FetchOpenNonStepChildren(parentID string) ([]string, error) {
	listChildren, err := q.fetchListChildren(parentID)
	if err != nil {
		return nil, err
	}

	var ids []string
	for _, c := range listChildren {
		if c.Status != "open" && c.Status != "in_progress" {
			continue
		}
		if hasLabel(c.Labels, "step") {
			continue
		}
		ids = append(ids, c.ID)
	}
	return ids, nil
}

// fetchStepLabeledIDs returns the set of child IDs that have the "step" label.
func (q *CLIBeadsQuerier) fetchStepLabeledIDs(parentID string) (map[string]bool, error) {
	cmd := exec.Command("bd", "list", "--parent", parentID, "--label", "step", "--all", "--json", "--limit", "0")
	cmd.Dir = q.WorkDir
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("bd list --parent --label step: %w", err)
	}

	var items []beadsListChild
	if err := json.Unmarshal(out, &items); err != nil {
		return nil, fmt.Errorf("parsing bd list output: %w", err)
	}

	ids := make(map[string]bool, len(items))
	for _, item := range items {
		ids[item.ID] = true
	}
	return ids, nil
}

// fetchListChildren runs bd list --parent --all --json and returns children with labels.
func (q *CLIBeadsQuerier) fetchListChildren(parentID string) ([]beadsListChild, error) {
	cmd := exec.Command("bd", "list", "--parent", parentID, "--all", "--json", "--limit", "0")
	cmd.Dir = q.WorkDir
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("bd list --parent: %w", err)
	}

	var items []beadsListChild
	if err := json.Unmarshal(out, &items); err != nil {
		return nil, fmt.Errorf("parsing bd list output: %w", err)
	}
	return items, nil
}

func hasLabel(labels []string, target string) bool {
	for _, l := range labels {
		if l == target {
			return true
		}
	}
	return false
}

// fetchChildren runs bd show --children --json and parses the map-keyed response.
func (q *CLIBeadsQuerier) fetchChildren(parentID string) ([]beadsChild, error) {
	cmd := exec.Command("bd", "show", parentID, "--children", "--json")
	cmd.Dir = q.WorkDir
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("bd show --children --json: %w", err)
	}

	var result map[string][]beadsChild
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, fmt.Errorf("parsing bd output: %w", err)
	}

	return result[parentID], nil
}

// parseSequence extracts a sequence number from the notes field.
// Expected format: "sequence:N" (as set during decompose).
func parseSequence(notes string) int {
	for _, line := range strings.Split(notes, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "sequence:") {
			val := strings.TrimSpace(strings.TrimPrefix(line, "sequence:"))
			n := 0
			fmt.Sscanf(val, "%d", &n)
			return n
		}
	}
	return 0
}

// FormatSteps renders an ordered list of steps for prompt injection.
func FormatSteps(steps []Step) string {
	var b strings.Builder
	for i, s := range steps {
		fmt.Fprintf(&b, "%d. %s — %s\n", i+1, s.ID, s.Title)
	}
	return b.String()
}
