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

package v1beta1

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/propertyprovider"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
)

const (
	exampleClusterPropertyName             = "example"
	exampleClusterPropertyValue            = "2"
	examplePropertyProviderCondition       = "ExampleConditionType"
	examplePropertyProviderConditionStatus = metav1.ConditionTrue
	examplePropertyProviderReason          = "ExampleReason"
	examplePropertyProviderMessage         = "ExampleMessage"

	imcName = "imc-1"

	nodeName1 = "node-1"
	nodeName2 = "node-2"
	nodeName3 = "node-3"

	podName1 = "pod-1"
	podName2 = "pod-2"
	podName3 = "pod-3"
	podName4 = "pod-4"
	podName5 = "pod-5"

	containerName1 = "container-1"
	containerName2 = "container-2"
)

var (
	ignoreLTTConditionField = cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime")
	ignoreAllTimeFields     = cmpopts.IgnoreTypes(time.Time{}, metav1.Time{})

	sortByConditionType = cmpopts.SortSlices(func(a, b metav1.Condition) bool {
		return a.Type < b.Type
	})
)

func TestMarkInternalMemberClusterJoined(t *testing.T) {
	r := Reconciler{recorder: utils.NewFakeRecorder(1)}
	internalMemberCluster := &clusterv1beta1.InternalMemberCluster{}

	r.markInternalMemberClusterJoined(internalMemberCluster)

	// check that the correct event is emitted
	event := <-r.recorder.(*record.FakeRecorder).Events
	expected := utils.GetEventString(internalMemberCluster, corev1.EventTypeNormal, EventReasonInternalMemberClusterJoined, "internal member cluster joined")
	assert.Equal(t, expected, event, utils.TestCaseMsg, "TestMarkInternalMemberClusterJoined")

	// Check expected condition.
	expectedCondition := metav1.Condition{Type: string(clusterv1beta1.AgentJoined), Status: metav1.ConditionTrue, Reason: EventReasonInternalMemberClusterJoined}
	actualCondition := internalMemberCluster.GetConditionWithType(clusterv1beta1.MemberAgent, expectedCondition.Type)
	assert.Equal(t, "", cmp.Diff(expectedCondition, *(actualCondition), cmpopts.IgnoreTypes(time.Time{})), utils.TestCaseMsg, "TestMarkInternalMemberClusterJoined")
}

func TestMarkInternalMemberClusterLeft(t *testing.T) {
	r := Reconciler{recorder: utils.NewFakeRecorder(1)}
	internalMemberCluster := &clusterv1beta1.InternalMemberCluster{}

	r.markInternalMemberClusterLeft(internalMemberCluster)

	// check that the correct event is emitted
	event := <-r.recorder.(*record.FakeRecorder).Events
	expected := utils.GetEventString(internalMemberCluster, corev1.EventTypeNormal, EventReasonInternalMemberClusterLeft, "internal member cluster left")
	assert.Equal(t, expected, event, utils.TestCaseMsg, "TestMarkInternalMemberClusterLeft")

	// Check expected conditions.
	expectedCondition := metav1.Condition{Type: string(clusterv1beta1.AgentJoined), Status: metav1.ConditionFalse, Reason: EventReasonInternalMemberClusterLeft}
	actualCondition := internalMemberCluster.GetConditionWithType(clusterv1beta1.MemberAgent, expectedCondition.Type)
	assert.Equal(t, "", cmp.Diff(expectedCondition, *(actualCondition), cmpopts.IgnoreTypes(time.Time{})), utils.TestCaseMsg, "TestMarkInternalMemberClusterLeft")
}

func TestUpdateMemberAgentHeartBeat(t *testing.T) {
	internalMemberCluster := &clusterv1beta1.InternalMemberCluster{}

	updateMemberAgentHeartBeat(internalMemberCluster)
	lastReceivedHeartBeat := internalMemberCluster.Status.AgentStatus[0].LastReceivedHeartbeat
	assert.NotNil(t, lastReceivedHeartBeat)

	updateMemberAgentHeartBeat(internalMemberCluster)
	newLastReceivedHeartBeat := internalMemberCluster.Status.AgentStatus[0].LastReceivedHeartbeat
	assert.NotEqual(t, lastReceivedHeartBeat, newLastReceivedHeartBeat)
}

func TestMarkInternalMemberClusterHealthy(t *testing.T) {
	r := Reconciler{recorder: utils.NewFakeRecorder(1)}
	internalMemberCluster := &clusterv1beta1.InternalMemberCluster{}

	r.markInternalMemberClusterHealthy(internalMemberCluster)

	// check that the correct event is emitted
	event := <-r.recorder.(*record.FakeRecorder).Events
	expected := utils.GetEventString(internalMemberCluster, corev1.EventTypeNormal, EventReasonInternalMemberClusterHealthy, "internal member cluster healthy")
	assert.Equal(t, expected, event, utils.TestCaseMsg, "TestMarkInternalMemberClusterHealthy")

	// Check expected conditions.
	expectedCondition := metav1.Condition{Type: string(clusterv1beta1.AgentHealthy), Status: metav1.ConditionTrue, Reason: EventReasonInternalMemberClusterHealthy}
	actualCondition := internalMemberCluster.GetConditionWithType(clusterv1beta1.MemberAgent, expectedCondition.Type)
	assert.Equal(t, "", cmp.Diff(expectedCondition, *(actualCondition), cmpopts.IgnoreTypes(time.Time{})), utils.TestCaseMsg, "TestMarkInternalMemberClusterHealthy")
}

func TestMarkInternalMemberClusterHeartbeatUnhealthy(t *testing.T) {
	internalMemberCluster := &clusterv1beta1.InternalMemberCluster{}
	err := errors.New("rand-err-msg")
	r := Reconciler{recorder: utils.NewFakeRecorder(1)}

	r.markInternalMemberClusterUnhealthy(internalMemberCluster, err)

	// check that the correct event is emitted
	event := <-r.recorder.(*record.FakeRecorder).Events
	expected := utils.GetEventString(internalMemberCluster, corev1.EventTypeWarning, EventReasonInternalMemberClusterUnhealthy, "internal member cluster unhealthy")
	assert.Equal(t, expected, event, utils.TestCaseMsg, "TestMarkInternalMemberClusterHeartbeatUnhealthy")

	// Check expected conditions.
	expectedCondition := metav1.Condition{Type: string(clusterv1beta1.AgentHealthy), Status: metav1.ConditionFalse, Reason: EventReasonInternalMemberClusterUnhealthy, Message: "rand-err-msg"}
	actualCondition := internalMemberCluster.GetConditionWithType(clusterv1beta1.MemberAgent, expectedCondition.Type)
	assert.Equal(t, "", cmp.Diff(expectedCondition, *(actualCondition), cmpopts.IgnoreTypes(time.Time{})), utils.TestCaseMsg, "TestMarkInternalMemberClusterHeartbeatUnhealthy")
}

func TestUpdateInternalMemberClusterWithRetry(t *testing.T) {
	lessRetriesForRetriable := 0
	lessRetriesForNonRetriable := 0
	moreRetriesForRetriable := 0

	testCases := map[string]struct {
		r                     *Reconciler
		internalMemberCluster *clusterv1beta1.InternalMemberCluster
		retries               int
		wantRetries           int
		wantErr               error
	}{
		"succeed without retries if no errors": {
			retries: 0,
			r: &Reconciler{hubClient: &test.MockClient{
				MockStatusUpdate: func(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
					return nil
				}}},
			internalMemberCluster: &clusterv1beta1.InternalMemberCluster{},
			wantErr:               nil,
		},
		"succeed with retries for retriable errors: TooManyRequests": {
			r: &Reconciler{hubClient: &test.MockClient{
				MockStatusUpdate: func(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
					lessRetriesForRetriable++
					if lessRetriesForRetriable >= 3 {
						return nil
					}
					return apierrors.NewTooManyRequests("", 0)
				}}},
			internalMemberCluster: &clusterv1beta1.InternalMemberCluster{
				Spec: clusterv1beta1.InternalMemberClusterSpec{HeartbeatPeriodSeconds: int32(3)},
			},
			wantErr: nil,
		},
		"fail without retries for non-retriable errors: Invalid": {
			r: &Reconciler{hubClient: &test.MockClient{
				MockStatusUpdate: func(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
					lessRetriesForNonRetriable++
					if lessRetriesForNonRetriable >= 3 {
						return nil
					}
					return apierrors.NewInvalid(schema.GroupKind{}, "", field.ErrorList{})
				}}},
			internalMemberCluster: &clusterv1beta1.InternalMemberCluster{},
			wantErr:               apierrors.NewInvalid(schema.GroupKind{}, "", field.ErrorList{}),
		},
		"fail if too many retries for retirable errors: TooManyRequests": {
			r: &Reconciler{hubClient: &test.MockClient{
				MockStatusUpdate: func(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
					moreRetriesForRetriable++
					if moreRetriesForRetriable >= 100 {
						return nil
					}
					return apierrors.NewTooManyRequests("", 0)
				}}},
			internalMemberCluster: &clusterv1beta1.InternalMemberCluster{},
			wantErr:               apierrors.NewTooManyRequests("", 0),
		},
	}

	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			err := testCase.r.updateInternalMemberClusterWithRetry(context.Background(), testCase.internalMemberCluster)
			assert.Equal(t, testCase.wantErr, err, utils.TestCaseMsg, testName)
		})
	}
}

func TestSetConditionWithType(t *testing.T) {
	testCases := map[string]struct {
		internalMemberCluster *clusterv1beta1.InternalMemberCluster
		condition             metav1.Condition
		wantedAgentStatus     *clusterv1beta1.AgentStatus
	}{
		"Agent Status array is empty": {
			internalMemberCluster: &clusterv1beta1.InternalMemberCluster{},
			condition: metav1.Condition{
				Type:   string(clusterv1beta1.AgentJoined),
				Status: metav1.ConditionTrue,
				Reason: EventReasonInternalMemberClusterJoined,
			},
			wantedAgentStatus: &clusterv1beta1.AgentStatus{
				Type: clusterv1beta1.MemberAgent,
				Conditions: []metav1.Condition{
					{
						Type:   string(clusterv1beta1.AgentJoined),
						Status: metav1.ConditionTrue,
						Reason: EventReasonInternalMemberClusterJoined,
					},
				},
			},
		},
		"Agent Status array is non-empty": {
			internalMemberCluster: &clusterv1beta1.InternalMemberCluster{
				Status: clusterv1beta1.InternalMemberClusterStatus{
					AgentStatus: []clusterv1beta1.AgentStatus{
						{
							Type:       clusterv1beta1.MultiClusterServiceAgent,
							Conditions: []metav1.Condition{},
						},
					},
				},
			},
			condition: metav1.Condition{
				Type:   string(clusterv1beta1.AgentJoined),
				Status: metav1.ConditionTrue,
				Reason: EventReasonInternalMemberClusterJoined,
			},
			wantedAgentStatus: &clusterv1beta1.AgentStatus{
				Type: clusterv1beta1.MemberAgent,
				Conditions: []metav1.Condition{
					{
						Type:   string(clusterv1beta1.AgentJoined),
						Status: metav1.ConditionTrue,
						Reason: EventReasonInternalMemberClusterJoined,
					},
				},
			},
		},
		"Agent Status exists within Internal member cluster": {
			internalMemberCluster: &clusterv1beta1.InternalMemberCluster{
				Status: clusterv1beta1.InternalMemberClusterStatus{
					AgentStatus: []clusterv1beta1.AgentStatus{
						{
							Type: clusterv1beta1.MemberAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(clusterv1beta1.AgentJoined),
									Status: metav1.ConditionTrue,
									Reason: EventReasonInternalMemberClusterJoined,
								},
							},
						},
					},
				},
			},
			condition: metav1.Condition{
				Type:   string(clusterv1beta1.AgentHealthy),
				Status: metav1.ConditionTrue,
				Reason: EventReasonInternalMemberClusterHealthy,
			},
			wantedAgentStatus: &clusterv1beta1.AgentStatus{
				Type: clusterv1beta1.MemberAgent,
				Conditions: []metav1.Condition{
					{
						Type:   string(clusterv1beta1.AgentJoined),
						Status: metav1.ConditionTrue,
						Reason: EventReasonInternalMemberClusterJoined,
					},
					{
						Type:   string(clusterv1beta1.AgentHealthy),
						Status: metav1.ConditionTrue,
						Reason: EventReasonInternalMemberClusterHealthy,
					},
				},
			},
		},
	}

	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			testCase.internalMemberCluster.SetConditionsWithType(clusterv1beta1.MemberAgent, testCase.condition)
			assert.Equal(t, "", cmp.Diff(testCase.wantedAgentStatus, testCase.internalMemberCluster.GetAgentStatus(clusterv1beta1.MemberAgent), cmpopts.IgnoreTypes(time.Time{})))
		})
	}
}

func TestGetConditionWithType(t *testing.T) {
	testCases := map[string]struct {
		internalMemberCluster *clusterv1beta1.InternalMemberCluster
		conditionType         string
		wantedCondition       *metav1.Condition
	}{
		"Condition exists": {
			internalMemberCluster: &clusterv1beta1.InternalMemberCluster{
				Status: clusterv1beta1.InternalMemberClusterStatus{
					AgentStatus: []clusterv1beta1.AgentStatus{
						{
							Type: clusterv1beta1.MemberAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(clusterv1beta1.ConditionTypeMemberClusterJoined),
									Status: metav1.ConditionTrue,
									Reason: EventReasonInternalMemberClusterJoined,
								},
							},
						},
					},
				},
			},
			conditionType: string(clusterv1beta1.AgentJoined),
			wantedCondition: &metav1.Condition{
				Type:   string(clusterv1beta1.ConditionTypeMemberClusterJoined),
				Status: metav1.ConditionTrue,
				Reason: EventReasonInternalMemberClusterJoined,
			},
		},
		"Condition doesn't exist": {
			internalMemberCluster: &clusterv1beta1.InternalMemberCluster{
				Status: clusterv1beta1.InternalMemberClusterStatus{
					AgentStatus: []clusterv1beta1.AgentStatus{
						{
							Type: clusterv1beta1.MemberAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(clusterv1beta1.ConditionTypeMemberClusterJoined),
									Status: metav1.ConditionTrue,
									Reason: EventReasonInternalMemberClusterJoined,
								},
							},
						},
					},
				},
			},
			conditionType:   string(clusterv1beta1.AgentHealthy),
			wantedCondition: nil,
		},
		"Agent Status doesn't exist": {
			internalMemberCluster: &clusterv1beta1.InternalMemberCluster{
				Status: clusterv1beta1.InternalMemberClusterStatus{},
			},
			conditionType:   string(clusterv1beta1.AgentJoined),
			wantedCondition: nil,
		},
	}

	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			actualCondition := testCase.internalMemberCluster.GetConditionWithType(clusterv1beta1.MemberAgent, testCase.conditionType)
			assert.Equal(t, testCase.wantedCondition, actualCondition)
		})
	}
}

// noReturnProvider is a property provider that does not return any properties until the
// hold channel is closed.
type noReturnProvider struct {
	hold <-chan struct{}
}

var _ propertyprovider.PropertyProvider = &noReturnProvider{}

func (p *noReturnProvider) Start(_ context.Context, _ *rest.Config) error {
	return nil
}

func (p *noReturnProvider) Collect(ctx context.Context) propertyprovider.PropertyCollectionResponse {
	<-ctx.Done()
	return propertyprovider.PropertyCollectionResponse{}
}

// TestReportClusterPropertiesWithPropertyProviderTooManyCalls tests
// the reportClusterPropertiesWithPropertyProvider method, specifically when the property provider
// receives too many calls.
func TestReportClusterPropertiesWithPropertyProviderTooManyCalls(t *testing.T) {
	h := make(chan struct{})
	nrpp := &noReturnProvider{hold: h}

	testCases := []struct {
		name    string
		imc     *clusterv1beta1.InternalMemberCluster
		wantIMC *clusterv1beta1.InternalMemberCluster
	}{
		{
			name: "too many calls",
			imc: &clusterv1beta1.InternalMemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: imcName,
				},
			},
			wantIMC: &clusterv1beta1.InternalMemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: imcName,
				},
				Status: clusterv1beta1.InternalMemberClusterStatus{
					Conditions: []metav1.Condition{
						{
							Type:    string(clusterv1beta1.ConditionTypeClusterPropertyCollectionSucceeded),
							Status:  metav1.ConditionFalse,
							Reason:  ClusterPropertyCollectionFailedTooManyCallsReason,
							Message: ClusterPropertyCollectionFailedTooManyCallsMessage,
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			r := &Reconciler{
				propertyProviderCfg: &propertyProviderConfig{
					propertyProvider: nrpp,
				},
				recorder: utils.NewFakeRecorder(maxQueuedPropertyCollectionCalls + 1),
			}
			for i := 0; i < maxQueuedPropertyCollectionCalls; i++ {
				// Invoke the method with no expectations for returns.
				go func() {
					r.reportClusterPropertiesWithPropertyProvider(ctx, tc.imc.DeepCopy()) //nolint:all
					// Linting is disabled for this line as we are discarding the returned error intentionally.
				}()
			}

			for {
				// Wait for the prev. calls to get queued.
				if r.propertyProviderCfg.queuedPropertyCollectionCalls.Load() == int32(maxQueuedPropertyCollectionCalls) {
					break
				}
			}

			if err := r.reportClusterPropertiesWithPropertyProvider(ctx, tc.imc); err == nil {
				t.Fatalf("reportClusterPropertiesWithPropertyProvider(), got no error, want error")
			}

			if diff := cmp.Diff(tc.wantIMC, tc.imc, ignoreLTTConditionField); diff != "" {
				t.Fatalf("internalMemberCluster, (-got, +want):\n%s", diff)
			}

			// Unblock the stuck goroutines.
			close(h)
		})
	}
}

// TestReportClusterPropertiesWithPropertyProviderTimedOut tests the
// reportClusterPropertiesWithPropertyProvider method, specifically when the property provider
// times out.
func TestReportClusterPropertiesWithPropertyProviderTimedOut(t *testing.T) {
	h := make(chan struct{})
	nrpp := &noReturnProvider{hold: h}

	testCases := []struct {
		name    string
		imc     *clusterv1beta1.InternalMemberCluster
		wantIMC *clusterv1beta1.InternalMemberCluster
	}{
		{
			name: "timed out",
			imc: &clusterv1beta1.InternalMemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: imcName,
				},
			},
			wantIMC: &clusterv1beta1.InternalMemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: imcName,
				},
				Status: clusterv1beta1.InternalMemberClusterStatus{
					Conditions: []metav1.Condition{
						{
							Type:    string(clusterv1beta1.ConditionTypeClusterPropertyCollectionSucceeded),
							Status:  metav1.ConditionFalse,
							Reason:  ClusterPropertyCollectionTimedOutReason,
							Message: ClusterPropertyCollectionTimedOutMessage,
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			r := &Reconciler{
				propertyProviderCfg: &propertyProviderConfig{
					propertyProvider: nrpp,
				},
				recorder: utils.NewFakeRecorder(1),
			}

			if err := r.reportClusterPropertiesWithPropertyProvider(ctx, tc.imc); err == nil {
				t.Fatalf("reportClusterPropertiesWithPropertyProvider(), got no error, want error")
			}

			if diff := cmp.Diff(tc.wantIMC, tc.imc, ignoreLTTConditionField); diff != "" {
				t.Fatalf("internalMemberCluster, (-got, +want):\n%s", diff)
			}

			// Unblock the stuck property provider.
			close(h)
		})
	}
}

// dummyProvider is a property provider that returns some static properties for testing
// purposes.
type dummyProvider struct{}

var _ propertyprovider.PropertyProvider = &dummyProvider{}

func (p *dummyProvider) Start(_ context.Context, _ *rest.Config) error {
	return nil
}

func (p *dummyProvider) Collect(_ context.Context) propertyprovider.PropertyCollectionResponse {
	return propertyprovider.PropertyCollectionResponse{
		Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
			exampleClusterPropertyName: {
				Value: exampleClusterPropertyValue,
			},
		},
		Resources: clusterv1beta1.ResourceUsage{
			Capacity: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10"),
				corev1.ResourceMemory: resource.MustParse("10Gi"),
			},
			Allocatable: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("5"),
				corev1.ResourceMemory: resource.MustParse("5Gi"),
			},
			Available: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("5"),
				corev1.ResourceMemory: resource.MustParse("5Gi"),
			},
		},
		Conditions: []metav1.Condition{
			{
				Type:    examplePropertyProviderCondition,
				Status:  examplePropertyProviderConditionStatus,
				Reason:  examplePropertyProviderReason,
				Message: examplePropertyProviderMessage,
			},
		},
	}
}

// TestReportClusterPropertiesWithPropertyProvider tests the reportClusterPropertiesWithPropertyProvider method.
func TestReportClusterPropertiesWithPropertyProvider(t *testing.T) {
	imcGeneration := 10

	testCases := []struct {
		name    string
		imc     *clusterv1beta1.InternalMemberCluster
		wantIMC *clusterv1beta1.InternalMemberCluster
	}{
		{
			name: "property collection successful",
			imc: &clusterv1beta1.InternalMemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:       imcName,
					Generation: int64(imcGeneration),
				},
			},
			wantIMC: &clusterv1beta1.InternalMemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:       imcName,
					Generation: int64(imcGeneration),
				},
				Status: clusterv1beta1.InternalMemberClusterStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(clusterv1beta1.ConditionTypeClusterPropertyCollectionSucceeded),
							Status:             metav1.ConditionTrue,
							Reason:             ClusterPropertyCollectionSucceededReason,
							Message:            ClusterPropertyCollectionSucceededMessage,
							ObservedGeneration: int64(imcGeneration),
						},
						{
							Type:               examplePropertyProviderCondition,
							Status:             examplePropertyProviderConditionStatus,
							Reason:             examplePropertyProviderReason,
							Message:            examplePropertyProviderMessage,
							ObservedGeneration: int64(imcGeneration),
						},
					},
					Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
						exampleClusterPropertyName: {
							Value: exampleClusterPropertyValue,
						},
					},
					ResourceUsage: clusterv1beta1.ResourceUsage{
						Capacity: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("10"),
							corev1.ResourceMemory: resource.MustParse("10Gi"),
						},
						Allocatable: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("5"),
							corev1.ResourceMemory: resource.MustParse("5Gi"),
						},
						Available: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("5"),
							corev1.ResourceMemory: resource.MustParse("5Gi"),
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			r := &Reconciler{
				propertyProviderCfg: &propertyProviderConfig{
					propertyProvider: &dummyProvider{},
				},
				recorder: utils.NewFakeRecorder(1),
			}

			if err := r.reportClusterPropertiesWithPropertyProvider(ctx, tc.imc); err != nil {
				t.Fatalf("reportClusterPropertiesWithPropertyProvider(), got error %v, want no error", err)
			}

			if diff := cmp.Diff(tc.imc, tc.wantIMC, ignoreLTTConditionField); diff != "" {
				t.Fatalf("internalMemberCluster, (-got, +want):\n%s", diff)
			}
		})
	}
}

// TestUpdateResourceStats tests the updateResourceStats method.
func TestUpdateResourceStats(t *testing.T) {
	timeStarted := time.Now()
	imcTemplate := &clusterv1beta1.InternalMemberCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: imcName,
		},
		Spec: clusterv1beta1.InternalMemberClusterSpec{},
	}

	nodes := []*corev1.Node{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: nodeName1,
			},
			Spec: corev1.NodeSpec{},
			Status: corev1.NodeStatus{
				Capacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("10"),
					corev1.ResourceMemory: resource.MustParse("10Gi"),
				},
				Allocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("8"),
					corev1.ResourceMemory: resource.MustParse("8Gi"),
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: nodeName2,
			},
			Spec: corev1.NodeSpec{},
			Status: corev1.NodeStatus{
				Capacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("15"),
					corev1.ResourceMemory: resource.MustParse("10Gi"),
				},
				Allocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("12"),
					corev1.ResourceMemory: resource.MustParse("8Gi"),
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: nodeName3,
			},
			Spec: corev1.NodeSpec{},
			Status: corev1.NodeStatus{
				Capacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("6"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
				},
				Allocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("5"),
					corev1.ResourceMemory: resource.MustParse("3Gi"),
				},
			},
		},
	}

	testCases := []struct {
		name          string
		nodes         []*corev1.Node
		pods          []*corev1.Pod
		wantIMCStatus clusterv1beta1.InternalMemberClusterStatus
	}{
		{
			name:  "report resource usage when there are multiple nodes but no pods",
			nodes: nodes,
			pods:  []*corev1.Pod{},
			wantIMCStatus: clusterv1beta1.InternalMemberClusterStatus{
				Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
					propertyprovider.NodeCountProperty: {
						Value: "3",
					},
				},
				ResourceUsage: clusterv1beta1.ResourceUsage{
					Capacity: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("31"),
						corev1.ResourceMemory: resource.MustParse("24Gi"),
					},
					Allocatable: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("25"),
						corev1.ResourceMemory: resource.MustParse("19Gi"),
					},
					Available: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("25"),
						corev1.ResourceMemory: resource.MustParse("19Gi"),
					},
				},
			},
		},
		{
			name:  "report resource usage when there aremultiple nodes,+ multiple pods",
			nodes: nodes,
			pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: podName1,
					},
					Spec: corev1.PodSpec{
						NodeName: nodeName1,
						Containers: []corev1.Container{
							{
								Name: containerName1,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("2"),
										corev1.ResourceMemory: resource.MustParse("1Gi"),
									},
								},
							},
							{
								Name: containerName2,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1"),
										corev1.ResourceMemory: resource.MustParse("2Gi"),
									},
								},
							},
						},
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: podName2,
					},
					Spec: corev1.PodSpec{
						NodeName: nodeName2,
						Containers: []corev1.Container{
							{
								Name: containerName1,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1.5"),
										corev1.ResourceMemory: resource.MustParse("3Gi"),
									},
								},
							},
							{
								Name: containerName2,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("3"),
										corev1.ResourceMemory: resource.MustParse("3Gi"),
									},
								},
							},
						},
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: podName3,
					},
					Spec: corev1.PodSpec{
						NodeName: nodeName3,
						Containers: []corev1.Container{
							{
								Name: containerName1,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1"),
										corev1.ResourceMemory: resource.MustParse("1Gi"),
									},
								},
							},
						},
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
					},
				},
			},
			wantIMCStatus: clusterv1beta1.InternalMemberClusterStatus{
				Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
					propertyprovider.NodeCountProperty: {
						Value: "3",
					},
				},
				ResourceUsage: clusterv1beta1.ResourceUsage{
					Capacity: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("31"),
						corev1.ResourceMemory: resource.MustParse("24Gi"),
					},
					Allocatable: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("25"),
						corev1.ResourceMemory: resource.MustParse("19Gi"),
					},
					Available: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("16.5"),
						corev1.ResourceMemory: resource.MustParse("9Gi"),
					},
				},
			},
		},
		{
			name:  "report resource usage when there are pods that are not yet scheduled (should not be accounted for in the usage report)",
			nodes: nodes,
			pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: podName1,
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: containerName1,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("2"),
										corev1.ResourceMemory: resource.MustParse("1Gi"),
									},
								},
							},
							{
								Name: containerName2,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1"),
										corev1.ResourceMemory: resource.MustParse("2Gi"),
									},
								},
							},
						},
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
					},
				},
			},
			wantIMCStatus: clusterv1beta1.InternalMemberClusterStatus{
				Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
					propertyprovider.NodeCountProperty: {
						Value: "3",
					},
				},
				ResourceUsage: clusterv1beta1.ResourceUsage{
					Capacity: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("31"),
						corev1.ResourceMemory: resource.MustParse("24Gi"),
					},
					Allocatable: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("25"),
						corev1.ResourceMemory: resource.MustParse("19Gi"),
					},
					Available: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("25"),
						corev1.ResourceMemory: resource.MustParse("19Gi"),
					},
				},
			},
		},
		{
			name:  "report resource usage when there are pods that are succeeded (should not be accounted for in the usage report)",
			nodes: nodes,
			pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: podName1,
					},
					Spec: corev1.PodSpec{
						NodeName: nodeName1,
						Containers: []corev1.Container{
							{
								Name: containerName1,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("2"),
										corev1.ResourceMemory: resource.MustParse("1Gi"),
									},
								},
							},
							{
								Name: containerName2,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1"),
										corev1.ResourceMemory: resource.MustParse("2Gi"),
									},
								},
							},
						},
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodSucceeded,
					},
				},
			},
			wantIMCStatus: clusterv1beta1.InternalMemberClusterStatus{
				Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
					propertyprovider.NodeCountProperty: {
						Value: "3",
					},
				},
				ResourceUsage: clusterv1beta1.ResourceUsage{
					Capacity: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("31"),
						corev1.ResourceMemory: resource.MustParse("24Gi"),
					},
					Allocatable: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("25"),
						corev1.ResourceMemory: resource.MustParse("19Gi"),
					},
					Available: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("25"),
						corev1.ResourceMemory: resource.MustParse("19Gi"),
					},
				},
			},
		},
		{
			name:  "report resource usage when there are pods that are failed (should not be accounted for in the usage report)",
			nodes: nodes,
			pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: podName1,
					},
					Spec: corev1.PodSpec{
						NodeName: nodeName1,
						Containers: []corev1.Container{
							{
								Name: containerName1,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("2"),
										corev1.ResourceMemory: resource.MustParse("1Gi"),
									},
								},
							},
							{
								Name: containerName2,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1"),
										corev1.ResourceMemory: resource.MustParse("2Gi"),
									},
								},
							},
						},
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodFailed,
					},
				},
			},
			wantIMCStatus: clusterv1beta1.InternalMemberClusterStatus{
				Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
					propertyprovider.NodeCountProperty: {
						Value: "3",
					},
				},
				ResourceUsage: clusterv1beta1.ResourceUsage{
					Capacity: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("31"),
						corev1.ResourceMemory: resource.MustParse("24Gi"),
					},
					Allocatable: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("25"),
						corev1.ResourceMemory: resource.MustParse("19Gi"),
					},
					Available: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("25"),
						corev1.ResourceMemory: resource.MustParse("19Gi"),
					},
				},
			},
		},
		{
			name:  "report resource usage when there occurs inconsistency (more used CPU than allocatable)",
			nodes: nodes,
			pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: podName1,
					},
					Spec: corev1.PodSpec{
						NodeName: nodeName1,
						Containers: []corev1.Container{
							{
								Name: containerName1,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("200"),
										corev1.ResourceMemory: resource.MustParse("10Gi"),
									},
								},
							},
						},
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
					},
				},
			},
			wantIMCStatus: clusterv1beta1.InternalMemberClusterStatus{
				Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
					propertyprovider.NodeCountProperty: {
						Value: "3",
					},
				},
				ResourceUsage: clusterv1beta1.ResourceUsage{
					Capacity: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("31"),
						corev1.ResourceMemory: resource.MustParse("24Gi"),
					},
					Allocatable: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("25"),
						corev1.ResourceMemory: resource.MustParse("19Gi"),
					},
					Available: corev1.ResourceList{
						corev1.ResourceCPU:    resource.Quantity{},
						corev1.ResourceMemory: resource.MustParse("9Gi"),
					},
				},
			},
		},
		{
			name:  "report resource usage when there occurs inconsistency (more used memory than allocatable)",
			nodes: nodes,
			pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: podName1,
					},
					Spec: corev1.PodSpec{
						NodeName: nodeName1,
						Containers: []corev1.Container{
							{
								Name: containerName1,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("2"),
										corev1.ResourceMemory: resource.MustParse("100Gi"),
									},
								},
							},
						},
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
					},
				},
			},
			wantIMCStatus: clusterv1beta1.InternalMemberClusterStatus{
				Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
					propertyprovider.NodeCountProperty: {
						Value: "3",
					},
				},
				ResourceUsage: clusterv1beta1.ResourceUsage{
					Capacity: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("31"),
						corev1.ResourceMemory: resource.MustParse("24Gi"),
					},
					Allocatable: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("25"),
						corev1.ResourceMemory: resource.MustParse("19Gi"),
					},
					Available: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("23"),
						corev1.ResourceMemory: resource.Quantity{},
					},
				},
			},
		},
	}

	ctx := context.Background()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add scheme (corev1): %v", err)
	}
	if err := clusterv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add scheme (clusterv1beta1): %v", err)
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeClientBuilder := fake.NewClientBuilder().WithScheme(scheme)
			for _, obj := range tc.nodes {
				fakeClientBuilder.WithObjects(obj)
			}
			for _, obj := range tc.pods {
				fakeClientBuilder.WithObjects(obj)
				fakeClientBuilder.WithStatusSubresource(obj)
			}
			fakeClient := fakeClientBuilder.Build()

			r := &Reconciler{
				memberClient: fakeClient,
			}

			imc := imcTemplate.DeepCopy()
			if err := r.updateResourceStats(ctx, imc); err != nil {
				t.Fatalf("updateResourceStats(), got error %v, want no error", err)
			}

			if diff := cmp.Diff(imc.Status, tc.wantIMCStatus, ignoreAllTimeFields); diff != "" {
				t.Fatalf("InternalMemberCluster status (-got, +want):\n%s", diff)
			}

			// Verify if the observation time has been set.
			for pn, pv := range imc.Status.Properties {
				if pv.ObservationTime.Before(&metav1.Time{Time: timeStarted}) {
					t.Fatalf("observation time for property %s is before the start time", pn)
				}
			}
			if imc.Status.ResourceUsage.ObservationTime.Before(&metav1.Time{Time: timeStarted}) {
				t.Fatalf("observation time for resource usage is before the start time")
			}
		})
	}
}

// failedToStartProvider is a property provider that are expected to fail upon startup.
type failedToStartProvider struct{}

var _ propertyprovider.PropertyProvider = &failedToStartProvider{}

func (p *failedToStartProvider) Start(_ context.Context, _ *rest.Config) error {
	return fmt.Errorf("expected to fail")
}

func (p *failedToStartProvider) Collect(_ context.Context) propertyprovider.PropertyCollectionResponse {
	return propertyprovider.PropertyCollectionResponse{}
}

// startTimedOutProvider is a property provider that will not return when its Start() method
// is called until the hold channel is closed.
type startTimedOutProvider struct {
	hold <-chan struct{}
}

var _ propertyprovider.PropertyProvider = &startTimedOutProvider{}

func (p *startTimedOutProvider) Start(_ context.Context, _ *rest.Config) error {
	<-p.hold
	return nil
}

func (p *startTimedOutProvider) Collect(_ context.Context) propertyprovider.PropertyCollectionResponse {
	return propertyprovider.PropertyCollectionResponse{}
}

func TestConnectToPropertyProvider(t *testing.T) {
	imcTemplate := &clusterv1beta1.InternalMemberCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: imcName,
		},
		Spec: clusterv1beta1.InternalMemberClusterSpec{},
	}

	// Note that to verify the correctness of the behavior, the data here is set to be
	// explicitly different from the those returned by the dummy property provider.
	nodes := []*corev1.Node{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: nodeName1,
			},
			Spec: corev1.NodeSpec{},
			Status: corev1.NodeStatus{
				Capacity: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("10"),
					corev1.ResourceMemory: resource.MustParse("10Gi"),
				},
				Allocatable: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("8"),
					corev1.ResourceMemory: resource.MustParse("8Gi"),
				},
			},
		},
	}
	pods := []*corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: podName1,
			},
			Spec: corev1.PodSpec{
				NodeName: nodeName1,
				Containers: []corev1.Container{
					{
						Name: containerName1,
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("2"),
								corev1.ResourceMemory: resource.MustParse("1Gi"),
							},
						},
					},
					{
						Name: containerName2,
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("1"),
								corev1.ResourceMemory: resource.MustParse("2Gi"),
							},
						},
					},
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
			},
		},
	}

	// Summarize the expected properties and resource usage.
	expectedProperties := map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
		propertyprovider.NodeCountProperty: {
			Value: fmt.Sprintf("%d", len(nodes)),
		},
	}

	expectedCPUTotalCapacity := resource.Quantity{}
	expectedMemoryTotalCapacity := resource.Quantity{}
	expectedCPUAllocatableCapacity := resource.Quantity{}
	expectedMemoryAllocatableCapacity := resource.Quantity{}
	for _, node := range nodes {
		expectedCPUTotalCapacity.Add(node.Status.Capacity[corev1.ResourceCPU])
		expectedMemoryTotalCapacity.Add(node.Status.Capacity[corev1.ResourceMemory])
		expectedCPUAllocatableCapacity.Add(node.Status.Allocatable[corev1.ResourceCPU])
		expectedMemoryAllocatableCapacity.Add(node.Status.Allocatable[corev1.ResourceMemory])
	}
	expectedCPUAvailableCapacity := expectedCPUAllocatableCapacity.DeepCopy()
	expectedMemoryAvailableCapacity := expectedMemoryAllocatableCapacity.DeepCopy()
	for _, pod := range pods {
		if pod.Spec.NodeName == "" || pod.Status.Phase != corev1.PodRunning {
			continue
		}

		for _, container := range pod.Spec.Containers {
			expectedCPUAvailableCapacity.Sub(container.Resources.Requests[corev1.ResourceCPU])
			expectedMemoryAvailableCapacity.Sub(container.Resources.Requests[corev1.ResourceMemory])
		}
	}

	expectedResourceUsage := clusterv1beta1.ResourceUsage{
		Capacity: corev1.ResourceList{
			corev1.ResourceCPU:    expectedCPUTotalCapacity,
			corev1.ResourceMemory: expectedMemoryTotalCapacity,
		},
		Allocatable: corev1.ResourceList{
			corev1.ResourceCPU:    expectedCPUAllocatableCapacity,
			corev1.ResourceMemory: expectedMemoryAllocatableCapacity,
		},
		Available: corev1.ResourceList{
			corev1.ResourceCPU:    expectedCPUAvailableCapacity,
			corev1.ResourceMemory: expectedMemoryAvailableCapacity,
		},
	}

	h := make(chan struct{})
	// Always close the channel to avoid leaks.
	defer func() {
		close(h)
	}()

	failedToStartP := &failedToStartProvider{}
	dummyP := &dummyProvider{}
	startTimedOutP := &startTimedOutProvider{hold: h}

	ctx := context.Background()

	testCases := []struct {
		name             string
		propertyProvider propertyprovider.PropertyProvider
		wantIMCStatus    clusterv1beta1.InternalMemberClusterStatus
	}{
		{
			name:             "provider failed to start",
			propertyProvider: failedToStartP,
			wantIMCStatus: clusterv1beta1.InternalMemberClusterStatus{
				Properties:    expectedProperties,
				ResourceUsage: expectedResourceUsage,
				Conditions: []metav1.Condition{
					{
						Type:               string(clusterv1beta1.ConditionTypeClusterPropertyProviderStarted),
						Status:             metav1.ConditionFalse,
						Reason:             ClusterPropertyProviderStartedFailedReason,
						Message:            fmt.Sprintf(ClusterPropertyProviderStartedFailedMessage, "expected to fail"),
						ObservedGeneration: imcTemplate.GetGeneration(),
					},
				},
			},
		},
		{
			name: "no provider is set up, fall back to the default behavior",
			wantIMCStatus: clusterv1beta1.InternalMemberClusterStatus{
				Properties:    expectedProperties,
				ResourceUsage: expectedResourceUsage,
			},
		},
		{
			name:             "provider starts up successfully",
			propertyProvider: dummyP,
			wantIMCStatus: clusterv1beta1.InternalMemberClusterStatus{
				Properties:    dummyP.Collect(ctx).Properties,
				ResourceUsage: dummyP.Collect(ctx).Resources,
				Conditions: []metav1.Condition{
					{
						Type:    examplePropertyProviderCondition,
						Status:  examplePropertyProviderConditionStatus,
						Reason:  examplePropertyProviderReason,
						Message: examplePropertyProviderMessage,
					},
					{
						Type:               string(clusterv1beta1.ConditionTypeClusterPropertyCollectionSucceeded),
						Status:             metav1.ConditionTrue,
						Reason:             ClusterPropertyCollectionSucceededReason,
						Message:            ClusterPropertyCollectionSucceededMessage,
						ObservedGeneration: imcTemplate.GetGeneration(),
					},
					{
						Type:               string(clusterv1beta1.ConditionTypeClusterPropertyProviderStarted),
						Status:             metav1.ConditionTrue,
						Reason:             ClusterPropertyProviderStartedReason,
						Message:            ClusterPropertyProviderStartedMessage,
						ObservedGeneration: imcTemplate.GetGeneration(),
					},
				},
			},
		},
		{
			name:             "provider failed to start within given time",
			propertyProvider: startTimedOutP,
			wantIMCStatus: clusterv1beta1.InternalMemberClusterStatus{
				Properties:    expectedProperties,
				ResourceUsage: expectedResourceUsage,
				Conditions: []metav1.Condition{
					{
						Type:               string(clusterv1beta1.ConditionTypeClusterPropertyProviderStarted),
						Status:             metav1.ConditionFalse,
						Reason:             ClusterPropertyProviderStartedTimedOutReason,
						Message:            ClusterPropertyProviderStartedTimedOutMessage,
						ObservedGeneration: imcTemplate.GetGeneration(),
					},
				},
			},
		},
	}

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add scheme (corev1): %v", err)
	}
	if err := clusterv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add scheme (clusterv1beta1): %v", err)
	}

	fakeClientBuilder := fake.NewClientBuilder().WithScheme(scheme)
	for _, obj := range nodes {
		fakeClientBuilder.WithObjects(obj)
	}
	for _, obj := range pods {
		fakeClientBuilder.WithObjects(obj)
		fakeClientBuilder.WithStatusSubresource(obj)
	}
	fakeClient := fakeClientBuilder.Build()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			r := &Reconciler{
				memberClient: fakeClient,
				propertyProviderCfg: &propertyProviderConfig{
					propertyProvider: tc.propertyProvider,
				},
				recorder: utils.NewFakeRecorder(1),
			}

			imc := imcTemplate.DeepCopy()
			if err := r.connectToPropertyProvider(ctx, imc); err != nil {
				t.Fatalf("connectToPropertyProvider(), got error %v, want no error", err)
			}

			if diff := cmp.Diff(
				imc.Status, tc.wantIMCStatus,
				ignoreAllTimeFields,
				ignoreLTTConditionField,
				sortByConditionType,
			); diff != "" {
				t.Fatalf("InternalMemberCluster status (-got, +want):\n%s", diff)
			}
		})
	}
}
