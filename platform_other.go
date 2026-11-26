//go:build !darwin && !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const (
	pasteKeys       = "Ctrl+Shift+V"
	fileManagerName = "your file manager"
	zoomOutKeys     = "Ctrl and the minus key"
)

var folderPermissionHelp = []string{
	"The exporter isn't allowed to save files here. Move the WhatsApp Exporter",
	"folder to your home folder and open it again.",
}

const desktopAccessHint = ""

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

func openURL(url string) error {
	return exec.Command("xdg-open", url).Start()
}

func revealFile(path string) {
	if path == "" {
		return
	}
	_ = exec.Command("xdg-open", filepath.Dir(path)).Start()
}

func closeTerminalWindow() {}
