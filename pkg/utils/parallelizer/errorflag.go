/*
Copyright 2025 The KubeFleet Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
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
