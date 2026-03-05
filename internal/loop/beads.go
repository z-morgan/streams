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
	ListOpenChildren(parentID string) ([]string, error)
	FetchOrderedSteps(parentID string) ([]Step, error)
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
