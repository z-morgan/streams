package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"time"

	"github.com/zmorgan/streams/internal/loop"
	"github.com/zmorgan/streams/internal/runtime"
	"github.com/zmorgan/streams/internal/runtime/claude"
	"github.com/zmorgan/streams/internal/stream"
)

func main() {
	os.Exit(run())
}

func run() int {
	task := flag.String("task", "", "task description (required)")
	dir := flag.String("dir", ".", "working directory")
	beadsParent := flag.String("beads-parent", "", "beads parent issue ID (creates one if not provided)")
	maxIterations := flag.Int("max-iterations", 10, "maximum iteration count")
	maxBudget := flag.String("max-budget-per-step", "2.00", "max USD budget per CLI invocation")
	flag.Parse()

	if *task == "" {
		fmt.Fprintln(os.Stderr, "error: --task is required")
		flag.Usage()
		return 1
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	// Resolve working directory to absolute path.
	workDir, err := resolveDir(*dir)
	if err != nil {
		slog.Error("failed to resolve working directory", "dir", *dir, "err", err)
		return 1
	}

	// Create beads parent issue if not provided.
	parentID := *beadsParent
	if parentID == "" {
		id, err := createBeadsParent(*task, workDir)
		if err != nil {
			slog.Error("failed to create beads parent issue", "err", err)
			return 1
		}
		parentID = id
		slog.Info("created beads parent issue", "id", parentID)
	}

	// Get current HEAD for BaseSHA.
	baseSHA, err := gitHead(workDir)
	if err != nil {
		slog.Error("failed to get git HEAD", "err", err)
		return 1
	}

	// Create git worktree.
	streamID := parentID
	branch := "streams/" + streamID
	worktreePath := workDir + "/.streams/worktrees/" + streamID
	if err := createWorktree(workDir, worktreePath, branch); err != nil {
		slog.Error("failed to create git worktree", "err", err)
		return 1
	}

	s := &stream.Stream{
		ID:            streamID,
		Name:          *task,
		Task:          *task,
		Mode:          stream.ModeAutonomous,
		Status:        stream.StatusCreated,
		Pipeline:      []string{"coding"},
		PipelineIndex: 0,
		BeadsParentID: parentID,
		BaseSHA:       baseSHA,
		Branch:        branch,
		WorkTree:      worktreePath,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	rt := &budgetRuntime{
		inner:     &claude.Runtime{WorkDir: worktreePath},
		maxBudget: *maxBudget,
	}
	beads := &loop.CLIBeadsQuerier{WorkDir: worktreePath}
	phase := &loop.CodingPhase{}

	// Set up context with signal handling.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		<-sigCh
		slog.Info("received interrupt, cancelling...")
		cancel()
	}()

	loop.Run(ctx, s, phase, rt, beads, *maxIterations)

	switch {
	case s.GetStatus() == stream.StatusStopped:
		slog.Info("stream stopped (cancelled)")
		return 0
	case s.Converged:
		slog.Info("stream converged", "iterations", s.GetIteration()+1)
		return 0
	case s.LastError != nil:
		slog.Error("stream error", "kind", s.LastError.Kind, "step", s.LastError.Step, "msg", s.LastError.Message)
		return 1
	default:
		slog.Warn("max iterations reached", "max", *maxIterations)
		return 2
	}
}

func resolveDir(dir string) (string, error) {
	if dir == "." {
		return os.Getwd()
	}
	info, err := os.Stat(dir)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", dir)
	}
	if !strings.HasPrefix(dir, "/") {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		return wd + "/" + dir, nil
	}
	return dir, nil
}

func createBeadsParent(task, workDir string) (string, error) {
	cmd := exec.Command("bd", "create", "--title", task, "--type", "task", "--priority", "2")
	cmd.Dir = workDir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("bd create: %w", err)
	}
	line := strings.TrimSpace(string(out))
	return parseBeadsID(line), nil
}

func parseBeadsID(output string) string {
	// Format: "✓ Created streams-abc: ..." or similar
	fields := strings.Fields(output)
	for _, f := range fields {
		f = strings.TrimSuffix(f, ":")
		if strings.Contains(f, "-") && !strings.HasPrefix(f, "✓") && !strings.EqualFold(f, "Created") {
			return f
		}
	}
	return strings.TrimSpace(output)
}

func gitHead(workDir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = workDir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func createWorktree(repoDir, worktreePath, branch string) error {
	cmd := exec.Command("git", "worktree", "add", worktreePath, "-b", branch)
	cmd.Dir = repoDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree add: %s: %w", out, err)
	}
	return nil
}

// budgetRuntime wraps a runtime to inject max-budget-usd into every request.
type budgetRuntime struct {
	inner     *claude.Runtime
	maxBudget string
}

func (b *budgetRuntime) Run(ctx context.Context, req runtime.Request) (*runtime.Response, error) {
	if req.Options == nil {
		req.Options = make(map[string]string)
	}
	req.Options["maxBudgetUsd"] = b.maxBudget
	return b.inner.Run(ctx, req)
}
