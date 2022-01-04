package releasecontroller

// TerminalError is a wrapper that indicates the error should be logged but the queue
// key should not be requeued.
type TerminalError struct {
	error
}

func CreateTerminalError(err error) error {
	return TerminalError{err}
}

func IsTerminalError(err error) bool {
	if _, ok := err.(TerminalError); ok {
		return true
	} else {
		return false
	}
}
