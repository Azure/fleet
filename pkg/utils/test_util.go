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

package utils

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/onsi/gomega/format"
	"google.golang.org/protobuf/encoding/protojson"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"

	computev1 "go.goms.io/fleet/apis/protos/azure/compute/v1"
	"go.goms.io/fleet/pkg/clients/httputil"
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

// TestMapper interface for abstract class.
type TestMapper struct {
	meta.RESTMapper
}

func (m TestMapper) RESTMapping(gk schema.GroupKind, _ ...string) (*meta.RESTMapping, error) {
	if gk.Kind == "ClusterRole" {
		return &meta.RESTMapping{
			Resource:         ClusterRoleGVR,
			GroupVersionKind: ClusterRoleGVK,
			Scope:            nil,
		}, nil
	}
	if gk.Kind == "Deployment" {
		return &meta.RESTMapping{
			Resource:         DeploymentGVR,
			GroupVersionKind: DeploymentGVK,
			Scope:            nil,
		}, nil
	}
	return nil, errors.New("test error: mapping does not exist")
}

// CreateMockAttributeBasedVMSizeRecommenderServer creates a mock HTTP server for testing AttributeBasedVMSizeRecommenderClient.
func CreateMockAttributeBasedVMSizeRecommenderServer(t *testing.T, httpStatusCode int) *httptest.Server {
	// Create mock server
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request method
		if r.Method != http.MethodPost {
			t.Errorf("Mock PropertyChecker method () = %s, want POST request", r.Method)
		}

		// Verify headers
		if r.Header.Get(httputil.HeaderContentTypeKey) != httputil.HeaderContentTypeJSON {
			t.Errorf("Mock PropertyChecker content () = %s, want %s", r.Header.Get(httputil.HeaderContentTypeKey), httputil.HeaderContentTypeJSON)
		}
		if r.Header.Get(httputil.HeaderAcceptKey) != httputil.HeaderContentTypeJSON {
			t.Errorf("Mock PropertyChecker accept () = %s, want %s", r.Header.Get(httputil.HeaderAcceptKey), httputil.HeaderContentTypeJSON)
		}

		// Verify request body using proto json for proper proto3 one of support
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("failed to read request body: %v", err)
		}
		var req computev1.GenerateAttributeBasedRecommendationsRequest
		unmarshaler := protojson.UnmarshalOptions{
			DiscardUnknown: true,
		}
		if err := unmarshaler.Unmarshal(body, &req); err != nil {
			t.Fatalf("failed to unmarshal request body: %v", err)
		}

		// Write mock response with status code from test case
		if httpStatusCode == 0 {
			httpStatusCode = http.StatusOK
		}
		w.Header().Set(httputil.HeaderContentTypeKey, httputil.HeaderContentTypeJSON)
		w.WriteHeader(httpStatusCode)

		// Mock the expected response from the Azure API.
		mockAzureResponse := `{
					"recommendedVmSizes": {
						"regularVmSizes": [
							{
								"family": "Dsv3",
								"name": "Standard_D2s_v3",
								"size": "D2"
							},
							{
								"family": "Standard",
								"name": "Standard_B1s",
								"size": "Standard_B1s"
							}
						]
					}
				}`

		if _, err := w.Write([]byte(mockAzureResponse)); err != nil {
			t.Fatalf("failed to write mock response: %v", err)
		}
	}))
}
