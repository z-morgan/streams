package terminal

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

// LaunchScript detects the terminal environment and opens a new tab running
// the given script. The title parameter is used for tmux window names (may be
// truncated or ignored by other terminals).
func LaunchScript(scriptPath, title string) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("new-tab launch requires macOS")
	}

	if os.Getenv("TMUX") != "" {
		return launchTmuxWindow(scriptPath, title)
	}
	switch os.Getenv("TERM_PROGRAM") {
	case "ghostty":
		return launchGhosttyTab(scriptPath)
	case "iTerm.app":
		return launchITermTab(scriptPath)
	default:
		return launchTerminalTab(scriptPath)
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

func launchGhosttyTab(script string) error {
	apple := fmt.Sprintf(`tell application "Ghostty" to activate
delay 0.2
tell application "System Events" to tell process "Ghostty"
	keystroke "t" using command down
end tell
delay 0.5
tell application "System Events" to tell process "Ghostty"
	keystroke %q
	keystroke return
end tell`, script)
	return exec.Command("osascript", "-e", apple).Run()
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
	apple := fmt.Sprintf(`tell application "Terminal" to activate
delay 0.2
tell application "System Events" to tell process "Terminal"
	keystroke "t" using command down
end tell
delay 0.5
tell application "Terminal"
	do script %q in front window
end tell`, script)
	return exec.Command("osascript", "-e", apple).Run()
}
