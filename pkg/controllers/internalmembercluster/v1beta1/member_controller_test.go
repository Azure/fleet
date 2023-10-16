/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package v1beta1

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	"go.goms.io/fleet/pkg/utils"
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
