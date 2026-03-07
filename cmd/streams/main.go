package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

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
	pipeline := flag.String("pipeline", "coding", "comma-separated pipeline phases (e.g. research,plan,decompose,coding)")
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

	var pipelinePhases []string
	for _, p := range strings.Split(*pipeline, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			pipelinePhases = append(pipelinePhases, p)
		}
	}

	s := &store.Store{Root: storeRoot}
	orch := orchestrator.New(s, orchestrator.Config{
		MaxIterations: *maxIterations,
		MaxBudgetUSD:  *maxBudget,
		RepoDir:       workDir,
		Pipeline:      pipelinePhases,
	})

	if err := orch.LoadExisting(); err != nil {
		slog.Error("failed to load existing streams", "err", err)
		return 1
	}

	if *headless {
		return runHeadless(orch, workDir, *task, *maxIterations, *maxBudget)
	}

	return runTUI(orch, storeRoot)
}

func runTUI(orch *orchestrator.Orchestrator, storeRoot string) int {
	if err := os.MkdirAll(storeRoot, 0o755); err != nil {
		slog.Error("failed to create data directory", "path", storeRoot, "err", err)
		return 1
	}

	logPath := filepath.Join(storeRoot, "streams.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		slog.Error("failed to open log file", "path", logPath, "err", err)
		return 1
	}
	defer logFile.Close()

	logger := slog.New(slog.NewTextHandler(logFile, nil))
	slog.SetDefault(logger)

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

	st, err := orch.Create(task, nil)
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

	rt := &runtime.BudgetRuntime{
		Inner:     &claude.Runtime{WorkDir: st.WorkTree},
		MaxBudget: maxBudget,
	}
	beads := &loop.CLIBeadsQuerier{WorkDir: st.WorkTree}
	git := &loop.CLIGitQuerier{}
	phaseName := st.Pipeline[st.PipelineIndex]
	phase, phaseErr := loop.NewPhase(phaseName)
	if phaseErr != nil {
		slog.Error("failed to create phase", "phase", phaseName, "err", phaseErr)
		return 1
	}

	loop.Run(ctx, st, phase, rt, beads, git, maxIterations, loop.NewPhase, nil)

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

