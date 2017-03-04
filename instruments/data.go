package instruments

import (
	"sync"
	"sync/atomic"

	"github.com/influx6/octo"
)

// DataInstrumentationRecorder defines a struct which implements the octo.DataInstrumentation
// interface, providing concurrency-safe map style recordings of given operations.
type DataInstrumentationRecorder struct {
	collectionLock sync.Mutex
	collected      map[string]octo.DataInstrument
	callable       func(octo.DataInstrument)
}

// NewDataInstrumentationRecorder returns a new instance of a DataInstrumentationRecorder.
// It allows provision of a optional function to call when a DataInstrument is added
// or updated.
func NewDataInstrumentationRecorder(onUpdate func(octo.DataInstrument)) *DataInstrumentationRecorder {
	var dis DataInstrumentationRecorder
	dis.collected = make(map[string]octo.DataInstrument)
	dis.callable = onUpdate

	return &dis
}

// GetDataInstruments returns  a slice of all the data instruement stored
func (d *DataInstrumentationRecorder) GetDataInstruments() []octo.DataInstrument {
	d.collectionLock.Lock()
	defer d.collectionLock.Unlock()

	var all []octo.DataInstrument

	for _, item := range d.collected {
		all = append(all, item)
	}

	return all
}

// RecordDataOp records the giving information as regards data reads/write operations.
func (d *DataInstrumentationRecorder) RecordDataOp(op octo.DataOpType, context string, err error, data []byte, meta map[string]interface{}) {
	instrument := d.getOrAdd(context)

	switch op {
	case octo.DataRead:
		atomic.AddInt64(&instrument.TotalReads, 1)
		instrument.Reads = append(instrument.Reads, octo.SingleDataInstrument{
			Data:  data,
			Error: err,
			Meta:  meta,
		})

	case octo.DataTransform:
		atomic.AddInt64(&instrument.TotalTransforms, 1)
		instrument.Transforms = append(instrument.Transforms, octo.SingleDataInstrument{
			Data:  data,
			Error: err,
			Meta:  meta,
		})

	case octo.DataWrite:
		atomic.AddInt64(&instrument.TotalWrites, 1)
		instrument.Writes = append(instrument.Writes, octo.SingleDataInstrument{
			Data:  data,
			Error: err,
			Meta:  meta,
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

// getOrAdd retrieves a giving octo.DataInstrument or creating a new object for the giving
// instrument.
func (d *DataInstrumentationRecorder) getOrAdd(context string) octo.DataInstrument {
	d.collectionLock.Lock()
	defer d.collectionLock.Unlock()

	ds, ok := d.collected[context]
	if !ok {
		ds = octo.DataInstrument{Context: context}
		d.collected[context] = ds
	}

	return ds
}
