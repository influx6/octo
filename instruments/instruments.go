package instruments

import "github.com/influx6/octo"

// InstrumentAttr defines an struct for configuring the function passed to the
// created instruemental objects.
type InstrumentAttr struct {
	Log                octo.Logs // optional value as StdLogger will be used instead if nil.
	OnDataUpdate       func(octo.DataInstrument)
	OnGoroutineUpdate  func(octo.GoroutineInstrument)
	OnConnectionUpdate func(octo.ConnectionInstrument)
}

// Instrument returns a new Insturmentation object based on the InstrumentationBase
// struct and passes the giving functions from the InstrumentAttr as the function
// to be called for each instrumentation.
func Instrument(attr InstrumentAttr) octo.Instrumentation {
	if attr.Log == nil {
		attr.Log = &StdLogger{}
	}

	return InstrumentationBase{
		Logs:                      attr.Log,
		DataInstrumentation:       NewDataInstrumentationRecorder(attr.OnDataUpdate),
		GoRoutineInstrumentation:  NewGoroutineInstrumentationRecorder(attr.OnGoroutineUpdate),
		ConnectionInstrumentation: NewConnectionInstrumentationRecorder(attr.OnConnectionUpdate),
	}
}

// NewInstrumentation returns a new InstrumentationBase instance which unifies
// the instrumentation provides for providing a unified instrumentation object.
func NewInstrumentation(log octo.Logs, datas octo.DataInstrumentation, gor octo.GoRoutineInstrumentation, conns octo.ConnectionInstrumentation) octo.Instrumentation {
	return InstrumentationBase{
		Logs:                      log,
		DataInstrumentation:       datas,
		GoRoutineInstrumentation:  gor,
		ConnectionInstrumentation: conns,
	}
}

// InstrumentationBase defines a struct which provides a implementation for the
// Instrumentation interface.
type InstrumentationBase struct {
	octo.Logs
	octo.DataInstrumentation
	octo.GoRoutineInstrumentation
	octo.ConnectionInstrumentation
}
