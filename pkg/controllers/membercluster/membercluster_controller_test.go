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
	namespace1 = "fleet-mc1"
	namespace2 = "fleet-mc2"
	namespace3 = "fleet-mc3"
)

func TestReconcilerCheckAndCreateNamespace(t *testing.T) {
	createMock := func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
		o := obj.(*corev1.Namespace)
		if o.Name == namespace2 {
			return nil
		}
		return errors.New("namespace cannot be created")
	}

	memberCluster := fleetv1alpha1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: "mc2"}}
	expectedEvent := utils.GetEventString(&memberCluster, corev1.EventTypeNormal, eventReasonNamespaceCreated, "Namespace was created")

	tests := map[string]struct {
		r                   *Reconciler
		memberCluster       *fleetv1alpha1.MemberCluster
		wantedNamespaceName string
		wantedEvent         string
		wantedError         error
	}{
		"namespace exists": {
			r: &Reconciler{
				Client: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
						o := obj.(*corev1.Namespace)
						*o = corev1.Namespace{
							ObjectMeta: metav1.ObjectMeta{Name: namespace1},
						}
						return nil
					},
				},
			},
			memberCluster:       &fleetv1alpha1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: "mc1"}},
			wantedNamespaceName: namespace1,
			wantedError:         nil,
		},
		"namespace doesn't exist": {
			r: &Reconciler{
				Client: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
						return apierrors.NewNotFound(schema.GroupResource{Group: "", Resource: "Namespace"}, "namespace")
					},
					MockCreate: createMock},
				recorder: utils.NewFakeRecorder(1),
			},
			memberCluster:       &memberCluster,
			wantedNamespaceName: namespace2,
			wantedEvent:         expectedEvent,
			wantedError:         nil,
		},
		"namespace create error": {
			r: &Reconciler{
				Client: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
						return apierrors.NewNotFound(schema.GroupResource{Group: "", Resource: "Namespace"}, "namespace")
					},
					MockCreate: createMock},
			},
			memberCluster:       &fleetv1alpha1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: "mc3"}},
			wantedNamespaceName: "",
			wantedError:         errors.New("namespace cannot be created"),
		},
		"namespace get error": {
			r: &Reconciler{
				Client: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
						return errors.New("namespace cannot be retrieved")
					},
				},
			},
			memberCluster:       &fleetv1alpha1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: "mc4"}},
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
	createMock := func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
		o := obj.(*rbacv1.Role)
		if o.Name == "fleet-role-mc3" && o.Namespace == namespace3 {
			return nil
		}
		return errors.New("role cannot be created")
	}

	updateMock := func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
		o := obj.(*rbacv1.Role)
		if o.Name == "fleet-role-mc6" && o.Namespace == "fleet-mc6" {
			return errors.New("role cannot be updated")
		}
		return nil
	}

	expectedMemberCluster1 := fleetv1alpha1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: "mc2"}}
	expectedMemberCluster2 := fleetv1alpha1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: "mc3"}}
	expectedEvent1 := utils.GetEventString(&expectedMemberCluster1, corev1.EventTypeNormal, eventReasonRoleUpdated, "role was updated")
	expectedEvent2 := utils.GetEventString(&expectedMemberCluster2, corev1.EventTypeNormal, eventReasonRoleCreated, "role was created")

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
				Client: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
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
						return nil
					},
				},
			},
			memberCluster:  &fleetv1alpha1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: "mc1"}},
			namespaceName:  namespace1,
			wantedRoleName: "fleet-role-mc1",
			wantedError:    nil,
		},
		"role exists but with diff": {
			r: &Reconciler{
				Client: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
						o := obj.(*rbacv1.Role)
						*o = rbacv1.Role{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "fleet-role-mc2",
								Namespace: namespace2,
							},
						}
						return nil
					},
					MockUpdate: updateMock},
				recorder: utils.NewFakeRecorder(1),
			},
			memberCluster:  &expectedMemberCluster1,
			namespaceName:  namespace2,
			wantedRoleName: "fleet-role-mc2",
			wantedEvent:    expectedEvent1,
			wantedError:    nil,
		},
		"role doesn't exist": {
			r: &Reconciler{
				Client: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
						return apierrors.NewNotFound(schema.GroupResource{Group: "", Resource: "Namespace"}, "namespace")
					},
					MockCreate: createMock},
				recorder: utils.NewFakeRecorder(1),
			},
			memberCluster:  &expectedMemberCluster2,
			namespaceName:  namespace3,
			wantedRoleName: "fleet-role-mc3",
			wantedEvent:    expectedEvent2,
			wantedError:    nil,
		},
		"role create error": {
			r: &Reconciler{
				Client: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
						return apierrors.NewNotFound(schema.GroupResource{Group: "", Resource: "Namespace"}, "namespace")
					},
					MockCreate: createMock},
			},
			memberCluster:  &fleetv1alpha1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: "mc4"}},
			namespaceName:  "fleet-mc4",
			wantedRoleName: "",
			wantedError:    errors.New("role cannot be created"),
		},
		"role get error": {
			r: &Reconciler{
				Client: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
						return errors.New("role cannot be retrieved")
					},
				},
			},
			memberCluster:  &fleetv1alpha1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: "mc5"}},
			namespaceName:  "fleet-mc5",
			wantedRoleName: "",
			wantedError:    errors.New("role cannot be retrieved"),
		},
		"role update error": {
			r: &Reconciler{
				Client: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
						return nil
					},
					MockUpdate: updateMock},
			},
			memberCluster:  &fleetv1alpha1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: "mc6"}},
			namespaceName:  "fleet-mc6",
			wantedRoleName: "",
			wantedError:    errors.New("role cannot be updated"),
		},
	}

	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			got, err := tt.r.syncRole(context.Background(), tt.memberCluster, tt.namespaceName)
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

	createMock := func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
		o := obj.(*rbacv1.RoleBinding)
		if o.Name == "fleet-rolebinding-mc3" && o.Namespace == namespace3 {
			return nil
		}
		return errors.New("role binding cannot be created")
	}

	updateMock := func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
		o := obj.(*rbacv1.RoleBinding)
		if o.Name == "fleet-rolebinding-mc6" && o.Namespace == "fleet-mc6" {
			return errors.New("role binding cannot be updated")
		}
		return nil
	}

	expectedMemberCluster1 := fleetv1alpha1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: "mc2"}}
	expectedMemberCluster2 := fleetv1alpha1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: "mc3"}}
	expectedEvent1 := utils.GetEventString(&expectedMemberCluster1, corev1.EventTypeNormal, eventReasonRoleBindingUpdated, "role binding was updated")
	expectedEvent2 := utils.GetEventString(&expectedMemberCluster2, corev1.EventTypeNormal, eventReasonRoleBindingCreated, "role binding was created")

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
				Client: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
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
						return nil
					},
				},
			},
			memberCluster: &fleetv1alpha1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: "mc1"}},
			namespaceName: namespace1,
			roleName:      "fleet-role-mc1",
			identity:      identity,
			wantedError:   nil,
		},
		"role binding but with diff": {
			r: &Reconciler{
				Client: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
						o := obj.(*rbacv1.RoleBinding)
						*o = rbacv1.RoleBinding{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "fleet-rolebinding-mc2",
								Namespace: namespace2,
							},
						}
						return nil
					},
					MockUpdate: updateMock},
				recorder: utils.NewFakeRecorder(1),
			},
			memberCluster: &expectedMemberCluster1,
			namespaceName: namespace2,
			roleName:      "fleet-role-mc2",
			identity:      identity,
			wantedEvent:   expectedEvent1,
			wantedError:   nil,
		},
		"role binding doesn't exist": {
			r: &Reconciler{
				Client: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
						return apierrors.NewNotFound(schema.GroupResource{Group: "", Resource: "Namespace"}, "namespace")
					},
					MockCreate: createMock},
				recorder: utils.NewFakeRecorder(1),
			},
			memberCluster: &expectedMemberCluster2,
			namespaceName: namespace3,
			roleName:      "fleet-role-mc3",
			identity:      identity,
			wantedEvent:   expectedEvent2,
			wantedError:   nil,
		},
		"role binding create error": {
			r: &Reconciler{
				Client: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
						return apierrors.NewNotFound(schema.GroupResource{Group: "", Resource: "Namespace"}, "namespace")
					},
					MockCreate: createMock},
			},
			memberCluster: &fleetv1alpha1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: "mc4"}},
			namespaceName: "fleet-mc4",
			roleName:      "fleet-role-mc4",
			identity:      identity,
			wantedError:   errors.New("role binding cannot be created"),
		},
		"role binding get error": {
			r: &Reconciler{
				Client: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
						return errors.New("role binding cannot be retrieved")
					},
				},
			},
			memberCluster: &fleetv1alpha1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: "mc5"}},
			namespaceName: "fleet-mc5",
			roleName:      "fleet-role-mc5",
			identity:      identity,
			wantedError:   errors.New("role binding cannot be retrieved"),
		},
		"role binding update error": {
			r: &Reconciler{
				Client: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
						return nil
					},
					MockUpdate: updateMock},
			},
			memberCluster: &fleetv1alpha1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: "mc6"}},
			namespaceName: "fleet-mc6",
			roleName:      "fleet-role-mc6",
			identity:      identity,
			wantedError:   errors.New("role binding cannot be updated"),
		},
	}

	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			err := tt.r.syncRoleBinding(context.Background(), tt.memberCluster, tt.namespaceName, tt.roleName, identity)
			if tt.r.recorder != nil {
				fakeRecorder := tt.r.recorder.(*record.FakeRecorder)
				event := <-fakeRecorder.Events
				assert.Equal(t, tt.wantedEvent, event)
			}
			assert.Equal(t, tt.wantedError, err, utils.TestCaseMsg, testName)
		})
	}
}

func TestMarkInternalMemberClusterStateJoin(t *testing.T) {
	updateMock := func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
		o := obj.(*fleetv1alpha1.InternalMemberCluster)
		if o.Name == "mc3" {
			return errors.New("internal member cluster cannot be updated")
		}
		return nil
	}

	createMock := func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
		o := obj.(*fleetv1alpha1.InternalMemberCluster)
		if o.Name == "mc5" {
			return errors.New("internal member cluster cannot be created")
		}
		return nil
	}

	expectedMemberCluster1 := fleetv1alpha1.MemberCluster{
		TypeMeta:   metav1.TypeMeta{Kind: "MemberCluster", APIVersion: fleetv1alpha1.GroupVersion.Version},
		ObjectMeta: metav1.ObjectMeta{Name: "mc1", UID: "mc1-UID"},
		Spec:       fleetv1alpha1.MemberClusterSpec{State: fleetv1alpha1.ClusterStateLeave},
	}

	expectedMemberCluster2 := fleetv1alpha1.MemberCluster{
		TypeMeta:   metav1.TypeMeta{Kind: "MemberCluster", APIVersion: fleetv1alpha1.GroupVersion.String()},
		ObjectMeta: metav1.ObjectMeta{Name: "mc4", UID: "mc4-UID"},
		Spec:       fleetv1alpha1.MemberClusterSpec{State: fleetv1alpha1.ClusterStateJoin},
	}

	controllerBool := true
	expectedEvent1 := utils.GetEventString(&expectedMemberCluster1, corev1.EventTypeNormal, eventReasonIMCSpecUpdated, fmt.Sprintf("internal member cluster spec is marked as %s", expectedMemberCluster1.Spec.State))
	expectedEvent2 := utils.GetEventString(&expectedMemberCluster2, corev1.EventTypeNormal, eventReasonIMCCreated, "Internal member cluster was created")

	tests := map[string]struct {
		r                           *Reconciler
		memberCluster               *fleetv1alpha1.MemberCluster
		namespaceName               string
		wantedEvent                 string
		wantedInternalMemberCluster *fleetv1alpha1.InternalMemberCluster
		wantedError                 error
	}{
		"internal member cluster exists and spec is updated": {
			r: &Reconciler{
				Client: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
						o := obj.(*fleetv1alpha1.InternalMemberCluster)
						*o = fleetv1alpha1.InternalMemberCluster{
							Spec:       fleetv1alpha1.InternalMemberClusterSpec{State: fleetv1alpha1.ClusterStateJoin},
							ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace},
						}
						return nil
					},
					MockUpdate: updateMock},
				recorder: utils.NewFakeRecorder(1),
			},
			memberCluster: &expectedMemberCluster1,
			namespaceName: namespace1,
			wantedEvent:   expectedEvent1,
			wantedInternalMemberCluster: &fleetv1alpha1.InternalMemberCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "mc1", Namespace: namespace1},
				Spec:       fleetv1alpha1.InternalMemberClusterSpec{State: fleetv1alpha1.ClusterStateLeave},
			},
			wantedError: nil,
		},
		"internal member cluster exists and spec is not updated ": {
			r: &Reconciler{
				Client: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
						o := obj.(*fleetv1alpha1.InternalMemberCluster)
						*o = fleetv1alpha1.InternalMemberCluster{
							Spec:       fleetv1alpha1.InternalMemberClusterSpec{State: fleetv1alpha1.ClusterStateLeave},
							ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace},
						}
						return nil
					},
					MockUpdate: updateMock},
			},
			memberCluster: &fleetv1alpha1.MemberCluster{
				TypeMeta:   metav1.TypeMeta{Kind: "MemberCluster", APIVersion: fleetv1alpha1.GroupVersion.Version},
				ObjectMeta: metav1.ObjectMeta{Name: "mc2", UID: "mc2-UID"},
				Spec:       fleetv1alpha1.MemberClusterSpec{State: fleetv1alpha1.ClusterStateLeave},
			},
			namespaceName: namespace2,
			wantedInternalMemberCluster: &fleetv1alpha1.InternalMemberCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "mc2", Namespace: namespace2},
				Spec:       fleetv1alpha1.InternalMemberClusterSpec{State: fleetv1alpha1.ClusterStateLeave},
			},
			wantedError: nil,
		},
		"internal member cluster update error": {
			r: &Reconciler{Client: &test.MockClient{
				MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
					o := obj.(*fleetv1alpha1.InternalMemberCluster)
					*o = fleetv1alpha1.InternalMemberCluster{
						Spec:       fleetv1alpha1.InternalMemberClusterSpec{State: fleetv1alpha1.ClusterStateJoin},
						ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace},
					}
					return nil
				},
				MockUpdate: updateMock}},
			memberCluster: &fleetv1alpha1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "mc3"},
				Spec:       fleetv1alpha1.MemberClusterSpec{State: fleetv1alpha1.ClusterStateLeave},
			},
			namespaceName: namespace3,
			wantedInternalMemberCluster: &fleetv1alpha1.InternalMemberCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "mc3", Namespace: namespace3},
				Spec:       fleetv1alpha1.InternalMemberClusterSpec{State: fleetv1alpha1.ClusterStateLeave},
			},
			wantedError: errors.New("internal member cluster cannot be updated"),
		},
		"internal member cluster gets created": {
			r: &Reconciler{
				Client: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
						return apierrors.NewNotFound(schema.GroupResource{Group: "", Resource: "InternalMemberCluster"}, key.Name)
					},
					MockCreate: createMock},
				recorder: utils.NewFakeRecorder(1),
			},
			memberCluster: &expectedMemberCluster2,
			namespaceName: "fleet-mc4",
			wantedInternalMemberCluster: &fleetv1alpha1.InternalMemberCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "mc4", Namespace: "fleet-mc4", OwnerReferences: []metav1.OwnerReference{
					{APIVersion: expectedMemberCluster2.APIVersion, Kind: expectedMemberCluster2.Kind, Name: expectedMemberCluster2.Name, UID: expectedMemberCluster2.UID, Controller: &controllerBool}}},
				Spec: fleetv1alpha1.InternalMemberClusterSpec{State: fleetv1alpha1.ClusterStateJoin},
			},
			wantedEvent: expectedEvent2,
			wantedError: nil,
		},
		"internal member cluster create error": {
			r: &Reconciler{
				Client: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
						return apierrors.NewNotFound(schema.GroupResource{Group: "", Resource: "InternalMemberCluster"}, key.Name)
					},
					MockCreate: createMock},
			},
			memberCluster: &fleetv1alpha1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: "mc5"}},
			namespaceName: "fleet-mc5",
			wantedError:   errors.New("internal member cluster cannot be created"),
		},
		"internal member cluster get error": {
			r: &Reconciler{
				Client: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
						return errors.New("internal member cluster cannot be retrieved")
					},
				},
			},
			memberCluster: &fleetv1alpha1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: "mc6"}},
			namespaceName: "fleet-mc6",
			wantedError:   errors.New("internal member cluster cannot be retrieved"),
		},
	}

	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			imc, err := tt.r.markInternalMemberClusterStateJoin(context.Background(), tt.memberCluster, tt.namespaceName)
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
			Kind:       fleetv1alpha1.InternalMemberClusterKind,
			APIVersion: fleetv1alpha1.GroupVersion.String(),
		},
	}
	markMemberClusterJoined(recorder, memberCluster)

	// check that the correct event is emitted
	event := <-recorder.Events
	expected := utils.GetEventString(memberCluster, corev1.EventTypeNormal, reasonMemberClusterJoined, "member cluster is joined")
	assert.Equal(t, expected, event)

	// Check expected conditions.
	expectedConditions := []metav1.Condition{
		{Type: fleetv1alpha1.ConditionTypeMemberClusterJoin, Status: metav1.ConditionTrue, Reason: reasonMemberClusterJoined},
	}

	for i := range expectedConditions {
		actualCondition := memberCluster.GetCondition(expectedConditions[i].Type)
		assert.Equal(t, "", cmp.Diff(&expectedConditions[i], actualCondition, cmpopts.IgnoreTypes(time.Time{})))
	}
}

func TestCopyMemberClusterStatusFromInternalMC(t *testing.T) {
	var count int
	imc := fleetv1alpha1.InternalMemberCluster{}
	heartBeatCondition := metav1.Condition{
		Type:   fleetv1alpha1.ConditionTypeInternalMemberClusterHeartbeat,
		Status: metav1.ConditionTrue,
		Reason: "InternalMemberClusterHeartbeatReceived",
	}
	imc.SetConditions(heartBeatCondition)

	tests := map[string]struct {
		r                     *Reconciler
		internalMemberCluster *fleetv1alpha1.InternalMemberCluster
		memberCluster         *fleetv1alpha1.MemberCluster
		wantErr               error
		verifyNumberOfRetry   func() bool
	}{
		"mark and update member cluster as Joined": {
			r: &Reconciler{Client: &test.MockClient{
				MockStatusUpdate: func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
					count++
					return nil
				}},
				recorder: utils.NewFakeRecorder(1),
			},
			memberCluster:         &fleetv1alpha1.MemberCluster{},
			internalMemberCluster: &imc,
			wantErr:               nil,
			verifyNumberOfRetry: func() bool {
				return count == 0
			},
		},
		"mark and update member cluster as left within heartbeat": {
			r: &Reconciler{Client: &test.MockClient{
				MockStatusUpdate: func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
					count++
					if count == 3 {
						return nil
					}
					return apierrors.NewConflict(schema.GroupResource{}, "", errors.New("error"))
				}},
				recorder: utils.NewFakeRecorder(10),
			},
			memberCluster:         &fleetv1alpha1.MemberCluster{Spec: fleetv1alpha1.MemberClusterSpec{HeartbeatPeriodSeconds: int32(5)}},
			internalMemberCluster: &imc,
			wantErr:               nil,
			verifyNumberOfRetry: func() bool {
				return count == 3
			},
		},
		"error updating exceeding cap for exponential backoff": {
			r: &Reconciler{Client: &test.MockClient{
				MockStatusUpdate: func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
					count++
					return apierrors.NewServerTimeout(schema.GroupResource{}, "", 1)
				}},
				recorder: utils.NewFakeRecorder(10),
			},
			memberCluster:         &fleetv1alpha1.MemberCluster{},
			internalMemberCluster: &imc,
			wantErr:               apierrors.NewServerTimeout(schema.GroupResource{}, "", 1),
			verifyNumberOfRetry: func() bool {
				return count > 0
			},
		},
		"error updating within cap with error different from conflict/serverTimeout": {
			r: &Reconciler{Client: &test.MockClient{
				MockStatusUpdate: func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
					count++
					return errors.New("random update error")
				}},
				recorder: utils.NewFakeRecorder(1),
			},
			memberCluster:         &fleetv1alpha1.MemberCluster{},
			internalMemberCluster: &imc,
			wantErr:               errors.New("random update error"),
			verifyNumberOfRetry: func() bool {
				return count == 0
			},
		},
	}

	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			count = -1
			err := tt.r.copyMemberClusterStatusFromInternalMC(context.Background(), tt.memberCluster, tt.internalMemberCluster)
			assert.Equal(t, tt.wantErr, err, utils.TestCaseMsg, testName)
			assert.Equal(t, tt.verifyNumberOfRetry(), true, utils.TestCaseMsg, testName)
		})
	}
}

func TestUpdateMemberClusterStatusAsLeft(t *testing.T) {
	var count int
	tests := map[string]struct {
		r                   *Reconciler
		memberCluster       *fleetv1alpha1.MemberCluster
		wantErr             error
		verifyNumberOfRetry func() bool
	}{
		"mark and update member cluster as left": {
			r: &Reconciler{Client: &test.MockClient{
				MockStatusUpdate: func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
					count++
					return nil
				}},
				recorder: utils.NewFakeRecorder(1),
			},
			memberCluster: &fleetv1alpha1.MemberCluster{},
			wantErr:       nil,
			verifyNumberOfRetry: func() bool {
				return count == 0
			},
		},
		"mark and update member cluster as left within heartbeat": {
			r: &Reconciler{Client: &test.MockClient{
				MockStatusUpdate: func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
					count++
					if count == 3 {
						return nil
					}
					return apierrors.NewConflict(schema.GroupResource{}, "", errors.New("error"))
				}},
				recorder: utils.NewFakeRecorder(10),
			},
			memberCluster: &fleetv1alpha1.MemberCluster{Spec: fleetv1alpha1.MemberClusterSpec{HeartbeatPeriodSeconds: int32(5)}},
			wantErr:       nil,
			verifyNumberOfRetry: func() bool {
				return count == 3
			},
		},
		"error updating exceeding cap for exponential backoff": {
			r: &Reconciler{Client: &test.MockClient{
				MockStatusUpdate: func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
					count++
					return apierrors.NewServerTimeout(schema.GroupResource{}, "", 1)
				}},
				recorder: utils.NewFakeRecorder(10),
			},
			memberCluster: &fleetv1alpha1.MemberCluster{},
			wantErr:       apierrors.NewServerTimeout(schema.GroupResource{}, "", 1),
			verifyNumberOfRetry: func() bool {
				return count > 0
			},
		},
		"error updating within cap with error different from conflict/serverTimeout": {
			r: &Reconciler{Client: &test.MockClient{
				MockStatusUpdate: func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
					count++
					return errors.New("random update error")
				}},
				recorder: utils.NewFakeRecorder(1),
			},
			memberCluster: &fleetv1alpha1.MemberCluster{},
			wantErr:       errors.New("random update error"),
			verifyNumberOfRetry: func() bool {
				return count == 0
			},
		},
	}

	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			count = -1
			err := tt.r.updateMemberClusterStatusAsLeft(context.Background(), tt.memberCluster)
			assert.Equal(t, tt.wantErr, err, utils.TestCaseMsg, testName)
			assert.Equal(t, tt.verifyNumberOfRetry(), true, utils.TestCaseMsg, testName)
		})
	}
}
