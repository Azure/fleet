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
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils"
)

const (
	namespace1 = "fleet-member-mc1"
	namespace2 = "fleet-member-mc2"
	namespace3 = "fleet-member-mc3"

	clusterPropertyName1  = "cluster-property-1"
	clusterPropertyName2  = "cluster-property-2"
	clusterPropertyValue1 = "property-value-1"
	clusterPropertyValue2 = "property-value-2"

	propertyProviderConditionType1    = "ProviderConditionType1"
	propertyProviderConditionStatus1  = metav1.ConditionTrue
	propertyProviderConditionReason1  = "ProviderConditionReason1"
	propertyProviderConditionMessage1 = "property provider condition 1 message"
	propertyProviderConditionType2    = "ProviderConditionType2"
	propertyProviderConditionStatus2  = metav1.ConditionFalse
	propertyProviderConditionReason2  = "ProviderConditionReason2"
	propertyProviderConditionMessage2 = "property provider condition 2 message"
)

func TestSyncNamespace(t *testing.T) {
	tests := map[string]struct {
		r                   *Reconciler
		memberCluster       *clusterv1beta1.MemberCluster
		wantedNamespaceName string
		wantedEvent         string
		wantedError         string
	}{
		"namespace doesn't exist": {
			r: &Reconciler{
				Client: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
						return apierrors.NewNotFound(schema.GroupResource{}, "")
					},
					MockCreate: func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
						return nil
					},
				},
				recorder: utils.NewFakeRecorder(1),
			},
			memberCluster:       &clusterv1beta1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: "mc1"}},
			wantedNamespaceName: namespace1,
			wantedEvent:         utils.GetEventString(&clusterv1beta1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: "mc1"}}, corev1.EventTypeNormal, eventReasonNamespaceCreated, "Namespace was created"),
			wantedError:         "",
		},
		"namespace exists without label": {
			r: &Reconciler{
				Client: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
						o := obj.(*corev1.Namespace)
						*o = corev1.Namespace{
							ObjectMeta: metav1.ObjectMeta{
								Name:   namespace1,
								Labels: map[string]string{},
							},
						}
						return nil
					},
					MockPatch: func(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
						return nil
					},
				},
				recorder: utils.NewFakeRecorder(1),
			},
			memberCluster:       &clusterv1beta1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: "mc1"}},
			wantedNamespaceName: namespace1,
			wantedEvent:         utils.GetEventString(&clusterv1beta1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: "mc1"}}, corev1.EventTypeNormal, eventReasonNamespacePatched, "Namespace was patched"),
			wantedError:         "",
		},
		"namespace exists with label": {
			r: &Reconciler{
				Client: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
						o := obj.(*corev1.Namespace)
						*o = corev1.Namespace{
							ObjectMeta: metav1.ObjectMeta{
								Name:   namespace1,
								Labels: map[string]string{placementv1beta1.FleetResourceLabelKey: "true"},
							},
						}
						return nil
					},
				},
			},
			memberCluster:       &clusterv1beta1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: "mc1"}},
			wantedNamespaceName: namespace1,
			wantedError:         "",
		},
		"namespace create error": {
			r: &Reconciler{
				Client: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
						return apierrors.NewNotFound(schema.GroupResource{}, "")
					},
					MockCreate: func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
						return errors.New("namespace cannot be created")
					},
				},
			},
			memberCluster:       &clusterv1beta1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: "mc3"}},
			wantedNamespaceName: "",
			wantedError:         "namespace cannot be created",
		},
		"namespace get error": {
			r: &Reconciler{
				Client: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
						return errors.New("namespace cannot be retrieved")
					},
				},
			},
			memberCluster:       &clusterv1beta1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: "mc4"}},
			wantedNamespaceName: "",
			wantedError:         "namespace cannot be retrieved",
		},
		"namespace patch error": {
			r: &Reconciler{
				Client: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
						o := obj.(*corev1.Namespace)
						*o = corev1.Namespace{
							ObjectMeta: metav1.ObjectMeta{
								Name:   namespace1,
								Labels: map[string]string{},
							},
						}
						return nil
					},
					MockPatch: func(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
						return errors.New("namespace cannot be patched")
					},
				},
			},
			memberCluster:       &clusterv1beta1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: "mc1"}},
			wantedNamespaceName: "",
			wantedError:         "namespace cannot be patched",
		},
	}

	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			got, err := tt.r.syncNamespace(context.Background(), tt.memberCluster)
			if tt.r.recorder != nil {
				fakeRecorder := tt.r.recorder.(*record.FakeRecorder)
				event := <-fakeRecorder.Events
				assert.Equal(t, tt.wantedEvent, event)
			}
			if tt.wantedError == "" {
				assert.Equal(t, err, nil, utils.TestCaseMsg, testName)
			} else {
				assert.Contains(t, err.Error(), tt.wantedError, utils.TestCaseMsg, testName)
			}
			assert.Equalf(t, tt.wantedNamespaceName, got, utils.TestCaseMsg, testName)
		})
	}
}

func TestSyncRole(t *testing.T) {
	expectedMemberCluster1 := clusterv1beta1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: "mc2"}}
	expectedMemberCluster2 := clusterv1beta1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: "mc3"}}
	expectedEvent1 := utils.GetEventString(&expectedMemberCluster1, corev1.EventTypeNormal, eventReasonRoleUpdated, "role was updated")
	expectedEvent2 := utils.GetEventString(&expectedMemberCluster2, corev1.EventTypeNormal, eventReasonRoleCreated, "role was created")

	tests := map[string]struct {
		r              *Reconciler
		memberCluster  *clusterv1beta1.MemberCluster
		namespaceName  string
		wantedRoleName string
		wantedEvent    string
		wantedError    string
	}{
		"role exists but no diff": {
			r: &Reconciler{
				Client: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
						o := obj.(*rbacv1.Role)
						*o = rbacv1.Role{
							TypeMeta: metav1.TypeMeta{
								Kind:       "Role",
								APIVersion: rbacv1.SchemeGroupVersion.String(),
							},
							ObjectMeta: metav1.ObjectMeta{
								Name:      "fleet-role-mc1",
								Namespace: namespace1,
							},
							Rules: []rbacv1.PolicyRule{utils.FleetClusterRule, utils.FleetPlacementRule, utils.FleetNetworkRule, utils.EventRule},
						}
						return nil
					},
				},
			},
			memberCluster:  &clusterv1beta1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: "mc1"}},
			namespaceName:  namespace1,
			wantedRoleName: "fleet-role-mc1",
			wantedError:    "",
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
					MockUpdate: func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
						return nil
					},
				},
				recorder: utils.NewFakeRecorder(1),
			},
			memberCluster:  &expectedMemberCluster1,
			namespaceName:  namespace2,
			wantedRoleName: "fleet-role-mc2",
			wantedEvent:    expectedEvent1,
			wantedError:    "",
		},
		"role doesn't exist": {
			r: &Reconciler{
				Client: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
						return apierrors.NewNotFound(schema.GroupResource{Group: "", Resource: "Namespace"}, "namespace")
					},
					MockCreate: func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
						return nil
					},
				},
				recorder: utils.NewFakeRecorder(1),
			},
			memberCluster:  &expectedMemberCluster2,
			namespaceName:  namespace3,
			wantedRoleName: "fleet-role-mc3",
			wantedEvent:    expectedEvent2,
			wantedError:    "",
		},
		"role create error": {
			r: &Reconciler{
				Client: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
						return apierrors.NewNotFound(schema.GroupResource{Group: "", Resource: "Namespace"}, "namespace")
					},
					MockCreate: func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
						return errors.New("role cannot be created")
					}},
			},
			memberCluster:  &clusterv1beta1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: "mc4"}},
			namespaceName:  "fleet-mc4",
			wantedRoleName: "",
			wantedError:    "role cannot be created",
		},
		"role get error": {
			r: &Reconciler{
				Client: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
						return errors.New("role cannot be retrieved")
					},
				},
			},
			memberCluster:  &clusterv1beta1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: "mc5"}},
			namespaceName:  "fleet-mc5",
			wantedRoleName: "",
			wantedError:    "role cannot be retrieved",
		},
		"role update error": {
			r: &Reconciler{
				Client: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
						o := obj.(*rbacv1.Role)
						*o = rbacv1.Role{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "fleet-role-mc6",
								Namespace: "fleet-mc6",
							},
						}
						return nil
					},
					MockUpdate: func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
						return errors.New("role cannot be updated")
					},
				},
			},
			memberCluster:  &clusterv1beta1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: "mc6"}},
			namespaceName:  "fleet-mc6",
			wantedRoleName: "",
			wantedError:    "role cannot be updated",
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
			if tt.wantedError == "" {
				assert.Equal(t, err, nil, utils.TestCaseMsg, testName)
			} else {
				assert.Contains(t, err.Error(), tt.wantedError, utils.TestCaseMsg, testName)
			}
			assert.Equalf(t, tt.wantedRoleName, got, utils.TestCaseMsg, testName)
		})
	}
}

func TestSyncRoleBinding(t *testing.T) {
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

	expectedMemberCluster1 := clusterv1beta1.MemberCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "mc2"},
		Spec:       clusterv1beta1.MemberClusterSpec{Identity: identity},
	}
	expectedMemberCluster2 := clusterv1beta1.MemberCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "mc3"},
		Spec:       clusterv1beta1.MemberClusterSpec{Identity: identity},
	}
	expectedEvent1 := utils.GetEventString(&expectedMemberCluster1, corev1.EventTypeNormal, eventReasonRoleBindingUpdated, "role binding was updated")
	expectedEvent2 := utils.GetEventString(&expectedMemberCluster2, corev1.EventTypeNormal, eventReasonRoleBindingCreated, "role binding was created")

	tests := map[string]struct {
		r             *Reconciler
		memberCluster *clusterv1beta1.MemberCluster
		namespaceName string
		roleName      string
		wantedEvent   string
		wantedError   string
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
			memberCluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "mc1"},
				Spec:       clusterv1beta1.MemberClusterSpec{Identity: identity},
			},
			namespaceName: namespace1,
			roleName:      "fleet-role-mc1",
			wantedError:   "",
		},
		"role binding but with diff": {
			r: &Reconciler{
				Client: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
						roleRef := rbacv1.RoleRef{
							APIGroup: rbacv1.GroupName,
							Kind:     "Role",
							Name:     "fleet-role-mc2",
						}
						o := obj.(*rbacv1.RoleBinding)
						*o = rbacv1.RoleBinding{
							TypeMeta: metav1.TypeMeta{
								Kind:       "RoleBinding",
								APIVersion: rbacv1.SchemeGroupVersion.String(),
							},
							ObjectMeta: metav1.ObjectMeta{
								Name:      "fleet-rolebinding-mc2",
								Namespace: namespace2,
							},
							Subjects: []rbacv1.Subject{{Kind: "User", Name: "MemberClusterIdentity1"}},
							RoleRef:  roleRef,
						}
						return nil
					},
					MockUpdate: updateMock},
				recorder: utils.NewFakeRecorder(1),
			},
			memberCluster: &expectedMemberCluster1,
			namespaceName: namespace2,
			roleName:      "fleet-role-mc2",
			wantedEvent:   expectedEvent1,
			wantedError:   "",
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
			wantedEvent:   expectedEvent2,
			wantedError:   "",
		},
		"role binding create error": {
			r: &Reconciler{
				Client: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
						return apierrors.NewNotFound(schema.GroupResource{Group: "", Resource: "Namespace"}, "namespace")
					},
					MockCreate: createMock},
			},
			memberCluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "mc4"},
				Spec:       clusterv1beta1.MemberClusterSpec{Identity: identity},
			},
			namespaceName: "fleet-mc4",
			roleName:      "fleet-role-mc4",
			wantedError:   "role binding cannot be created",
		},
		"role binding get error": {
			r: &Reconciler{
				Client: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
						return errors.New("role binding cannot be retrieved")
					},
				},
			},
			memberCluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "mc5"},
				Spec:       clusterv1beta1.MemberClusterSpec{Identity: identity},
			},
			namespaceName: "fleet-mc5",
			roleName:      "fleet-role-mc5",
			wantedError:   "role binding cannot be retrieved",
		},
		"role binding update error": {
			r: &Reconciler{
				Client: &test.MockClient{
					MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
						return nil
					},
					MockUpdate: updateMock},
			},
			memberCluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "mc6"},
				Spec:       clusterv1beta1.MemberClusterSpec{Identity: identity},
			},
			namespaceName: "fleet-mc6",
			roleName:      "fleet-role-mc6",
			wantedError:   "role binding cannot be updated",
		},
	}

	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			err := tt.r.syncRoleBinding(context.Background(), tt.memberCluster, tt.namespaceName, tt.roleName)
			if tt.r.recorder != nil {
				fakeRecorder := tt.r.recorder.(*record.FakeRecorder)
				event := <-fakeRecorder.Events
				assert.Equal(t, tt.wantedEvent, event)
			}
			if tt.wantedError == "" {
				assert.Equal(t, err, nil, utils.TestCaseMsg, testName)
			} else {
				assert.Contains(t, err.Error(), tt.wantedError, utils.TestCaseMsg, testName)
			}
		})
	}
}

func TestSyncInternalMemberCluster(t *testing.T) {
	deleteTime := metav1.Now()
	updateMock := func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
		o := obj.(*clusterv1beta1.InternalMemberCluster)
		if o.Name == "mc3" {
			return errors.New("internal member cluster cannot be updated")
		}
		return nil
	}

	createMock := func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
		o := obj.(*clusterv1beta1.InternalMemberCluster)
		if o.Name == "mc5" {
			return errors.New("internal member cluster cannot be created")
		}
		return nil
	}

	expectedLeavingMemberCluster := clusterv1beta1.MemberCluster{
		TypeMeta:   metav1.TypeMeta{Kind: "MemberCluster", APIVersion: clusterv1beta1.GroupVersion.Version},
		ObjectMeta: metav1.ObjectMeta{Name: "mc1", UID: "mc1-UID", DeletionTimestamp: &deleteTime},
		Spec:       clusterv1beta1.MemberClusterSpec{HeartbeatPeriodSeconds: 10},
	}

	expectedMemberCluster2 := clusterv1beta1.MemberCluster{
		TypeMeta:   metav1.TypeMeta{Kind: "MemberCluster", APIVersion: clusterv1beta1.GroupVersion.String()},
		ObjectMeta: metav1.ObjectMeta{Name: "mc4", UID: "mc4-UID"},
		Spec:       clusterv1beta1.MemberClusterSpec{HeartbeatPeriodSeconds: 30},
	}

	expectedEvent1 := utils.GetEventString(&expectedLeavingMemberCluster, corev1.EventTypeNormal, eventReasonIMCSpecUpdated, "internal member cluster spec updated")
	expectedEvent2 := utils.GetEventString(&expectedMemberCluster2, corev1.EventTypeNormal, eventReasonIMCCreated, "Internal member cluster was created")

	tests := map[string]struct {
		r                               *Reconciler
		memberCluster                   *clusterv1beta1.MemberCluster
		namespaceName                   string
		internalMemberCluster           *clusterv1beta1.InternalMemberCluster
		wantedEvent                     string
		wantedInternalMemberClusterSpec *clusterv1beta1.InternalMemberClusterSpec
		wantedError                     string
	}{
		"internal member cluster exists and spec is updated": {
			r: &Reconciler{
				Client: &test.MockClient{
					MockUpdate: updateMock},
				recorder: utils.NewFakeRecorder(1),
			},
			memberCluster: &expectedLeavingMemberCluster,
			namespaceName: namespace1,
			internalMemberCluster: &clusterv1beta1.InternalMemberCluster{
				Spec:       clusterv1beta1.InternalMemberClusterSpec{State: clusterv1beta1.ClusterStateJoin},
				ObjectMeta: metav1.ObjectMeta{Name: "mc1", Namespace: namespace1},
			},
			wantedEvent:                     expectedEvent1,
			wantedInternalMemberClusterSpec: &clusterv1beta1.InternalMemberClusterSpec{State: clusterv1beta1.ClusterStateLeave, HeartbeatPeriodSeconds: 10},
			wantedError:                     "",
		},
		"internal member cluster exists and spec is not updated ": {
			r: &Reconciler{
				Client: &test.MockClient{
					MockUpdate: updateMock},
			},
			memberCluster: &clusterv1beta1.MemberCluster{
				TypeMeta:   metav1.TypeMeta{Kind: "MemberCluster", APIVersion: clusterv1beta1.GroupVersion.Version},
				ObjectMeta: metav1.ObjectMeta{Name: "mc2", UID: "mc2-UID", DeletionTimestamp: &deleteTime},
			},
			namespaceName: namespace2,
			internalMemberCluster: &clusterv1beta1.InternalMemberCluster{
				Spec:       clusterv1beta1.InternalMemberClusterSpec{State: clusterv1beta1.ClusterStateLeave},
				ObjectMeta: metav1.ObjectMeta{Name: "mc2", Namespace: namespace2},
			},
			wantedInternalMemberClusterSpec: &clusterv1beta1.InternalMemberClusterSpec{State: clusterv1beta1.ClusterStateLeave},
			wantedError:                     "",
		},
		"internal member cluster update error": {
			r: &Reconciler{Client: &test.MockClient{
				MockUpdate: updateMock}},
			memberCluster: &clusterv1beta1.MemberCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "mc3", DeletionTimestamp: &deleteTime},
			},
			namespaceName: namespace3,
			internalMemberCluster: &clusterv1beta1.InternalMemberCluster{
				Spec:       clusterv1beta1.InternalMemberClusterSpec{State: clusterv1beta1.ClusterStateJoin},
				ObjectMeta: metav1.ObjectMeta{Name: "mc3", Namespace: namespace2},
			},
			wantedInternalMemberClusterSpec: nil,
			wantedError:                     "internal member cluster cannot be updated",
		},
		"internal member cluster gets created": {
			r: &Reconciler{
				Client: &test.MockClient{
					MockCreate: createMock},
				recorder: utils.NewFakeRecorder(1),
			},
			memberCluster:                   &expectedMemberCluster2,
			namespaceName:                   "fleet-mc4",
			internalMemberCluster:           nil,
			wantedInternalMemberClusterSpec: &clusterv1beta1.InternalMemberClusterSpec{State: clusterv1beta1.ClusterStateJoin, HeartbeatPeriodSeconds: 30},
			wantedEvent:                     expectedEvent2,
			wantedError:                     "",
		},
		"internal member cluster create error": {
			r: &Reconciler{
				Client: &test.MockClient{
					MockCreate: createMock},
			},
			memberCluster:                   &clusterv1beta1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: "mc5"}},
			namespaceName:                   "fleet-mc5",
			internalMemberCluster:           nil,
			wantedInternalMemberClusterSpec: nil,
			wantedError:                     "internal member cluster cannot be created",
		},
	}

	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			got, err := tt.r.syncInternalMemberCluster(context.Background(), tt.memberCluster, tt.namespaceName, tt.internalMemberCluster)
			if tt.r.recorder != nil {
				fakeRecorder := tt.r.recorder.(*record.FakeRecorder)
				event := <-fakeRecorder.Events
				assert.Equal(t, tt.wantedEvent, event)
			}
			if tt.wantedInternalMemberClusterSpec != nil {
				assert.Equal(t, *tt.wantedInternalMemberClusterSpec, got.Spec, utils.TestCaseMsg, testName)
			}
			if tt.wantedError == "" {
				assert.Equal(t, err, nil, utils.TestCaseMsg, testName)
			} else {
				assert.Contains(t, err.Error(), tt.wantedError, utils.TestCaseMsg, testName)
			}
		})
	}
}

func TestMarkMemberClusterJoined(t *testing.T) {
	recorder := utils.NewFakeRecorder(1)
	memberCluster := &clusterv1beta1.MemberCluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       clusterv1beta1.InternalMemberClusterKind,
			APIVersion: clusterv1beta1.GroupVersion.String(),
		},
	}
	markMemberClusterJoined(recorder, memberCluster)

	// check that the correct event is emitted
	event := <-recorder.Events
	expected := utils.GetEventString(memberCluster, corev1.EventTypeNormal, reasonMemberClusterJoined, "member cluster joined")
	assert.Equal(t, expected, event)

	// Check expected conditions.
	expectedConditions := []metav1.Condition{
		{Type: string(clusterv1beta1.ConditionTypeMemberClusterJoined), Status: metav1.ConditionTrue, Reason: reasonMemberClusterJoined},
	}

	for i := range expectedConditions {
		actualCondition := memberCluster.GetCondition(expectedConditions[i].Type)
		assert.Equal(t, "", cmp.Diff(&expectedConditions[i], actualCondition, cmpopts.IgnoreTypes(time.Time{})))
	}
}

func TestSyncInternalMemberClusterStatus(t *testing.T) {
	now := metav1.Now()
	tests := map[string]struct {
		r                     *Reconciler
		internalMemberCluster *clusterv1beta1.InternalMemberCluster
		memberCluster         *clusterv1beta1.MemberCluster
		wantedMemberCluster   *clusterv1beta1.MemberCluster
	}{
		"copy with Joined condition": {
			r: &Reconciler{
				recorder: utils.NewFakeRecorder(1),
				agents: map[clusterv1beta1.AgentType]bool{
					clusterv1beta1.MemberAgent:              true,
					clusterv1beta1.ServiceExportImportAgent: true,
				},
			},
			internalMemberCluster: &clusterv1beta1.InternalMemberCluster{
				Status: clusterv1beta1.InternalMemberClusterStatus{
					Conditions: []metav1.Condition{
						{
							Type:               propertyProviderConditionType1,
							Status:             propertyProviderConditionStatus1,
							Reason:             propertyProviderConditionReason1,
							Message:            propertyProviderConditionMessage1,
							LastTransitionTime: now,
						},
						{
							Type:               propertyProviderConditionType2,
							Status:             propertyProviderConditionStatus2,
							Reason:             propertyProviderConditionReason2,
							Message:            propertyProviderConditionMessage2,
							LastTransitionTime: now,
						},
					},
					Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
						clusterPropertyName1: {
							Value:           clusterPropertyValue1,
							ObservationTime: now,
						},
						clusterPropertyName2: {
							Value:           clusterPropertyValue2,
							ObservationTime: now,
						},
					},
					ResourceUsage: clusterv1beta1.ResourceUsage{
						Capacity: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("16"),
							corev1.ResourceMemory: resource.MustParse("24Gi"),
						},
						Allocatable: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("12"),
							corev1.ResourceMemory: resource.MustParse("20Gi"),
						},
						Available: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("3.6"),
							corev1.ResourceMemory: resource.MustParse("4Gi"),
						},
						ObservationTime: now,
					},
					AgentStatus: []clusterv1beta1.AgentStatus{
						{
							Type: clusterv1beta1.MemberAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(clusterv1beta1.AgentJoined),
									Status: metav1.ConditionTrue,
									Reason: "Joined",
								},
							},
							LastReceivedHeartbeat: now,
						},
						{
							Type: clusterv1beta1.ServiceExportImportAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(clusterv1beta1.AgentJoined),
									Status: metav1.ConditionTrue,
									Reason: "Joined",
								},
							},
							LastReceivedHeartbeat: now,
						},
					},
				},
			},
			memberCluster: &clusterv1beta1.MemberCluster{},
			wantedMemberCluster: &clusterv1beta1.MemberCluster{
				Status: clusterv1beta1.MemberClusterStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(clusterv1beta1.ConditionTypeMemberClusterJoined),
							Status: metav1.ConditionTrue,
							Reason: reasonMemberClusterJoined,
						},
						{
							Type:    propertyProviderConditionType1,
							Status:  propertyProviderConditionStatus1,
							Reason:  propertyProviderConditionReason1,
							Message: propertyProviderConditionMessage1,
						},
						{
							Type:    propertyProviderConditionType2,
							Status:  propertyProviderConditionStatus2,
							Reason:  propertyProviderConditionReason2,
							Message: propertyProviderConditionMessage2,
						},
					},
					Properties: map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue{
						clusterPropertyName1: {
							Value:           clusterPropertyValue1,
							ObservationTime: now,
						},
						clusterPropertyName2: {
							Value:           clusterPropertyValue2,
							ObservationTime: now,
						},
					},
					ResourceUsage: clusterv1beta1.ResourceUsage{
						Capacity: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("16"),
							corev1.ResourceMemory: resource.MustParse("24Gi"),
						},
						Allocatable: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("12"),
							corev1.ResourceMemory: resource.MustParse("20Gi"),
						},
						Available: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("3.6"),
							corev1.ResourceMemory: resource.MustParse("4Gi"),
						},
						ObservationTime: now,
					},
					AgentStatus: []clusterv1beta1.AgentStatus{
						{
							Type: clusterv1beta1.MemberAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(clusterv1beta1.AgentJoined),
									Status: metav1.ConditionTrue,
									Reason: "Joined",
								},
							},
							LastReceivedHeartbeat: now,
						},
						{
							Type: clusterv1beta1.ServiceExportImportAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(clusterv1beta1.AgentJoined),
									Status: metav1.ConditionTrue,
									Reason: "Joined",
								},
							},
							LastReceivedHeartbeat: now,
						},
					},
				},
			},
		},
		"copy with Left condition": {
			r: &Reconciler{
				recorder: utils.NewFakeRecorder(2),
				agents: map[clusterv1beta1.AgentType]bool{
					clusterv1beta1.MemberAgent:              true,
					clusterv1beta1.ServiceExportImportAgent: true,
				},
			},
			internalMemberCluster: &clusterv1beta1.InternalMemberCluster{
				Status: clusterv1beta1.InternalMemberClusterStatus{
					ResourceUsage: clusterv1beta1.ResourceUsage{
						Capacity: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("100m"),
						},
						Allocatable: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("1Gi"),
						},
						ObservationTime: now,
					},
					AgentStatus: []clusterv1beta1.AgentStatus{
						{
							Type: clusterv1beta1.MemberAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(clusterv1beta1.AgentJoined),
									Status: metav1.ConditionFalse,
									Reason: "Left",
								},
							},
							LastReceivedHeartbeat: now,
						},
						{
							Type: clusterv1beta1.ServiceExportImportAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(clusterv1beta1.AgentJoined),
									Status: metav1.ConditionFalse,
									Reason: "Left",
								},
							},
							LastReceivedHeartbeat: now,
						},
					},
				},
			},
			memberCluster: &clusterv1beta1.MemberCluster{},
			wantedMemberCluster: &clusterv1beta1.MemberCluster{
				Status: clusterv1beta1.MemberClusterStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(clusterv1beta1.ConditionTypeMemberClusterJoined),
							Status: metav1.ConditionFalse,
							Reason: reasonMemberClusterLeft,
						},
						{
							Type:   string(clusterv1beta1.ConditionTypeMemberClusterReadyToJoin),
							Status: metav1.ConditionFalse,
							Reason: reasonMemberClusterNotReadyToJoin,
						},
					},
					ResourceUsage: clusterv1beta1.ResourceUsage{
						Capacity: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("100m"),
						},
						Allocatable: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("1Gi"),
						},
						ObservationTime: now,
					},
					AgentStatus: []clusterv1beta1.AgentStatus{
						{
							Type: clusterv1beta1.MemberAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(clusterv1beta1.AgentJoined),
									Status: metav1.ConditionFalse,
									Reason: "Left",
								},
							},
							LastReceivedHeartbeat: now,
						},
						{
							Type: clusterv1beta1.ServiceExportImportAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(clusterv1beta1.AgentJoined),
									Status: metav1.ConditionFalse,
									Reason: "Left",
								},
							},
							LastReceivedHeartbeat: now,
						},
					},
				},
			},
		},
		"copy with Unknown condition": {
			r: &Reconciler{
				recorder: utils.NewFakeRecorder(1),
				agents: map[clusterv1beta1.AgentType]bool{
					clusterv1beta1.MemberAgent:              true,
					clusterv1beta1.ServiceExportImportAgent: true,
				},
			},
			internalMemberCluster: &clusterv1beta1.InternalMemberCluster{
				Status: clusterv1beta1.InternalMemberClusterStatus{
					ResourceUsage: clusterv1beta1.ResourceUsage{
						Capacity: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("100m"),
						},
						Allocatable: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("1Gi"),
						},
						ObservationTime: now,
					},
					AgentStatus: []clusterv1beta1.AgentStatus{
						{
							Type: clusterv1beta1.MemberAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(clusterv1beta1.AgentJoined),
									Status: metav1.ConditionTrue,
									Reason: "Joined",
								},
							},
							LastReceivedHeartbeat: now,
						},
						{
							Type: clusterv1beta1.ServiceExportImportAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(clusterv1beta1.AgentJoined),
									Status: metav1.ConditionFalse,
									Reason: "Left",
								},
							},
							LastReceivedHeartbeat: now,
						},
					},
				},
			},
			memberCluster: &clusterv1beta1.MemberCluster{},
			wantedMemberCluster: &clusterv1beta1.MemberCluster{
				Status: clusterv1beta1.MemberClusterStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(clusterv1beta1.ConditionTypeMemberClusterJoined),
							Status: metav1.ConditionUnknown,
							Reason: reasonMemberClusterUnknown,
						},
					},
					ResourceUsage: clusterv1beta1.ResourceUsage{
						Capacity: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("100m"),
						},
						Allocatable: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("1Gi"),
						},
						ObservationTime: now,
					},
					AgentStatus: []clusterv1beta1.AgentStatus{
						{
							Type: clusterv1beta1.MemberAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(clusterv1beta1.AgentJoined),
									Status: metav1.ConditionTrue,
									Reason: "Joined",
								},
							},
							LastReceivedHeartbeat: now,
						},
						{
							Type: clusterv1beta1.ServiceExportImportAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(clusterv1beta1.AgentJoined),
									Status: metav1.ConditionFalse,
									Reason: "Left",
								},
							},
							LastReceivedHeartbeat: now,
						},
					},
				},
			},
		},
		"No Agent Status": {
			r: &Reconciler{
				recorder: utils.NewFakeRecorder(1),
				agents: map[clusterv1beta1.AgentType]bool{
					clusterv1beta1.MemberAgent: true,
				},
			},
			internalMemberCluster: &clusterv1beta1.InternalMemberCluster{
				Status: clusterv1beta1.InternalMemberClusterStatus{
					ResourceUsage: clusterv1beta1.ResourceUsage{
						Capacity: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("100m"),
						},
						Allocatable: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("1Gi"),
						},
						ObservationTime: now,
					},
				},
			},
			memberCluster: &clusterv1beta1.MemberCluster{},
			wantedMemberCluster: &clusterv1beta1.MemberCluster{
				Status: clusterv1beta1.MemberClusterStatus{
					ResourceUsage: clusterv1beta1.ResourceUsage{
						Capacity: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("100m"),
						},
						Allocatable: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("1Gi"),
						},
						ObservationTime: now,
					},
					Conditions: []metav1.Condition{
						{
							Type:   string(clusterv1beta1.ConditionTypeMemberClusterJoined),
							Status: metav1.ConditionUnknown,
							Reason: reasonMemberClusterUnknown,
						},
					},
				},
			},
		},
		"Internal member cluster is nil": {
			r: &Reconciler{
				recorder: utils.NewFakeRecorder(1),
				agents: map[clusterv1beta1.AgentType]bool{
					clusterv1beta1.MemberAgent: true,
				},
			},
			internalMemberCluster: nil,
			memberCluster:         &clusterv1beta1.MemberCluster{},
			wantedMemberCluster:   &clusterv1beta1.MemberCluster{},
		},
		"other agent type reported in the status and should be ignored": {
			r: &Reconciler{
				recorder: utils.NewFakeRecorder(1),
				agents: map[clusterv1beta1.AgentType]bool{
					clusterv1beta1.MemberAgent:              true,
					clusterv1beta1.ServiceExportImportAgent: true,
				},
			},
			internalMemberCluster: &clusterv1beta1.InternalMemberCluster{
				Status: clusterv1beta1.InternalMemberClusterStatus{
					ResourceUsage: clusterv1beta1.ResourceUsage{
						Capacity: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("100m"),
						},
						Allocatable: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("1Gi"),
						},
						ObservationTime: now,
					},
					AgentStatus: []clusterv1beta1.AgentStatus{
						{
							Type: clusterv1beta1.MemberAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(clusterv1beta1.AgentJoined),
									Status: metav1.ConditionTrue,
									Reason: "Joined",
								},
							},
							LastReceivedHeartbeat: now,
						},
						{
							Type: clusterv1beta1.ServiceExportImportAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(clusterv1beta1.AgentJoined),
									Status: metav1.ConditionTrue,
									Reason: "Joined",
								},
							},
							LastReceivedHeartbeat: now,
						},
						{
							Type: clusterv1beta1.MultiClusterServiceAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(clusterv1beta1.AgentJoined),
									Status: metav1.ConditionFalse,
									Reason: "Left",
								},
							},
							LastReceivedHeartbeat: now,
						},
					},
				},
			},
			memberCluster: &clusterv1beta1.MemberCluster{},
			wantedMemberCluster: &clusterv1beta1.MemberCluster{
				Status: clusterv1beta1.MemberClusterStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(clusterv1beta1.ConditionTypeMemberClusterJoined),
							Status: metav1.ConditionTrue,
							Reason: reasonMemberClusterJoined,
						},
					},
					ResourceUsage: clusterv1beta1.ResourceUsage{
						Capacity: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("100m"),
						},
						Allocatable: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("1Gi"),
						},
						ObservationTime: now,
					},
					AgentStatus: []clusterv1beta1.AgentStatus{
						{
							Type: clusterv1beta1.MemberAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(clusterv1beta1.AgentJoined),
									Status: metav1.ConditionTrue,
									Reason: "Joined",
								},
							},
							LastReceivedHeartbeat: now,
						},
						{
							Type: clusterv1beta1.ServiceExportImportAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(clusterv1beta1.AgentJoined),
									Status: metav1.ConditionTrue,
									Reason: "Joined",
								},
							},
							LastReceivedHeartbeat: now,
						},
						{
							Type: clusterv1beta1.MultiClusterServiceAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(clusterv1beta1.AgentJoined),
									Status: metav1.ConditionFalse,
									Reason: "Left",
								},
							},
							LastReceivedHeartbeat: now,
						},
					},
				},
			},
		},
		"less agent type reported in the status": {
			r: &Reconciler{
				recorder: utils.NewFakeRecorder(1),
				agents: map[clusterv1beta1.AgentType]bool{
					clusterv1beta1.MemberAgent:              true,
					clusterv1beta1.ServiceExportImportAgent: true,
				},
			},
			internalMemberCluster: &clusterv1beta1.InternalMemberCluster{
				Status: clusterv1beta1.InternalMemberClusterStatus{
					ResourceUsage: clusterv1beta1.ResourceUsage{
						Capacity: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("100m"),
						},
						Allocatable: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("1Gi"),
						},
						ObservationTime: now,
					},
					AgentStatus: []clusterv1beta1.AgentStatus{
						{
							Type: clusterv1beta1.MemberAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(clusterv1beta1.AgentJoined),
									Status: metav1.ConditionTrue,
									Reason: "Joined",
								},
							},
							LastReceivedHeartbeat: now,
						},
					},
				},
			},
			memberCluster: &clusterv1beta1.MemberCluster{},
			wantedMemberCluster: &clusterv1beta1.MemberCluster{
				Status: clusterv1beta1.MemberClusterStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(clusterv1beta1.ConditionTypeMemberClusterJoined),
							Status: metav1.ConditionUnknown,
							Reason: reasonMemberClusterUnknown,
						},
					},
					ResourceUsage: clusterv1beta1.ResourceUsage{
						Capacity: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("100m"),
						},
						Allocatable: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("1Gi"),
						},
						ObservationTime: now,
					},
					AgentStatus: []clusterv1beta1.AgentStatus{
						{
							Type: clusterv1beta1.MemberAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(clusterv1beta1.AgentJoined),
									Status: metav1.ConditionTrue,
									Reason: "Joined",
								},
							},
							LastReceivedHeartbeat: now,
						},
					},
				},
			},
		},
		"condition is not reported in the status": {
			r: &Reconciler{
				recorder: utils.NewFakeRecorder(1),
				agents: map[clusterv1beta1.AgentType]bool{
					clusterv1beta1.MemberAgent:              true,
					clusterv1beta1.ServiceExportImportAgent: true,
				},
			},
			internalMemberCluster: &clusterv1beta1.InternalMemberCluster{
				Status: clusterv1beta1.InternalMemberClusterStatus{
					ResourceUsage: clusterv1beta1.ResourceUsage{
						Capacity: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("100m"),
						},
						Allocatable: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("1Gi"),
						},
						ObservationTime: now,
					},
					AgentStatus: []clusterv1beta1.AgentStatus{
						{
							Type: clusterv1beta1.MemberAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(clusterv1beta1.AgentJoined),
									Status: metav1.ConditionTrue,
									Reason: "Joined",
								},
							},
							LastReceivedHeartbeat: now,
						},
						{
							Type:                  clusterv1beta1.ServiceExportImportAgent,
							LastReceivedHeartbeat: now,
						},
					},
				},
			},
			memberCluster: &clusterv1beta1.MemberCluster{},
			wantedMemberCluster: &clusterv1beta1.MemberCluster{
				Status: clusterv1beta1.MemberClusterStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(clusterv1beta1.ConditionTypeMemberClusterJoined),
							Status: metav1.ConditionUnknown,
							Reason: reasonMemberClusterUnknown,
						},
					},
					ResourceUsage: clusterv1beta1.ResourceUsage{
						Capacity: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("100m"),
						},
						Allocatable: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("1Gi"),
						},
						ObservationTime: now,
					},
					AgentStatus: []clusterv1beta1.AgentStatus{
						{
							Type: clusterv1beta1.MemberAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(clusterv1beta1.AgentJoined),
									Status: metav1.ConditionTrue,
									Reason: "Joined",
								},
							},
							LastReceivedHeartbeat: now,
						},
						{
							Type:                  clusterv1beta1.ServiceExportImportAgent,
							LastReceivedHeartbeat: now,
						},
					},
				},
			},
		},
		"agent type is not reported in the status": {
			r: &Reconciler{
				recorder: utils.NewFakeRecorder(1),
				agents: map[clusterv1beta1.AgentType]bool{
					clusterv1beta1.MemberAgent:              true,
					clusterv1beta1.ServiceExportImportAgent: true,
				},
			},
			internalMemberCluster: &clusterv1beta1.InternalMemberCluster{
				Status: clusterv1beta1.InternalMemberClusterStatus{
					ResourceUsage: clusterv1beta1.ResourceUsage{
						Capacity: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("100m"),
						},
						Allocatable: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("1Gi"),
						},
						ObservationTime: now,
					},
					AgentStatus: []clusterv1beta1.AgentStatus{
						{
							Type: clusterv1beta1.MemberAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(clusterv1beta1.AgentJoined),
									Status: metav1.ConditionTrue,
									Reason: "Joined",
								},
							},
							LastReceivedHeartbeat: now,
						},
						{
							Type:                  clusterv1beta1.MultiClusterServiceAgent,
							LastReceivedHeartbeat: now,
						},
					},
				},
			},
			memberCluster: &clusterv1beta1.MemberCluster{},
			wantedMemberCluster: &clusterv1beta1.MemberCluster{
				Status: clusterv1beta1.MemberClusterStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(clusterv1beta1.ConditionTypeMemberClusterJoined),
							Status: metav1.ConditionUnknown,
							Reason: reasonMemberClusterUnknown,
						},
					},
					ResourceUsage: clusterv1beta1.ResourceUsage{
						Capacity: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("100m"),
						},
						Allocatable: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("1Gi"),
						},
						ObservationTime: now,
					},
					AgentStatus: []clusterv1beta1.AgentStatus{
						{
							Type: clusterv1beta1.MemberAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(clusterv1beta1.AgentJoined),
									Status: metav1.ConditionTrue,
									Reason: "Joined",
								},
							},
							LastReceivedHeartbeat: now,
						},
						{
							Type:                  clusterv1beta1.MultiClusterServiceAgent,
							LastReceivedHeartbeat: now,
						},
					},
				},
			},
		},
	}

	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			tt.r.syncInternalMemberClusterStatus(tt.internalMemberCluster, tt.memberCluster)

			// Compare the Joined condition.
			diff := cmp.Diff(tt.wantedMemberCluster.GetCondition(string(clusterv1beta1.ConditionTypeMemberClusterJoined)),
				tt.memberCluster.GetCondition(string(clusterv1beta1.ConditionTypeMemberClusterJoined)),
				cmpopts.IgnoreTypes(time.Time{}))
			assert.Equal(t, "", diff)

			// Compare the property provider conditions (if present).
			diff = cmp.Diff(tt.wantedMemberCluster.GetCondition(propertyProviderConditionType1),
				tt.memberCluster.GetCondition(propertyProviderConditionType1),
				cmpopts.IgnoreTypes(time.Time{}))
			assert.Equal(t, "", diff)

			diff = cmp.Diff(tt.wantedMemberCluster.GetCondition(propertyProviderConditionType2),
				tt.memberCluster.GetCondition(propertyProviderConditionType2),
				cmpopts.IgnoreTypes(time.Time{}))
			assert.Equal(t, "", diff)

			// Compare the properties (if present).
			assert.Equal(t, tt.wantedMemberCluster.Status.Properties, tt.memberCluster.Status.Properties)
			// Compare the resource usage.
			assert.Equal(t, tt.wantedMemberCluster.Status.ResourceUsage, tt.memberCluster.Status.ResourceUsage)
			// Compare the agent status.
			assert.Equal(t, tt.wantedMemberCluster.Status.AgentStatus, tt.memberCluster.Status.AgentStatus)
		})
	}
}

func TestUpdateMemberClusterStatus(t *testing.T) {
	var count int
	tests := map[string]struct {
		r                   *Reconciler
		memberCluster       *clusterv1beta1.MemberCluster
		wantedError         string
		verifyNumberOfRetry func() bool
	}{
		"update member cluster status": {
			r: &Reconciler{Client: &test.MockClient{
				MockStatusUpdate: func(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
					count++
					return nil
				}},
				recorder: utils.NewFakeRecorder(1),
			},
			memberCluster: &clusterv1beta1.MemberCluster{},
			wantedError:   "",
			verifyNumberOfRetry: func() bool {
				return count == 0
			},
		},
		"update member cluster status within cap": {
			r: &Reconciler{Client: &test.MockClient{
				MockStatusUpdate: func(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
					count++
					if count == 3 {
						return nil
					}
					return apierrors.NewServerTimeout(schema.GroupResource{}, "", 1)
				}},
				recorder: utils.NewFakeRecorder(10),
			},
			memberCluster: &clusterv1beta1.MemberCluster{Spec: clusterv1beta1.MemberClusterSpec{HeartbeatPeriodSeconds: int32(5)}},
			wantedError:   "",
			verifyNumberOfRetry: func() bool {
				return count == 3
			},
		},
		"error updating exceeding cap for exponential backoff": {
			r: &Reconciler{Client: &test.MockClient{
				MockStatusUpdate: func(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
					count++
					return apierrors.NewServerTimeout(schema.GroupResource{}, "", 1)
				}},
				recorder: utils.NewFakeRecorder(10),
			},
			memberCluster: &clusterv1beta1.MemberCluster{},
			wantedError:   "The  operation against  could not be completed at this time, please try again.",
			verifyNumberOfRetry: func() bool {
				return count > 0
			},
		},
		"error updating within cap with error different from conflict/serverTimeout": {
			r: &Reconciler{Client: &test.MockClient{
				MockStatusUpdate: func(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
					count++
					return errors.New("random update error")
				}},
				recorder: utils.NewFakeRecorder(1),
			},
			memberCluster: &clusterv1beta1.MemberCluster{},
			wantedError:   "random update error",
			verifyNumberOfRetry: func() bool {
				return count == 0
			},
		},
	}

	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			count = -1
			err := tt.r.updateMemberClusterStatus(context.Background(), tt.memberCluster)
			if tt.wantedError == "" {
				assert.Equal(t, err, nil, utils.TestCaseMsg, testName)
			} else {
				assert.Contains(t, err.Error(), tt.wantedError, utils.TestCaseMsg, testName)
			}
			assert.Equal(t, tt.verifyNumberOfRetry(), true, utils.TestCaseMsg, testName)
		})
	}
}
