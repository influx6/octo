package instruments

import (
	"fmt"
	"os"
)

// StdLogger defines a structure that implements the octo.Log interface.
type StdLogger struct{}

// Log exposes methods to giving logger to the internal testing.T object.
func (StdLogger) Log(level string, namespace string, function string, message string, items ...interface{}) {
	fmt.Fprintf(os.Stdout, "%s : %s : %s : %s\n", level, namespace, function, fmt.Sprintf(message, items...))
}
