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

const (
	crpPrefix = "load-test-placement-"
	nsPrefix  = "load-test-ns-"
)

var (
	labelKey = "workload.azure.com/load"
)

var (
	LoadTestApplyCount = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "workload_apply_total",
		Help: "Total number of placement",
	}, []string{"concurrency", "fleetSize", "result"})

	LoadTestApplyLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "workload_apply_latency",
		Help:    "Length of time from placement change to it is applied to the member cluster",
		Buckets: []float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.7, 0.9, 1.0, 1.25, 1.5, 1.75, 2.0, 3.0, 4.0, 5, 7, 9, 10, 13, 17, 20, 23, 27, 30, 36, 45, 60, 90, 120, 150, 180},
	}, []string{"concurrency", "fleetSize"})

	LoadTestDeleteCount = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "workload_delete_total",
		Help: "Total number of placement delete",
	}, []string{"concurrency", "fleetSize", "result"})

	LoadTestDeleteLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "workload_delete_latency",
		Help:    "Length of time from resource deletion to it is deleted from the member cluster",
		Buckets: []float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.7, 0.9, 1.0, 1.25, 1.5, 1.75, 2.0, 3.0, 4.0, 5, 7, 9, 10, 13, 17, 20, 23, 27, 30, 36, 45, 60, 90, 120, 150, 180},
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
			continue
		}
		// check if the condition is true
		cond := crp.GetCondition(string(v1alpha1.ResourcePlacementStatusConditionTypeApplied))
		if cond == nil || cond.Status == metav1.ConditionUnknown {
			klog.V(5).Infof("the cluster resource placement `%s` is pending", crpName)
		} else if cond != nil && cond.Status == metav1.ConditionTrue {
			// succeeded
			klog.V(3).Infof("the cluster resource placement `%s` succeeded", crpName)
			LoadTestApplyCount.WithLabelValues(currency, fleetSize, "succeed").Inc()
			LoadTestApplyLatency.WithLabelValues(currency, fleetSize).Observe(time.Since(startTime).Seconds())
			break
		}
		if time.Now().After(applyDeadline) {
			if cond != nil && cond.Status == metav1.ConditionFalse {
				// failed
				klog.V(2).Infof("the cluster resource placement `%s` failed", crpName)
				LoadTestApplyCount.WithLabelValues(currency, fleetSize, "failed").Inc()
			} else {
				// timeout
				klog.V(2).Infof("the cluster resource placement `%s` timeout", crpName)
				LoadTestApplyCount.WithLabelValues(currency, fleetSize, "timeout").Inc()
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
				LoadTestDeleteCount.WithLabelValues(currency, fleetSize, "timeout").Inc()
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
			LoadTestDeleteCount.WithLabelValues(currency, fleetSize, "succeed").Inc()
			LoadTestDeleteLatency.WithLabelValues(currency, fleetSize).Observe(time.Since(startTime).Seconds())
			break
		}
		if time.Now().After(deleteDeadline) {
			// timeout
			klog.V(3).Infof("the cluster resource placement `%s` delete timeout", crpName)
			LoadTestDeleteCount.WithLabelValues(currency, fleetSize, "timeout").Inc()
			break
		}
	}
}
