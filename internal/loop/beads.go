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

// BeadsQuerier abstracts the beads CLI for testability.
type BeadsQuerier interface {
	CountOpenChildren(parentID string) (int, error)
	FetchOrderedSteps(parentID string) ([]Step, error)
}

// CLIBeadsQuerier shells out to the bd CLI to query beads issues.
type CLIBeadsQuerier struct {
	WorkDir string
}

// beadsChild represents the JSON structure returned by bd show --children --json.
type beadsChild struct {
	ID       string                 `json:"id"`
	Title    string                 `json:"title"`
	Status   string                 `json:"status"`
	Metadata map[string]interface{} `json:"metadata"`
}

func (q *CLIBeadsQuerier) CountOpenChildren(parentID string) (int, error) {
	cmd := exec.Command("bd", "show", parentID, "--children", "--json")
	cmd.Dir = q.WorkDir
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("bd show --children --json: %w", err)
	}

	var children []beadsChild
	if err := json.Unmarshal(out, &children); err != nil {
		return 0, fmt.Errorf("parsing bd output: %w", err)
	}

	count := 0
	for _, c := range children {
		if c.Status == "open" || c.Status == "in_progress" {
			count++
		}
	}
	return count, nil
}

func (q *CLIBeadsQuerier) FetchOrderedSteps(parentID string) ([]Step, error) {
	cmd := exec.Command("bd", "show", parentID, "--children", "--json")
	cmd.Dir = q.WorkDir
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("bd show --children --json: %w", err)
	}

	var children []beadsChild
	if err := json.Unmarshal(out, &children); err != nil {
		return nil, fmt.Errorf("parsing bd output: %w", err)
	}

	var steps []Step
	for _, c := range children {
		if c.Status != "open" && c.Status != "in_progress" {
			continue
		}
		seq := 0
		if c.Metadata != nil {
			if v, ok := c.Metadata["sequence"]; ok {
				if f, ok := v.(float64); ok {
					seq = int(f)
				}
			}
		}
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

// FormatSteps renders an ordered list of steps for prompt injection.
func FormatSteps(steps []Step) string {
	var b strings.Builder
	for i, s := range steps {
		fmt.Fprintf(&b, "%d. %s — %s\n", i+1, s.ID, s.Title)
	}
	return b.String()
}
