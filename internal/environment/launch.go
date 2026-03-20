package environment

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/zmorgan/streams/internal/environment/scaffold"
	"github.com/zmorgan/streams/internal/terminal"
)

// LaunchSetupInTab opens a new terminal tab with an interactive Claude session
// pre-loaded with a system prompt for configuring multi-environment support.
func LaunchSetupInTab(projectDir string) error {
	profile := scaffold.DetectProfile(projectDir)
	hasExistingConfig := configExists(projectDir)
	prompt := BuildSetupPrompt(profile, projectDir, hasExistingConfig)

	// Write system prompt to a temp file.
	promptFile, err := os.CreateTemp("", "streams-env-prompt-*.txt")
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
	scriptFile, err := os.CreateTemp("", "streams-env-launch-*.sh")
	if err != nil {
		os.Remove(promptFile.Name())
		return fmt.Errorf("creating launcher script: %w", err)
	}
	scriptContent := fmt.Sprintf("#!/bin/bash\nPROMPT_FILE=%q\nSELF=%q\ncd %q\nprompt=$(<\"$PROMPT_FILE\")\nrm -f \"$PROMPT_FILE\" \"$SELF\"\nexec claude --system-prompt \"$prompt\"\n",
		promptFile.Name(), scriptFile.Name(), projectDir)

	if _, err := scriptFile.WriteString(scriptContent); err != nil {
		os.Remove(promptFile.Name())
		os.Remove(scriptFile.Name())
		return fmt.Errorf("writing launcher script: %w", err)
	}
	scriptFile.Close()
	os.Chmod(scriptFile.Name(), 0o755)

	return terminal.LaunchScript(scriptFile.Name(), "env setup")
}

func configExists(projectDir string) bool {
	path := filepath.Join(projectDir, ".streams", configFileName)
	_, err := os.Stat(path)
	return err == nil
}
