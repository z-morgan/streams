package loop

import (
	"os/exec"
	"strings"
)

// GitQuerier abstracts git operations for testability.
type GitQuerier interface {
	HeadSHA(workDir string) (string, error)
	DiffStat(workDir, fromSHA string) (string, error)
	CommitsBetween(workDir, fromSHA, toSHA string) ([]string, error)
}

// CLIGitQuerier shells out to git.
type CLIGitQuerier struct{}

func (q *CLIGitQuerier) HeadSHA(workDir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = workDir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (q *CLIGitQuerier) DiffStat(workDir, fromSHA string) (string, error) {
	cmd := exec.Command("git", "diff", "--stat", fromSHA+"..HEAD")
	cmd.Dir = workDir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (q *CLIGitQuerier) CommitsBetween(workDir, fromSHA, toSHA string) ([]string, error) {
	cmd := exec.Command("git", "log", "--format=%H", fromSHA+".."+toSHA)
	cmd.Dir = workDir
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	text := strings.TrimSpace(string(out))
	if text == "" {
		return nil, nil
	}
	return strings.Split(text, "\n"), nil
}
