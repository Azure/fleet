package memberinternalmembercluster

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	v12 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/kubernetes/pkg/apis/core"

	"go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/controllers/common"
	"go.goms.io/fleet/pkg/utils"
)

func TestMarkInternalMemberClusterJoined(t *testing.T) {
	r := Reconciler{recorder: utils.NewFakeRecorder(1)}
	internalMemberCluster := &v1alpha1.InternalMemberCluster{}

	r.markInternalMemberClusterJoined(internalMemberCluster)

	// check that the correct event is emitted
	event := <-r.recorder.(*record.FakeRecorder).Events
	expected := utils.GetEventString(internalMemberCluster, v12.EventTypeNormal, eventReasonInternalMemberClusterJoined, "internal member cluster joined")

	assert.Equal(t, expected, event, utils.TestCaseMsg, "TestMarkInternalMemberClusterJoined")

	// Check expected conditions.
	expectedConditions := []metav1.Condition{
		{Type: v1alpha1.ConditionTypeInternalMemberClusterJoin, Status: metav1.ConditionTrue, Reason: eventReasonInternalMemberClusterJoined},
		{Type: common.ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: common.ReasonReconcileSuccess},
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
	expected := utils.GetEventString(internalMemberCluster, v12.EventTypeNormal, eventReasonInternalMemberClusterLeft, "internal member cluster left")

	assert.Equal(t, expected, event, utils.TestCaseMsg, "TestMarkInternalMemberClusterLeft")

	// Check expected conditions.
	expectedConditions := []metav1.Condition{
		{Type: v1alpha1.ConditionTypeInternalMemberClusterJoin, Status: metav1.ConditionFalse, Reason: eventReasonInternalMemberClusterLeft},
		{Type: common.ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: common.ReasonReconcileSuccess},
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
	expected := utils.GetEventString(internalMemberCluster, core.EventTypeNormal, eventReasonInternalMemberClusterUnknown, "internal member cluster unknown")

	assert.Equal(t, expected, event, utils.TestCaseMsg, "TestMarkInternalMemberClusterUnknown")

	// Check expected conditions.
	expectedConditions := []metav1.Condition{
		{Type: v1alpha1.ConditionTypeMembershipJoin, Status: metav1.ConditionUnknown, Reason: eventReasonInternalMemberClusterUnknown},
		{Type: common.ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: common.ReasonReconcileSuccess},
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
	expected := utils.GetEventString(internalMemberCluster, v12.EventTypeNormal, eventReasonInternalMemberClusterHBReceived, "internal member cluster heartbeat received")

	assert.Equal(t, expected, event, utils.TestCaseMsg, "TestMarkInternalMemberClusterHeartbeatReceived")

	// Check expected conditions.
	expectedConditions := []metav1.Condition{
		{Type: v1alpha1.ConditionTypeInternalMemberClusterHeartbeat, Status: metav1.ConditionTrue, Reason: eventReasonInternalMemberClusterHBReceived},
		{Type: common.ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: common.ReasonReconcileSuccess},
	}

	for _, expectedCondition := range expectedConditions {
		actualCondition := internalMemberCluster.GetCondition(expectedCondition.Type)
		assert.Equal(t, "", cmp.Diff(expectedCondition, *(actualCondition), cmpopts.IgnoreTypes(time.Time{})), utils.TestCaseMsg, "TestMarkInternalMemberClusterHeartbeatReceived")
	}
}

func TestMarkInternalMemberClusterHeartbeatUnknown(t *testing.T) {
	internalMemberCluster := &v1alpha1.InternalMemberCluster{}
	r := Reconciler{recorder: utils.NewFakeRecorder(1)}

	r.markInternalMemberClusterHeartbeatUnknown(internalMemberCluster)

	// check that the correct event is emitted
	event := <-r.recorder.(*record.FakeRecorder).Events
	expected := utils.GetEventString(internalMemberCluster, core.EventTypeNormal, eventReasonInternalMemberClusterHBUnknown, "internal member cluster heartbeat unknown")

	assert.Equal(t, expected, event, utils.TestCaseMsg, "TestMarkInternalMemberClusterHeartbeatUnknown")

	// Check expected conditions.
	expectedConditions := []metav1.Condition{
		{Type: v1alpha1.ConditionTypeInternalMemberClusterHeartbeat, Status: metav1.ConditionUnknown, Reason: eventReasonInternalMemberClusterHBUnknown},
		{Type: common.ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: common.ReasonReconcileSuccess},
	}
	for _, expectedCondition := range expectedConditions {
		actualCondition := internalMemberCluster.GetCondition(expectedCondition.Type)
		assert.Equal(t, "", cmp.Diff(expectedCondition, *(actualCondition), cmpopts.IgnoreTypes(time.Time{})), utils.TestCaseMsg, "TestMarkInternalMemberClusterHeartbeatUnknown")
	}
}
