package diagnosis

import (
	"os/exec"

	"github.com/zmorgan/streams/internal/stream"
)

// SpawnCmd constructs an exec.Cmd for an interactive claude CLI session
// pre-loaded with the diagnosis system prompt for the given stream.
// workDir is the directory where claude should run (typically the repo dir).
// The caller is responsible for executing the command (e.g., via tea.ExecProcess).
func SpawnCmd(s *stream.Stream, storeRoot, workDir string) *exec.Cmd {
	prompt := BuildSystemPrompt(s, storeRoot)
	cmd := exec.Command("claude", "--system-prompt", prompt)
	cmd.Dir = workDir
	return cmd
}
