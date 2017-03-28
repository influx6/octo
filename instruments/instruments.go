package instruments

import (
	"fmt"
	"os"

	"github.com/influx6/octo"
)

//================================================================================

type instruments struct {
	octo.Logs
	octo.Events
}

// Instruments returns a giving struct which implements the octo.Insturmentation
// interface and provides logging and event delivery capabilities.
func Instruments(logger octo.Logs, events octo.Events) octo.Instrumentation {
	if logger == nil {
		logger = &StdLogger{}
	}

	if events == nil {
		events = &EventDelivery{}
	}

	return &instruments{
		Logs:   logger,
		Events: events,
	}
}

//================================================================================

// StdLogger defines a structure that implements the octo.Log interface.
type StdLogger struct{}

// Log exposes methods to giving logger to the internal testing.T object.
func (StdLogger) Log(level string, namespace string, function string, message string, items ...interface{}) {
	fmt.Fprintf(os.Stdout, "%s : %s : %s : %s\n", level, namespace, function, fmt.Sprintf(message, items...))
}

//================================================================================

// EventDelivery defines a struct which exposes a method to add events to the under
// line slice.
type EventDelivery struct{}

// NotifyEvent adds the provided events into the underline map.
func (e *EventDelivery) NotifyEvent(ev octo.Event) error {
	return nil
}
