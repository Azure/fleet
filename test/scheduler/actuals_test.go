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

package tests

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
)

// This file features common actuals (and utilities for generating actuals) in the test suites.

func noBindingsCreatedForPlacementActual(placementKey types.NamespacedName) func() error {
	return func() error {
		bindingList, err := listBindings(placementKey)
		if err != nil {
			return fmt.Errorf("failed to list bindings for placement %s: %w", placementKey, err)
		}

		// Check that the returned list is empty.
		if bindingCount := len(bindingList.GetBindingObjs()); bindingCount != 0 {
			return fmt.Errorf("%d bindings have been created unexpectedly", bindingCount)
		}

		return nil
	}
}

func placementSchedulerFinalizerAddedActual(placementKey types.NamespacedName) func() error {
	return func() error {
		// Retrieve the placement.
		var placement placementv1beta1.PlacementObj
		if placementKey.Namespace == "" {
			// Retrieve CRP.
			placement = &placementv1beta1.ClusterResourcePlacement{}
		} else {
			// Retrieve RP.
			placement = &placementv1beta1.ResourcePlacement{}
		}
		if err := hubClient.Get(ctx, types.NamespacedName{Name: placementKey.Name, Namespace: placementKey.Namespace}, placement); err != nil {
			return err
		}

		// Check that the scheduler finalizer has been added.
		if !controllerutil.ContainsFinalizer(placement, placementv1beta1.SchedulerCleanupFinalizer) {
			return fmt.Errorf("scheduler cleanup finalizer has not been added")
		}

		return nil
	}
}

func placementSchedulerFinalizerRemovedActual(placementKey types.NamespacedName) func() error {
	return func() error {
		// Retrieve the placement.
		var placement placementv1beta1.PlacementObj
		if placementKey.Namespace == "" {
			// Retrieve CRP.
			placement = &placementv1beta1.ClusterResourcePlacement{}
		} else {
			// Retrieve RP.
			placement = &placementv1beta1.ResourcePlacement{}
		}
		if err := hubClient.Get(ctx, types.NamespacedName{Name: placementKey.Name, Namespace: placementKey.Namespace}, placement); err != nil {
			return err
		}

		// Check that the scheduler finalizer has been removed.
		if controllerutil.ContainsFinalizer(placement, placementv1beta1.SchedulerCleanupFinalizer) {
			return fmt.Errorf("scheduler cleanup finalizer is still present")
		}

		return nil
	}
}

func scheduledBindingsCreatedOrUpdatedForClustersActual(clusters []string, scoreByCluster map[string]*placementv1beta1.ClusterScore, placementKey types.NamespacedName, policySnapshotName string) func() error {
	return func() error {
		bindingList, err := listBindings(placementKey)
		if err != nil {
			return fmt.Errorf("failed to list bindings for placement %s: %w", placementKey, err)
		}
		// Find all the scheduled bindings.
		scheduled := []placementv1beta1.BindingObj{}
		clusterMap := make(map[string]bool)
		for _, name := range clusters {
			clusterMap[name] = true
		}
		for _, binding := range bindingList.GetBindingObjs() {
			if _, ok := clusterMap[binding.GetBindingSpec().TargetCluster]; ok && binding.GetBindingSpec().State == placementv1beta1.BindingStateScheduled {
				scheduled = append(scheduled, binding)
			}
		}

		// Verify that scheduled bindings are created as expected.
		wantScheduled := []placementv1beta1.BindingObj{}
		for _, name := range clusters {
			score := scoreByCluster[name]
			var binding placementv1beta1.BindingObj
			if placementKey.Namespace == "" {
				// Create CRB.
				binding = &placementv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingNamePlaceholder,
						Labels: map[string]string{
							placementv1beta1.PlacementTrackingLabel: placementKey.Name,
						},
						Finalizers: []string{placementv1beta1.SchedulerBindingCleanupFinalizer},
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:                        placementv1beta1.BindingStateScheduled,
						SchedulingPolicySnapshotName: policySnapshotName,
						TargetCluster:                name,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterName:  name,
							Selected:     true,
							ClusterScore: score,
						},
					},
				}
			} else {
				// Create RB.
				binding = &placementv1beta1.ResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      bindingNamePlaceholder,
						Namespace: placementKey.Namespace,
						Labels: map[string]string{
							placementv1beta1.PlacementTrackingLabel: placementKey.Name,
						},
						Finalizers: []string{placementv1beta1.SchedulerBindingCleanupFinalizer},
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:                        placementv1beta1.BindingStateScheduled,
						SchedulingPolicySnapshotName: policySnapshotName,
						TargetCluster:                name,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterName:  name,
							Selected:     true,
							ClusterScore: score,
						},
					},
				}
			}
			wantScheduled = append(wantScheduled, binding)
		}

		if diff := cmp.Diff(scheduled, wantScheduled, ignoreResourceBindingFields...); diff != "" {
			return fmt.Errorf("scheduled bindings are not created as expected; diff (-got, +want): %s", diff)
		}

		// Verify that binding names are formatted correctly.
		for _, binding := range bindingList.GetBindingObjs() {
			wantPrefix := fmt.Sprintf("%s-%s", placementKey.Name, binding.GetBindingSpec().TargetCluster)
			if !strings.HasPrefix(binding.GetName(), wantPrefix) {
				return fmt.Errorf("binding name %s is not formatted correctly; want prefix %s", binding.GetName(), wantPrefix)
			}
		}

		return nil
	}
}

func boundBindingsCreatedOrUpdatedForClustersActual(clusters []string, scoreByCluster map[string]*placementv1beta1.ClusterScore, placementKey types.NamespacedName, policySnapshotName string) func() error {
	return func() error {
		bindingList, err := listBindings(placementKey)
		if err != nil {
			return fmt.Errorf("failed to list bindings for placement %s: %w", placementKey, err)
		}

		bound := []placementv1beta1.BindingObj{}
		clusterMap := make(map[string]bool)
		for _, name := range clusters {
			clusterMap[name] = true
		}
		for _, binding := range bindingList.GetBindingObjs() {
			if _, ok := clusterMap[binding.GetBindingSpec().TargetCluster]; ok && binding.GetBindingSpec().State == placementv1beta1.BindingStateBound {
				bound = append(bound, binding)
			}
		}

		wantBound := []placementv1beta1.BindingObj{}
		if placementKey.Namespace == "" {
			for _, name := range clusters {
				score := scoreByCluster[name]
				binding := &placementv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingNamePlaceholder,
						Labels: map[string]string{
							placementv1beta1.PlacementTrackingLabel: placementKey.Name,
						},
						Finalizers: []string{placementv1beta1.SchedulerBindingCleanupFinalizer},
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:                        placementv1beta1.BindingStateBound,
						SchedulingPolicySnapshotName: policySnapshotName,
						TargetCluster:                name,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterName:  name,
							Selected:     true,
							ClusterScore: score,
						},
					},
				}
				wantBound = append(wantBound, binding)
			}
		} else {
			for _, name := range clusters {
				score := scoreByCluster[name]
				binding := &placementv1beta1.ResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      bindingNamePlaceholder,
						Namespace: placementKey.Namespace,
						Labels: map[string]string{
							placementv1beta1.PlacementTrackingLabel: placementKey.Name,
						},
						Finalizers: []string{placementv1beta1.SchedulerBindingCleanupFinalizer},
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:                        placementv1beta1.BindingStateBound,
						SchedulingPolicySnapshotName: policySnapshotName,
						TargetCluster:                name,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterName:  name,
							Selected:     true,
							ClusterScore: score,
						},
					},
				}
				wantBound = append(wantBound, binding)
			}
		}

		if diff := cmp.Diff(bound, wantBound, ignoreResourceBindingFields...); diff != "" {
			return fmt.Errorf("bound bindings are not updated as expected; diff (-got, +want): %s", diff)
		}

		// Verify that binding names are formatted correctly.
		for _, binding := range bindingList.GetBindingObjs() {
			wantPrefix := fmt.Sprintf("%s-%s", placementKey.Name, binding.GetBindingSpec().TargetCluster)
			if !strings.HasPrefix(binding.GetName(), wantPrefix) {
				return fmt.Errorf("binding name %s is not formatted correctly; want prefix %s", binding.GetName(), wantPrefix)
			}
		}

		return nil
	}
}

func unscheduledBindingsCreatedOrUpdatedForClustersActual(clusters []string, scoreByCluster map[string]*placementv1beta1.ClusterScore, placementKey types.NamespacedName, policySnapshotName string) func() error {
	return func() error {
		bindingList, err := listBindings(placementKey)
		if err != nil {
			return fmt.Errorf("failed to list bindings for placement %s: %w", placementKey, err)
		}

		unscheduled := []placementv1beta1.BindingObj{}
		clusterMap := make(map[string]bool)
		for _, name := range clusters {
			clusterMap[name] = true
		}
		for _, binding := range bindingList.GetBindingObjs() {
			if _, ok := clusterMap[binding.GetBindingSpec().TargetCluster]; ok && binding.GetBindingSpec().State == placementv1beta1.BindingStateUnscheduled {
				unscheduled = append(unscheduled, binding)
			}
		}
		// TODO (rzhang): fix me, compare the annotations when we know its previous state
		wantUnscheduled := []placementv1beta1.BindingObj{}
		if placementKey.Namespace == "" {
			for _, name := range clusters {
				score := scoreByCluster[name]
				binding := &placementv1beta1.ClusterResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name: bindingNamePlaceholder,
						Labels: map[string]string{
							placementv1beta1.PlacementTrackingLabel: placementKey.Name,
						},
						Finalizers: []string{placementv1beta1.SchedulerBindingCleanupFinalizer},
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:                        placementv1beta1.BindingStateUnscheduled,
						SchedulingPolicySnapshotName: policySnapshotName,
						TargetCluster:                name,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterName:  name,
							Selected:     true,
							ClusterScore: score,
						},
					},
				}
				wantUnscheduled = append(wantUnscheduled, binding)
			}
		} else {
			for _, name := range clusters {
				score := scoreByCluster[name]
				binding := &placementv1beta1.ResourceBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      bindingNamePlaceholder,
						Namespace: placementKey.Namespace,
						Labels: map[string]string{
							placementv1beta1.PlacementTrackingLabel: placementKey.Name,
						},
						Finalizers: []string{placementv1beta1.SchedulerBindingCleanupFinalizer},
					},
					Spec: placementv1beta1.ResourceBindingSpec{
						State:                        placementv1beta1.BindingStateUnscheduled,
						SchedulingPolicySnapshotName: policySnapshotName,
						TargetCluster:                name,
						ClusterDecision: placementv1beta1.ClusterDecision{
							ClusterName:  name,
							Selected:     true,
							ClusterScore: score,
						},
					},
				}
				wantUnscheduled = append(wantUnscheduled, binding)
			}
		}

		if diff := cmp.Diff(unscheduled, wantUnscheduled, ignoreResourceBindingFields...); diff != "" {
			return fmt.Errorf("unscheduled bindings are not updated as expected; diff (-got, +want): %s", diff)
		}

		// Verify that binding names are formatted correctly.
		for _, binding := range bindingList.GetBindingObjs() {
			wantPrefix := fmt.Sprintf("%s-%s", placementKey.Name, binding.GetBindingSpec().TargetCluster)
			if !strings.HasPrefix(binding.GetName(), wantPrefix) {
				return fmt.Errorf("binding name %s is not formatted correctly; want prefix %s", binding.GetName(), wantPrefix)
			}
		}

		return nil
	}
}

func noBindingsCreatedForClustersActual(clusters []string, placementKey types.NamespacedName) func() error {
	// Build a map for clusters for quicker lookup.
	clusterMap := map[string]bool{}
	for _, name := range clusters {
		clusterMap[name] = true
	}

	return func() error {
		// List all bindings.
		bindingList, err := listBindings(placementKey)
		if err != nil {
			return fmt.Errorf("failed to list bindings for placement %s: %w", placementKey, err)
		}

		bindings := bindingList.GetBindingObjs()
		for _, binding := range bindings {
			if _, ok := clusterMap[binding.GetBindingSpec().TargetCluster]; ok {
				return fmt.Errorf("binding %s for cluster %s has been created unexpectedly", binding.GetName(), binding.GetBindingSpec().TargetCluster)
			}
		}

		return nil
	}
}

func pickFixedPolicySnapshotStatusUpdatedActual(valid, invalidOrNotFound []string, policySnapshotKey types.NamespacedName) func() error {
	return func() error {
		policySnapshot, err := getSchedulingPolicySnapshot(policySnapshotKey)
		if err != nil {
			return fmt.Errorf("failed to get policy snapshot %s: %w", policySnapshotKey, err)
		}

		// Verify that the observed RP generation field is populated correctly.
		wantRPGeneration := policySnapshot.GetAnnotations()[placementv1beta1.CRPGenerationAnnotation]
		observedRPGeneration := policySnapshot.GetPolicySnapshotStatus().ObservedCRPGeneration
		if strconv.FormatInt(observedRPGeneration, 10) != wantRPGeneration {
			return fmt.Errorf("policy snapshot observed RP generation not match: want %s, got %d", wantRPGeneration, observedRPGeneration)
		}

		// Verify that cluster decisions are populated correctly.
		wantClusterDecisions := []placementv1beta1.ClusterDecision{}
		for _, clusterName := range valid {
			wantClusterDecisions = append(wantClusterDecisions, placementv1beta1.ClusterDecision{
				ClusterName: clusterName,
				Selected:    true,
			})
		}
		for _, clusterName := range invalidOrNotFound {
			wantClusterDecisions = append(wantClusterDecisions, placementv1beta1.ClusterDecision{
				ClusterName: clusterName,
				Selected:    false,
			})
		}
		if diff := cmp.Diff(policySnapshot.GetPolicySnapshotStatus().ClusterDecisions, wantClusterDecisions, ignoreClusterDecisionReasonField, cmpopts.SortSlices(lessFuncClusterDecision)); diff != "" {
			return fmt.Errorf("policy snapshot status cluster decisions (-got, +want): %s", diff)
		}

		// Verify that the scheduled condition is added correctly.
		scheduledCondition := meta.FindStatusCondition(policySnapshot.GetPolicySnapshotStatus().Conditions, string(placementv1beta1.PolicySnapshotScheduled))
		var wantScheduledCondition *metav1.Condition
		if len(invalidOrNotFound) == 0 {
			wantScheduledCondition = &metav1.Condition{
				Type:               string(placementv1beta1.PolicySnapshotScheduled),
				Status:             metav1.ConditionTrue,
				ObservedGeneration: policySnapshot.GetGeneration(),
			}
		} else {
			wantScheduledCondition = &metav1.Condition{
				Type:               string(placementv1beta1.PolicySnapshotScheduled),
				Status:             metav1.ConditionFalse,
				ObservedGeneration: policySnapshot.GetGeneration(),
			}
		}
		if diff := cmp.Diff(scheduledCondition, wantScheduledCondition, ignoreConditionTimeReasonAndMessageFields); diff != "" {
			return fmt.Errorf("policy snapshot status scheduled condition (-got, +want): %s", diff)
		}

		return nil
	}
}

func pickAllPolicySnapshotStatusUpdatedActual(scored, filtered []string, policySnapshotKey types.NamespacedName) func() error {
	return func() error {
		policySnapshot, err := getSchedulingPolicySnapshot(policySnapshotKey)
		if err != nil {
			return fmt.Errorf("failed to get policy snapshot %s: %w", policySnapshotKey, err)
		}

		// Verify that the observed RP generation field is populated correctly.
		wantRPGeneration := policySnapshot.GetAnnotations()[placementv1beta1.CRPGenerationAnnotation]
		observedRPGeneration := policySnapshot.GetPolicySnapshotStatus().ObservedCRPGeneration
		if strconv.FormatInt(observedRPGeneration, 10) != wantRPGeneration {
			return fmt.Errorf("policy snapshot observed RP generation not match: want %s, got %d", wantRPGeneration, observedRPGeneration)
		}

		// Verify that cluster decisions are populated correctly.
		wantClusterDecisions := []placementv1beta1.ClusterDecision{}
		for _, clusterName := range scored {
			wantClusterDecisions = append(wantClusterDecisions, placementv1beta1.ClusterDecision{
				ClusterName:  clusterName,
				Selected:     true,
				ClusterScore: &zeroScore,
			})
		}
		for _, clusterName := range filtered {
			wantClusterDecisions = append(wantClusterDecisions, placementv1beta1.ClusterDecision{
				ClusterName: clusterName,
				Selected:    false,
			})
		}
		if diff := cmp.Diff(policySnapshot.GetPolicySnapshotStatus().ClusterDecisions, wantClusterDecisions, ignoreClusterDecisionReasonField, cmpopts.SortSlices(lessFuncClusterDecision)); diff != "" {
			return fmt.Errorf("policy snapshot status cluster decisions (-got, +want): %s", diff)
		}

		// Verify that the scheduled condition is added correctly.
		scheduledCondition := meta.FindStatusCondition(policySnapshot.GetPolicySnapshotStatus().Conditions, string(placementv1beta1.PolicySnapshotScheduled))
		wantScheduledCondition := &metav1.Condition{
			Type:               string(placementv1beta1.PolicySnapshotScheduled),
			Status:             metav1.ConditionTrue,
			ObservedGeneration: policySnapshot.GetGeneration(),
		}

		if diff := cmp.Diff(scheduledCondition, wantScheduledCondition, ignoreConditionTimeReasonAndMessageFields); diff != "" {
			return fmt.Errorf("policy snapshot status scheduled condition (-got, +want): %s", diff)
		}

		return nil
	}
}

func hasNScheduledOrBoundBindingsPresentActual(placementKey types.NamespacedName, clusters []string) func() error {
	clusterMap := make(map[string]bool)
	for _, name := range clusters {
		clusterMap[name] = true
	}

	return func() error {
		bindingList, err := listBindings(placementKey)
		if err != nil {
			return fmt.Errorf("failed to list bindings for placement %s: %w", placementKey, err)
		}

		matchedScheduledOrBoundBindingCount := 0
		for _, binding := range bindingList.GetBindingObjs() {
			// A match is found iff the binding is of the scheduled or bound state, and its
			// target cluster is in the given list.
			//
			// We do not simply check against the state here as there exists a rare case where
			// the system might be in an in-between state and happen to have just the enough
			// number of bindings (though not the wanted ones).
			_, matched := clusterMap[binding.GetBindingSpec().TargetCluster]
			if (binding.GetBindingSpec().State == placementv1beta1.BindingStateBound || binding.GetBindingSpec().State == placementv1beta1.BindingStateScheduled) && matched {
				matchedScheduledOrBoundBindingCount++
			}
		}

		if matchedScheduledOrBoundBindingCount != len(clusterMap) {
			return fmt.Errorf("got %d, want %d matched scheduled or bound bindings", matchedScheduledOrBoundBindingCount, len(clusterMap))
		}

		return nil
	}
}

func pickNPolicySnapshotStatusUpdatedActual(
	numOfClusters int,
	picked, notPicked, filtered []string,
	scoreByCluster map[string]*placementv1beta1.ClusterScore,
	policySnapshotKey types.NamespacedName,
	opts []cmp.Option,
) func() error {
	return func() error {
		policySnapshot, err := getSchedulingPolicySnapshot(policySnapshotKey)
		if err != nil {
			return fmt.Errorf("failed to get policy snapshot %s: %w", policySnapshotKey, err)
		}

		// Verify that the observed RP generation field is populated correctly.
		wantRPGeneration := policySnapshot.GetAnnotations()[placementv1beta1.CRPGenerationAnnotation]
		observedRPGeneration := policySnapshot.GetPolicySnapshotStatus().ObservedCRPGeneration
		if strconv.FormatInt(observedRPGeneration, 10) != wantRPGeneration {
			return fmt.Errorf("policy snapshot observed RP generation not match: want %s, got %d", wantRPGeneration, observedRPGeneration)
		}

		// Verify that cluster decisions are populated correctly.
		wantClusterDecisions := []placementv1beta1.ClusterDecision{}
		for _, clusterName := range picked {
			wantClusterDecisions = append(wantClusterDecisions, placementv1beta1.ClusterDecision{
				ClusterName:  clusterName,
				Selected:     true,
				ClusterScore: scoreByCluster[clusterName],
			})
		}
		for _, clusterName := range notPicked {
			wantClusterDecisions = append(wantClusterDecisions, placementv1beta1.ClusterDecision{
				ClusterName:  clusterName,
				Selected:     false,
				ClusterScore: scoreByCluster[clusterName],
			})
		}
		for _, clusterName := range filtered {
			wantClusterDecisions = append(wantClusterDecisions, placementv1beta1.ClusterDecision{
				ClusterName: clusterName,
				Selected:    false,
			})
		}
		if diff := cmp.Diff(
			policySnapshot.GetPolicySnapshotStatus().ClusterDecisions, wantClusterDecisions,
			opts...,
		); diff != "" {
			return fmt.Errorf("policy snapshot status cluster decisions (-got, +want): %s", diff)
		}

		// Verify that the scheduled condition is added correctly.
		scheduledCondition := meta.FindStatusCondition(policySnapshot.GetPolicySnapshotStatus().Conditions, string(placementv1beta1.PolicySnapshotScheduled))
		wantScheduledCondition := &metav1.Condition{
			Type:               string(placementv1beta1.PolicySnapshotScheduled),
			Status:             metav1.ConditionTrue,
			ObservedGeneration: policySnapshot.GetGeneration(),
		}
		if len(picked) != numOfClusters {
			wantScheduledCondition = &metav1.Condition{
				Type:               string(placementv1beta1.PolicySnapshotScheduled),
				Status:             metav1.ConditionFalse,
				ObservedGeneration: policySnapshot.GetGeneration(),
			}
		}

		if diff := cmp.Diff(scheduledCondition, wantScheduledCondition, ignoreConditionTimeReasonAndMessageFields); diff != "" {
			return fmt.Errorf("policy snapshot status scheduled condition (-got, +want): %s", diff)
		}

		return nil
	}
}
