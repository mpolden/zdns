package log

import (
	"io"
	"log"
)

// A Logger wraps a standard log.Logger.
type Logger struct {
	logger *log.Logger
}

// New creates a new logger wrapping a standard log.Logger.
func New(out io.Writer, logPrefix string) *Logger {
	logger := &Logger{logger: log.New(out, logPrefix, 0)}
	return logger
}

// Printf delegates to Printf of log.Logger.
func (l *Logger) Printf(format string, v ...interface{}) { l.logger.Printf(format, v...) }

// Fatal delegates to Fatal of log.Logger.
func (l *Logger) Fatal(v ...interface{}) { l.logger.Fatal(v...) }
