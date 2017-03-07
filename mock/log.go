package mock

import (
	"fmt"
	"log"
	"testing"
)

// Logger defines a structure that implements the octo.Log interface.
type Logger struct{}

// NewLogger returns a new instance of a Logger.
func NewLogger() *Logger {
	return &Logger{}
}

// Log exposes methods to giving logger to the internal testing.T object.
func (l *Logger) Log(level string, namespace string, function string, message string, items ...interface{}) {
	if testing.Verbose() {
		log.Output(2, fmt.Sprintf("%s : %s : %s : %s\n", level, namespace, function, fmt.Sprintf(message, items...)))
	}
}

//================================================================================

// StdLogger defines a structure that implements the octo.Log interface.
type StdLogger struct{}

// Log exposes methods to giving logger to the internal testing.T object.
func (StdLogger) Log(level string, namespace string, function string, message string, items ...interface{}) {
	fmt.Printf("%s : %s : %s : %s\n", level, namespace, function, fmt.Sprintf(message, items...))
}
