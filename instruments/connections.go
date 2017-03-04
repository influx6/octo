package instruments

import (
	"sync"
	"sync/atomic"

	"github.com/influx6/octo"
)

// ConnectionInstrumentationRecorder defines a struct which implements the octo.ConnectionInstrumentation
// interface, providing concurrency-safe map style recordings of given operations.
type ConnectionInstrumentationRecorder struct {
	collectionLock sync.Mutex
	collected      map[string]octo.ConnectionInstrument
	callable       func(octo.ConnectionInstrument)
}

// NewConnectionInstrumentationRecorder returns a new instance of a ConnectionInstrumentationRecorder.
// It allows provision of a optional function to call when a ConnectionInstrument is added
// or updated.
func NewConnectionInstrumentationRecorder(onUpdate func(octo.ConnectionInstrument)) *ConnectionInstrumentationRecorder {
	var dis ConnectionInstrumentationRecorder
	dis.collected = make(map[string]octo.ConnectionInstrument)
	dis.callable = onUpdate

	return &dis
}

// GetConnectionInstruments returns  a slice of all the Connection instruement stored
func (d *ConnectionInstrumentationRecorder) GetConnectionInstruments() []octo.ConnectionInstrument {
	d.collectionLock.Lock()
	defer d.collectionLock.Unlock()

	var all []octo.ConnectionInstrument

	for _, item := range d.collected {
		all = append(all, item)
	}

	return all
}

// RecordConnectionOp records the giving information as regards Connections based operations.
func (d *ConnectionInstrumentationRecorder) RecordConnectionOp(op octo.ConnectionType, context string, from string, to string, meta map[string]interface{}) {
	instrument := d.getOrAdd(context)

	atomic.AddInt64(&instrument.TotalConnections, 1)

	switch op {
	case octo.ConnectionConnet:
		instrument.Connects = append(instrument.Connects, octo.SingleConnectionInstrument{
			Source: from,
			Target: to,
			Meta:   meta,
		})
	case octo.ConnectionDisconnect:
		instrument.Disconnects = append(instrument.Disconnects, octo.SingleConnectionInstrument{
			Source: from,
			Target: to,
			Meta:   meta,
		})
	case octo.ConnectionAuthenticate:
		instrument.Authentications = append(instrument.Authentications, octo.SingleConnectionInstrument{
			Source: from,
			Target: to,
			Meta:   meta,
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

// getOrAdd retrieves a giving octo.ConnectionInstrument or creating a new object for the giving
// instrument.
func (d *ConnectionInstrumentationRecorder) getOrAdd(context string) octo.ConnectionInstrument {
	d.collectionLock.Lock()
	defer d.collectionLock.Unlock()

	ds, ok := d.collected[context]
	if !ok {
		ds = octo.ConnectionInstrument{Context: context}
		d.collected[context] = ds
	}

	return ds
}
