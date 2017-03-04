package instruments

import (
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/influx6/octo"
)

// GoroutineInstrumentationRecorder defines a struct which implements the octo.GoroutineInstrumentation
// interface, providing concurrency-safe map style recordings of given operations.
type GoroutineInstrumentationRecorder struct {
	collectionLock sync.Mutex
	collected      map[string]octo.GoroutineInstrument
	callable       func(octo.GoroutineInstrument)
}

// NewGoroutineInstrumentationRecorder returns a new instance of a GoroutineInstrumentationRecorder.
// It allows provision of a optional function to call when a GoroutineInstrument is added
// or updated.
func NewGoroutineInstrumentationRecorder(onUpdate func(octo.GoroutineInstrument)) *GoroutineInstrumentationRecorder {
	var dis GoroutineInstrumentationRecorder
	dis.collected = make(map[string]octo.GoroutineInstrument)
	dis.callable = onUpdate

	return &dis
}

// GetGoroutineInstruments returns  a slice of all the Goroutine instruement stored
func (d *GoroutineInstrumentationRecorder) GetGoroutineInstruments() []octo.GoroutineInstrument {
	d.collectionLock.Lock()
	defer d.collectionLock.Unlock()

	var all []octo.GoroutineInstrument

	for _, item := range d.collected {
		all = append(all, item)
	}

	return all
}

// RecordGoroutineOp records the giving information as regards Goroutines based operations.
func (d *GoroutineInstrumentationRecorder) RecordGoroutineOp(op octo.GoroutineOpType, context string, meta map[string]interface{}) {
	instrument := d.getOrAdd(context)

	atomic.AddInt64(&instrument.Total, 1)

	switch op {
	case octo.GoroutineOpened:

		_, file, line, _ := runtime.Caller(1)

		trace := make([]byte, 1<<16)
		trace = trace[:runtime.Stack(trace, true)]

		instrument.Opened = append(instrument.Opened, octo.SingleGoroutineInstrument{
			Time:  time.Now(),
			Meta:  meta,
			Stack: trace,
			File:  file,
			Line:  line,
		})

	case octo.GoroutineClosed:

		_, file, line, _ := runtime.Caller(1)

		trace := make([]byte, 1<<16)
		trace = trace[:runtime.Stack(trace, true)]

		instrument.Closed = append(instrument.Closed, octo.SingleGoroutineInstrument{
			Time:  time.Now(),
			Meta:  meta,
			Stack: trace,
			File:  file,
			Line:  line,
		})
	}

	d.collectionLock.Lock()
	{
		d.collected[context] = instrument
	}
	d.collectionLock.Unlock()

	if d.callable != nil {
		d.callable(instrument)
	}
}

// getOrAdd retrieves a giving octo.GoroutineInstrument or creating a new object for the giving
// instrument.
func (d *GoroutineInstrumentationRecorder) getOrAdd(context string) octo.GoroutineInstrument {
	d.collectionLock.Lock()
	defer d.collectionLock.Unlock()

	ds, ok := d.collected[context]
	if !ok {
		ds = octo.GoroutineInstrument{Context: context}
		d.collected[context] = ds
	}

	return ds
}
