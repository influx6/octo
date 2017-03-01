package octo

// InstrumentationBase defines a struct which provides a implementation for the
// Instrumentation interface.
type InstrumentationBase struct {
	Logs
	TimeInstrumentation
	EventInstrumentation
}

// NewInstrumentationBase returns a new InstrumentationBase instance which unifies
// the instrumentation provides for providing a unified instrumentation object.
func NewInstrumentationBase(log Logs, times TimeInstrumentation, events EventInstrumentation) InstrumentationBase {
	return InstrumentationBase{
		Logs:                 log,
		TimeInstrumentation:  times,
		EventInstrumentation: events,
	}
}
