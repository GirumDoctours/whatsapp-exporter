package main

import (
	"io"
	"os"
	"path/filepath"
)

func desktopSaveFolder(t *terminal) (string, bool) {
	desktop, err := desktopFolder()
	if err != nil {
		logSetup.Warnf("Couldn't find the Desktop: %v", err)
		return "", false
	}
	if desktopAccessHint != "" {
		t.say("  %s", t.paint(ansiDim, desktopAccessHint))
	}
	if !canWriteTo(desktop) {
		logSetup.Warnf("The Desktop at %s isn't writable", desktop)
		return "", false
	}
	logSetup.Infof("ZIPs will be saved to %s", desktop)
	return desktop, true
}

func canWriteTo(dir string) bool {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return false
	}
	probe, err := os.CreateTemp(dir, ".whatsapp-exporter-*")
	if err != nil {
		return false
	}
	name := probe.Name()
	_ = probe.Close()
	return os.Remove(name) == nil
}

func moveInto(path, dir string) (string, error) {
	if sameDir(filepath.Dir(path), dir) {
		return path, nil
	}
	target := availablePath(filepath.Join(dir, filepath.Base(path)))
	if err := os.Rename(path, target); err == nil {
		return target, nil
	}
	if err := copyFile(path, target); err != nil {
		return path, err
	}
	_ = os.Remove(path)
	return target, nil
}

func copyFile(from, to string) error {
	source, err := os.Open(from)
	if err != nil {
		return err
	}
	defer source.Close()
	partial := to + ".partial"
	target, err := os.Create(partial)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(target, source)
	syncErr := target.Sync()
	closeErr := target.Close()
	for _, err := range []error{copyErr, syncErr, closeErr} {
		if err != nil {
			_ = os.Remove(partial)
			return err
		}
	}
	if err := os.Rename(partial, to); err != nil {
		_ = os.Remove(partial)
		return err
	}
	return nil
}

func sameDir(a, b string) bool {
	left, errLeft := os.Stat(a)
	right, errRight := os.Stat(b)
	if errLeft != nil || errRight != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return os.SameFile(left, right)
}
