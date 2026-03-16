package diagnosis

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/zmorgan/streams/internal/stream"
)

// LaunchInTab opens a new terminal tab with an interactive claude CLI session
// pre-loaded with the diagnosis system prompt for the given stream.
func LaunchInTab(s *stream.Stream, storeRoot, workDir string) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("new-tab launch requires macOS")
	}

	prompt := BuildSystemPrompt(s, storeRoot)

	// Write system prompt to a temp file.
	promptFile, err := os.CreateTemp("", "streams-diag-prompt-*.txt")
	if err != nil {
		return fmt.Errorf("creating temp prompt file: %w", err)
	}
	if _, err := promptFile.WriteString(prompt); err != nil {
		os.Remove(promptFile.Name())
		return fmt.Errorf("writing prompt file: %w", err)
	}
	promptFile.Close()

	// Write a launcher script that reads the prompt, cleans up temp files,
	// and execs into claude.
	scriptFile, err := os.CreateTemp("", "streams-diag-launch-*.sh")
	if err != nil {
		os.Remove(promptFile.Name())
		return fmt.Errorf("creating launcher script: %w", err)
	}
	scriptContent := fmt.Sprintf("#!/bin/bash\nPROMPT_FILE=%q\nSELF=%q\ncd %q\nprompt=$(<\"$PROMPT_FILE\")\nrm -f \"$PROMPT_FILE\" \"$SELF\"\nexec claude --system-prompt \"$prompt\"\n",
		promptFile.Name(), scriptFile.Name(), workDir)

	if _, err := scriptFile.WriteString(scriptContent); err != nil {
		os.Remove(promptFile.Name())
		os.Remove(scriptFile.Name())
		return fmt.Errorf("writing launcher script: %w", err)
	}
	scriptFile.Close()
	os.Chmod(scriptFile.Name(), 0o755)

	// Detect terminal environment and open the appropriate new tab.
	if os.Getenv("TMUX") != "" {
		return launchTmuxWindow(scriptFile.Name(), s.Task)
	}
	switch os.Getenv("TERM_PROGRAM") {
	case "ghostty":
		return launchGhosttyWindow(scriptFile.Name())
	case "iTerm.app":
		return launchITermTab(scriptFile.Name())
	default:
		return launchTerminalTab(scriptFile.Name())
	}
}

func launchTmuxWindow(script, title string) error {
	args := []string{"new-window"}
	if title != "" {
		if len(title) > 30 {
			title = title[:30]
		}
		args = append(args, "-n", title)
	}
	args = append(args, script)
	return exec.Command("tmux", args...).Run()
}

func launchGhosttyWindow(script string) error {
	return exec.Command("open", "-na", "Ghostty.app", "--args", "-e", script).Run()
}

func launchITermTab(script string) error {
	apple := fmt.Sprintf(`tell application "iTerm2"
	tell current window
		create tab with default profile
		tell current session
			write text %q
		end tell
	end tell
end tell`, script)
	return exec.Command("osascript", "-e", apple).Run()
}

func launchTerminalTab(script string) error {
	apple := fmt.Sprintf(`tell application "Terminal"
	activate
	do script %q
end tell`, script)
	return exec.Command("osascript", "-e", apple).Run()
}
