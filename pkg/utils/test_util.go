/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package utils

import (
	"fmt"
	"os"
	"strings"

	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/onsi/gomega/format"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
)

var (
	genericCodecs = serializer.NewCodecFactory(scheme.Scheme)
	genericCodec  = genericCodecs.UniversalDeserializer()

	// IgnoreConditionLTTAndMessageFields is a cmpopts.IgnoreFields that ignores the LastTransitionTime and Message fields
	IgnoreConditionLTTAndMessageFields = cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime", "Message")
)

const (
	// TestCaseMsg is used in the table driven test
	TestCaseMsg string = "\nTest case:  %s"
)

// NewFakeRecorder makes a new fake event recorder that prints the object.
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

// GetObjectFromRawExtension returns an object decoded from the raw byte array.
func GetObjectFromRawExtension(rawByte []byte, obj runtime.Object) error {
	json, err := yaml.ToJSON(rawByte)
	if err != nil {
		return err
	}
	return runtime.DecodeInto(genericCodec, json, obj)
}

// GetObjectFromManifest returns a runtime object decoded from the file.
func GetObjectFromManifest(relativeFilePath string, obj runtime.Object) error {
	// Read files, create manifest
	fileRaw, err := os.ReadFile(relativeFilePath)
	if err != nil {
		return err
	}
	return GetObjectFromRawExtension(fileRaw, obj)
}

// NewResourceList returns a resource list for test purpose.
func NewResourceList() v1.ResourceList {
	return v1.ResourceList{
		v1.ResourceCPU:    resource.MustParse("100m"),
		v1.ResourceMemory: resource.MustParse("5Gi"),
	}
}

// NewTestNodes return a set of nodes for test purpose. Those nodes have random names and capacities/ allocatable.
func NewTestNodes(ns string) []v1.Node {
	numOfNodes := RandSecureInt(10)
	var nodes []v1.Node
	for i := int64(0); i < numOfNodes; i++ {
		node := v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "rand-" + strings.ToLower(RandStr()) + "-node",
				Namespace: ns,
			},
			Status: v1.NodeStatus{
				Allocatable: NewResourceList(), Capacity: NewResourceList()},
		}
		nodes = append(nodes, node)
	}
	return nodes
}

// NotFoundMatcher matches the error to be not found.
type NotFoundMatcher struct {
}

// Match matches the api error.
func (matcher NotFoundMatcher) Match(actual interface{}) (success bool, err error) {
	if actual == nil {
		return false, nil
	}
	actualError := actual.(error)
	return apierrors.IsNotFound(actualError), nil
}

// FailureMessage builds an error message.
func (matcher NotFoundMatcher) FailureMessage(actual interface{}) (message string) {
	return format.Message(actual, "to be not found")
}

// NegatedFailureMessage builds an error message.
func (matcher NotFoundMatcher) NegatedFailureMessage(actual interface{}) (message string) {
	return format.Message(actual, "to be found")
}

// AlreadyExistMatcher matches the error to be already exist
type AlreadyExistMatcher struct {
}

// Match matches error.
func (matcher AlreadyExistMatcher) Match(actual interface{}) (success bool, err error) {
	if actual == nil {
		return false, nil
	}
	actualError := actual.(error)
	return apierrors.IsAlreadyExists(actualError), nil
}

// FailureMessage builds an error message.
func (matcher AlreadyExistMatcher) FailureMessage(actual interface{}) (message string) {
	return format.Message(actual, "to be already exist")
}

// NegatedFailureMessage builds an error message.
func (matcher AlreadyExistMatcher) NegatedFailureMessage(actual interface{}) (message string) {
	return format.Message(actual, "not to be already exist")
}
