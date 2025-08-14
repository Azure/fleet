/*
Copyright 2025 The KubeFleet Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package clusterresourceplacement

import (
	"context"
	"encoding/json"
	"testing"

	"gomodules.xyz/jsonpatch/v2"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/defaulter"
)

func TestMutatingHandle(t *testing.T) {
	crpWithNoRevisionHistoryLimit := &placementv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-crp-no-revisionhistory",
		},
		Spec: placementv1beta1.PlacementSpec{
			ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{resourceSelector},
			Policy: &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickAllPlacementType,
			},
			Strategy: placementv1beta1.RolloutStrategy{
				Type: placementv1beta1.RollingUpdateRolloutStrategyType,
				RollingUpdate: &placementv1beta1.RollingUpdateConfig{
					MaxUnavailable:           ptr.To(intstr.FromInt(1)),
					MaxSurge:                 ptr.To(intstr.FromInt(2)),
					UnavailablePeriodSeconds: ptr.To(60),
				},
				ApplyStrategy: &placementv1beta1.ApplyStrategy{
					Type:             placementv1beta1.ApplyStrategyTypeClientSideApply,
					ComparisonOption: placementv1beta1.ComparisonOptionTypePartialComparison,
					WhenToApply:      placementv1beta1.WhenToApplyTypeAlways,
					WhenToTakeOver:   placementv1beta1.WhenToTakeOverTypeAlways,
				},
			},
			// RevisionHistoryLimit omitted
		},
	}

	crpWithNoPolicy := &placementv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-crp-no-policy",
		},
		Spec: placementv1beta1.PlacementSpec{
			ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{resourceSelector},
			// Policy omitted
			Strategy: placementv1beta1.RolloutStrategy{
				Type: placementv1beta1.RollingUpdateRolloutStrategyType,
				RollingUpdate: &placementv1beta1.RollingUpdateConfig{
					MaxUnavailable:           ptr.To(intstr.FromInt(1)),
					MaxSurge:                 ptr.To(intstr.FromInt(2)),
					UnavailablePeriodSeconds: ptr.To(60),
				},
				ApplyStrategy: &placementv1beta1.ApplyStrategy{
					Type:             placementv1beta1.ApplyStrategyTypeClientSideApply,
					ComparisonOption: placementv1beta1.ComparisonOptionTypePartialComparison,
					WhenToApply:      placementv1beta1.WhenToApplyTypeAlways,
					WhenToTakeOver:   placementv1beta1.WhenToTakeOverTypeAlways,
				},
			},
			RevisionHistoryLimit: ptr.To(int32(15)),
		},
	}

	crpWithNoStrategy := &placementv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-crp-no-strategy",
		},
		Spec: placementv1beta1.PlacementSpec{
			ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{resourceSelector},
			Policy: &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickAllPlacementType,
			},
			// Strategy omitted
			RevisionHistoryLimit: ptr.To(int32(15)),
		},
	}

	crpWithNoApplyStrategy := &placementv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-crp-no-apply-strategy",
		},
		Spec: placementv1beta1.PlacementSpec{
			ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{resourceSelector},
			Policy: &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickAllPlacementType,
			},
			Strategy: placementv1beta1.RolloutStrategy{
				Type: placementv1beta1.RollingUpdateRolloutStrategyType,
				RollingUpdate: &placementv1beta1.RollingUpdateConfig{
					MaxUnavailable:           ptr.To(intstr.FromInt(1)),
					MaxSurge:                 ptr.To(intstr.FromInt(3)),
					UnavailablePeriodSeconds: ptr.To(60),
				},
				// ApplyStrategy omitted
			},
			RevisionHistoryLimit: ptr.To(int32(1)),
		},
	}

	crpWithNoServerSideApplyConfig := &placementv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-crp-no-serverside-apply-config",
		},
		Spec: placementv1beta1.PlacementSpec{
			ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{resourceSelector},
			Policy: &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickAllPlacementType,
			},
			Strategy: placementv1beta1.RolloutStrategy{
				Type: placementv1beta1.RollingUpdateRolloutStrategyType,
				RollingUpdate: &placementv1beta1.RollingUpdateConfig{
					MaxUnavailable:           ptr.To(intstr.FromString("30%")),
					MaxSurge:                 ptr.To(intstr.FromString("50%")),
					UnavailablePeriodSeconds: ptr.To(int(15)),
				},
				ApplyStrategy: &placementv1beta1.ApplyStrategy{
					Type:             placementv1beta1.ApplyStrategyTypeServerSideApply,
					ComparisonOption: placementv1beta1.ComparisonOptionTypePartialComparison,
					WhenToApply:      placementv1beta1.WhenToApplyTypeAlways,
					WhenToTakeOver:   placementv1beta1.WhenToTakeOverTypeAlways,
					// ServerSideApplyConfig omitted
				},
			},
			RevisionHistoryLimit: ptr.To(int32(2)),
		},
	}

	crpWithNoRollingUpdateConfig := &placementv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-crp-no-rolling-update-config",
		},
		Spec: placementv1beta1.PlacementSpec{
			ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{resourceSelector},
			Policy: &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickFixedPlacementType,
				ClusterNames:  []string{"cluster1", "cluster2"},
			},
			Strategy: placementv1beta1.RolloutStrategy{
				Type: placementv1beta1.RollingUpdateRolloutStrategyType,
				// RollingUpdateConfig omitted
				ApplyStrategy: &placementv1beta1.ApplyStrategy{
					Type:             placementv1beta1.ApplyStrategyTypeClientSideApply,
					ComparisonOption: placementv1beta1.ComparisonOptionTypePartialComparison,
					WhenToApply:      placementv1beta1.WhenToApplyTypeAlways,
					WhenToTakeOver:   placementv1beta1.WhenToTakeOverTypeAlways,
				},
			},
			RevisionHistoryLimit: ptr.To(int32(15)),
		},
	}

	crpWithNoTolerationOperator := &placementv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-crp-no-toleration-operator",
		},
		Spec: placementv1beta1.PlacementSpec{
			ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{resourceSelector},
			Policy: &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickAllPlacementType,
				Tolerations: []placementv1beta1.Toleration{
					{
						Key:   "key",
						Value: "value",
						// Operator omitted
					},
				},
			},
			Strategy: placementv1beta1.RolloutStrategy{
				Type: placementv1beta1.ExternalRolloutStrategyType,
				ApplyStrategy: &placementv1beta1.ApplyStrategy{
					Type:             placementv1beta1.ApplyStrategyTypeClientSideApply,
					ComparisonOption: placementv1beta1.ComparisonOptionTypePartialComparison,
					WhenToApply:      placementv1beta1.WhenToApplyTypeIfNotDrifted,
					WhenToTakeOver:   placementv1beta1.WhenToTakeOverTypeNever,
				},
			},
			RevisionHistoryLimit: ptr.To(int32(15)),
		},
	}

	crpWithTopologySpreadConstraints := &placementv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-crp-topology-spread-constraints",
		},
		Spec: placementv1beta1.PlacementSpec{
			ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{resourceSelector},
			Policy: &placementv1beta1.PlacementPolicy{
				PlacementType: placementv1beta1.PickAllPlacementType,
				TopologySpreadConstraints: []placementv1beta1.TopologySpreadConstraint{
					{
						MaxSkew:     ptr.To(int32(1)),
						TopologyKey: "topology.kubernetes.io/zone",
					},
				},
			},
			Strategy: placementv1beta1.RolloutStrategy{
				Type: placementv1beta1.ExternalRolloutStrategyType,
				ApplyStrategy: &placementv1beta1.ApplyStrategy{
					Type:             placementv1beta1.ApplyStrategyTypeClientSideApply,
					ComparisonOption: placementv1beta1.ComparisonOptionTypePartialComparison,
					WhenToApply:      placementv1beta1.WhenToApplyTypeIfNotDrifted,
					WhenToTakeOver:   placementv1beta1.WhenToTakeOverTypeNever,
				},
			},
			RevisionHistoryLimit: ptr.To(int32(15)),
		},
	}

	crpWithAllFields := &placementv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-crp-all-fields",
		},
		Spec: placementv1beta1.PlacementSpec{
			ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{resourceSelector},
			Policy: &placementv1beta1.PlacementPolicy{
				PlacementType:    placementv1beta1.PickNPlacementType,
				NumberOfClusters: ptr.To(int32(3)),
				TopologySpreadConstraints: []placementv1beta1.TopologySpreadConstraint{
					{
						TopologyKey:       "kubernetes.io/hostname",
						MaxSkew:           ptr.To(int32(1)),
						WhenUnsatisfiable: placementv1beta1.DoNotSchedule,
					},
				},
				Tolerations: []placementv1beta1.Toleration{
					{
						Key:      "key",
						Value:    "value",
						Operator: corev1.TolerationOpEqual,
					},
				},
			},
			Strategy: placementv1beta1.RolloutStrategy{
				Type: placementv1beta1.RollingUpdateRolloutStrategyType,
				RollingUpdate: &placementv1beta1.RollingUpdateConfig{
					MaxUnavailable:           ptr.To(intstr.FromInt(1)),
					MaxSurge:                 ptr.To(intstr.FromInt(2)),
					UnavailablePeriodSeconds: ptr.To(60),
				},
				ApplyStrategy: &placementv1beta1.ApplyStrategy{
					Type:             placementv1beta1.ApplyStrategyTypeClientSideApply,
					ComparisonOption: placementv1beta1.ComparisonOptionTypePartialComparison,
					WhenToApply:      placementv1beta1.WhenToApplyTypeAlways,
					WhenToTakeOver:   placementv1beta1.WhenToTakeOverTypeAlways,
				},
			},
			RevisionHistoryLimit: ptr.To(int32(10)),
		},
	}

	crpWithNoRevisionHistoryLimitBytes, _ := json.Marshal(crpWithNoRevisionHistoryLimit)
	crpWithNoPolicyBytes, _ := json.Marshal(crpWithNoPolicy)
	crpWithNoStrategyBytes, _ := json.Marshal(crpWithNoStrategy)
	crpWithNoApplyStrategyBytes, _ := json.Marshal(crpWithNoApplyStrategy)
	crpWithNoRollingUpdateConfigBytes, _ := json.Marshal(crpWithNoRollingUpdateConfig)
	crpWithNoTolerationOperatorBytes, _ := json.Marshal(crpWithNoTolerationOperator)
	crpWithNoServerSideApplyConfigBytes, _ := json.Marshal(crpWithNoServerSideApplyConfig)
	crpWithTopologySpreadConstraintsBytes, _ := json.Marshal(crpWithTopologySpreadConstraints)
	crpWithAllFieldsBytes, _ := json.Marshal(crpWithAllFields)

	// Update cases
	crpUpdateMissingFieldsOld := crpWithAllFields.DeepCopy()
	crpUpdateMissingFieldsOld.ObjectMeta.Name = "test-crp-update-missing"
	crpUpdateMissingFieldsNew := &placementv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-crp-update-missing",
		},
		Spec: placementv1beta1.PlacementSpec{
			ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{resourceSelector},
			Policy: &placementv1beta1.PlacementPolicy{
				PlacementType:    placementv1beta1.PickNPlacementType, // Policy change is immutable
				NumberOfClusters: ptr.To(int32(3)),
			},
		},
	}

	crpUpdateChangeFieldOld := crpWithAllFields.DeepCopy()
	crpUpdateChangeFieldOld.ObjectMeta.Name = "test-crp-update-change-field"
	crpUpdateChangeFieldNew := &placementv1beta1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-crp-update-change-field",
		},
		Spec: placementv1beta1.PlacementSpec{
			ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{resourceSelector},
			Policy: &placementv1beta1.PlacementPolicy{
				PlacementType:    placementv1beta1.PickNPlacementType,
				NumberOfClusters: ptr.To(int32(5)), // Changed from 3 to 5
				Tolerations:      []placementv1beta1.Toleration{{Key: "foo", Value: "bar"}},
			},
			Strategy: placementv1beta1.RolloutStrategy{
				Type: placementv1beta1.RollingUpdateRolloutStrategyType,
				RollingUpdate: &placementv1beta1.RollingUpdateConfig{
					MaxUnavailable:           ptr.To(intstr.FromInt(1)),
					MaxSurge:                 ptr.To(intstr.FromInt(2)),
					UnavailablePeriodSeconds: ptr.To(60),
				},
				ApplyStrategy: &placementv1beta1.ApplyStrategy{
					Type:             placementv1beta1.ApplyStrategyTypeServerSideApply, // Changed from ClientSideApply to ServerSideApply
					ComparisonOption: placementv1beta1.ComparisonOptionTypePartialComparison,
					WhenToApply:      placementv1beta1.WhenToApplyTypeAlways,
					WhenToTakeOver:   placementv1beta1.WhenToTakeOverTypeAlways,
				},
			},
			RevisionHistoryLimit: ptr.To(int32(10)),
		},
	}

	crpUpdateAllFieldsOld := crpWithAllFields.DeepCopy()
	crpUpdateAllFieldsNew := crpWithAllFields.DeepCopy()

	crpUpdateChangeFieldOldBytes, _ := json.Marshal(crpUpdateChangeFieldOld)
	crpUpdateChangeFieldNewBytes, _ := json.Marshal(crpUpdateChangeFieldNew)
	crpUpdateMissingFieldsOldBytes, _ := json.Marshal(crpUpdateMissingFieldsOld)
	crpUpdateMissingFieldsNewBytes, _ := json.Marshal(crpUpdateMissingFieldsNew)

	crpUpdateAllFieldsOldBytes, _ := json.Marshal(crpUpdateAllFieldsOld)
	crpUpdateAllFieldsNewBytes, _ := json.Marshal(crpUpdateAllFieldsNew)

	scheme := runtime.NewScheme()
	assert.Nil(t, placementv1beta1.AddToScheme(scheme))
	decoder := admission.NewDecoder(scheme)
	mutator := &clusterResourcePlacementMutator{decoder: decoder}

	testCases := map[string]struct {
		req          admission.Request
		wantResponse admission.Response
	}{
		"should default missing revisionHistoryLimit (CREATE)": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-crp-no-revisionhistory",
					Object: runtime.RawExtension{
						Raw:    crpWithNoRevisionHistoryLimitBytes,
						Object: crpWithNoRevisionHistoryLimit,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"system:masters"},
					},
					RequestKind: &utils.ClusterResourcePlacementMetaGVK,
					Operation:   admissionv1.Create,
				},
			},
			wantResponse: admission.Response{
				Patches: []jsonpatch.JsonPatchOperation{
					{
						Operation: "add",
						Path:      "/spec/revisionHistoryLimit",
						Value:     float64(defaulter.DefaultRevisionHistoryLimitValue),
					},
				},
				AdmissionResponse: admissionv1.AdmissionResponse{
					Allowed:   true,
					PatchType: ptr.To(admissionv1.PatchTypeJSONPatch),
				},
			},
		},
		"should default missing policy (CREATE)": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-crp-no-policy",
					Object: runtime.RawExtension{
						Raw:    crpWithNoPolicyBytes,
						Object: crpWithNoPolicy,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"system:masters"},
					},
					RequestKind: &utils.ClusterResourcePlacementMetaGVK,
					Operation:   admissionv1.Create,
				},
			},
			wantResponse: admission.Response{
				Patches: []jsonpatch.JsonPatchOperation{
					{
						Operation: "add",
						Path:      "/spec/policy",
						Value:     map[string]any{"placementType": string(placementv1beta1.PickAllPlacementType)},
					},
				},
				AdmissionResponse: admissionv1.AdmissionResponse{
					Allowed:   true,
					PatchType: ptr.To(admissionv1.PatchTypeJSONPatch),
				},
			},
		},
		"should default missing strategy (CREATE)": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-crp-no-strategy",
					Object: runtime.RawExtension{
						Raw:    crpWithNoStrategyBytes,
						Object: crpWithNoStrategy,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"system:masters"},
					},
					RequestKind: &utils.ClusterResourcePlacementMetaGVK,
					Operation:   admissionv1.Create,
				},
			},
			wantResponse: admission.Response{
				AdmissionResponse: admissionv1.AdmissionResponse{
					Allowed:   true,
					PatchType: ptr.To(admissionv1.PatchTypeJSONPatch),
				},
				Patches: []jsonpatch.JsonPatchOperation{
					{
						Operation: "add",
						Path:      "/spec/strategy/type",
						Value:     string(placementv1beta1.RollingUpdateRolloutStrategyType),
					},
					{
						Operation: "add",
						Path:      "/spec/strategy/rollingUpdate",
						Value: map[string]any{
							"maxSurge":                 defaulter.DefaultMaxSurgeValue,
							"maxUnavailable":           defaulter.DefaultMaxUnavailableValue,
							"unavailablePeriodSeconds": float64(defaulter.DefaultUnavailablePeriodSeconds),
						},
					},
					{
						Operation: "add",
						Path:      "/spec/strategy/applyStrategy",
						Value: map[string]any{
							"comparisonOption": string(placementv1beta1.ComparisonOptionTypePartialComparison),
							"type":             string(placementv1beta1.ApplyStrategyTypeClientSideApply),
							"whenToApply":      string(placementv1beta1.WhenToApplyTypeAlways),
							"whenToTakeOver":   string(placementv1beta1.WhenToTakeOverTypeAlways),
						},
					},
				},
			},
		},
		"should default missing apply strategy (CREATE)": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-crp-no-apply-strategy",
					Object: runtime.RawExtension{
						Raw:    crpWithNoApplyStrategyBytes,
						Object: crpWithNoApplyStrategy,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"system:masters"},
					},
					RequestKind: &utils.ClusterResourcePlacementMetaGVK,
					Operation:   admissionv1.Create,
				},
			},
			wantResponse: admission.Response{
				AdmissionResponse: admissionv1.AdmissionResponse{
					Allowed:   true,
					PatchType: ptr.To(admissionv1.PatchTypeJSONPatch),
				},
				Patches: []jsonpatch.JsonPatchOperation{
					{
						Operation: "add",
						Path:      "/spec/strategy/applyStrategy",
						Value: map[string]any{
							"comparisonOption": string(placementv1beta1.ComparisonOptionTypePartialComparison),
							"type":             string(placementv1beta1.ApplyStrategyTypeClientSideApply),
							"whenToApply":      string(placementv1beta1.WhenToApplyTypeAlways),
							"whenToTakeOver":   string(placementv1beta1.WhenToTakeOverTypeAlways),
						},
					},
				},
			},
		},
		"should default missing rolling update config (CREATE)": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-crp-no-rolling-update-config",
					Object: runtime.RawExtension{
						Raw:    crpWithNoRollingUpdateConfigBytes,
						Object: crpWithNoRollingUpdateConfig,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"system:masters"},
					},
					RequestKind: &utils.ClusterResourcePlacementMetaGVK,
					Operation:   admissionv1.Create,
				},
			},
			wantResponse: admission.Response{
				AdmissionResponse: admissionv1.AdmissionResponse{
					Allowed:   true,
					PatchType: ptr.To(admissionv1.PatchTypeJSONPatch),
				},
				Patches: []jsonpatch.JsonPatchOperation{
					{
						Operation: "add",
						Path:      "/spec/strategy/rollingUpdate",
						Value: map[string]any{
							"maxSurge":                 defaulter.DefaultMaxSurgeValue,
							"maxUnavailable":           defaulter.DefaultMaxUnavailableValue,
							"unavailablePeriodSeconds": float64(defaulter.DefaultUnavailablePeriodSeconds),
						},
					},
				},
			},
		},
		"should default missing toleration operator (CREATE)": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-crp-no-toleration-operator",
					Object: runtime.RawExtension{
						Raw:    crpWithNoTolerationOperatorBytes,
						Object: crpWithNoTolerationOperator,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"system:masters"},
					},
					RequestKind: &utils.ClusterResourcePlacementMetaGVK,
					Operation:   admissionv1.Create,
				},
			},
			wantResponse: admission.Response{
				AdmissionResponse: admissionv1.AdmissionResponse{
					Allowed:   true,
					PatchType: ptr.To(admissionv1.PatchTypeJSONPatch),
				},
				Patches: []jsonpatch.JsonPatchOperation{
					{
						Operation: "add",
						Path:      "/spec/policy/tolerations/0/operator",
						Value:     string(corev1.TolerationOpEqual),
					},
				},
			},
		},
		"should default missing server-side apply config (CREATE)": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-crp-no-serverside-apply-config",
					Object: runtime.RawExtension{
						Raw:    crpWithNoServerSideApplyConfigBytes,
						Object: crpWithNoServerSideApplyConfig,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"system:masters"},
					},
					RequestKind: &utils.ClusterResourcePlacementMetaGVK,
					Operation:   admissionv1.Create,
				},
			},
			wantResponse: admission.Response{
				Patches: []jsonpatch.JsonPatchOperation{
					{
						Operation: "add",
						Path:      "/spec/strategy/applyStrategy/serverSideApplyConfig",
						Value: map[string]any{
							"force": false,
						},
					},
				},
				AdmissionResponse: admissionv1.AdmissionResponse{
					Allowed:   true,
					PatchType: ptr.To(admissionv1.PatchTypeJSONPatch),
				},
			},
		},
		"should default missing topology spread constraints fields(CREATE)": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-crp-topology-spread-constraints",
					Object: runtime.RawExtension{
						Raw:    crpWithTopologySpreadConstraintsBytes,
						Object: crpWithTopologySpreadConstraints,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"system:masters"},
					},
					RequestKind: &utils.ClusterResourcePlacementMetaGVK,
					Operation:   admissionv1.Create,
				},
			},
			wantResponse: admission.Response{
				Patches: []jsonpatch.JsonPatchOperation{
					{
						Operation: "add",
						Path:      "/spec/policy/topologySpreadConstraints/0/whenUnsatisfiable",
						Value:     string(placementv1beta1.DoNotSchedule),
					},
				},
				AdmissionResponse: admissionv1.AdmissionResponse{
					Allowed:   true,
					PatchType: ptr.To(admissionv1.PatchTypeJSONPatch),
				},
			},
		},
		"should not patch if all fields present (CREATE)": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-crp-all-fields",
					Object: runtime.RawExtension{
						Raw:    crpWithAllFieldsBytes,
						Object: crpWithAllFields,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"system:masters"},
					},
					RequestKind: &utils.ClusterResourcePlacementMetaGVK,
					Operation:   admissionv1.Create,
				},
			},
			wantResponse: admission.Response{
				AdmissionResponse: admissionv1.AdmissionResponse{
					Allowed: true,
				},
				Patches: []jsonpatch.JsonPatchOperation{},
			},
		},
		// Update cases
		"should default missing fields (UPDATE)": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-crp-update-missing",
					OldObject: runtime.RawExtension{
						Raw:    crpUpdateMissingFieldsOldBytes,
						Object: crpUpdateMissingFieldsOld,
					},
					Object: runtime.RawExtension{
						Raw:    crpUpdateMissingFieldsNewBytes,
						Object: crpUpdateMissingFieldsNew,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"system:masters"},
					},
					RequestKind: &utils.ClusterResourcePlacementMetaGVK,
					Operation:   admissionv1.Update,
				},
			},
			wantResponse: admission.Response{
				AdmissionResponse: admissionv1.AdmissionResponse{
					Allowed:   true,
					PatchType: ptr.To(admissionv1.PatchTypeJSONPatch),
				},
				Patches: []jsonpatch.JsonPatchOperation{
					{
						Operation: "add",
						Path:      "/spec/strategy/type",
						Value:     string(placementv1beta1.RollingUpdateRolloutStrategyType),
					},
					{
						Operation: "add",
						Path:      "/spec/strategy/rollingUpdate",
						Value: map[string]any{
							"maxSurge":                 defaulter.DefaultMaxSurgeValue,
							"maxUnavailable":           defaulter.DefaultMaxUnavailableValue,
							"unavailablePeriodSeconds": float64(defaulter.DefaultUnavailablePeriodSeconds),
						},
					},
					{
						Operation: "add",
						Path:      "/spec/strategy/applyStrategy",
						Value: map[string]any{
							"comparisonOption": string(placementv1beta1.ComparisonOptionTypePartialComparison),
							"type":             string(placementv1beta1.ApplyStrategyTypeClientSideApply),
							"whenToApply":      string(placementv1beta1.WhenToApplyTypeAlways),
							"whenToTakeOver":   string(placementv1beta1.WhenToTakeOverTypeAlways),
						},
					},
					{
						Operation: "add",
						Path:      "/spec/revisionHistoryLimit",
						Value:     float64(defaulter.DefaultRevisionHistoryLimitValue),
					},
				},
			},
		},
		"should not patch if all fields present (UPDATE)": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-crp-all-fields",
					OldObject: runtime.RawExtension{
						Raw:    crpUpdateAllFieldsOldBytes,
						Object: crpUpdateAllFieldsOld,
					},
					Object: runtime.RawExtension{
						Raw:    crpUpdateAllFieldsNewBytes,
						Object: crpUpdateAllFieldsNew,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"system:masters"},
					},
					RequestKind: &utils.ClusterResourcePlacementMetaGVK,
					Operation:   admissionv1.Update,
				},
			},
			wantResponse: admission.Response{
				AdmissionResponse: admissionv1.AdmissionResponse{
					Allowed: true,
				},
				Patches: []jsonpatch.JsonPatchOperation{},
			},
		},
		"should patch default if a field is changed (UPDATE)": {
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Name: "test-crp-all-fields",
					OldObject: runtime.RawExtension{
						Raw:    crpUpdateChangeFieldOldBytes,
						Object: crpUpdateChangeFieldOld,
					},
					Object: runtime.RawExtension{
						Raw:    crpUpdateChangeFieldNewBytes,
						Object: crpUpdateChangeFieldNew,
					},
					UserInfo: authenticationv1.UserInfo{
						Username: "test-user",
						Groups:   []string{"system:masters"},
					},
					RequestKind: &utils.ClusterResourcePlacementMetaGVK,
					Operation:   admissionv1.Update,
				},
			},
			wantResponse: admission.Response{
				AdmissionResponse: admissionv1.AdmissionResponse{
					Allowed:   true,
					PatchType: ptr.To(admissionv1.PatchTypeJSONPatch),
				},
				Patches: []jsonpatch.JsonPatchOperation{
					{
						Operation: "add",
						Path:      "/spec/policy/tolerations/0/operator",
						Value:     string(corev1.TolerationOpEqual),
					},
					{
						Operation: "add",
						Path:      "/spec/strategy/applyStrategy/serverSideApplyConfig",
						Value:     map[string]any{"force": bool(false)},
					},
				},
			},
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			resp := mutator.Handle(context.Background(), tc.req)
			cmpOptions := []cmp.Option{
				cmpopts.SortSlices(func(a, b jsonpatch.JsonPatchOperation) bool {
					if a.Path == b.Path {
						return a.Operation < b.Operation
					}
					return a.Path < b.Path
				}),
			}
			if diff := cmp.Diff(tc.wantResponse, resp, cmpOptions...); diff != "" {
				t.Errorf("Handle() mismatch (-want, got):\n%s", diff)
			}
		})
	}
}
