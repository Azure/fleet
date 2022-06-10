/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package membercluster

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
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
)

const (
	namespace1     = "fleet-mc1"
	namespace2     = "fleet-mc2"
	namespace3     = "fleet-mc3"
	namespace4     = "fleet-mc4"
	memberCluster1 = "mc1"
	memberCluster2 = "mc2"
	memberCluster3 = "mc3"
)

func TestReconcilerCheckAndCreateNamespace(t *testing.T) {
	getMock := func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
		if key.Name == namespace2 || key.Name == namespace3 {
			return apierrors.NewNotFound(schema.GroupResource{Group: "", Resource: "Namespace"}, "namespace")
		} else if key.Name == namespace1 {
			o := obj.(*corev1.Namespace)
			*o = corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespace1,
				},
			}
		} else if key.Name == namespace4 {
			return fmt.Errorf("namespace cannot be retrieved")
		}
		return nil
	}

	createMock := func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
		o := obj.(*corev1.Namespace)
		if o.Name == namespace2 {
			return nil
		}
		return fmt.Errorf("namespace cannot be created")
	}

	memberCluster1 := fleetv1alpha1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: memberCluster1}}
	memberCluster2 := fleetv1alpha1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: memberCluster2}}
	memberCluster3 := fleetv1alpha1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: memberCluster3}}
	memberCluster4 := fleetv1alpha1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: "mc4"}}

	expectedEvent := utils.GetEventString(&memberCluster2, corev1.EventTypeNormal, namespaceCreated, "Namespace was created")

	tests := map[string]struct {
		r                   *Reconciler
		memberCluster       *fleetv1alpha1.MemberCluster
		wantedNamespaceName string
		wantedEvent         string
		wantedError         error
	}{
		"namespace exists": {
			r: &Reconciler{
				Client: &test.MockClient{MockGet: getMock},
			},
			memberCluster:       &memberCluster1,
			wantedNamespaceName: namespace1,
			wantedError:         nil,
		},
		"namespace doesn't exist": {
			r: &Reconciler{
				Client:   &test.MockClient{MockGet: getMock, MockCreate: createMock},
				recorder: utils.NewFakeRecorder(1),
			},
			memberCluster:       &memberCluster2,
			wantedNamespaceName: namespace2,
			wantedEvent:         expectedEvent,
			wantedError:         nil,
		},
		"namespace create error": {
			r: &Reconciler{
				Client: &test.MockClient{MockGet: getMock, MockCreate: createMock},
			},
			memberCluster:       &memberCluster3,
			wantedNamespaceName: "",
			wantedError:         errors.New("namespace cannot be created"),
		},
		"namespace get error": {
			r: &Reconciler{
				Client: &test.MockClient{MockGet: getMock},
			},
			memberCluster:       &memberCluster4,
			wantedNamespaceName: "",
			wantedError:         errors.New("namespace cannot be retrieved"),
		},
	}

	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			got, err := tt.r.checkAndCreateNamespace(context.Background(), tt.memberCluster)
			if tt.r.recorder != nil {
				fakeRecorder := tt.r.recorder.(*record.FakeRecorder)
				event := <-fakeRecorder.Events
				assert.Equal(t, tt.wantedEvent, event)
			}
			assert.Equal(t, tt.wantedError, err, utils.TestCaseMsg, testName)
			assert.Equalf(t, tt.wantedNamespaceName, got, utils.TestCaseMsg, testName)
		})
	}
}

func TestReconcilerCheckAndCreateRole(t *testing.T) {
	getMock := func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
		if key.Name == "fleet-role-mc3" && key.Namespace == namespace3 || key.Name == "fleet-role-mc4" && key.Namespace == namespace4 {
			return apierrors.NewNotFound(schema.GroupResource{Group: "", Resource: "Namespace"}, "namespace")
		} else if key.Name == "fleet-role-mc1" && key.Namespace == namespace1 {
			o := obj.(*rbacv1.Role)
			verbs := []string{"get", "list", "update", "patch", "watch"}
			apiGroups := []string{"", fleetv1alpha1.GroupVersion.Group}
			resources := []string{"*"}

			rule := rbacv1.PolicyRule{
				Verbs:     verbs,
				APIGroups: apiGroups,
				Resources: resources,
			}
			*o = rbacv1.Role{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Role",
					APIVersion: rbacv1.SchemeGroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "fleet-role-mc1",
					Namespace: namespace1,
				},
				Rules: []rbacv1.PolicyRule{rule},
			}
		} else if key.Name == "fleet-role-mc2" && key.Namespace == namespace2 {
			o := obj.(*rbacv1.Role)
			*o = rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "fleet-role-mc2",
					Namespace: namespace2,
				},
			}
		} else if key.Name == "fleet-role-mc5" && key.Namespace == "fleet-mc5" {
			return fmt.Errorf("role cannot be retrieved")
		}
		return nil
	}

	createMock := func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
		o := obj.(*rbacv1.Role)
		if o.Name == "fleet-role-mc3" && o.Namespace == namespace3 {
			return nil
		}
		return fmt.Errorf("role cannot be created")
	}

	updateMock := func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
		o := obj.(*rbacv1.Role)
		if o.Name == "fleet-role-mc6" && o.Namespace == "fleet-mc6" {
			return fmt.Errorf("role cannot be updated")
		}
		return nil
	}

	memberCluster1 := fleetv1alpha1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: memberCluster1}}
	memberCluster2 := fleetv1alpha1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: memberCluster2}}
	memberCluster3 := fleetv1alpha1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: memberCluster3}}
	memberCluster4 := fleetv1alpha1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: "mc4"}}
	memberCluster5 := fleetv1alpha1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: "mc5"}}
	memberCluster6 := fleetv1alpha1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: "mc6"}}

	expectedEvent1 := utils.GetEventString(&memberCluster2, corev1.EventTypeNormal, roleUpdated, "role was updated")
	expectedEvent2 := utils.GetEventString(&memberCluster3, corev1.EventTypeNormal, roleCreated, "role was created")

	tests := map[string]struct {
		r              *Reconciler
		memberCluster  *fleetv1alpha1.MemberCluster
		namespaceName  string
		wantedRoleName string
		wantedEvent    string
		wantedError    error
	}{
		"role exists but no diff": {
			r: &Reconciler{
				Client: &test.MockClient{MockGet: getMock},
			},
			memberCluster:  &memberCluster1,
			namespaceName:  namespace1,
			wantedRoleName: "fleet-role-mc1",
			wantedError:    nil,
		},
		"role exists but with diff": {
			r: &Reconciler{
				Client:   &test.MockClient{MockGet: getMock, MockUpdate: updateMock},
				recorder: utils.NewFakeRecorder(1),
			},
			memberCluster:  &memberCluster2,
			namespaceName:  namespace2,
			wantedRoleName: "fleet-role-mc2",
			wantedEvent:    expectedEvent1,
			wantedError:    nil,
		},
		"role doesn't exist": {
			r: &Reconciler{
				Client:   &test.MockClient{MockGet: getMock, MockCreate: createMock},
				recorder: utils.NewFakeRecorder(1),
			},
			memberCluster:  &memberCluster3,
			namespaceName:  namespace3,
			wantedRoleName: "fleet-role-mc3",
			wantedEvent:    expectedEvent2,
			wantedError:    nil,
		},
		"role create error": {
			r: &Reconciler{
				Client: &test.MockClient{MockGet: getMock, MockCreate: createMock},
			},
			memberCluster:  &memberCluster4,
			namespaceName:  namespace4,
			wantedRoleName: "",
			wantedError:    errors.New("role cannot be created"),
		},
		"role get error": {
			r: &Reconciler{
				Client: &test.MockClient{MockGet: getMock},
			},
			memberCluster:  &memberCluster5,
			namespaceName:  "fleet-mc5",
			wantedRoleName: "",
			wantedError:    errors.New("role cannot be retrieved"),
		},
		"role update error": {
			r: &Reconciler{
				Client: &test.MockClient{MockGet: getMock, MockUpdate: updateMock},
			},
			memberCluster:  &memberCluster6,
			namespaceName:  "fleet-mc6",
			wantedRoleName: "",
			wantedError:    errors.New("role cannot be updated"),
		},
	}

	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			got, err := tt.r.checkAndCreateRole(context.Background(), tt.memberCluster, tt.namespaceName)
			if tt.r.recorder != nil {
				fakeRecorder := tt.r.recorder.(*record.FakeRecorder)
				event := <-fakeRecorder.Events
				assert.Equal(t, tt.wantedEvent, event)
			}
			assert.Equal(t, tt.wantedError, err, utils.TestCaseMsg, testName)
			assert.Equalf(t, tt.wantedRoleName, got, utils.TestCaseMsg, testName)
		})
	}
}

func TestReconcilerCheckAndCreateRolebinding(t *testing.T) {
	identity := rbacv1.Subject{
		Kind: "User",
		Name: "MemberClusterIdentity",
	}
	getMock := func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
		if key.Name == "fleet-rolebinding-mc3" && key.Namespace == namespace3 || key.Name == "fleet-rolebinding-mc4" && key.Namespace == namespace4 {
			return apierrors.NewNotFound(schema.GroupResource{Group: "", Resource: "Namespace"}, "namespace")
		} else if key.Name == "fleet-rolebinding-mc1" && key.Namespace == namespace1 {
			roleRef := rbacv1.RoleRef{
				APIGroup: rbacv1.GroupName,
				Kind:     "Role",
				Name:     "fleet-role-mc1",
			}
			o := obj.(*rbacv1.RoleBinding)
			*o = rbacv1.RoleBinding{
				TypeMeta: metav1.TypeMeta{
					Kind:       "RoleBinding",
					APIVersion: rbacv1.SchemeGroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "fleet-rolebinding-mc1",
					Namespace: namespace1,
				},
				Subjects: []rbacv1.Subject{identity},
				RoleRef:  roleRef,
			}
		} else if key.Name == "fleet-rolebinding-mc2" && key.Namespace == namespace2 {
			o := obj.(*rbacv1.RoleBinding)
			*o = rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "fleet-rolebinding-mc2",
					Namespace: namespace2,
				},
			}
		} else if key.Name == "fleet-rolebinding-mc5" && key.Namespace == "fleet-mc5" {
			return fmt.Errorf("role binding cannot be retrieved")
		}
		return nil
	}

	createMock := func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
		o := obj.(*rbacv1.RoleBinding)
		if o.Name == "fleet-rolebinding-mc3" && o.Namespace == namespace3 {
			return nil
		}
		return fmt.Errorf("role binding cannot be created")
	}

	updateMock := func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
		o := obj.(*rbacv1.RoleBinding)
		if o.Name == "fleet-rolebinding-mc6" && o.Namespace == "fleet-mc6" {
			return fmt.Errorf("role binding cannot be updated")
		}
		return nil
	}

	memberCluster1 := fleetv1alpha1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: memberCluster1}}
	memberCluster2 := fleetv1alpha1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: memberCluster2}}
	memberCluster3 := fleetv1alpha1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: memberCluster3}}
	memberCluster4 := fleetv1alpha1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: "mc4"}}
	memberCluster5 := fleetv1alpha1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: "mc5"}}
	memberCluster6 := fleetv1alpha1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: "mc6"}}

	expectedEvent1 := utils.GetEventString(&memberCluster2, corev1.EventTypeNormal, roleBindingUpdated, "role binding was updated")
	expectedEvent2 := utils.GetEventString(&memberCluster3, corev1.EventTypeNormal, roleBindingCreated, "role binding was created")

	tests := map[string]struct {
		r             *Reconciler
		memberCluster *fleetv1alpha1.MemberCluster
		namespaceName string
		roleName      string
		identity      rbacv1.Subject
		wantedEvent   string
		wantedError   error
	}{
		"role binding but no diff": {
			r: &Reconciler{
				Client: &test.MockClient{MockGet: getMock},
			},
			memberCluster: &memberCluster1,
			namespaceName: namespace1,
			roleName:      "fleet-role-mc1",
			identity:      identity,
			wantedError:   nil,
		},
		"role binding but with diff": {
			r: &Reconciler{
				Client:   &test.MockClient{MockGet: getMock, MockUpdate: updateMock},
				recorder: utils.NewFakeRecorder(1),
			},
			memberCluster: &memberCluster2,
			namespaceName: namespace2,
			roleName:      "fleet-role-mc2",
			identity:      identity,
			wantedEvent:   expectedEvent1,
			wantedError:   nil,
		},
		"role binding doesn't exist": {
			r: &Reconciler{
				Client:   &test.MockClient{MockGet: getMock, MockCreate: createMock},
				recorder: utils.NewFakeRecorder(1),
			},
			memberCluster: &memberCluster3,
			namespaceName: namespace3,
			roleName:      "fleet-role-mc3",
			identity:      identity,
			wantedEvent:   expectedEvent2,
			wantedError:   nil,
		},
		"role binding create error": {
			r: &Reconciler{
				Client: &test.MockClient{MockGet: getMock, MockCreate: createMock},
			},
			memberCluster: &memberCluster4,
			namespaceName: namespace4,
			roleName:      "fleet-role-mc4",
			identity:      identity,
			wantedError:   errors.New("role binding cannot be created"),
		},
		"role binding get error": {
			r: &Reconciler{
				Client: &test.MockClient{MockGet: getMock},
			},
			memberCluster: &memberCluster5,
			namespaceName: "fleet-mc5",
			roleName:      "fleet-role-mc5",
			identity:      identity,
			wantedError:   errors.New("role binding cannot be retrieved"),
		},
		"role binding update error": {
			r: &Reconciler{
				Client: &test.MockClient{MockGet: getMock, MockUpdate: updateMock},
			},
			memberCluster: &memberCluster6,
			namespaceName: "fleet-mc6",
			roleName:      "fleet-role-mc6",
			identity:      identity,
			wantedError:   errors.New("role binding cannot be updated"),
		},
	}

	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			err := tt.r.checkAndCreateRoleBinding(context.Background(), tt.memberCluster, tt.namespaceName, tt.roleName, identity)
			if tt.r.recorder != nil {
				fakeRecorder := tt.r.recorder.(*record.FakeRecorder)
				event := <-fakeRecorder.Events
				assert.Equal(t, tt.wantedEvent, event)
			}
			assert.Equal(t, tt.wantedError, err, utils.TestCaseMsg, testName)
		})
	}
}

func TestCheckAndCreateInternalMemberCluster(t *testing.T) {
	getMock := func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
		if key.Name == memberCluster2 && key.Namespace == namespace2 || key.Name == memberCluster3 && key.Namespace == namespace3 {
			return apierrors.NewNotFound(schema.GroupResource{Group: "", Resource: "Namespace"}, "namespace")
		} else if key.Name == memberCluster1 && key.Namespace == namespace1 {
			o := obj.(*fleetv1alpha1.InternalMemberCluster)
			*o = fleetv1alpha1.InternalMemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      memberCluster1,
					Namespace: namespace1,
				},
			}
		} else if key.Name == "mc4" && key.Namespace == namespace4 {
			return fmt.Errorf("internal member cluster cannot be retrieved")
		}
		return nil
	}

	createMock := func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
		o := obj.(*fleetv1alpha1.InternalMemberCluster)
		if o.Name == memberCluster2 && o.Namespace == namespace2 {
			return nil
		}
		return fmt.Errorf("internal member cluster cannot be updated")
	}

	memberCluster1 := fleetv1alpha1.MemberCluster{
		TypeMeta:   metav1.TypeMeta{Kind: "MemberCluster", APIVersion: fleetv1alpha1.GroupVersion.Version},
		ObjectMeta: metav1.ObjectMeta{Name: memberCluster1, UID: "mc1-UID"},
	}
	memberCluster2 := fleetv1alpha1.MemberCluster{
		TypeMeta:   metav1.TypeMeta{Kind: "MemberCluster", APIVersion: fleetv1alpha1.GroupVersion.Version},
		ObjectMeta: metav1.ObjectMeta{Name: memberCluster2, UID: "mc2-UID"},
	}
	memberCluster3 := fleetv1alpha1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: memberCluster3}}
	memberCluster4 := fleetv1alpha1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: "mc4"}}

	controllerBool := true
	expectedEvent := utils.GetEventString(&memberCluster2, corev1.EventTypeNormal, internalMemberClusterCreated, "Internal member cluster was created")
	expectedOwnerRef2 := metav1.OwnerReference{APIVersion: memberCluster2.APIVersion, Kind: memberCluster2.Kind, Name: memberCluster2.Name, UID: memberCluster2.UID, Controller: &controllerBool}
	expectedInternalMemberCluster1 := fleetv1alpha1.InternalMemberCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "mc1", Namespace: namespace1},
	}
	expectedInternalMemberCluster2 := fleetv1alpha1.InternalMemberCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "mc2", Namespace: namespace2, OwnerReferences: []metav1.OwnerReference{expectedOwnerRef2}},
		Spec:       fleetv1alpha1.InternalMemberClusterSpec{State: fleetv1alpha1.ClusterStateJoin},
	}

	tests := map[string]struct {
		r                           *Reconciler
		memberCluster               *fleetv1alpha1.MemberCluster
		namespaceName               string
		wantedEvent                 string
		wantedInternalMemberCluster *fleetv1alpha1.InternalMemberCluster
		wantedError                 error
	}{
		"internal member cluster exists": {
			r:                           &Reconciler{Client: &test.MockClient{MockGet: getMock}},
			memberCluster:               &memberCluster1,
			namespaceName:               namespace1,
			wantedInternalMemberCluster: &expectedInternalMemberCluster1,
			wantedError:                 nil,
		},
		"internal member cluster doesn't exist": {
			r: &Reconciler{
				Client:   &test.MockClient{MockGet: getMock, MockCreate: createMock},
				recorder: utils.NewFakeRecorder(1),
			},
			memberCluster:               &memberCluster2,
			namespaceName:               namespace2,
			wantedEvent:                 expectedEvent,
			wantedInternalMemberCluster: &expectedInternalMemberCluster2,
			wantedError:                 nil,
		},
		"internal member create error": {
			r:                           &Reconciler{Client: &test.MockClient{MockGet: getMock, MockCreate: createMock}},
			memberCluster:               &memberCluster3,
			namespaceName:               namespace3,
			wantedInternalMemberCluster: nil,
			wantedError:                 errors.New("internal member cluster cannot be updated"),
		},
		"internal member get error": {
			r:                           &Reconciler{Client: &test.MockClient{MockGet: getMock}},
			memberCluster:               &memberCluster4,
			namespaceName:               namespace4,
			wantedInternalMemberCluster: nil,
			wantedError:                 errors.New("internal member cluster cannot be retrieved"),
		},
	}

	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			imc, err := tt.r.checkAndCreateInternalMemberCluster(context.Background(), tt.memberCluster, tt.namespaceName)
			if tt.r.recorder != nil {
				fakeRecorder := tt.r.recorder.(*record.FakeRecorder)
				event := <-fakeRecorder.Events
				assert.Equal(t, tt.wantedEvent, event)
			}
			assert.Equal(t, tt.wantedInternalMemberCluster, imc, utils.TestCaseMsg, testName)
			assert.Equal(t, tt.wantedError, err, utils.TestCaseMsg, testName)
		})
	}
}

func TestMarkMemberClusterJoined(t *testing.T) {
	recorder := utils.NewFakeRecorder(1)
	memberCluster := &fleetv1alpha1.MemberCluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       InternalMemberClusterKind.Kind,
			APIVersion: InternalMemberClusterKind.GroupVersion().String(),
		},
	}
	markMemberClusterJoined(recorder, memberCluster)

	// check that the correct event is emitted
	event := <-recorder.Events
	expected := utils.GetEventString(memberCluster, corev1.EventTypeNormal, memberClusterJoined, "member cluster is joined")
	assert.Equal(t, expected, event)

	// Check expected conditions.
	expectedConditions := []metav1.Condition{
		{Type: fleetv1alpha1.ConditionTypeMemberClusterJoin, Status: metav1.ConditionTrue, Reason: memberClusterJoined},
	}

	for i := range expectedConditions {
		actualCondition := memberCluster.GetCondition(expectedConditions[i].Type)
		assert.Equal(t, "", cmp.Diff(&expectedConditions[i], actualCondition, cmpopts.IgnoreTypes(time.Time{})))
	}
}

func TestMarkMemberClusterHeartbeatReceived(t *testing.T) {
	recorder := utils.NewFakeRecorder(1)
	memberCluster := &fleetv1alpha1.MemberCluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       InternalMemberClusterKind.Kind,
			APIVersion: InternalMemberClusterKind.GroupVersion().String(),
		},
	}
	markMemberClusterHeartbeatReceived(recorder, memberCluster)

	// check that the correct event is emitted
	event := <-recorder.Events
	expected := utils.GetEventString(memberCluster, corev1.EventTypeNormal, heartBeatReceived, "member cluster heartbeat received")
	assert.Equal(t, expected, event)

	// Check expected conditions.
	expectedConditions := []metav1.Condition{
		{Type: fleetv1alpha1.ConditionTypeInternalMemberClusterHeartbeat, Status: metav1.ConditionTrue, Reason: heartBeatReceived},
		{Type: utils.ConditionTypeSynced, Status: metav1.ConditionTrue, Reason: utils.ReasonReconcileSuccess},
	}

	for i := range expectedConditions {
		actualCondition := memberCluster.GetCondition(expectedConditions[i].Type)
		assert.Equal(t, "", cmp.Diff(&expectedConditions[i], actualCondition, cmpopts.IgnoreTypes(time.Time{})))
	}
}

func TestUpdateInternalMemberClusterSpec(t *testing.T) {
	getMock := func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
		if key.Name == memberCluster1 && key.Namespace == namespace1 || key.Name == memberCluster3 && key.Namespace == namespace3 {
			o := obj.(*fleetv1alpha1.InternalMemberCluster)
			*o = fleetv1alpha1.InternalMemberCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
				},
			}
		} else if key.Name == memberCluster2 && key.Namespace == namespace2 {
			return fmt.Errorf("internal member cluster cannot be retrieved")
		}
		return nil
	}

	patchMock := test.NewMockPatchFn(nil, func(obj client.Object) error {
		imc := obj.(*fleetv1alpha1.InternalMemberCluster)
		if imc.Name == memberCluster3 && imc.Namespace == namespace3 {
			return fmt.Errorf("internal member cluster cannot be patched")
		}
		return nil
	})

	memberCluster1 := fleetv1alpha1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: memberCluster1}}
	memberCluster2 := fleetv1alpha1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: memberCluster2}}
	memberCluster3 := fleetv1alpha1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: memberCluster3}}

	expectedEvent := utils.GetEventString(&memberCluster1, corev1.EventTypeNormal, internalMemberClusterSpecUpdated, "internal member cluster spec is marked as leave")

	tests := map[string]struct {
		r             *Reconciler
		memberCluster *fleetv1alpha1.MemberCluster
		wantedEvent   string
		wantedError   error
	}{
		"internal member cluster spec is updated": {
			r: &Reconciler{
				Client:   &test.MockClient{MockGet: getMock, MockPatch: patchMock},
				recorder: utils.NewFakeRecorder(1),
			},
			memberCluster: &memberCluster1,
			wantedEvent:   expectedEvent,
			wantedError:   nil,
		},
		"internal member cluster get error": {
			r: &Reconciler{
				Client: &test.MockClient{MockGet: getMock, MockPatch: patchMock},
			},
			memberCluster: &memberCluster2,
			wantedEvent:   "",
			wantedError:   errors.New("internal member cluster cannot be retrieved"),
		},
		"internal member cluster patch error": {
			r: &Reconciler{
				Client: &test.MockClient{MockGet: getMock, MockPatch: patchMock},
			},
			memberCluster: &memberCluster3,
			wantedEvent:   "",
			wantedError:   errors.New("internal member cluster cannot be patched"),
		},
	}

	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			err := tt.r.updateInternalMemberClusterSpec(context.Background(), tt.memberCluster)
			if tt.r.recorder != nil {
				fakeRecorder := tt.r.recorder.(*record.FakeRecorder)
				event := <-fakeRecorder.Events
				assert.Equal(t, tt.wantedEvent, event)
			}
			assert.Equal(t, tt.wantedError, err, utils.TestCaseMsg, testName)
		})
	}
}
