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

	"go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/controllers/common"
	"go.goms.io/fleet/pkg/utils"
)

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
