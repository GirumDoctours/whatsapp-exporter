package main

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	waLog "go.mau.fi/whatsmeow/util/log"
)

type fileLogger struct {
	mu     *sync.Mutex
	out    io.Writer
	module string
	debug  bool
}

func newFileLogger(path string) (*fileLogger, io.Closer, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, nil, err
	}
	logger := &fileLogger{
		mu:     &sync.Mutex{},
		out:    file,
		module: "Exporter",
		debug:  os.Getenv("WHATSAPP_EXPORTER_DEBUG") != "",
	}
	return logger, file, nil
}

func (l *fileLogger) write(level, msg string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(l.out, "%s [%s %s] %s\n", time.Now().Format("2006-01-02 15:04:05.000"), l.module, level, fmt.Sprintf(msg, args...))
}

func (l *fileLogger) Warnf(msg string, args ...any)  { l.write("WARN", msg, args...) }
func (l *fileLogger) Errorf(msg string, args ...any) { l.write("ERROR", msg, args...) }
func (l *fileLogger) Infof(msg string, args ...any)  { l.write("INFO", msg, args...) }

func (l *fileLogger) Debugf(msg string, args ...any) {
	if l.debug {
		l.write("DEBUG", msg, args...)
	}
}

func (l *fileLogger) Sub(module string) waLog.Logger {
	return &fileLogger{mu: l.mu, out: l.out, module: l.module + "/" + module, debug: l.debug}
}
