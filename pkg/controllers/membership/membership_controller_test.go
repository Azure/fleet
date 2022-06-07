package membership

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
)

func TestGetInternalMemberClusterState(t *testing.T) {
	r := Reconciler{}
	updateClusterState := func() {
		for i := 0; i < 5; i++ {
			r.clusterStateLock.Lock()
			r.internalMemberClusterState = v1alpha1.ClusterStateJoin
			r.clusterStateLock.Unlock()
			time.Sleep(1 * time.Millisecond)
		}
		for i := 0; i < 5; i++ {
			r.clusterStateLock.Lock()
			r.internalMemberClusterState = v1alpha1.ClusterStateLeave
			r.clusterStateLock.Unlock()
			time.Sleep(1 * time.Millisecond)
		}
	}
	go updateClusterState()

	time.Sleep(2 * time.Millisecond)
	membershipClusterState := r.getInternalMemberClusterState()
	assert.Equal(t, v1alpha1.ClusterStateJoin, membershipClusterState)

	time.Sleep(5 * time.Millisecond)
	membershipClusterState = r.getInternalMemberClusterState()
	assert.Equal(t, v1alpha1.ClusterStateLeave, membershipClusterState)
}

func TestMarkMembershipJoined(t *testing.T) {
	r := Reconciler{recorder: utils.NewFakeRecorder(1)}
	membership := &v1alpha1.Membership{}

	r.markMembershipJoined(membership)

	// check that the correct event is emitted
	event := <-r.recorder.(*record.FakeRecorder).Events
	expected := utils.GetEventString(membership, corev1.EventTypeNormal, eventReasonMembershipJoined, "membership joined")

	assert.Equal(t, expected, event)

	// Check expected conditions.
	expectedConditions := []metav1.Condition{
		{Type: v1alpha1.ConditionTypeMembershipJoin, Status: metav1.ConditionTrue, Reason: eventReasonMembershipJoined},
		{Type: utils.ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: utils.ReasonReconcileSuccess},
	}

	for _, expectedCondition := range expectedConditions {
		actualCondition := membership.GetCondition(expectedCondition.Type)
		assert.Equal(t, "", cmp.Diff(expectedCondition, *(actualCondition), cmpopts.IgnoreTypes(time.Time{})))
	}
}

func TestMarkMembershipLeft(t *testing.T) {
	r := Reconciler{recorder: utils.NewFakeRecorder(1)}
	membership := &v1alpha1.Membership{}

	r.markMembershipLeft(membership)

	// check that the correct event is emitted
	event := <-r.recorder.(*record.FakeRecorder).Events
	expected := utils.GetEventString(membership, corev1.EventTypeNormal, eventReasonMembershipLeft, "membership left")

	assert.Equal(t, expected, event)

	// Check expected conditions.
	expectedConditions := []metav1.Condition{
		{Type: v1alpha1.ConditionTypeMembershipJoin, Status: metav1.ConditionFalse, Reason: eventReasonMembershipLeft},
		{Type: utils.ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: utils.ReasonReconcileSuccess},
	}

	for _, expectedCondition := range expectedConditions {
		actualCondition := membership.GetCondition(expectedCondition.Type)
		assert.Equal(t, "", cmp.Diff(expectedCondition, *(actualCondition), cmpopts.IgnoreTypes(time.Time{})))
	}
}

func TestMarkMembershipUnknown(t *testing.T) {
	membership := &v1alpha1.Membership{}
	r := Reconciler{recorder: utils.NewFakeRecorder(1)}

	r.markMembershipUnknown(membership)

	// check that the correct event is emitted
	event := <-r.recorder.(*record.FakeRecorder).Events
	expected := utils.GetEventString(membership, corev1.EventTypeNormal, eventReasonMembershipUnknown, "membership unknown")

	assert.Equal(t, expected, event)

	// Check expected conditions.
	expectedConditions := []metav1.Condition{
		{Type: v1alpha1.ConditionTypeMembershipJoin, Status: metav1.ConditionUnknown, Reason: eventReasonMembershipUnknown},
		{Type: utils.ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: utils.ReasonReconcileSuccess},
	}
	for _, expectedCondition := range expectedConditions {
		actualCondition := membership.GetCondition(expectedCondition.Type)
		assert.Equal(t, "", cmp.Diff(expectedCondition, *(actualCondition), cmpopts.IgnoreTypes(time.Time{})))
	}
}

func TestWatchInternalMemberClusterChan(t *testing.T) {
	internalMemberClusterChan := make(chan v1alpha1.ClusterState)
	r := Reconciler{
		membershipChan: internalMemberClusterChan,
	}

	go r.watchInternalMemberClusterChan()

	for i := 0; i < 10; i++ {
		internalMemberClusterChan <- v1alpha1.ClusterStateJoin
	}
	s := r.getInternalMemberClusterState()
	assert.Equal(t, v1alpha1.ClusterStateJoin, s)

	for i := 0; i < 10; i++ {
		internalMemberClusterChan <- v1alpha1.ClusterStateLeave
	}
	s = r.getInternalMemberClusterState()
	assert.Equal(t, v1alpha1.ClusterStateLeave, s)
}
