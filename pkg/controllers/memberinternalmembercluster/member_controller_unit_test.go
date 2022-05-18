package memberinternalmembercluster

import (
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	v12 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/kubernetes/pkg/apis/core"

	"go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/controllers/common"
	"go.goms.io/fleet/pkg/utils"
)

func TestMemberReconcilerGetClusterUsage(t *testing.T) {
	testCases := map[string]struct {
		nodes            v12.NodeList
		wantClusterUsage ClusterUsage
	}{
		"happy path": {
			nodes: v12.NodeList{Items: []v12.Node{{
				Status: v12.NodeStatus{Capacity: v12.ResourceList{
					v12.ResourceCPU:    *(resource.NewQuantity(10, "")),
					v12.ResourceMemory: *(resource.NewQuantity(10, "")),
				}, Allocatable: v12.ResourceList{
					v12.ResourceCPU:    *(resource.NewQuantity(10, "")),
					v12.ResourceMemory: *(resource.NewQuantity(10, "")),
				}},
			}, {
				Status: v12.NodeStatus{Capacity: v12.ResourceList{
					v12.ResourceCPU:    *(resource.NewQuantity(10, "")),
					v12.ResourceMemory: *(resource.NewQuantity(10, "")),
				}, Allocatable: v12.ResourceList{
					v12.ResourceCPU:    *(resource.NewQuantity(10, "")),
					v12.ResourceMemory: *(resource.NewQuantity(10, "")),
				}},
			}}},
			wantClusterUsage: ClusterUsage{
				AllocatableCPU:    *(resource.NewQuantity(20, "")),
				AllocatableMemory: *(resource.NewQuantity(20, "")),
				CapacityCPU:       *(resource.NewQuantity(20, "")),
				CapacityMemory:    *(resource.NewQuantity(20, "")),
			},
		},
	}
	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			clusterUsage := getClusterUsage(testCase.nodes)
			assert.Equalf(t, testCase.wantClusterUsage, clusterUsage, utils.TestCaseMsg, testName)
		})
	}
}

func TestMemberReconcilerCheckHeartbeatReceived(t *testing.T) {
	testCases := map[string]struct {
		nodes                 v12.NodeList
		wantHeartBeatReceived bool
		clusterHeartbeat      int32
	}{
		"happy path": {
			nodes: v12.NodeList{Items: []v12.Node{{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"node-role.kubernetes.io/control-plane": "rand-id"}},
				Status: v12.NodeStatus{
					Conditions: []v12.NodeCondition{{
						LastHeartbeatTime: metav1.NewTime(time.Now()),
					}},
				},
			}}},
			wantHeartBeatReceived: true,
			clusterHeartbeat:      1,
		},
		"heartbeat not received - last received heartbeat happens before clusterHeartbeat timeframe": {
			nodes: v12.NodeList{Items: []v12.Node{{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"node-role.kubernetes.io/control-plane": "rand-id"}},
				Status: v12.NodeStatus{
					Conditions: []v12.NodeCondition{{
						LastHeartbeatTime: metav1.NewTime(time.Now().Add(time.Duration(-10000000000))), // last heartbeat happens 10 seconds ago
					}},
				},
			}}},
			wantHeartBeatReceived: false,
			clusterHeartbeat:      1,
		},
		"heartbeat not received - no last heartbeat": {
			nodes: v12.NodeList{Items: []v12.Node{{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"node-role.kubernetes.io/control-plane": "rand-id"}},
				Status:     v12.NodeStatus{},
			}}},
			wantHeartBeatReceived: false,
			clusterHeartbeat:      1,
		},
	}
	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			heartbeatReceived := checkHeartbeatReceived(testCase.nodes, testCase.clusterHeartbeat)
			assert.Equalf(t, testCase.wantHeartBeatReceived, heartbeatReceived, utils.TestCaseMsg, testName)
		})
	}
}

func TestMarkInternalMemberClusterJoinSucceed(t *testing.T) {
	r := Reconciler{recorder: utils.NewFakeRecorder(1)}
	internalMemberCluster := &v1alpha1.InternalMemberCluster{}

	r.markInternalMemberClusterJoinSucceed(internalMemberCluster)

	// check that the correct event is emitted
	event := <-r.recorder.(*record.FakeRecorder).Events
	expected := utils.GetEventString(internalMemberCluster, v12.EventTypeNormal, reasonInternalMemberClusterJoined, "internal member cluster join succeed")

	assert.Equal(t, expected, event, utils.TestCaseMsg, "TestMarkInternalMemberClusterJoinSucceed")

	// Check expected conditions.
	expectedConditions := []metav1.Condition{
		{Type: v1alpha1.ConditionTypeInternalMemberClusterJoin, Status: metav1.ConditionTrue, Reason: reasonInternalMemberClusterJoined},
		{Type: common.ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: common.ReasonReconcileSuccess},
	}

	for _, expectedCondition := range expectedConditions {
		actualCondition := internalMemberCluster.GetCondition(expectedCondition.Type)
		assert.Equal(t, "", cmp.Diff(expectedCondition, *(actualCondition), cmpopts.IgnoreTypes(time.Time{})), utils.TestCaseMsg, "TestMarkInternalMemberClusterJoinSucceed")
	}
}

func TestMarkInternalMemberClusterJoinUnknown(t *testing.T) {
	tests := map[string]struct {
		wantEventType  string
		err            error
		wantConditions []metav1.Condition
		recorder       *record.FakeRecorder
	}{
		"nil error": {
			wantEventType: core.EventTypeNormal,
			err:           nil,
			wantConditions: []metav1.Condition{
				{Type: v1alpha1.ConditionTypeMembershipJoin, Status: metav1.ConditionUnknown, Reason: reasonInternalMemberClusterJoinUnknown},
				{Type: common.ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: common.ReasonReconcileSuccess},
			},
			recorder: utils.NewFakeRecorder(1),
		},
		"error": {
			wantEventType: core.EventTypeWarning,
			err:           errors.New("rand-err-msg"),
			wantConditions: []metav1.Condition{
				{Type: v1alpha1.ConditionTypeMembershipJoin, Status: metav1.ConditionUnknown, Reason: reasonInternalMemberClusterJoinUnknown, Message: "rand-err-msg"},
				{Type: common.ConditionTypeSynced, Status: metav1.ConditionFalse, Reason: common.ReasonReconcileError, Message: "rand-err-msg"},
			},
			recorder: utils.NewFakeRecorder(1),
		},
	}
	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			membership := &v1alpha1.Membership{}
			r := Reconciler{recorder: utils.NewFakeRecorder(1)}

			r.markInternalMemberClusterJoinUnknown(membership, tt.err)

			// check that the correct event is emitted
			event := <-r.recorder.(*record.FakeRecorder).Events
			expected := utils.GetEventString(membership, tt.wantEventType, reasonInternalMemberClusterJoinUnknown, "internal member cluster join unknown")

			assert.Equal(t, expected, event, utils.TestCaseMsg, testName)

			// Check expected conditions.
			for _, expectedCondition := range tt.wantConditions {
				actualCondition := membership.GetCondition(expectedCondition.Type)
				assert.Equal(t, "", cmp.Diff(expectedCondition, *(actualCondition), cmpopts.IgnoreTypes(time.Time{})), utils.TestCaseMsg, testName)
			}
		})
	}
}

func TestMarkInternalMemberClusterHeartbeatReceived(t *testing.T) {
	r := Reconciler{recorder: utils.NewFakeRecorder(1)}
	internalMemberCluster := &v1alpha1.InternalMemberCluster{}

	r.markInternalMemberClusterHeartbeatReceived(internalMemberCluster)

	// check that the correct event is emitted
	event := <-r.recorder.(*record.FakeRecorder).Events
	expected := utils.GetEventString(internalMemberCluster, v12.EventTypeNormal, reasonInternalMemberClusterHeartbeatReceived, "internal member cluster heartbeat received")

	assert.Equal(t, expected, event, utils.TestCaseMsg, "TestMarkInternalMemberClusterHeartbeatReceived")

	// Check expected conditions.
	expectedConditions := []metav1.Condition{
		{Type: v1alpha1.ConditionTypeInternalMemberClusterHeartbeat, Status: metav1.ConditionTrue, Reason: reasonInternalMemberClusterHeartbeatReceived},
		{Type: common.ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: common.ReasonReconcileSuccess},
	}

	for _, expectedCondition := range expectedConditions {
		actualCondition := internalMemberCluster.GetCondition(expectedCondition.Type)
		assert.Equal(t, "", cmp.Diff(expectedCondition, *(actualCondition), cmpopts.IgnoreTypes(time.Time{})), utils.TestCaseMsg, "TestMarkInternalMemberClusterHeartbeatReceived")
	}
}

func TestMarkInternalMemberClusterHeartbeatUnknown(t *testing.T) {
	tests := map[string]struct {
		wantEventType  string
		err            error
		wantConditions []metav1.Condition
		recorder       *record.FakeRecorder
	}{
		"nil error": {
			wantEventType: core.EventTypeNormal,
			err:           nil,
			wantConditions: []metav1.Condition{
				{Type: v1alpha1.ConditionTypeInternalMemberClusterHeartbeat, Status: metav1.ConditionUnknown, Reason: reasonInternalMemberClusterHeartbeatUnknown},
				{Type: common.ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: common.ReasonReconcileSuccess},
			},
			recorder: utils.NewFakeRecorder(1),
		},
		"error": {
			wantEventType: core.EventTypeWarning,
			err:           errors.New("rand-err-msg"),
			wantConditions: []metav1.Condition{
				{Type: v1alpha1.ConditionTypeInternalMemberClusterHeartbeat, Status: metav1.ConditionUnknown, Reason: reasonInternalMemberClusterHeartbeatUnknown, Message: "rand-err-msg"},
				{Type: common.ConditionTypeSynced, Status: metav1.ConditionFalse, Reason: common.ReasonReconcileError, Message: "rand-err-msg"},
			},
			recorder: utils.NewFakeRecorder(1),
		},
	}
	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			internalMemberCluster := &v1alpha1.InternalMemberCluster{}
			r := Reconciler{recorder: utils.NewFakeRecorder(1)}

			r.markInternalMemberClusterHeartbeatUnknown(internalMemberCluster, tt.err)

			// check that the correct event is emitted
			event := <-r.recorder.(*record.FakeRecorder).Events
			expected := utils.GetEventString(internalMemberCluster, tt.wantEventType, reasonInternalMemberClusterHeartbeatUnknown, "internal member cluster heartbeat unknown")

			assert.Equal(t, expected, event, utils.TestCaseMsg, testName)

			// Check expected conditions.
			for _, expectedCondition := range tt.wantConditions {
				actualCondition := internalMemberCluster.GetCondition(expectedCondition.Type)
				assert.Equal(t, "", cmp.Diff(expectedCondition, *(actualCondition), cmpopts.IgnoreTypes(time.Time{})), utils.TestCaseMsg, testName)
			}
		})
	}
}
