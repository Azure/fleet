package util

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"go.goms.io/fleet/pkg/utils/condition"

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
	}, []string{"concurrency", "fleetSize", "result"})

	LoadTestApplyLatencyMetric = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "workload_apply_latency",
		Help:    "Length of time from placement change to it is applied to the member cluster",
		Buckets: []float64{0.1, 0.5, 1.0, 2.0, 3, 4, 6, 8, 10, 13, 16, 20, 23, 26, 30, 37, 45, 60, 90, 120, 150, 180, 300, 600, 1200, 1500, 3000},
	}, []string{"concurrency", "fleetSize"})

	deleteSuccessCount        atomic.Int32
	deleteTimeoutCount        atomic.Int32
	LoadTestDeleteCountMetric = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "workload_delete_total",
		Help: "Total number of placement delete",
	}, []string{"concurrency", "fleetSize", "result"})

	LoadTestDeleteLatencyMetric = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "workload_delete_latency",
		Help:    "Length of time from resource deletion to it is deleted from the member cluster",
		Buckets: []float64{0.1, 0.5, 1.0, 1.25, 1.5, 1.75, 2.0, 3, 4, 6, 8, 10, 13, 16, 20, 23, 26, 30, 37, 45, 60, 90, 120, 150, 180, 300, 600, 1200, 1500, 3000},
	}, []string{"concurrency", "fleetSize"})

	updateSuccessCount        atomic.Int32
	updateFailCount           atomic.Int32
	updateTimeoutCount        atomic.Int32
	LoadTestUpdateCountMetric = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "workload_update_total",
		Help: "Total number of placement updates",
	}, []string{"concurrency", "fleetSize", "result"})

	LoadTestUpdateLatencyMetric = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "workload_update_latency",
		Help:    "Length of time from placement change to it is applied to the member cluster",
		Buckets: []float64{0.1, 0.5, 1.0, 2.0, 3, 4, 6, 8, 10, 13, 16, 20, 23, 26, 30, 37, 45, 60, 90, 120, 150, 180, 300, 600, 1200, 1500, 3000},
	}, []string{"concurrency", "fleetSize"})
)

func MeasureOnePlacement(ctx context.Context, hubClient client.Client, deadline, interval time.Duration, maxCurrentPlacement int, clusterNames ClusterNames, crpFile string) error {
	crpName := crpPrefix + utilrand.String(10)
	nsName := nsPrefix + utilrand.String(10)
	currency := strconv.Itoa(maxCurrentPlacement)
	fleetSize := "0"
	defer klog.Flush()
	defer deleteNamespace(context.Background(), hubClient, nsName) //nolint

	klog.Infof("create the resources in namespace `%s` in the hub cluster", nsName)
	if err := applyTestManifests(ctx, hubClient, nsName); err != nil {
		klog.ErrorS(err, "failed to apply namespaced resources", "namespace", nsName)
		return err
	}

	klog.Infof("create the cluster resource placement `%s` in the hub cluster", crpName)
	crp := &v1beta1.ClusterResourcePlacement{}
	if err := createCRP(crp, crpFile, crpName, nsName); err != nil {
		klog.ErrorS(err, "failed to create crp", "namespace", nsName, "crp", crpName)
		return err
	}

	defer hubClient.Delete(context.Background(), crp) //nolint
	if err := hubClient.Create(ctx, crp); err != nil {
		klog.ErrorS(err, "failed to apply crp", "namespace", nsName, "crp", crpName)
		return err
	}
	crpCount.Inc()

	klog.Infof("verify that the cluster resource placement `%s` is applied", crpName)
	fleetSize, clusterNames = collectApplyMetrics(ctx, hubClient, deadline, interval, crpName, currency, fleetSize, clusterNames)
	if fleetSize == "0" {
		return nil
	}
	klog.Infof("remove the namespaced resources applied by the placement `%s`", crpName)
	deletionStartTime := time.Now()
	if err := deleteTestManifests(ctx, hubClient, nsName); err != nil {
		klog.ErrorS(err, "failed to delete test manifests", "namespace", nsName, "crp", crpName)
		return err
	}
	collectDeleteMetrics(ctx, hubClient, deadline, interval, crpName, clusterNames, currency, fleetSize)

	// wait for the status of the CRP and make sure all conditions are all true
	klog.Infof("verify cluster resource placement `%s` is updated", crpName)
	waitForCrpToComplete(ctx, hubClient, deadline, interval, deletionStartTime, crpName, currency, fleetSize)
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

		case <-ctx.Done():
			// Context has been cancelled, exit the loop
			// timeout
			klog.Infof("the cluster resource placement `%s` timeout", crpName)
			LoadTestApplyCountMetric.WithLabelValues(currency, fleetSize, "timeout").Inc()
			applyTimeoutCount.Inc()
			return fleetSize, clusterNames
		case <-timer.C:
			// Deadline has been reached
			if condition.IsConditionStatusTrue(cond, 1) {
				// succeeded
				klog.Infof("the cluster resource placement `%s` succeeded", crpName)
				endTime := time.Since(startTime)
				if fleetSize, clusterNames, err = getFleetSize(crp, clusterNames); err != nil {
					klog.ErrorS(err, "Failed to get fleet size.")
					return fleetSize, nil
				}
				LoadTestApplyCountMetric.WithLabelValues(currency, fleetSize, "succeed").Inc()
				applySuccessCount.Inc()
				LoadTestApplyLatencyMetric.WithLabelValues(currency, fleetSize).Observe(endTime.Seconds())
				return fleetSize, clusterNames
			} else {
				// failed
				klog.Infof("the cluster resource placement `%s` failed", crpName)
				LoadTestApplyCountMetric.WithLabelValues(currency, fleetSize, "failed").Inc()
				applyFailCount.Inc()
				return fleetSize, clusterNames
			}
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
				LoadTestApplyCountMetric.WithLabelValues(currency, fleetSize, "succeed").Inc()
				applySuccessCount.Inc()
				LoadTestApplyLatencyMetric.WithLabelValues(currency, fleetSize).Observe(endTime.Seconds())
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
func collectDeleteMetrics(ctx context.Context, hubClient client.Client, deadline, pollInterval time.Duration, crpName string, clusterNames ClusterNames, currency string, fleetSize string) {
	var crp v1beta1.ClusterResourcePlacement
	startTime := time.Now()
	klog.Infof("verify that the applied resources on cluster resource placement `%s` are deleted", crpName)

	// Create a timer and a ticker
	timer := time.NewTimer(deadline)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			// Context has been cancelled, exit the loop
			klog.V(3).Infof("the cluster resource placement `%s` delete timeout", crpName)
			LoadTestDeleteCountMetric.WithLabelValues(currency, fleetSize, "timeout").Inc()
			deleteTimeoutCount.Inc()
			return
		case <-timer.C:
			// Deadline has been reached
			// timeout
			klog.V(3).Infof("the cluster resource placement `%s` delete timeout", crpName)
			LoadTestDeleteCountMetric.WithLabelValues(currency, fleetSize, "timeout").Inc()
			deleteTimeoutCount.Inc()
			return
		case <-ticker.C:
			// Interval for CRP status check
			if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName, Namespace: ""}, &crp); err != nil {
				klog.ErrorS(err, "failed to get crp", "crp", crpName)
				continue
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
				LoadTestDeleteCountMetric.WithLabelValues(currency, fleetSize, "succeed").Inc()
				deleteSuccessCount.Inc()
				LoadTestDeleteLatencyMetric.WithLabelValues(currency, fleetSize).Observe(time.Since(startTime).Seconds())
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
		case <-ctx.Done():
			// Context has been cancelled, exit the loop
			klog.V(3).Infof("the cluster resource placement `%s` timeout", crpName)
			LoadTestUpdateCountMetric.WithLabelValues(currency, fleetSize, "timeout").Inc()
			updateTimeoutCount.Inc()
			return
		case <-timer.C:
			// Deadline has been reached
			if condition.IsConditionStatusTrue(appliedCond, 1) && condition.IsConditionStatusTrue(synchronizedCond, 1) && condition.IsConditionStatusTrue(scheduledCond, 1) {
				// succeeded
				klog.V(3).Infof("the cluster resource placement `%s` succeeded", crpName)
				LoadTestUpdateCountMetric.WithLabelValues(currency, fleetSize, "succeed").Inc()
				updateSuccessCount.Inc()
				LoadTestUpdateLatencyMetric.WithLabelValues(currency, fleetSize).Observe(time.Since(deletionStartTime).Seconds())
				return
			} else {
				// failed
				klog.V(3).Infof("the cluster resource placement `%s` failed", crpName)
				LoadTestUpdateCountMetric.WithLabelValues(currency, fleetSize, "failed").Inc()
				updateFailCount.Inc()
				return
			}
		case <-ticker.C:
			// Interval for CRP status check
			if condition.IsConditionStatusTrue(appliedCond, 1) && condition.IsConditionStatusTrue(synchronizedCond, 1) && condition.IsConditionStatusTrue(scheduledCond, 1) {
				// succeeded
				klog.V(3).Infof("the cluster resource placement `%s` succeeded", crpName)
				LoadTestUpdateCountMetric.WithLabelValues(currency, fleetSize, "succeed").Inc()
				updateSuccessCount.Inc()
				LoadTestUpdateLatencyMetric.WithLabelValues(currency, fleetSize).Observe(time.Since(deletionStartTime).Seconds())
				return
			} else if appliedCond == nil || appliedCond.Status == metav1.ConditionUnknown || synchronizedCond == nil || synchronizedCond.Status == metav1.ConditionUnknown || scheduledCond == nil || scheduledCond.Status == metav1.ConditionUnknown {
				klog.V(3).Infof("the cluster resource placement `%s` is pending", crpName)
			} else if condition.IsConditionStatusFalse(appliedCond, 1) && condition.IsConditionStatusFalse(synchronizedCond, 1) && condition.IsConditionStatusFalse(scheduledCond, 1) {
				// failed
				klog.V(2).Infof("the cluster resource placement `%s` failed. try again", crpName)
			}
		}
	}
}

func PrintTestMetrics() {
	klog.Infof("CRP count", crpCount.Load())

	klog.InfoS("Placement apply result", "total applySuccessCount", applySuccessCount.Load(), "applyFailCount", applyFailCount.Load(), "applyTimeoutCount", applyTimeoutCount.Load())

	klog.InfoS("Placement delete result", "total deleteSuccessCount", deleteSuccessCount.Load(), "deleteTimeoutCount", deleteTimeoutCount.Load())

	klog.InfoS("Placement update result", "total updateSuccessCount", updateSuccessCount.Load(), "updateFailCount", updateFailCount.Load(), "updateTimeoutCount", updateTimeoutCount.Load())
}
