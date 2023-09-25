/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
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

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

// This file features common actuals (and utilities for generating actuals) in the test suites.

var (
	noBindingCreatedActual = func() error {
		// List all bindings.
		bindingList := &fleetv1beta1.ClusterResourceBindingList{}
		if err := hubClient.List(ctx, bindingList); err != nil {
			return err
		}

		// Check that the returned list is empty.
		if bindingCount := len(bindingList.Items); bindingCount != 0 {
			return fmt.Errorf("%d bindings have been created unexpectedly", bindingCount)
		}

		return nil
	}
)

func crpSchedulerFinalizerAddedActual(crpName string) func() error {
	return func() error {
		// Retrieve the CRP.
		crp := &fleetv1beta1.ClusterResourcePlacement{}
		if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
			return err
		}

		// Check that the scheduler finalizer has been added.
		if !controllerutil.ContainsFinalizer(crp, fleetv1beta1.SchedulerCRPCleanupFinalizer) {
			return fmt.Errorf("scheduler cleanup finalizer has not been added")
		}

		return nil
	}
}

func crpSchedulerFinalizerRemovedActual(crpName string) func() error {
	return func() error {
		// Retrieve the CRP.
		crp := &fleetv1beta1.ClusterResourcePlacement{}
		if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp); err != nil {
			return err
		}

		// Check that the scheduler finalizer has been added.
		if controllerutil.ContainsFinalizer(crp, fleetv1beta1.SchedulerCRPCleanupFinalizer) {
			return fmt.Errorf("scheduler cleanup finalizer is still present")
		}

		return nil
	}
}

func scheduledBindingsCreatedForClustersActual(clusters []string, scoreByCluster map[string]*fleetv1beta1.ClusterScore, crpName, policySnapshotName string) func() error {
	return func() error {
		// List all bindings.
		bindingList := &fleetv1beta1.ClusterResourceBindingList{}
		if err := hubClient.List(ctx, bindingList); err != nil {
			return err
		}

		// Find all the scheduled bindings.
		scheduled := []fleetv1beta1.ClusterResourceBinding{}
		for _, binding := range bindingList.Items {
			if binding.Spec.State == fleetv1beta1.BindingStateScheduled {
				scheduled = append(scheduled, binding)
			}
		}

		// Verify that scheduled bindings are created as expected.
		wantScheduled := []fleetv1beta1.ClusterResourceBinding{}
		for _, name := range clusters {
			score := scoreByCluster[name]
			binding := fleetv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: bindingNamePlaceholder,
					Labels: map[string]string{
						fleetv1beta1.CRPTrackingLabel: crpName,
					},
				},
				Spec: fleetv1beta1.ResourceBindingSpec{
					State:                        fleetv1beta1.BindingStateScheduled,
					SchedulingPolicySnapshotName: policySnapshotName,
					TargetCluster:                name,
					ClusterDecision: fleetv1beta1.ClusterDecision{
						ClusterName:  name,
						Selected:     true,
						ClusterScore: score,
					},
				},
			}
			wantScheduled = append(wantScheduled, binding)
		}

		if diff := cmp.Diff(scheduled, wantScheduled, ignoreResourceBindingFields...); diff != "" {
			return fmt.Errorf("scheduled bindings are not created as expected; diff (-got, +want): %s", diff)
		}

		// Verify that binding names are formatted correctly.
		for _, binding := range bindingList.Items {
			wantPrefix := fmt.Sprintf("%s-%s", crpName, binding.Spec.TargetCluster)
			if !strings.HasPrefix(binding.Name, wantPrefix) {
				return fmt.Errorf("binding name %s is not formatted correctly; want prefix %s", binding.Name, wantPrefix)
			}
		}

		return nil
	}
}

func boundBindingsUpdatedForClustersActual(clusters []string, scoreByCluster map[string]*fleetv1beta1.ClusterScore, crpName, policySnapshotName string) func() error {
	return func() error {
		bindingList := &fleetv1beta1.ClusterResourceBindingList{}
		if err := hubClient.List(ctx, bindingList); err != nil {
			return err
		}

		bound := []fleetv1beta1.ClusterResourceBinding{}
		for _, binding := range bindingList.Items {
			if binding.Spec.State == fleetv1beta1.BindingStateBound {
				bound = append(bound, binding)
			}
		}

		wantBound := []fleetv1beta1.ClusterResourceBinding{}
		for _, name := range clusters {
			score := scoreByCluster[name]
			binding := fleetv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: bindingNamePlaceholder,
					Labels: map[string]string{
						fleetv1beta1.CRPTrackingLabel: crpName,
					},
				},
				Spec: fleetv1beta1.ResourceBindingSpec{
					State:                        fleetv1beta1.BindingStateBound,
					SchedulingPolicySnapshotName: policySnapshotName,
					TargetCluster:                name,
					ClusterDecision: fleetv1beta1.ClusterDecision{
						ClusterName:  name,
						Selected:     true,
						ClusterScore: score,
					},
				},
			}
			wantBound = append(wantBound, binding)
		}

		if diff := cmp.Diff(bound, wantBound, ignoreResourceBindingFields...); diff != "" {
			return fmt.Errorf("bound bindings are not updated as expected; diff (-got, +want): %s", diff)
		}

		// Verify that binding names are formatted correctly.
		for _, binding := range bindingList.Items {
			wantPrefix := fmt.Sprintf("%s-%s", crpName, binding.Spec.TargetCluster)
			if !strings.HasPrefix(binding.Name, wantPrefix) {
				return fmt.Errorf("binding name %s is not formatted correctly; want prefix %s", binding.Name, wantPrefix)
			}
		}

		return nil
	}
}

func scheduledBindingsUpdatedForClustersActual(clusters []string, scoreByCluster map[string]*fleetv1beta1.ClusterScore, crpName, policySnapshotName string) func() error {
	return func() error {
		bindingList := &fleetv1beta1.ClusterResourceBindingList{}
		if err := hubClient.List(ctx, bindingList); err != nil {
			return err
		}

		scheduled := []fleetv1beta1.ClusterResourceBinding{}
		for _, binding := range bindingList.Items {
			if binding.Spec.State == fleetv1beta1.BindingStateScheduled {
				scheduled = append(scheduled, binding)
			}
		}

		wantScheduled := []fleetv1beta1.ClusterResourceBinding{}
		for _, name := range clusters {
			score := scoreByCluster[name]
			binding := fleetv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: bindingNamePlaceholder,
					Labels: map[string]string{
						fleetv1beta1.CRPTrackingLabel: crpName,
					},
				},
				Spec: fleetv1beta1.ResourceBindingSpec{
					State:                        fleetv1beta1.BindingStateScheduled,
					SchedulingPolicySnapshotName: policySnapshotName,
					TargetCluster:                name,
					ClusterDecision: fleetv1beta1.ClusterDecision{
						ClusterName:  name,
						Selected:     true,
						ClusterScore: score,
					},
				},
			}
			wantScheduled = append(wantScheduled, binding)
		}

		if diff := cmp.Diff(scheduled, wantScheduled, ignoreResourceBindingFields...); diff != "" {
			return fmt.Errorf("scheduled bindings are not updated as expected; diff (-got, +want): %s", diff)
		}

		// Verify that binding names are formatted correctly.
		for _, binding := range bindingList.Items {
			wantPrefix := fmt.Sprintf("%s-%s", crpName, binding.Spec.TargetCluster)
			if !strings.HasPrefix(binding.Name, wantPrefix) {
				return fmt.Errorf("binding name %s is not formatted correctly; want prefix %s", binding.Name, wantPrefix)
			}
		}

		return nil
	}
}

func unscheduledBindingsCreatedForClustersActual(clusters []string, scoreByCluster map[string]*fleetv1beta1.ClusterScore, crpName string, policySnapshotName string) func() error {
	return func() error {
		bindingList := &fleetv1beta1.ClusterResourceBindingList{}
		if err := hubClient.List(ctx, bindingList); err != nil {
			return err
		}

		unscheduled := []fleetv1beta1.ClusterResourceBinding{}
		for _, binding := range bindingList.Items {
			if binding.Spec.State == fleetv1beta1.BindingStateUnscheduled {
				unscheduled = append(unscheduled, binding)
			}
		}

		wantUnscheduled := []fleetv1beta1.ClusterResourceBinding{}
		for _, name := range clusters {
			score := scoreByCluster[name]
			binding := fleetv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: bindingNamePlaceholder,
					Labels: map[string]string{
						fleetv1beta1.CRPTrackingLabel: crpName,
					},
				},
				Spec: fleetv1beta1.ResourceBindingSpec{
					State:                        fleetv1beta1.BindingStateUnscheduled,
					SchedulingPolicySnapshotName: policySnapshotName,
					TargetCluster:                name,
					ClusterDecision: fleetv1beta1.ClusterDecision{
						ClusterName:  name,
						Selected:     true,
						ClusterScore: score,
					},
				},
			}
			wantUnscheduled = append(wantUnscheduled, binding)
		}

		if diff := cmp.Diff(unscheduled, wantUnscheduled, ignoreResourceBindingFields...); diff != "" {
			return fmt.Errorf("unscheduled bindings are not updated as expected; diff (-got, +want): %s", diff)
		}

		// Verify that binding names are formatted correctly.
		for _, binding := range bindingList.Items {
			wantPrefix := fmt.Sprintf("%s-%s", crpName, binding.Spec.TargetCluster)
			if !strings.HasPrefix(binding.Name, wantPrefix) {
				return fmt.Errorf("binding name %s is not formatted correctly; want prefix %s", binding.Name, wantPrefix)
			}
		}

		return nil
	}
}

func noBindingsCreatedForClustersActual(clusters []string) func() error {
	// Build a map for clusters for quicker lookup.
	clusterMap := map[string]bool{}
	for _, name := range clusters {
		clusterMap[name] = true
	}

	return func() error {
		bindingList := &fleetv1beta1.ClusterResourceBindingList{}
		if err := hubClient.List(ctx, bindingList); err != nil {
			return err
		}

		bindings := bindingList.Items
		for _, binding := range bindings {
			if _, ok := clusterMap[binding.Spec.TargetCluster]; ok {
				return fmt.Errorf("binding %s for cluster %s has been created unexpectedly", binding.Name, binding.Spec.TargetCluster)
			}
		}

		return nil
	}
}

func pickFixedPolicySnapshotStatusUpdatedActual(valid, invalidOrNotFound []string, policySnapshotName string) func() error {
	return func() error {
		policySnapshot := &fleetv1beta1.ClusterSchedulingPolicySnapshot{}
		if err := hubClient.Get(ctx, types.NamespacedName{Name: policySnapshotName}, policySnapshot); err != nil {
			return err
		}

		// Verify that the observed CRP generation field is populated correctly.
		wantCRPGeneration := policySnapshot.Annotations[fleetv1beta1.CRPGenerationAnnotation]
		observedCRPGeneration := policySnapshot.Status.ObservedCRPGeneration
		if strconv.FormatInt(observedCRPGeneration, 10) != wantCRPGeneration {
			return fmt.Errorf("policy snapshot observed CRP generation not match: want %s, got %d", wantCRPGeneration, observedCRPGeneration)
		}

		// Verify that cluster decisions are populated correctly.
		wantClusterDecisions := []fleetv1beta1.ClusterDecision{}
		for _, clusterName := range valid {
			wantClusterDecisions = append(wantClusterDecisions, fleetv1beta1.ClusterDecision{
				ClusterName: clusterName,
				Selected:    true,
			})
		}
		for _, clusterName := range invalidOrNotFound {
			wantClusterDecisions = append(wantClusterDecisions, fleetv1beta1.ClusterDecision{
				ClusterName: clusterName,
				Selected:    false,
			})
		}
		if diff := cmp.Diff(policySnapshot.Status.ClusterDecisions, wantClusterDecisions, ignoreClusterDecisionReasonField, cmpopts.SortSlices(lessFuncClusterDecision)); diff != "" {
			return fmt.Errorf("policy snapshot status cluster decisions (-got, +want): %s", diff)
		}

		// Verify that the scheduled condition is added correctly.
		scheduledCondition := meta.FindStatusCondition(policySnapshot.Status.Conditions, string(fleetv1beta1.PolicySnapshotScheduled))
		var wantScheduledCondition *metav1.Condition
		if len(invalidOrNotFound) == 0 {
			wantScheduledCondition = &metav1.Condition{
				Type:               string(fleetv1beta1.PolicySnapshotScheduled),
				Status:             metav1.ConditionTrue,
				ObservedGeneration: policySnapshot.Generation,
			}
		} else {
			wantScheduledCondition = &metav1.Condition{
				Type:               string(fleetv1beta1.PolicySnapshotScheduled),
				Status:             metav1.ConditionFalse,
				ObservedGeneration: policySnapshot.Generation,
			}
		}
		if diff := cmp.Diff(scheduledCondition, wantScheduledCondition, ignoreConditionTimeReasonAndMessageFields); diff != "" {
			return fmt.Errorf("policy snapshot status scheduled condition (-got, +want): %s", diff)
		}

		return nil
	}
}
