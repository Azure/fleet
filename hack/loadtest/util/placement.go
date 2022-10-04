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

const interval = 50 * time.Millisecond

var (
	labelKey = "workload.azure.com/load"
)

var (
	LoadTestApplyErrorCount = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "workload_apply_errors_total",
		Help: "Total number of placement errors",
	}, []string{"currency", "fleetSize", "mode"})

	LoadTestApplySuccessCount = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "workload_apply_total",
		Help: "Total number of placement",
	}, []string{"currency", "fleetSize"})

	LoadTestApplyLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name: "workload_apply_latency",
		Help: "Length of time from placement change to it is applied to the member cluster",
		Buckets: []float64{0.01, 0.025, 0.05, 0.1, 0.15, 0.2, 0.25, 0.3, 0.35, 0.4, 0.45, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0,
			1.25, 1.5, 1.75, 2.0, 2.5, 3.0, 3.5, 4.0, 4.5, 5, 6, 7, 8, 9, 10, 15, 20, 25, 30, 40, 50, 60},
	}, []string{"currency", "fleetSize"})

	LoadTestDeleteErrorCount = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "workload_delete_errors_total",
		Help: "Total number of placement delete errors",
	}, []string{"currency", "fleetSize"})

	LoadTestDeleteSuccessCount = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "workload_delete_total",
		Help: "Total number of placement deleted",
	}, []string{"currency", "fleetSize"})

	LoadTestDeleteLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name: "workload_delete_latency",
		Help: "Length of time from resource deletion to it is deleted from the member cluster",
		Buckets: []float64{0.01, 0.025, 0.05, 0.1, 0.15, 0.2, 0.25, 0.3, 0.35, 0.4, 0.45, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0,
			1.25, 1.5, 1.75, 2.0, 2.5, 3.0, 3.5, 4.0, 4.5, 5, 6, 7, 8, 9, 10, 15, 20, 25, 30, 40, 50, 60},
	}, []string{"currency", "fleetSize"})
)

func MeasureOnePlacement(ctx context.Context, hubClient client.Client, deadline time.Duration, maxCurrentPlacement int, clusterNames ClusterNames) error {
	crpName := "load-test-placement-" + utilrand.String(10)
	nsName := "load-test-ns-" + utilrand.String(10)
	fleetSize := strconv.Itoa(len(clusterNames))
	currency := strconv.Itoa(maxCurrentPlacement)

	defer klog.Flush()
	defer deleteNamespace(context.Background(), hubClient, nsName)
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
	defer hubClient.Delete(context.Background(), crp)
	if err := hubClient.Create(ctx, crp); err != nil {
		klog.ErrorS(err, "failed to apply crp", "namespace", nsName, "crp", crpName)
		return err
	}

	klog.Infof("verify that the cluster resource placement `%s` is applied", crpName)
	collectApplyMetrics(ctx, hubClient, deadline, crpName, currency, fleetSize)

	klog.Infof("remove the namespace referred by the placement `%s`", crpName)
	deleteNamespace(ctx, hubClient, nsName)
	collectDeleteMetrics(ctx, hubClient, deadline, crpName, clusterNames, currency, fleetSize)

	return hubClient.Delete(ctx, crp)
}

// collect the crp apply metrics
func collectApplyMetrics(ctx context.Context, hubClient client.Client, deadline time.Duration, crpName string, currency string, fleetSize string) time.Time {
	startTime := time.Now()
	applyDeadline := startTime.Add(deadline)
	var crp v1alpha1.ClusterResourcePlacement
	for {
		if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName, Namespace: ""}, &crp); err != nil {
			time.Sleep(interval)
			continue
		}
		// check if the condition is true
		cond := crp.GetCondition(string(v1alpha1.ResourcePlacementStatusConditionTypeApplied))
		if cond == nil || cond.Status == metav1.ConditionUnknown {
			// wait
			klog.V(5).Infof("the cluster resource placement `%s` is pending", crpName)
			time.Sleep(interval)
		} else if cond != nil && cond.Status == metav1.ConditionTrue {
			// succeeded
			klog.V(3).Infof("the cluster resource placement `%s` succeeded", crpName)
			LoadTestApplySuccessCount.WithLabelValues(currency, fleetSize).Inc()
			LoadTestApplyLatency.WithLabelValues(currency, fleetSize).Observe(time.Since(startTime).Seconds())
			break
		}
		if time.Now().After(applyDeadline) {
			if cond != nil && cond.Status == metav1.ConditionFalse {
				// failed
				klog.V(3).Infof("the cluster resource placement `%s` failed", crpName)
				LoadTestApplyErrorCount.WithLabelValues(currency, fleetSize, "failed").Inc()
			} else {
				// timeout
				klog.V(3).Infof("the cluster resource placement `%s` timeout", crpName)
				LoadTestApplyErrorCount.WithLabelValues(currency, fleetSize, "timeout").Inc()
			}
			break
		}
	}
	return startTime
}

func collectDeleteMetrics(ctx context.Context, hubClient client.Client, deadline time.Duration, crpName string, clusterNames ClusterNames, currency string, fleetSize string) {
	var crp v1alpha1.ClusterResourcePlacement
	startTime := time.Now()
	deleteDeadline := startTime.Add(deadline * 3)
	klog.Infof("verify that the cluster resource placement `%s` is deleted", crpName)
	for {
		if err := hubClient.Get(ctx, types.NamespacedName{Name: crpName, Namespace: ""}, &crp); err != nil {
			time.Sleep(interval)
			continue
		}
		if len(crp.Status.SelectedResources) != 1 {
			klog.V(3).Infof("the crp `%s` has not picked up the namespaced resource deleted change", crpName)
			// wait
			time.Sleep(interval)
			continue
		}
		// check if the change is picked up by the member agent
		var clusterWork workv1alpha1.Work
		removed := true
		for _, clusterName := range clusterNames {
			err := hubClient.Get(ctx, types.NamespacedName{Name: crpName, Namespace: fmt.Sprintf(utils.NamespaceNameFormat, clusterName)}, &clusterWork)
			if err != nil || (err == nil && len(clusterWork.Status.ManifestConditions) != 1) {
				klog.V(3).Infof("the resources `%s` in cluster namespace `%s` is not removed by the member agent yet", crpName, clusterName)
				removed = false
				break
			}
		}
		if !removed {
			// wait
			time.Sleep(interval)
		} else {
			// succeeded
			klog.V(3).Infof("the cluster resource placement `%s` delete succeeded", crpName)
			LoadTestDeleteSuccessCount.WithLabelValues(currency, fleetSize).Inc()
			LoadTestDeleteLatency.WithLabelValues(currency, fleetSize).Observe(time.Since(startTime).Seconds())
			break
		}
		if time.Now().After(deleteDeadline) {
			// timeout
			klog.V(3).Infof("the cluster resource placement `%s` delete timeout", crpName)
			LoadTestDeleteErrorCount.WithLabelValues(currency, fleetSize).Inc()
			break
		}
	}
}
