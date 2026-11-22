//go:build darwin

package main

import (
	"os"
	"os/exec"
	"strings"
	"syscall"
)

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
