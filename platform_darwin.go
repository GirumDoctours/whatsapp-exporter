//go:build darwin

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	pasteKeys       = "⌘V"
	fileManagerName = "Finder"
	zoomOutKeys     = "⌘ and the minus key"
)

var folderPermissionHelp = []string{
	"macOS didn't let Terminal save files here. Move the WhatsApp Exporter folder",
	"to your home folder and open it again, or allow Terminal in System Settings",
	"→ Privacy & Security → Files and Folders.",
}

const desktopAccessHint = "Your ZIP will be saved on your Desktop. If macOS asks whether Terminal can access it, click Allow."

func desktopFolder() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Desktop"), nil
}

func plainConsole() bool {
	return false
}

func growWindow(t *terminal, rows, cols int) {
	t.control(fmt.Sprintf("\033[8;%d;%dt", rows, cols))
}

const closeWindowScript = `on run argv
	set targetTTY to item 1 of argv
	tell application "Terminal"
		repeat with w in windows
			if (count of tabs of w) is 1 and tty of tab 1 of w is targetTTY then
				close w
				exit repeat
			end if
		end repeat
	end tell
end run`

func openURL(url string) error {
	return exec.Command("/usr/bin/open", url).Start()
}

func revealFile(path string) {
	if path == "" {
		return
	}
	_ = exec.Command("/usr/bin/open", "-R", path).Run()
}

func closeTerminalWindow() {
	if os.Getenv("TERM_PROGRAM") != "Apple_Terminal" {
		return
	}
	ttyCmd := exec.Command("/usr/bin/tty")
	ttyCmd.Stdin = os.Stdin
	out, err := ttyCmd.Output()
	tty := strings.TrimSpace(string(out))
	if err != nil || !strings.HasPrefix(tty, "/dev/") {
		return
	}
	closer := exec.Command("/bin/sh", "-c", `sleep 1; exec /usr/bin/osascript -e "$1" "$2"`, "sh", closeWindowScript, tty)
	closer.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	_ = closer.Start()
}
