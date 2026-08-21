package cmd

// PartialCommitError reports that a file replacement became visible at its
// destination, but a later durability step failed. Callers must not retry the
// operation as though nothing was committed.
type PartialCommitError struct {
	Err error
}

func (e *PartialCommitError) Error() string {
	if e == nil || e.Err == nil {
		return "file replacement committed but durability is uncertain"
	}
	return "file replacement committed but durability is uncertain: " + e.Err.Error()
}

func (e *PartialCommitError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Committed lets transaction callers distinguish a committed write warning
// from an error that left the destination unchanged.
func (e *PartialCommitError) Committed() bool { return e != nil }
