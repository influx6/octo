package mock

import (
	"fmt"
	"testing"
)

// Logger defines a structure that implements the octo.Log interface.
type Logger struct {
	t *testing.T
}

// NewLogger returns a new instance of a Logger.
func NewLogger(t *testing.T) *Logger {
	return &Logger{t}
}

// Log exposes methods to giving logger to the internal testing.T object.
func (l *Logger) Log(level string, namespace string, message string, items ...interface{}) {
	l.t.Logf("%s : %s : %s", level, namespace, fmt.Sprintf(message, items...))
}
