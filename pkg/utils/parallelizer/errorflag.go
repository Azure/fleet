/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package parallelizer

// ErrorFlag is a flag for indicating whether an error has occurred when running tasks in
// parallel with the parallelizer.
type ErrorFlag struct {
	errChan chan error
}

// Raise raises the error flag with an error; if the flag has been raised, it will return
// immediately.
func (f *ErrorFlag) Raise(err error) {
	select {
	case f.errChan <- err:
	default:
	}
}

// Lower lowers the error flag and returns the error that was used to raise the flag; it returns
// nil if the flag has not been raised.
func (f *ErrorFlag) Lower() error {
	select {
	case err := <-f.errChan:
		return err
	default:
		return nil
	}
}

// NewErrorFlag returns an error flag.
func NewErrorFlag() *ErrorFlag {
	return &ErrorFlag{
		errChan: make(chan error, 1),
	}
}
