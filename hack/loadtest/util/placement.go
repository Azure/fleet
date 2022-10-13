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
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	workv1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"

	"go.goms.io/fleet/apis/v1alpha1"
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
)

func MeasureOnePlacement(ctx context.Context, hubClient client.Client, deadline, interval time.Duration, maxCurrentPlacement int, clusterNames ClusterNames) error {
	crpName := crpPrefix + utilrand.String(10)
	nsName := nsPrefix + utilrand.String(10)
	fleetSize := strconv.Itoa(len(clusterNames))
	currency := strconv.Itoa(maxCurrentPlacement)

	defer klog.Flush()
	defer deleteNamespace(context.Background(), hubClient, nsName) //nolint

	klog.Infof("create the resources in namespace `%s` in the hub cluster", nsName)
	if err := applyTestManifests(ctx, hubClient, nsName); err != nil {
		klog.ErrorS(err, "failed to apply namespaced resources", "namespace", nsName)
		return err
	}

	klog.Infof("create the cluster resource placement `%s` in the hub cluster", crpName)
	crp := &v1alpha1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name: crpName,
		},
		Spec: v1alpha1.ClusterResourcePlacementSpec{
			ResourceSelectors: []v1alpha1.ClusterResourceSelector{
				{
					Group:   "",
					Version: "v1",
					Kind:    "Namespace",
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{labelKey: nsName},
					},
				},
				{
					Group:   apiextensionsv1.GroupName,
					Version: "v1",
					Kind:    "CustomResourceDefinition",
					Name:    "clonesets.apps.kruise.io",
				},
			},
		},
	}
	defer hubClient.Delete(context.Background(), crp) //nolint
	if err := hubClient.Create(ctx, crp); err != nil {
		klog.ErrorS(err, "failed to apply crp", "namespace", nsName, "crp", crpName)
		return err
	}

	klog.Infof("verify that the cluster resource placement `%s` is applied", crpName)
	collectApplyMetrics(ctx, hubClient, deadline, interval, crpName, currency, fleetSize)

	klog.Infof("remove the namespaced resources referred by the placement `%s`", crpName)
	if err := deleteTestManifests(ctx, hubClient, nsName); err != nil {
		klog.ErrorS(err, "failed to delete test manifests", "namespace", nsName, "crp", crpName)
		return err
	}
	collectDeleteMetrics(ctx, hubClient, deadline, interval, crpName, clusterNames, currency, fleetSize)

	return hubClient.Delete(ctx, crp)
}

// collect the crp apply metrics
func collectApplyMetrics(ctx context.Context, hubClient client.Client, deadline, pollInterval time.Duration, crpName string, currency string, fleetSize string) {
	startTime := time.Now()
	applyDeadline := startTime.Add(deadline)
	var crp v1alpha1.ClusterResourcePlacement
	var err error
	for ; ctx.Err() == nil; time.Sleep(pollInterval) {
		if err = hubClient.Get(ctx, types.NamespacedName{Name: crpName, Namespace: ""}, &crp); err != nil {
			klog.ErrorS(err, "failed to get crp", "crp", crpName)
			if time.Now().After(applyDeadline) {
				// timeout
				klog.V(2).Infof("the cluster resource placement `%s` timeout", crpName)
				LoadTestApplyCountMetric.WithLabelValues(currency, fleetSize, "timeout").Inc()
				applyTimeoutCount.Inc()
				break
			}
			continue
		}
		// check if the condition is true
		cond := crp.GetCondition(string(v1alpha1.ResourcePlacementStatusConditionTypeApplied))
		if cond == nil || cond.Status == metav1.ConditionUnknown {
			klog.V(5).Infof("the cluster resource placement `%s` is pending", crpName)
		} else if cond != nil && cond.Status == metav1.ConditionTrue {
			// succeeded
			klog.V(3).Infof("the cluster resource placement `%s` succeeded", crpName)
			LoadTestApplyCountMetric.WithLabelValues(currency, fleetSize, "succeed").Inc()
			applySuccessCount.Inc()
			LoadTestApplyLatencyMetric.WithLabelValues(currency, fleetSize).Observe(time.Since(startTime).Seconds())
			break
		}
		if time.Now().After(applyDeadline) {
			if cond != nil && cond.Status == metav1.ConditionFalse {
				// failed
				klog.V(2).Infof("the cluster resource placement `%s` failed", crpName)
				LoadTestApplyCountMetric.WithLabelValues(currency, fleetSize, "failed").Inc()
				applyFailCount.Inc()
			} else {
				// timeout
				klog.V(2).Infof("the cluster resource placement `%s` timeout", crpName)
				LoadTestApplyCountMetric.WithLabelValues(currency, fleetSize, "timeout").Inc()
				applyTimeoutCount.Inc()
			}
			break
		}
	}
}

func collectDeleteMetrics(ctx context.Context, hubClient client.Client, deadline, pollInterval time.Duration, crpName string, clusterNames ClusterNames, currency string, fleetSize string) {
	var crp v1alpha1.ClusterResourcePlacement
	startTime := time.Now()
	deleteDeadline := startTime.Add(deadline)
	klog.Infof("verify that the cluster resource placement `%s` is deleted", crpName)
	for ; ctx.Err() == nil; time.Sleep(pollInterval) {
		if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName, Namespace: ""}, &crp); err != nil {
			klog.ErrorS(err, "failed to get crp", "crp", crpName)
			continue
		}
		// the only thing it still selects are namespace and crd
		if len(crp.Status.SelectedResources) != 2 {
			klog.V(4).Infof("the crp `%s` has not picked up the namespaced resource deleted change", crpName)
			if time.Now().After(deleteDeadline) {
				// timeout
				klog.V(3).Infof("the cluster resource placement `%s` delete timeout", crpName)
				LoadTestDeleteCountMetric.WithLabelValues(currency, fleetSize, "timeout").Inc()
				deleteTimeoutCount.Inc()
				break
			}
			continue
		}
		// check if the change is picked up by the member agent
		var clusterWork workv1alpha1.Work
		allRemoved := true
		for _, clusterName := range clusterNames {
			err := hubClient.Get(ctx, types.NamespacedName{Name: crpName, Namespace: fmt.Sprintf(utils.NamespaceNameFormat, clusterName)}, &clusterWork)
			if err != nil || (err == nil && len(clusterWork.Status.ManifestConditions) != 2) {
				klog.V(4).Infof("the resources `%s` in cluster namespace `%s` is not removed by the member agent yet", crpName, clusterName)
				allRemoved = false
				break
			}
		}
		if allRemoved {
			// succeeded
			klog.V(3).Infof("the cluster resource placement `%s` delete succeeded", crpName)
			LoadTestDeleteCountMetric.WithLabelValues(currency, fleetSize, "succeed").Inc()
			deleteSuccessCount.Inc()
			LoadTestDeleteLatencyMetric.WithLabelValues(currency, fleetSize).Observe(time.Since(startTime).Seconds())
			break
		}
		if time.Now().After(deleteDeadline) {
			// timeout
			klog.V(3).Infof("the cluster resource placement `%s` delete timeout", crpName)
			LoadTestDeleteCountMetric.WithLabelValues(currency, fleetSize, "timeout").Inc()
			deleteTimeoutCount.Inc()
			break
		}
	}
}

func PrintTestMetrics() {
	klog.InfoS("Placement apply result", "total applySuccessCount", applySuccessCount.Load(), "applyFailCount", applyFailCount.Load(), "applyTimeoutCount", applyTimeoutCount.Load())

	klog.InfoS("Placement delete result", "total deleteSuccessCount", deleteSuccessCount.Load(), "deleteTimeoutCount", deleteTimeoutCount.Load())
}
