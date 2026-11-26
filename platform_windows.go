//go:build windows

package main

import (
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

const (
	pasteKeys       = "Ctrl+V or right-click"
	fileManagerName = "File Explorer"
	zoomOutKeys     = "Ctrl and the minus key"
	showMaximized   = 3
)

var folderPermissionHelp = []string{
	"Windows didn't let the exporter save files here. Move the WhatsApp Exporter",
	`folder into your user folder (C:\Users\<your name>) and open it again.`,
}

var (
	procGetConsoleWindow = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetConsoleWindow")
	procShowWindow       = windows.NewLazySystemDLL("user32.dll").NewProc("ShowWindow")
)

const desktopAccessHint = ""

func desktopFolder() (string, error) {
	return windows.KnownFolderPath(windows.FOLDERID_Desktop, 0)
}

func plainConsole() bool {
	return os.Getenv("WT_SESSION") == "" && os.Getenv("TERM_PROGRAM") == ""
}

func growWindow(*terminal, int, int) {
	window, _, _ := procGetConsoleWindow.Call()
	if window != 0 {
		_, _, _ = procShowWindow.Call(window, showMaximized)
	}
}

func openURL(url string) error {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}

func revealFile(path string) {
	if path == "" {
		return
	}
	explorer := exec.Command("explorer.exe")
	explorer.SysProcAttr = &syscall.SysProcAttr{CmdLine: `explorer.exe /select,"` + path + `"`}
	_ = explorer.Run()
}

func closeTerminalWindow() {}
