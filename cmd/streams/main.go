package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/zmorgan/streams/internal/loop"
	"github.com/zmorgan/streams/internal/orchestrator"
	"github.com/zmorgan/streams/internal/runtime"
	"github.com/zmorgan/streams/internal/runtime/claude"
	"github.com/zmorgan/streams/internal/store"
	"github.com/zmorgan/streams/internal/stream"
	"github.com/zmorgan/streams/internal/ui"
)

func main() {
	os.Exit(run())
}

func run() int {
	headless := flag.Bool("headless", false, "run a single stream without TUI")
	task := flag.String("task", "", "task description (required in headless mode)")
	dir := flag.String("dir", ".", "working directory")
	maxIterations := flag.Int("max-iterations", 10, "maximum iteration count")
	maxBudget := flag.String("max-budget-per-step", "2.00", "max USD budget per CLI invocation")
	dataDir := flag.String("data-dir", "", "directory for stream data (default: <dir>/.streams)")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	workDir, err := resolveDir(*dir)
	if err != nil {
		slog.Error("failed to resolve working directory", "dir", *dir, "err", err)
		return 1
	}

	storeRoot := *dataDir
	if storeRoot == "" {
		storeRoot = filepath.Join(workDir, ".streams")
	}

	s := &store.Store{Root: storeRoot}
	orch := orchestrator.New(s, orchestrator.Config{
		MaxIterations: *maxIterations,
		MaxBudgetUSD:  *maxBudget,
		RepoDir:       workDir,
	})

	if err := orch.LoadExisting(); err != nil {
		slog.Error("failed to load existing streams", "err", err)
		return 1
	}

	if *headless {
		return runHeadless(orch, workDir, *task, *maxIterations, *maxBudget)
	}

	return runTUI(orch)
}

func runTUI(orch *orchestrator.Orchestrator) int {
	p := tea.NewProgram(ui.New(orch), tea.WithAltScreen())
	orch.SetSink(&ui.EventSink{Program: p})

	if _, err := p.Run(); err != nil {
		slog.Error("TUI error", "err", err)
		return 1
	}
	return 0
}

func runHeadless(orch *orchestrator.Orchestrator, workDir, task string, maxIterations int, maxBudget string) int {
	if task == "" {
		fmt.Fprintln(os.Stderr, "error: --task is required in headless mode")
		flag.Usage()
		return 1
	}

	st, err := orch.Create(task)
	if err != nil {
		slog.Error("failed to create stream", "err", err)
		return 1
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		<-sigCh
		slog.Info("received interrupt, cancelling...")
		cancel()
	}()

	rt := &budgetRuntime{
		inner:     &claude.Runtime{WorkDir: st.WorkTree},
		maxBudget: maxBudget,
	}
	beads := &loop.CLIBeadsQuerier{WorkDir: st.WorkTree}
	phase := &loop.CodingPhase{}

	loop.Run(ctx, st, phase, rt, beads, maxIterations)

	switch {
	case st.GetStatus() == stream.StatusStopped:
		slog.Info("stream stopped (cancelled)")
		return 0
	case st.Converged:
		slog.Info("stream converged", "iterations", st.GetIteration()+1)
		return 0
	case st.LastError != nil:
		slog.Error("stream error", "kind", st.LastError.Kind, "step", st.LastError.Step, "msg", st.LastError.Message)
		return 1
	default:
		slog.Warn("max iterations reached", "max", maxIterations)
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
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	return abs, nil
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
