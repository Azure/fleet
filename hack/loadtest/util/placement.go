/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package util

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/atomic"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/condition"
)

const (
	crpPrefix = "load-test-placement-"
	nsPrefix  = "load-test-ns-"
)

var (
	labelKey = "workload.azure.com/load"
)

var (
	crpCount                 atomic.Int32
	applySuccessCount        atomic.Int32
	applyFailCount           atomic.Int32
	applyTimeoutCount        atomic.Int32
	LoadTestApplyCountMetric = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "workload_apply_total",
		Help: "Total number of placement",
	}, []string{"concurrency", "numTargetCluster", "result"})

	applyQuantile = promauto.NewSummary(prometheus.SummaryOpts{
		Name:       "quantile_apply_crp_latency",
		Help:       "quantiles for apply latency",
		Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
	})

	ApplyLatencyCountMetric = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "apply_crp_latency_count",
		Help: "latency of placement",
	}, []string{"concurrency", "numTargetCluster", "latency"})

	updateSuccessCount        atomic.Int32
	updateFailCount           atomic.Int32
	updateTimeoutCount        atomic.Int32
	LoadTestUpdateCountMetric = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "workload_update_total",
		Help: "Total number of placement updates",
	}, []string{"concurrency", "numTargetCluster", "result"})

	updateQuantile = promauto.NewSummary(prometheus.SummaryOpts{
		Name:       "quantile_update_latency",
		Help:       "quantiles for update latency",
		Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
	})

	UpdateLatencyCountMetric = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "update_crp_latency_count",
		Help: "update of placement",
	}, []string{"concurrency", "numTargetCluster", "latency"})
)

func MeasureOnePlacement(ctx context.Context, hubClient client.Client, deadline, interval time.Duration, maxCurrentPlacement int, clusterNames ClusterNames, crpFile string, useTestResources *bool) error {
	crpName := crpPrefix + utilrand.String(10)
	nsName := nsPrefix + utilrand.String(10)
	currency := strconv.Itoa(maxCurrentPlacement)
	fleetSize := "0"

	defer klog.Flush()

	if *useTestResources {
		defer func() {
			if err := deleteNamespace(context.Background(), hubClient, nsName); err != nil {
				klog.ErrorS(err, "failed to delete namespace", "namespace", nsName)
			}
		}()
		klog.Infof("create the resources in namespace `%s` in the hub cluster", nsName)
		if err := applyTestManifests(ctx, hubClient, nsName); err != nil {
			klog.ErrorS(err, "failed to apply namespaced resources", "namespace", nsName)
			return err
		}
	}

	klog.Infof("create the cluster resource placement `%s` in the hub cluster", crpName)
	crp := &v1beta1.ClusterResourcePlacement{}
	if err := createCRP(crp, crpFile, crpName, nsName, *useTestResources); err != nil {
		klog.ErrorS(err, "failed to create crp", "namespace", nsName, "crp", crpName)
		return err
	}

	defer hubClient.Delete(context.Background(), crp) //nolint
	if err := hubClient.Create(ctx, crp); err != nil {
		klog.ErrorS(err, "failed to apply crp", "namespace", nsName, "crp", crpName)
		LoadTestApplyCountMetric.WithLabelValues(currency, fleetSize, "failed").Inc()
		applyFailCount.Inc()
		return err
	}
	crpCount.Inc()

	klog.Infof("verify that the cluster resource placement `%s` is applied", crpName)
	fleetSize, clusterNames = collectApplyMetrics(ctx, hubClient, deadline, interval, crpName, currency, fleetSize, clusterNames)
	if fleetSize == "0" {
		return nil
	}

	deletionStartTime := time.Now()
	if *useTestResources {
		klog.Infof("remove the namespaced resources applied by the placement `%s`", crpName)
		if err := deleteTestManifests(ctx, hubClient, nsName); err != nil {
			klog.V(3).Infof("the cluster resource placement `%s` failed", crpName)
			LoadTestUpdateCountMetric.WithLabelValues(currency, fleetSize, "failed").Inc()
			updateFailCount.Inc()
			return err
		}
		resourcesDeletedCheck(ctx, hubClient, deadline, interval, crpName, clusterNames)

		// wait for the status of the CRP and make sure all conditions are all true
		klog.Infof("verify cluster resource placement `%s` is updated", crpName)
		waitForCrpToComplete(ctx, hubClient, deadline, interval, deletionStartTime, crpName, currency, fleetSize)
	}
	return hubClient.Delete(ctx, crp)
}

// collect the crp apply metrics
func collectApplyMetrics(ctx context.Context, hubClient client.Client, deadline, pollInterval time.Duration, crpName string, currency string, fleetSize string, clusterNames ClusterNames) (string, ClusterNames) {
	startTime := time.Now()
	var crp v1beta1.ClusterResourcePlacement
	var err error
	ticker := time.NewTicker(pollInterval)
	timer := time.NewTimer(deadline)
	defer ticker.Stop()
	defer timer.Stop()

	for {
		if err = hubClient.Get(ctx, types.NamespacedName{Name: crpName, Namespace: ""}, &crp); err != nil {
			klog.ErrorS(err, "failed to get crp", "crp", crpName)
		}
		cond := crp.GetCondition(string(v1beta1.ClusterResourcePlacementAppliedConditionType))
		select {
		case <-timer.C:
			// Deadline has been reached
			if condition.IsConditionStatusFalse(cond, 1) {
				// failed
				klog.Infof("the cluster resource placement `%s` failed", crpName)
				LoadTestApplyCountMetric.WithLabelValues(currency, fleetSize, "failed").Inc()
				applyFailCount.Inc()
				return fleetSize, clusterNames
			}
			// timeout
			klog.Infof("the cluster resource placement `%s` timeout", crpName)
			LoadTestApplyCountMetric.WithLabelValues(currency, fleetSize, "timeout").Inc()
			applyTimeoutCount.Inc()
			return fleetSize, clusterNames
		case <-ticker.C:
			// Interval for CRP status check
			if condition.IsConditionStatusTrue(cond, 1) {
				// succeeded
				klog.Infof("the cluster resource placement `%s` succeeded", crpName)
				endTime := time.Since(startTime)
				if fleetSize, clusterNames, err = getFleetSize(crp, clusterNames); err != nil {
					klog.ErrorS(err, "Failed to get fleet size.")
					return fleetSize, nil
				}
				applyQuantile.Observe(endTime.Seconds())
				ApplyLatencyCountMetric.WithLabelValues(currency, fleetSize, strconv.FormatFloat(endTime.Seconds(), 'f', 3, 64)).Inc()
				LoadTestApplyCountMetric.WithLabelValues(currency, fleetSize, "succeed").Inc()
				applySuccessCount.Inc()
				return fleetSize, clusterNames
			} else if cond == nil || cond.Status == metav1.ConditionUnknown {
				klog.V(2).Infof("the cluster resource placement `%s` is pending", crpName)
			} else if condition.IsConditionStatusFalse(cond, 1) {
				klog.Infof("the cluster resource placement `%s` failed. trying again.", crpName)
				continue
			}
		}
	}
}

// collect metrics for deleting resources
func resourcesDeletedCheck(ctx context.Context, hubClient client.Client, deadline, pollInterval time.Duration, crpName string, clusterNames ClusterNames) {
	var crp v1beta1.ClusterResourcePlacement
	klog.Infof("verify that the applied resources on cluster resource placement `%s` are deleted", crpName)

	// Create a timer and a ticker
	timer := time.NewTimer(deadline)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	defer timer.Stop()

	for {
		select {
		case <-timer.C:
			// Deadline has been reached
			// timeout
			klog.V(3).Infof("the cluster resource placement `%s` delete timeout", crpName)
			return
		case <-ticker.C:
			// Interval for CRP status check
			if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName, Namespace: ""}, &crp); err != nil {
				klog.ErrorS(err, "failed to get crp", "crp", crpName)
			}
			// the only thing it still selects are namespace and crd
			if len(crp.Status.SelectedResources) != 2 {
				klog.V(4).Infof("the crp `%s` has not picked up the namespaced resource deleted change", crpName)
				continue
			}
			// check if the change is picked up by the member agent
			var clusterWork v1beta1.Work
			allRemoved := true
			for _, clusterName := range clusterNames {
				err := hubClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf("%s-work", crpName), Namespace: fmt.Sprintf(utils.NamespaceNameFormat, clusterName)}, &clusterWork)
				if err != nil || len(clusterWork.Status.ManifestConditions) != 2 {
					klog.V(4).Infof("the resources `%s` in cluster namespace `%s` is not removed by the member agent yet", crpName, clusterName)
					allRemoved = false
					break
				}
			}
			if allRemoved {
				// succeeded
				klog.V(3).Infof("the applied resources on cluster resource placement `%s` delete succeeded", crpName)
				return
			}
		}
	}
}

// check crp updated/completed before deletion
func waitForCrpToComplete(ctx context.Context, hubClient client.Client, deadline, pollInterval time.Duration, deletionStartTime time.Time, crpName string, currency string, fleetSize string) {
	var crp v1beta1.ClusterResourcePlacement
	var err error
	timer := time.NewTimer(deadline)
	ticker := time.NewTicker(pollInterval)

	defer ticker.Stop()
	defer timer.Stop()

	for {
		if err = hubClient.Get(ctx, types.NamespacedName{Name: crpName, Namespace: ""}, &crp); err != nil {
			klog.ErrorS(err, "failed to get crp", "crp", crpName)
		}
		appliedCond := crp.GetCondition(string(v1beta1.ClusterResourcePlacementAppliedConditionType))
		synchronizedCond := crp.GetCondition(string(v1beta1.ClusterResourcePlacementSynchronizedConditionType))
		scheduledCond := crp.GetCondition(string(v1beta1.ClusterResourcePlacementScheduledConditionType))
		select {
		case <-timer.C:
			// Deadline has been reached
			if condition.IsConditionStatusFalse(appliedCond, 1) {
				// failed
				klog.V(3).Infof("the cluster resource placement `%s` failed", crpName)
				LoadTestUpdateCountMetric.WithLabelValues(currency, fleetSize, "failed").Inc()
				updateFailCount.Inc()
				return
			}
			klog.V(3).Infof("the cluster resource placement `%s` timeout", crpName)
			LoadTestUpdateCountMetric.WithLabelValues(currency, fleetSize, "timeout").Inc()
			updateTimeoutCount.Inc()
			return
		case <-ticker.C:
			// Interval for CRP status check
			if condition.IsConditionStatusTrue(appliedCond, 1) && condition.IsConditionStatusTrue(synchronizedCond, 1) && condition.IsConditionStatusTrue(scheduledCond, 1) {
				// succeeded
				klog.V(3).Infof("the cluster resource placement `%s` succeeded", crpName)
				var endTime = time.Since(deletionStartTime).Seconds()
				updateQuantile.Observe(endTime)
				UpdateLatencyCountMetric.WithLabelValues(currency, fleetSize, strconv.FormatFloat(endTime, 'f', 3, 64)).Inc()
				LoadTestUpdateCountMetric.WithLabelValues(currency, fleetSize, "succeed").Inc()
				updateSuccessCount.Inc()

				return
			} else if appliedCond == nil || appliedCond.Status == metav1.ConditionUnknown || synchronizedCond == nil || synchronizedCond.Status == metav1.ConditionUnknown || scheduledCond == nil || scheduledCond.Status == metav1.ConditionUnknown {
				klog.V(3).Infof("the cluster resource placement `%s` is pending", crpName)
			} else if condition.IsConditionStatusFalse(appliedCond, 1) {
				// failed
				klog.V(2).Infof("the cluster resource placement `%s` failed. try again", crpName)
			}
		}
	}
}

func PrintTestMetrics() {
	klog.Infof("CRP count %d", crpCount.Load())
	klog.InfoS("Placement apply result", "total applySuccessCount", applySuccessCount.Load(), "applyFailCount", applyFailCount.Load(), "applyTimeoutCount", applyTimeoutCount.Load())

	klog.InfoS("Placement update result", "total updateSuccessCount", updateSuccessCount.Load(), "updateFailCount", updateFailCount.Load(), "updateTimeoutCount", updateTimeoutCount.Load())
}
