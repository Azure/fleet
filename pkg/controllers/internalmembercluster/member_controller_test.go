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

	// Check expected conditions.
	expectedConditions := []metav1.Condition{
		{Type: v1alpha1.ConditionTypeInternalMemberClusterJoin, Status: metav1.ConditionTrue, Reason: eventReasonInternalMemberClusterJoined},
		{Type: utils.ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: utils.ReasonReconcileSuccess},
	}

	for _, expectedCondition := range expectedConditions {
		actualCondition := internalMemberCluster.GetCondition(expectedCondition.Type)
		assert.Equal(t, "", cmp.Diff(expectedCondition, *(actualCondition), cmpopts.IgnoreTypes(time.Time{})), utils.TestCaseMsg, "TestMarkInternalMemberClusterJoined")
	}
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
	expectedConditions := []metav1.Condition{
		{Type: v1alpha1.ConditionTypeInternalMemberClusterJoin, Status: metav1.ConditionFalse, Reason: eventReasonInternalMemberClusterLeft},
		{Type: utils.ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: utils.ReasonReconcileSuccess},
	}

	for _, expectedCondition := range expectedConditions {
		actualCondition := internalMemberCluster.GetCondition(expectedCondition.Type)
		assert.Equal(t, "", cmp.Diff(expectedCondition, *(actualCondition), cmpopts.IgnoreTypes(time.Time{})), utils.TestCaseMsg, "TestMarkInternalMemberClusterLeft")
	}
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
	expectedConditions := []metav1.Condition{
		{Type: v1alpha1.ConditionTypeMembershipJoin, Status: metav1.ConditionUnknown, Reason: eventReasonInternalMemberClusterUnknown},
		{Type: utils.ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: utils.ReasonReconcileSuccess},
	}
	for _, expectedCondition := range expectedConditions {
		actualCondition := internalMemberCluster.GetCondition(expectedCondition.Type)
		assert.Equal(t, "", cmp.Diff(expectedCondition, *(actualCondition), cmpopts.IgnoreTypes(time.Time{})), utils.TestCaseMsg, "TestMarkInternalMemberClusterUnknown")
	}
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
	expectedConditions := []metav1.Condition{
		{Type: v1alpha1.ConditionTypeInternalMemberClusterHeartbeat, Status: metav1.ConditionTrue, Reason: eventReasonInternalMemberClusterHBReceived},
		{Type: utils.ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: utils.ReasonReconcileSuccess},
	}

	for _, expectedCondition := range expectedConditions {
		actualCondition := internalMemberCluster.GetCondition(expectedCondition.Type)
		assert.Equal(t, "", cmp.Diff(expectedCondition, *(actualCondition), cmpopts.IgnoreTypes(time.Time{})), utils.TestCaseMsg, "TestMarkInternalMemberClusterHeartbeatReceived")
	}
}

func TestMarkInternalMemberClusterHeartbeatUnhealthy(t *testing.T) {
	internalMemberCluster := &v1alpha1.InternalMemberCluster{}
	err := errors.New("")
	r := Reconciler{recorder: utils.NewFakeRecorder(1)}

	r.markInternalMemberClusterUnhealthy(internalMemberCluster, err)

	// check that the correct event is emitted
	event := <-r.recorder.(*record.FakeRecorder).Events
	expected := utils.GetEventString(internalMemberCluster, corev1.EventTypeWarning, eventReasonInternalMemberClusterUnhealthy, "internal member cluster unhealthy")

	assert.Equal(t, expected, event, utils.TestCaseMsg, "TestMarkInternalMemberClusterHeartbeatUnhealthy")

	// Check expected conditions.
	expectedConditions := []metav1.Condition{
		{Type: v1alpha1.ConditionTypeInternalMemberClusterHeartbeat, Status: metav1.ConditionUnknown, Reason: eventReasonInternalMemberClusterUnhealthy},
		{Type: utils.ConditionTypeSynced, Status: metav1.ConditionFalse, Reason: utils.ReasonReconcileError},
	}
	for _, expectedCondition := range expectedConditions {
		actualCondition := internalMemberCluster.GetCondition(expectedCondition.Type)
		assert.Equal(t, "", cmp.Diff(expectedCondition, *(actualCondition), cmpopts.IgnoreTypes(time.Time{})), utils.TestCaseMsg, "TestMarkInternalMemberClusterHeartbeatUnhealthy")
	}
}

func TestUpdateInternalMemberClusterWithRetry(t *testing.T) {
	updateErr := errors.New("rand-err-msg")
	updateErrNotFound := apierrors.NewNotFound(schema.GroupResource{}, "")
	updateErrInvalid := apierrors.NewInvalid(schema.GroupKind{}, "", field.ErrorList{})
	numberOfRetrySuccess := 3
	hbPeriod := 5

	cntRetryUpdateHappyPath := -1
	cntRetryUpdateHappyPathWithRetry := -1
	cntRetryUpdateErr := -1
	cntRetryUpdateErrFailWithinHeartbeat := -1
	cntRetryUpdateErrInvalid := -1
	cntRetryUpdateErrNotFound := -1

	testCases := map[string]struct {
		verifyNumberOfRetry   func() bool
		internalMemberCluster *v1alpha1.InternalMemberCluster
		wantErr               error
		r                     *Reconciler
	}{
		"succeed- no update error": {
			wantErr: nil,
			r: &Reconciler{hubClient: &test.MockClient{
				MockStatusUpdate: func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
					cntRetryUpdateHappyPath++
					return nil
				}}},
			verifyNumberOfRetry: func() bool {
				return cntRetryUpdateHappyPath == 0
			},
			internalMemberCluster: &v1alpha1.InternalMemberCluster{},
		},
		"succeed- retry succeed within heartbeat": {
			wantErr: nil,
			r: &Reconciler{hubClient: &test.MockClient{
				MockStatusUpdate: func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
					cntRetryUpdateHappyPathWithRetry++
					if cntRetryUpdateHappyPathWithRetry == numberOfRetrySuccess {
						return nil
					}
					return updateErr
				}}},
			verifyNumberOfRetry: func() bool {
				return cntRetryUpdateHappyPathWithRetry == numberOfRetrySuccess
			},
			internalMemberCluster: &v1alpha1.InternalMemberCluster{
				Spec: v1alpha1.InternalMemberClusterSpec{HeartbeatPeriodSeconds: int32(hbPeriod)},
			},
		},
		"fail updating within heartbeat": {
			wantErr: updateErr,
			r: &Reconciler{hubClient: &test.MockClient{
				MockStatusUpdate: func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
					cntRetryUpdateErrFailWithinHeartbeat++
					if cntRetryUpdateErrFailWithinHeartbeat == 1000 { // big number of retry to let the operation overrun heartbeat period
						return nil
					}
					return updateErr
				}}},
			verifyNumberOfRetry: func() bool {
				return cntRetryUpdateErrFailWithinHeartbeat > 0
			},
			internalMemberCluster: &v1alpha1.InternalMemberCluster{
				Spec: v1alpha1.InternalMemberClusterSpec{HeartbeatPeriodSeconds: int32(hbPeriod)},
			},
		},
		"fail updating within heartbeat- all update call return error": {
			wantErr: updateErr,
			r: &Reconciler{hubClient: &test.MockClient{
				MockStatusUpdate: func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
					cntRetryUpdateErr++
					return updateErr
				}}},
			verifyNumberOfRetry: func() bool {
				return cntRetryUpdateErr > 0
			},
			internalMemberCluster: &v1alpha1.InternalMemberCluster{},
		},
		"fail updating within heartbeat- err not found": {
			wantErr: updateErrNotFound,
			r: &Reconciler{hubClient: &test.MockClient{
				MockStatusUpdate: func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
					cntRetryUpdateErrNotFound++
					return updateErrNotFound
				}}},
			verifyNumberOfRetry: func() bool {
				return cntRetryUpdateErrNotFound == 0
			},
			internalMemberCluster: &v1alpha1.InternalMemberCluster{},
		},
		"fail updating within heartbeat- err invalid": {
			wantErr: updateErrInvalid,
			r: &Reconciler{hubClient: &test.MockClient{
				MockStatusUpdate: func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
					cntRetryUpdateErrInvalid++
					return updateErrInvalid
				}}},
			verifyNumberOfRetry: func() bool {
				return cntRetryUpdateErrInvalid == 0
			},
			internalMemberCluster: &v1alpha1.InternalMemberCluster{},
		},
	}

	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			err := testCase.r.updateInternalMemberClusterWithRetry(context.Background(), testCase.internalMemberCluster)
			assert.Equal(t, testCase.wantErr, err, utils.TestCaseMsg, testName)
			assert.Equal(t, testCase.verifyNumberOfRetry(), true, utils.TestCaseMsg, testName)
		})
	}
}
