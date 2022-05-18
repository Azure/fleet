package utils

import "k8s.io/client-go/tools/record"

const (
	// TestCaseMsg is used in the table driven test
	TestCaseMsg string = "\nTest case:  %s"
)

func NewFakeRecorder(bufferSize int) *record.FakeRecorder {
	recorder := record.NewFakeRecorder(bufferSize)
	recorder.IncludeObject = true
	return recorder
}
