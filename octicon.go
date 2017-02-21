package octo

// Octicon defines a type which manages all TranmissionProtocol with it's underline
// system.
type Octicon struct {
	system System
	procs  []TransmissionProtocol
}

// New returns a new instance of a Octicon which manages the interaction
// between a series of systems and a series of TranmissionProtocol.
func New(system System) *Octicon {
	return &Octicon{
		system: system,
	}
}

// Use initiates the giving TransmissionProtocol with the internal system manager
// of the oction, add it into it's internal protocol list.
func (o *Octicon) Use(tx TransmissionProtocol) error {
	o.procs = append(o.procs, tx)
	return tx.Listen(o.system)
}

// Close closes all internal protocols and returns the last error received.
func (o *Octicon) Close() error {
	procs := o.procs
	o.procs = nil

	var err error
	for _, procs := range procs {
		err = procs.Close()
	}

	return err
}
