package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/zmorgan/streams/internal/config"
	"github.com/zmorgan/streams/internal/environment"
	"github.com/zmorgan/streams/internal/loop"
	"github.com/zmorgan/streams/internal/orchestrator"
	"github.com/zmorgan/streams/internal/runtime"
	"github.com/zmorgan/streams/internal/runtime/claude"
	"github.com/zmorgan/streams/internal/store"
	"github.com/zmorgan/streams/internal/stream"
	"github.com/zmorgan/streams/internal/ui"
)

// Version is set via ldflags at build time.
var Version = "dev"

func main() {
	os.Exit(run())
}

func run() int {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--version", "-v":
			fmt.Println("streams " + Version)
			return 0
		case "prompts":
			return runPrompts(os.Args[2:])
		case "config":
			return runConfig(os.Args[2:])
		}
	}

	headless := flag.Bool("headless", false, "run a single stream without TUI")
	task := flag.String("task", "", "task description (required in headless mode)")
	dir := flag.String("dir", ".", "working directory")
	flag.Int("max-iterations", 0, "maximum iteration count")
	flag.String("max-budget-per-step", "", "max USD budget per CLI invocation (\"0\" to disable)")
	dataDir := flag.String("data-dir", "", "directory for stream data (default: <dir>/.streams)")
	flag.String("pipeline", "", "comma-separated pipeline phases (e.g. research,plan,decompose,coding)")
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

	// Load persistent config (defaults ← user ← project ← CLI flags).
	fileCfg := config.Load(storeRoot)
	cliOverrides := flagOverrides()
	cfg := config.Merge(fileCfg, cliOverrides)

	var pipelinePhases []string
	if cfg.Pipeline != nil {
		for _, p := range strings.Split(*cfg.Pipeline, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				pipelinePhases = append(pipelinePhases, p)
			}
		}
	}

	maxIterations := 10
	if cfg.MaxIterations != nil {
		maxIterations = *cfg.MaxIterations
	}

	budgetUSD := ""
	if cfg.BudgetEnabled() {
		budgetUSD = *cfg.MaxBudgetPerStep
	}

	var polishSlots []string
	if cfg.PolishSlots != nil {
		for _, slot := range strings.Split(*cfg.PolishSlots, ",") {
			slot = strings.TrimSpace(slot)
			if slot != "" {
				polishSlots = append(polishSlots, slot)
			}
		}
	}

	envCfg, err := environment.LoadConfig(workDir)
	if err != nil {
		slog.Warn("failed to load environment config, environments disabled", "err", err)
	}
	envManager := environment.NewManager(envCfg)

	s := &store.Store{Root: storeRoot}
	orch := orchestrator.New(s, orchestrator.Config{
		MaxIterations: maxIterations,
		MaxBudgetUSD:  budgetUSD,
		RepoDir:       workDir,
		Pipeline:      pipelinePhases,
		PolishSlots:   polishSlots,
	}, envManager)

	if err := orch.LoadExisting(); err != nil {
		slog.Error("failed to load existing streams", "err", err)
		return 1
	}

	if *headless {
		return runHeadless(orch, workDir, *task, maxIterations, budgetUSD)
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

	p := tea.NewProgram(ui.New(orch))
	orch.SetSink(&ui.EventSink{Program: p})

	if _, err := p.Run(); err != nil {
		slog.Error("TUI error", "err", err)
		return 1
	}
	orch.TeardownEnvironments()
	return 0
}

func runHeadless(orch *orchestrator.Orchestrator, workDir, task string, maxIterations int, maxBudget string) int {
	if task == "" {
		fmt.Fprintln(os.Stderr, "error: --task is required in headless mode")
		flag.Usage()
		return 1
	}

	st, err := orch.Create(task, task, nil, nil, stream.NotifySettings{})
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

	var rt runtime.Runtime = &claude.Runtime{WorkDir: st.WorkTree}
	if maxBudget != "" {
		rt = &runtime.BudgetRuntime{Inner: rt, MaxBudget: maxBudget}
	}
	beads := &loop.CLIBeadsQuerier{WorkDir: st.WorkTree}
	git := &loop.CLIGitQuerier{}
	phaseName := st.Pipeline[st.PipelineIndex]
	phase, phaseErr := loop.NewPhase(phaseName)
	if phaseErr != nil {
		slog.Error("failed to create phase", "phase", phaseName, "err", phaseErr)
		return 1
	}

	storeRoot := orch.StoreRoot()
	promptDirs := []string{
		filepath.Join(storeRoot, "streams", st.ID, "prompts"),
		filepath.Join(storeRoot, "prompts"),
	}
	loop.Run(ctx, st, phase, rt, beads, git, maxIterations, loop.NewPhase, nil, promptDirs...)

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

func runPrompts(args []string) int {
	fs := flag.NewFlagSet("prompts", flag.ExitOnError)
	list := fs.Bool("list", false, "list all prompt template names")
	export := fs.String("export", "", "print the default template to stdout")
	fs.Parse(args)

	switch {
	case *list:
		for _, name := range loop.ListPromptNames() {
			fmt.Println(name)
		}
		return 0
	case *export != "":
		content, err := loop.ExportPrompt(*export)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Print(content)
		return 0
	default:
		fmt.Fprintln(os.Stderr, "usage: streams prompts --list | --export <name>")
		return 1
	}
}

func runConfig(args []string) int {
	dir := "."
	dataDir := ""
	storeRoot := func() string {
		workDir, err := resolveDir(dir)
		if err != nil {
			return ".streams"
		}
		if dataDir != "" {
			return dataDir
		}
		return filepath.Join(workDir, ".streams")
	}

	// "streams config" with no args — show effective config.
	if len(args) == 0 {
		cfg := config.Load(storeRoot())
		fmt.Print(config.Format(cfg))
		return 0
	}

	// "streams config set [--global] <key> <value>"
	if args[0] == "set" {
		fs := flag.NewFlagSet("config set", flag.ExitOnError)
		global := fs.Bool("global", false, "operate on user-level config (~/.config/streams/config.toml)")
		fs.Parse(args[1:])

		remaining := fs.Args()
		if len(remaining) != 2 {
			fmt.Fprintln(os.Stderr, "usage: streams config set [--global] <key> <value>")
			return 1
		}
		key, value := remaining[0], remaining[1]

		var path string
		if *global {
			path = config.UserConfigPath()
		} else {
			path = config.ProjectConfigPath(storeRoot())
		}

		if err := config.SetInFile(path, key, value); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		fmt.Printf("set %s = %q in %s\n", key, value, path)
		return 0
	}

	fmt.Fprintln(os.Stderr, "usage: streams config [set [--global] <key> <value>]")
	return 1
}

// flagOverrides builds a config.Config from only the CLI flags that were
// explicitly set by the user. Unset flags produce nil fields so they don't
// override file-based config.
func flagOverrides() config.Config {
	var cfg config.Config
	flag.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "max-budget-per-step":
			v := f.Value.String()
			cfg.MaxBudgetPerStep = &v
		case "max-iterations":
			if n, err := strconv.Atoi(f.Value.String()); err == nil {
				cfg.MaxIterations = &n
			}
		case "pipeline":
			v := f.Value.String()
			cfg.Pipeline = &v
		}
	})
	return cfg
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
