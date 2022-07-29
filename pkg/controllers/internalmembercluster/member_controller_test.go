/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package internalmembercluster

import (
	"context"
	"testing"
	"time"

	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
)

func TestMarkInternalMemberClusterJoined(t *testing.T) {
	r := Reconciler{recorder: utils.NewFakeRecorder(1)}
	internalMemberCluster := &v1alpha1.InternalMemberCluster{}

	r.markInternalMemberClusterJoined(internalMemberCluster)

	// check that the correct event is emitted
	event := <-r.recorder.(*record.FakeRecorder).Events
	expected := utils.GetEventString(internalMemberCluster, corev1.EventTypeNormal, eventReasonInternalMemberClusterJoined, "internal member cluster has joined")
	assert.Equal(t, expected, event, utils.TestCaseMsg, "TestMarkInternalMemberClusterJoined")

	// Check expected condition.
	expectedCondition := metav1.Condition{Type: v1alpha1.ConditionTypeInternalMemberClusterJoin, Status: metav1.ConditionTrue, Reason: eventReasonInternalMemberClusterJoined}
	actualCondition := internalMemberCluster.GetCondition(expectedCondition.Type)
	assert.Equal(t, "", cmp.Diff(expectedCondition, *(actualCondition), cmpopts.IgnoreTypes(time.Time{})), utils.TestCaseMsg, "TestMarkInternalMemberClusterJoined")
}

func TestMarkInternalMemberClusterLeft(t *testing.T) {
	r := Reconciler{recorder: utils.NewFakeRecorder(1)}
	internalMemberCluster := &v1alpha1.InternalMemberCluster{}

	r.markInternalMemberClusterLeft(internalMemberCluster)

	// check that the correct event is emitted
	event := <-r.recorder.(*record.FakeRecorder).Events
	expected := utils.GetEventString(internalMemberCluster, corev1.EventTypeNormal, eventReasonInternalMemberClusterLeft, "internal member cluster has left")
	assert.Equal(t, expected, event, utils.TestCaseMsg, "TestMarkInternalMemberClusterLeft")

	// Check expected conditions.
	expectedCondition := metav1.Condition{Type: v1alpha1.ConditionTypeInternalMemberClusterJoin, Status: metav1.ConditionFalse, Reason: eventReasonInternalMemberClusterLeft}
	actualCondition := internalMemberCluster.GetCondition(expectedCondition.Type)
	assert.Equal(t, "", cmp.Diff(expectedCondition, *(actualCondition), cmpopts.IgnoreTypes(time.Time{})), utils.TestCaseMsg, "TestMarkInternalMemberClusterLeft")
}

func TestMarkInternalMemberClusterUnknown(t *testing.T) {
	internalMemberCluster := &v1alpha1.InternalMemberCluster{}
	r := Reconciler{recorder: utils.NewFakeRecorder(1)}

	r.markInternalMemberClusterUnknown(internalMemberCluster)

	// check that the correct event is emitted
	event := <-r.recorder.(*record.FakeRecorder).Events
	expected := utils.GetEventString(internalMemberCluster, corev1.EventTypeNormal, eventReasonInternalMemberClusterUnknown, "internal member cluster join state unknown")
	assert.Equal(t, expected, event, utils.TestCaseMsg, "TestMarkInternalMemberClusterUnknown")

	// Check expected conditions.
	expectedCondition := metav1.Condition{Type: v1alpha1.ConditionTypeInternalMemberClusterJoin, Status: metav1.ConditionUnknown, Reason: eventReasonInternalMemberClusterUnknown}
	actualCondition := internalMemberCluster.GetCondition(expectedCondition.Type)
	assert.Equal(t, "", cmp.Diff(expectedCondition, *(actualCondition), cmpopts.IgnoreTypes(time.Time{})), utils.TestCaseMsg, "TestMarkInternalMemberClusterUnknown")
}

func TestMarkInternalMemberClusterHeartbeatReceived(t *testing.T) {
	r := Reconciler{recorder: utils.NewFakeRecorder(1)}
	internalMemberCluster := &v1alpha1.InternalMemberCluster{}

	r.markInternalMemberClusterHeartbeatReceived(internalMemberCluster)

	// check that the correct event is emitted
	event := <-r.recorder.(*record.FakeRecorder).Events
	expected := utils.GetEventString(internalMemberCluster, corev1.EventTypeNormal, eventReasonInternalMemberClusterHBReceived, "internal member cluster heartbeat received")
	assert.Equal(t, expected, event, utils.TestCaseMsg, "TestMarkInternalMemberClusterHeartbeatReceived")

	// Check expected conditions.
	expectedCondition := metav1.Condition{Type: v1alpha1.ConditionTypeInternalMemberClusterHeartbeat, Status: metav1.ConditionTrue, Reason: eventReasonInternalMemberClusterHBReceived}
	actualCondition := internalMemberCluster.GetCondition(expectedCondition.Type)
	assert.Equal(t, "", cmp.Diff(expectedCondition, *(actualCondition), cmpopts.IgnoreTypes(time.Time{})), utils.TestCaseMsg, "TestMarkInternalMemberClusterHeartbeatReceived")

	// Verify last transition time is updated.
	oldLastTransitionTime := internalMemberCluster.GetCondition(v1alpha1.ConditionTypeInternalMemberClusterHeartbeat).LastTransitionTime
	r.markInternalMemberClusterHeartbeatReceived(internalMemberCluster)
	newLastTransitionTime := internalMemberCluster.GetCondition(v1alpha1.ConditionTypeInternalMemberClusterHeartbeat).LastTransitionTime
	assert.Equal(t, true, newLastTransitionTime.After(oldLastTransitionTime.Time), utils.TestCaseMsg, "last transition time updated")
}

func TestMarkInternalMemberClusterHealthy(t *testing.T) {
	r := Reconciler{recorder: utils.NewFakeRecorder(1)}
	internalMemberCluster := &v1alpha1.InternalMemberCluster{}

	r.markInternalMemberClusterHealthy(internalMemberCluster)

	// check that the correct event is emitted
	event := <-r.recorder.(*record.FakeRecorder).Events
	expected := utils.GetEventString(internalMemberCluster, corev1.EventTypeNormal, eventReasonInternalMemberClusterHealthy, "internal member cluster healthy")
	assert.Equal(t, expected, event, utils.TestCaseMsg, "TestMarkInternalMemberClusterHealthy")

	// Check expected conditions.
	expectedCondition := metav1.Condition{Type: v1alpha1.ConditionTypeMemberClusterHealth, Status: metav1.ConditionTrue, Reason: eventReasonInternalMemberClusterHealthy}
	actualCondition := internalMemberCluster.GetCondition(expectedCondition.Type)
	assert.Equal(t, "", cmp.Diff(expectedCondition, *(actualCondition), cmpopts.IgnoreTypes(time.Time{})), utils.TestCaseMsg, "TestMarkInternalMemberClusterHealthy")
}

func TestMarkInternalMemberClusterHeartbeatUnhealthy(t *testing.T) {
	internalMemberCluster := &v1alpha1.InternalMemberCluster{}
	err := errors.New("rand-err-msg")
	r := Reconciler{recorder: utils.NewFakeRecorder(1)}

	r.markInternalMemberClusterUnhealthy(internalMemberCluster, err)

	// check that the correct event is emitted
	event := <-r.recorder.(*record.FakeRecorder).Events
	expected := utils.GetEventString(internalMemberCluster, corev1.EventTypeWarning, eventReasonInternalMemberClusterUnhealthy, "internal member cluster unhealthy")
	assert.Equal(t, expected, event, utils.TestCaseMsg, "TestMarkInternalMemberClusterHeartbeatUnhealthy")

	// Check expected conditions.
	expectedCondition := metav1.Condition{Type: v1alpha1.ConditionTypeInternalMemberClusterHealth, Status: metav1.ConditionFalse, Reason: eventReasonInternalMemberClusterUnhealthy, Message: "rand-err-msg"}
	actualCondition := internalMemberCluster.GetCondition(expectedCondition.Type)
	assert.Equal(t, "", cmp.Diff(expectedCondition, *(actualCondition), cmpopts.IgnoreTypes(time.Time{})), utils.TestCaseMsg, "TestMarkInternalMemberClusterHeartbeatUnhealthy")
}

func TestUpdateInternalMemberClusterWithRetry(t *testing.T) {
	lessRetriesForRetriable := 0
	lessRetriesForNonRetriable := 0
	moreRetriesForRetriable := 0

	testCases := map[string]struct {
		r                     *Reconciler
		internalMemberCluster *v1alpha1.InternalMemberCluster
		retries               int
		wantRetries           int
		wantErr               error
	}{
		"succeed without retries if no errors": {
			retries: 0,
			r: &Reconciler{hubClient: &test.MockClient{
				MockStatusUpdate: func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
					return nil
				}}},
			internalMemberCluster: &v1alpha1.InternalMemberCluster{},
			wantErr:               nil,
		},
		"succeed with retries for retriable errors: TooManyRequests": {
			r: &Reconciler{hubClient: &test.MockClient{
				MockStatusUpdate: func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
					lessRetriesForRetriable++
					if lessRetriesForRetriable >= 3 {
						return nil
					}
					return apierrors.NewTooManyRequests("", 0)
				}}},
			internalMemberCluster: &v1alpha1.InternalMemberCluster{
				Spec: v1alpha1.InternalMemberClusterSpec{HeartbeatPeriodSeconds: int32(3)},
			},
			wantErr: nil,
		},
		"fail without retries for non-retriable errors: Invalid": {
			r: &Reconciler{hubClient: &test.MockClient{
				MockStatusUpdate: func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
					lessRetriesForNonRetriable++
					if lessRetriesForNonRetriable >= 3 {
						return nil
					}
					return apierrors.NewInvalid(schema.GroupKind{}, "", field.ErrorList{})
				}}},
			internalMemberCluster: &v1alpha1.InternalMemberCluster{},
			wantErr:               apierrors.NewInvalid(schema.GroupKind{}, "", field.ErrorList{}),
		},
		"fail if too many retries for retirable errors: TooManyRequests": {
			r: &Reconciler{hubClient: &test.MockClient{
				MockStatusUpdate: func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
					moreRetriesForRetriable++
					if moreRetriesForRetriable >= 100 {
						return nil
					}
					return apierrors.NewTooManyRequests("", 0)
				}}},
			internalMemberCluster: &v1alpha1.InternalMemberCluster{},
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
