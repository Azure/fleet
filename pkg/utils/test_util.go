package utils

import (
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
)

const (
	// TestCaseMsg is used in the table driven test
	TestCaseMsg string = "\nTest case:  %s"
)

func NewFakeRecorder(bufferSize int) *record.FakeRecorder {
	recorder := record.NewFakeRecorder(bufferSize)
	recorder.IncludeObject = true
	return recorder
}

// GetEventString get the exact string literal of the event created by the fake event library.
func GetEventString(object runtime.Object, eventtype, reason, messageFmt string, args ...interface{}) string {
	return fmt.Sprintf(eventtype+" "+reason+" "+messageFmt, args...) +
		fmt.Sprintf(" involvedObject{kind=%s,apiVersion=%s}",
			object.GetObjectKind().GroupVersionKind().Kind, object.GetObjectKind().GroupVersionKind().GroupVersion())
}
