package loop

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zmorgan/streams/internal/runtime"
	"github.com/zmorgan/streams/internal/stream"
)

// gitRun runs a git command in the given directory. Fatals on error.
func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test",
		"GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=Test",
		"GIT_COMMITTER_EMAIL=test@test.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %s\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// initTestRepo creates a temp git repo with an initial commit and returns the
// directory and base SHA.
func initTestRepo(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("initial\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "file.txt")
	gitRun(t, dir, "commit", "-m", "initial")
	baseSHA := gitRun(t, dir, "rev-parse", "HEAD")
	return dir, baseSHA
}

func TestBeforeReview_NoCommits(t *testing.T) {
	dir, baseSHA := initTestRepo(t)
	p := &CodingPhase{}
	ctx := PhaseContext{
		Stream:  &stream.Stream{BaseSHA: baseSHA},
		WorkDir: dir,
	}

	if err := p.BeforeReview(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBeforeReview_SuccessfulAutosquash(t *testing.T) {
	dir, baseSHA := initTestRepo(t)

	// Create a commit and a fixup for it.
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "file.txt")
	gitRun(t, dir, "commit", "-m", "add feature")

	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("fixed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "file.txt")
	gitRun(t, dir, "commit", "-m", "fixup! add feature")

	p := &CodingPhase{}
	ctx := PhaseContext{
		Stream:  &stream.Stream{BaseSHA: baseSHA},
		WorkDir: dir,
	}

	if err := p.BeforeReview(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have squashed to one commit after base.
	logOut := gitRun(t, dir, "log", "--oneline", baseSHA+"..HEAD")
	lines := strings.Split(strings.TrimSpace(logOut), "\n")
	if len(lines) != 1 {
		t.Errorf("expected 1 commit after autosquash, got %d: %s", len(lines), logOut)
	}
}

func TestBeforeReview_RebaseFailsAgentResolves(t *testing.T) {
	dir, baseSHA := initTestRepo(t)

	// Create two commits that will conflict when a fixup targets the first.
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("line1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "file.txt")
	gitRun(t, dir, "commit", "-m", "first change")

	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("line1 modified\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "file.txt")
	gitRun(t, dir, "commit", "-m", "second change")

	// Fixup for "first change" that conflicts with "second change".
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("line1 rewritten\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "file.txt")
	gitRun(t, dir, "commit", "-m", "fixup! first change")

	// Mock runtime that resolves the conflict when called.
	agentRuntime := &mockRuntime{
		results: []mockResult{
			{resp: &runtime.Response{Text: "resolved conflicts"}},
		},
	}
	// Override Run to actually resolve the conflict in the repo.
	resolverRT := &conflictResolverRuntime{
		inner:   agentRuntime,
		workDir: dir,
	}

	p := &CodingPhase{}
	ctx := PhaseContext{
		Stream:  &stream.Stream{BaseSHA: baseSHA},
		Runtime: resolverRT,
		WorkDir: dir,
	}

	err := p.BeforeReview(ctx)
	if err != nil {
		t.Fatalf("expected agent to resolve conflict, got error: %v", err)
	}
}

func TestBeforeReview_RebaseFailsAgentFails(t *testing.T) {
	dir, baseSHA := initTestRepo(t)

	// Create conflicting commits like above.
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("line1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "file.txt")
	gitRun(t, dir, "commit", "-m", "first change")

	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("line1 modified\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "file.txt")
	gitRun(t, dir, "commit", "-m", "second change")

	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("line1 rewritten\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "file.txt")
	gitRun(t, dir, "commit", "-m", "fixup! first change")

	// Mock runtime that fails.
	agentRuntime := &mockRuntime{
		results: []mockResult{
			{err: errors.New("could not resolve conflict")},
		},
	}

	p := &CodingPhase{}
	ctx := PhaseContext{
		Stream:  &stream.Stream{BaseSHA: baseSHA},
		Runtime: agentRuntime,
		WorkDir: dir,
	}

	err := p.BeforeReview(ctx)
	if err == nil {
		t.Fatal("expected error when agent fails")
	}
	if !strings.Contains(err.Error(), "agent could not resolve") {
		t.Errorf("expected 'agent could not resolve' in error, got: %v", err)
	}

	// Worktree should be clean (rebase aborted).
	statusOut := gitRun(t, dir, "status", "--porcelain")
	if strings.TrimSpace(statusOut) != "" {
		t.Errorf("expected clean worktree after abort, got: %s", statusOut)
	}
}

// conflictResolverRuntime wraps a runtime and resolves git conflicts in the
// workDir before delegating to the inner runtime. This simulates what the
// rebase agent would do.
type conflictResolverRuntime struct {
	inner   runtime.Runtime
	workDir string
}

func (r *conflictResolverRuntime) Run(ctx context.Context, req runtime.Request) (*runtime.Response, error) {
	// Resolve conflicts in a loop, like the real agent would.
	for i := 0; i < 10; i++ {
		cmd := exec.Command("git", "diff", "--name-only", "--diff-filter=U")
		cmd.Dir = r.workDir
		out, err := cmd.Output()
		if err != nil || len(strings.TrimSpace(string(out))) == 0 {
			break
		}

		files := strings.Split(strings.TrimSpace(string(out)), "\n")
		for _, f := range files {
			path := filepath.Join(r.workDir, f)
			content, _ := os.ReadFile(path)
			resolved := resolveConflictMarkers(string(content))
			_ = os.WriteFile(path, []byte(resolved), 0o644)

			add := exec.Command("git", "add", f)
			add.Dir = r.workDir
			_ = add.Run()
		}

		cont := exec.Command("git", "-c", "core.editor=true", "rebase", "--continue")
		cont.Dir = r.workDir
		cont.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=Test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		contOut, contErr := cont.CombinedOutput()
		_ = contOut
		if contErr == nil {
			break // rebase completed
		}
		// contErr != nil means another conflict; loop again.
	}

	return r.inner.Run(ctx, req)
}

// resolveConflictMarkers takes content with git conflict markers and returns
// the "theirs" side (after =======, before >>>>>>>).
func resolveConflictMarkers(content string) string {
	var result strings.Builder
	inConflict := false
	takeTheirs := false
	for _, line := range strings.Split(content, "\n") {
		switch {
		case strings.HasPrefix(line, "<<<<<<<"):
			inConflict = true
			takeTheirs = false
		case strings.HasPrefix(line, "=======") && inConflict:
			takeTheirs = true
		case strings.HasPrefix(line, ">>>>>>>") && inConflict:
			inConflict = false
			takeTheirs = false
		case inConflict && takeTheirs:
			result.WriteString(line)
			result.WriteString("\n")
		case !inConflict:
			result.WriteString(line)
			result.WriteString("\n")
		}
	}
	return result.String()
}
