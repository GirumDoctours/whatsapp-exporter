package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	waLog "go.mau.fi/whatsmeow/util/log"
)

const (
	debugFileName      = "debug.txt"
	debugBufferLimit   = 64 << 20
	verboseLibraryEnv  = "WHATSAPP_EXPORTER_DEBUG"
	logTimeLayout      = "2006-01-02 15:04:05.000"
	libraryModulePrefx = "Exporter/WA"
)

type logLevel int

const (
	levelDebug logLevel = iota
	levelInfo
	levelWarn
	levelError
)

func (l logLevel) String() string {
	switch l {
	case levelDebug:
		return "DEBUG"
	case levelInfo:
		return "INFO"
	case levelWarn:
		return "WARN"
	}
	return "ERROR"
}

type logSink struct {
	mu        sync.Mutex
	writers   []io.Writer
	debug     bool
	verbose   bool
	buffer    bytes.Buffer
	truncated bool
}

type logger struct {
	sink   *logSink
	module string
}

var appLog = &logger{sink: &logSink{verbose: os.Getenv(verboseLibraryEnv) != ""}, module: "Exporter"}

func enableDebugLog() {
	appLog.sink.mu.Lock()
	appLog.sink.debug = true
	appLog.sink.mu.Unlock()
	appLog.Infof("Debug log on (%s %s/%s)", runtime.Version(), runtime.GOOS, runtime.GOARCH)
}

func debugLogEnabled() bool {
	appLog.sink.mu.Lock()
	defer appLog.sink.mu.Unlock()
	return appLog.sink.debug
}

func debugLogContents() []byte {
	s := appLog.sink
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]byte(nil), s.buffer.Bytes()...)
	if s.truncated {
		out = append(out, []byte("\n(debug log truncated at 64 MB)\n")...)
	}
	return out
}

func (l *logger) attach(w io.Writer) func() {
	s := l.sink
	s.mu.Lock()
	s.writers = append(s.writers, w)
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		for i, existing := range s.writers {
			if existing == w {
				s.writers = append(s.writers[:i], s.writers[i+1:]...)
				return
			}
		}
	}
}

func attachLogFile(path string) (io.Closer, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	detach := appLog.attach(file)
	return closerFunc(func() error {
		detach()
		return file.Close()
	}), nil
}

type closerFunc func() error

func (f closerFunc) Close() error { return f() }

func (l *logger) library() bool {
	return strings.HasPrefix(l.module, libraryModulePrefx)
}

func (l *logger) write(level logLevel, msg string, args ...any) {
	s := l.sink
	s.mu.Lock()
	defer s.mu.Unlock()
	debugOn := s.debug && (!l.library() || s.verbose)
	if level == levelDebug && !debugOn && !s.verbose {
		return
	}
	line := fmt.Sprintf("%s [%s %s] %s\n", time.Now().Format(logTimeLayout), l.module, level, fmt.Sprintf(msg, args...))
	for _, w := range s.writers {
		_, _ = io.WriteString(w, line)
	}
	if s.debug && (level >= levelInfo || debugOn) {
		if s.buffer.Len()+len(line) > debugBufferLimit {
			s.truncated = true
			return
		}
		s.buffer.WriteString(line)
	}
}

func (l *logger) Debugf(msg string, args ...any) { l.write(levelDebug, msg, args...) }
func (l *logger) Infof(msg string, args ...any)  { l.write(levelInfo, msg, args...) }
func (l *logger) Warnf(msg string, args ...any)  { l.write(levelWarn, msg, args...) }
func (l *logger) Errorf(msg string, args ...any) { l.write(levelError, msg, args...) }

func (l *logger) Sub(module string) waLog.Logger {
	return l.child(module)
}

func (l *logger) child(module string) *logger {
	return &logger{sink: l.sink, module: l.module + "/" + module}
}

func (l *logger) timed(what string) func(format string, args ...any) {
	started := time.Now()
	l.Debugf("%s: started", what)
	return func(format string, args ...any) {
		detail := ""
		if format != "" {
			detail = " · " + fmt.Sprintf(format, args...)
		}
		l.Infof("%s: done in %s%s", what, time.Since(started).Round(time.Millisecond), detail)
	}
}

var (
	logMain       = appLog.child("Main")
	logPipeline   = appLog.child("Pipeline")
	logExport     = appLog.child("Export")
	logRedact     = appLog.child("Redact")
	logAI         = appLog.child("AI")
	logMedia      = appLog.child("MediaCheck")
	logOCR        = appLog.child("OCR")
	logZip        = appLog.child("Zip")
	logSetup      = appLog.child("Setup")
	logTranscribe = appLog.child("Transcribe")
)
