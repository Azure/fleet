/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package eviction

import (
	"context"
	"fmt"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
	"go.goms.io/fleet/pkg/utils/condition"
)

var (
	lessFuncCondition = func(a, b metav1.Condition) bool {
		return a.Type < b.Type
	}
	evictionStatusCmpOptions = cmp.Options{
		cmpopts.SortSlices(lessFuncCondition),
		cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime"),
		cmpopts.EquateEmpty(),
	}
)

func StatusUpdatedActual(ctx context.Context, client client.Client, evictionName string, isValidEviction *IsValidEviction, isExecutedEviction *IsExecutedEviction) func() error {
	return func() error {
		var eviction placementv1alpha1.ClusterResourcePlacementEviction
		if err := client.Get(ctx, types.NamespacedName{Name: evictionName}, &eviction); err != nil {
			return err
		}
		var conditions []metav1.Condition
		if isValidEviction != nil {
			if isValidEviction.IsValid {
				validCondition := metav1.Condition{
					Type:               string(placementv1alpha1.PlacementEvictionConditionTypeValid),
					Status:             metav1.ConditionTrue,
					ObservedGeneration: eviction.GetGeneration(),
					Reason:             condition.ClusterResourcePlacementEvictionValidReason,
					Message:            isValidEviction.Msg,
				}
				conditions = append(conditions, validCondition)
			} else {
				invalidCondition := metav1.Condition{
					Type:               string(placementv1alpha1.PlacementEvictionConditionTypeValid),
					Status:             metav1.ConditionFalse,
					ObservedGeneration: eviction.GetGeneration(),
					Reason:             condition.ClusterResourcePlacementEvictionInvalidReason,
					Message:            isValidEviction.Msg,
				}
				conditions = append(conditions, invalidCondition)
			}
		}
		if isExecutedEviction != nil {
			if isExecutedEviction.IsExecuted {
				executedCondition := metav1.Condition{
					Type:               string(placementv1alpha1.PlacementEvictionConditionTypeExecuted),
					Status:             metav1.ConditionTrue,
					ObservedGeneration: eviction.GetGeneration(),
					Reason:             condition.ClusterResourcePlacementEvictionExecutedReason,
					Message:            isExecutedEviction.Msg,
				}
				conditions = append(conditions, executedCondition)
			} else {
				notExecutedCondition := metav1.Condition{
					Type:               string(placementv1alpha1.PlacementEvictionConditionTypeExecuted),
					Status:             metav1.ConditionFalse,
					ObservedGeneration: eviction.GetGeneration(),
					Reason:             condition.ClusterResourcePlacementEvictionNotExecutedReason,
					Message:            isExecutedEviction.Msg,
				}
				conditions = append(conditions, notExecutedCondition)
			}
		}
		wantStatus := placementv1alpha1.PlacementEvictionStatus{
			Conditions: conditions,
		}
		if diff := cmp.Diff(eviction.Status, wantStatus, evictionStatusCmpOptions...); diff != "" {
			return fmt.Errorf("CRP status diff (-got, +want): %s", diff)
		}
		return nil
	}
}

type IsValidEviction struct {
	IsValid bool
	Msg     string
}

type IsExecutedEviction struct {
	IsExecuted bool
	Msg        string
}
