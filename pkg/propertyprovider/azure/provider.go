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

// Package azure features the Azure property provider for Fleet.
package azure

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/propertyprovider"
	"github.com/kubefleet-dev/kubefleet/pkg/propertyprovider/azure/controllers"
	"github.com/kubefleet-dev/kubefleet/pkg/propertyprovider/azure/trackers"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/controller"
)

const (
	// A list of properties that the Azure property provider collects in addition to the
	// Fleet required ones.

	// PerCPUCoreCostProperty is a property that describes the average hourly cost of a CPU core in
	// a Kubernetes cluster.
	PerCPUCoreCostProperty = "kubernetes.azure.com/per-cpu-core-cost"
	// PerGBMemoryCostProperty is a property that describes the average cost of one GB of memory in
	// a Kubernetes cluster.
	PerGBMemoryCostProperty = "kubernetes.azure.com/per-gb-memory-cost"

	NodeCountPerSKUPropertyTmpl = "kubernetes.azure.com/vm-sizes/%s/count"

	CostPrecisionTemplate = "%.3f"
)

const (
	// The condition related values in use by the Azure property provider.
	CostPropertiesCollectionSucceededCondType   = "AKSClusterCostPropertiesCollectionSucceeded"
	CostPropertiesCollectionSucceededReason     = "CostsCalculated"
	CostPropertiesCollectionDegradedReason      = "CostsCalculationDegraded"
	CostPropertiesCollectionFailedReason        = "CostsCalculationFailed"
	CostPropertiesCollectionSucceededMsg        = "All cost properties have been collected successfully"
	CostPropertiesCollectionDegradedMsgTemplate = "Cost properties are collected in a degraded mode with the following warning(s): %v"
	CostPropertiesCollectionFailedMsgTemplate   = "An error has occurred when collecting cost properties: %v"
)

// PropertyProvider is the Azure property provider for Fleet.
type PropertyProvider struct {
	// The trackers.
	podTracker  *trackers.PodTracker
	nodeTracker *trackers.NodeTracker

	// The region where the Azure property provider resides.
	//
	// This is necessary as the pricing client requires that a region to be specified; it can
	// be either specified by the user or auto-discovered from the AKS cluster.
	region *string

	// The feature flags.
	isCostCollectionEnabled               bool
	isAvailableResourcesCollectionEnabled bool

	// The controller manager in use by the Azure property provider; this field is mostly reserved for
	// testing purposes.
	mgr ctrl.Manager
	// The names in use by the controllers managed by the property provider; these fields are exposed
	// to avoid name conflicts, though at this moment are mostly reserved for testing purposes.
	nodeControllerName string
	podControllerName  string
}

// Verify that the Azure property provider implements the MetricProvider interface at compile time.
var _ propertyprovider.PropertyProvider = &PropertyProvider{}

// Start starts the Azure property provider.
func (p *PropertyProvider) Start(ctx context.Context, config *rest.Config) error {
	klog.V(2).Info("Starting Azure property provider")

	podObj := client.Object(&corev1.Pod{})
	mgr, err := ctrl.NewManager(config, ctrl.Options{
		Scheme: scheme.Scheme,
		Cache: cache.Options{
			ByObject: map[client.Object]cache.ByObject{
				podObj: {
					// Set up field selectors so that API server will not send out watch events that
					// are not relevant to the pod watcher. This is essentially a trade-off between
					// in-memory check overhead and encoding/transmission overhead; for large clusters
					// with frequent pod creation/deletion ops, the trade-off seems to be worth it based
					// on current experimentation results.
					Field: fields.AndSelectors(
						fields.OneTermNotEqualSelector("spec.nodeName", ""),
						fields.OneTermNotEqualSelector("status.phase", string(corev1.PodSucceeded)),
						fields.OneTermNotEqualSelector("status.phase", string(corev1.PodFailed)),
					),
					// Drop irrelevant fields from the pod object; this can significantly reduce the
					// CPU and memory usage of the pod watcher, as less data is stored in cache.
					Transform: func(obj interface{}) (interface{}, error) {
						pod, ok := obj.(*corev1.Pod)
						if !ok {
							return nil, fmt.Errorf("failed to cast object to a pod object")
						}

						// The pod watcher only cares about a very limited set of pod fields,
						// specifically the pod's current phase, node name, and resource requests.

						// Drop unused metadata fields.
						pod.ObjectMeta.Labels = nil
						pod.ObjectMeta.Annotations = nil
						pod.ObjectMeta.OwnerReferences = nil
						pod.ObjectMeta.ManagedFields = nil

						// Drop the rest of the pod status as they are irrelevant to the pod watcher.
						pod.Status = corev1.PodStatus{
							Phase: pod.Status.Phase,
						}

						// Drop the unwanted pod spec fields.
						rebuiltedContainers := make([]corev1.Container, 0, len(pod.Spec.Containers))
						for idx := range pod.Spec.Containers {
							c := pod.Spec.Containers[idx]
							rebuiltedContainers = append(rebuiltedContainers, corev1.Container{
								Name:         c.Name,
								Image:        c.Image,
								Resources:    c.Resources,
								ResizePolicy: c.ResizePolicy,
							})
						}
						pod.Spec = corev1.PodSpec{
							NodeName:   pod.Spec.NodeName,
							Containers: rebuiltedContainers,
						}
						return pod, nil
					},
				},
			},
		},
		// Disable metric serving for the Azure property provider controller manager.
		//
		// Note that this will not stop the metrics from being collected and exported; as they
		// are registered via a top-level variable as a part of the controller runtime package,
		// which is also used by the Fleet member agent.
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
		// Disable health probe serving for the Azure property provider controller manager.
		HealthProbeBindAddress: "0",
		// Disable leader election for the Azure property provider.
		//
		// Note that for optimal performance, only the running instance of the Fleet member agent
		// (if there are multiple ones) should have the Azure property provider enabled; this can
		// be achieved by starting the Azure property provider only when an instance of the Fleet
		// member agent wins the leader election. It should be noted that running the Azure property
		// provider for multiple times will not incur any side effect other than some minor
		// performance costs, as at this moment the Azure property provider observes data individually
		// in a passive manner with no need for any centralized state.
		LeaderElection: false,
	})
	p.mgr = mgr

	if err != nil {
		klog.ErrorS(err, "Failed to start Azure property provider")
		return err
	}

	switch {
	case p.nodeTracker != nil:
		// A node tracker has been explicitly set; use it.
		klog.V(2).Info("A node tracker has been explicitly set")
	case p.isCostCollectionEnabled:
		// No node tracker has been set, and cost collection is enabled; set up a node tracker
		// using the default pricing client (the AKS Karpenter pricing client).
		klog.V(2).Info("Building a node tracker using the default AKS Karpenter pricing client")
		if p.region == nil || len(*p.region) == 0 {
			klog.V(2).Info("Auto-discover region as none has been specified")
			// Note that an API reader is passed here for the purpose of auto-discovering region
			// information from AKS nodes; at this time the cache from the controller manager
			// has not been initialized yet and as a result cached client is not yet available.
			//
			// This incurs the slightly higher overhead, however, as auto-discovery runs only
			// once, the performance impact is negligible.
			discoveredRegion, err := p.autoDiscoverRegionAndSetupTrackers(ctx, mgr.GetAPIReader())
			if err != nil {
				klog.ErrorS(err, "Failed to auto-discover region for the Azure property provider")
				return err
			}
			p.region = discoveredRegion
		}
		klog.V(2).Infof("Starting with the region set to %s", *p.region)
		pp := trackers.NewAKSKarpenterPricingClient(ctx, *p.region)
		p.nodeTracker = trackers.NewNodeTracker(pp)
	default:
		// No node tracker has been set, and cost collection is disabled; set up a node tracker
		// with no pricing provider.
		klog.V(2).Info("Building a node tracker with no pricing provider")
		p.nodeTracker = trackers.NewNodeTracker(nil)
	}

	// Set up the node reconciler.
	klog.V(2).Info("Setting up the node reconciler")
	nodeReconciler := &controllers.NodeReconciler{
		NT:     p.nodeTracker,
		Client: mgr.GetClient(),
	}
	if err := nodeReconciler.SetupWithManager(mgr, p.nodeControllerName); err != nil {
		klog.ErrorS(err, "Failed to start the node reconciler in the Azure property provider")
		return err
	}

	switch {
	case p.podTracker != nil:
		// A pod tracker has been explicitly set; use it.
		klog.V(2).Info("A pod tracker has been explicitly set")
	case !p.isAvailableResourcesCollectionEnabled:
		// Available resource collection is disabled; there is no need to set up a pod tracker, and
		// as a result there is no need to watch for pods either.
		klog.V(2).Info("Skipping pod tracker setup as available resources collection is disabled")
	default:
		// No pod tracker has been set, and available resources collection is enabled; set up
		// a pod tracker.
		klog.V(2).Info("Building a pod tracker")
		p.podTracker = trackers.NewPodTracker()

		klog.V(2).Info("Starting the pod reconciler")
		podReconciler := &controllers.PodReconciler{
			PT:     p.podTracker,
			Client: mgr.GetClient(),
		}
		if err := podReconciler.SetupWithManager(mgr, p.podControllerName); err != nil {
			klog.ErrorS(err, "Failed to start the pod reconciler in the Azure property provider")
			return err
		}
	}

	// Start the controller manager.
	//
	// Note that the controller manager will run in a separate goroutine to avoid blocking
	// the member agent.
	go func() {
		// This call will block until the context exits.
		if err := mgr.Start(ctx); err != nil {
			klog.ErrorS(err, "Failed to start the Azure property provider controller manager")
		}
	}()

	// Wait for the cache to sync.
	//
	// Note that this does not guarantee that any of the object changes has actually been
	// processed; it only implies that an initial state has been populated. Though for our
	// use case it might be good enough, considering that the only side effect is that
	// some exported properties might be skewed initially (e.g., nodes/pods not being tracked).
	//
	// An alternative is to perform a list for once during the startup, which might be
	// too expensive for a large cluster.
	mgr.GetCache().WaitForCacheSync(ctx)

	return nil
}

// Collect collects the properties of an AKS cluster.
func (p *PropertyProvider) Collect(ctx context.Context) propertyprovider.PropertyCollectionResponse {
	conds := make([]metav1.Condition, 0, 1)

	// Collect the non-resource properties.
	properties := make(map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue)

	// Collect node-count related properties.
	p.collectNodeCountRelatedProperties(ctx, properties)

	// Collect the cost properties (if enabled).
	if p.isCostCollectionEnabled {
		costConds := p.collectCosts(ctx, properties)
		conds = append(conds, costConds...)
	}

	// Collect the resource properties.

	// Collect the total and allocatable resource properties.
	resources := clusterv1beta1.ResourceUsage{}
	resources.Capacity = p.nodeTracker.TotalCapacity()
	resources.Allocatable = p.nodeTracker.TotalAllocatable()

	// Collect the available resource properties (if enabled).
	if p.isAvailableResourcesCollectionEnabled {
		p.collectAvailableResource(ctx, &resources)
	}

	// Return the collection response.
	return propertyprovider.PropertyCollectionResponse{
		Properties: properties,
		Resources:  resources,
		Conditions: conds,
	}
}

// collectNodeCountRelatedProperties collects the node-count related properties.
func (p *PropertyProvider) collectNodeCountRelatedProperties(_ context.Context, properties map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue) {
	now := metav1.Now()

	// Collect the total node count as a property.
	properties[propertyprovider.NodeCountProperty] = clusterv1beta1.PropertyValue{
		Value:           fmt.Sprintf("%d", p.nodeTracker.NodeCount()),
		ObservationTime: now,
	}

	// Collect the per-SKU node counts as properties.
	nodeCountPerSKU := p.nodeTracker.NodeCountPerSKU()
	for sku, count := range nodeCountPerSKU {
		pName := fmt.Sprintf(NodeCountPerSKUPropertyTmpl, sku)
		properties[clusterv1beta1.PropertyName(pName)] = clusterv1beta1.PropertyValue{
			Value:           fmt.Sprintf("%d", count),
			ObservationTime: now,
		}
	}
}

// collectCosts collects the cost information.
func (p *PropertyProvider) collectCosts(_ context.Context, properties map[clusterv1beta1.PropertyName]clusterv1beta1.PropertyValue) []metav1.Condition {
	conds := make([]metav1.Condition, 0, 1)

	perCPUCost, perGBMemoryCost, warnings, err := p.nodeTracker.Costs()
	switch {
	case err != nil:
		// An error occurred when calculating costs; do no set the cost properties and
		// track the error.
		conds = append(conds, metav1.Condition{
			Type:    CostPropertiesCollectionSucceededCondType,
			Status:  metav1.ConditionFalse,
			Reason:  CostPropertiesCollectionFailedReason,
			Message: fmt.Sprintf(CostPropertiesCollectionFailedMsgTemplate, err),
		})
	case len(warnings) > 0:
		// The costs are calculated, but some warnings have been issued; set the cost
		// properties and report the warnings as a condition.
		properties[PerCPUCoreCostProperty] = clusterv1beta1.PropertyValue{
			Value:           fmt.Sprintf(CostPrecisionTemplate, perCPUCost),
			ObservationTime: metav1.Now(),
		}
		properties[PerGBMemoryCostProperty] = clusterv1beta1.PropertyValue{
			Value:           fmt.Sprintf(CostPrecisionTemplate, perGBMemoryCost),
			ObservationTime: metav1.Now(),
		}
		conds = append(conds, metav1.Condition{
			Type:    CostPropertiesCollectionSucceededCondType,
			Status:  metav1.ConditionTrue,
			Reason:  CostPropertiesCollectionDegradedReason,
			Message: fmt.Sprintf(CostPropertiesCollectionDegradedMsgTemplate, warnings),
		})
	default:
		// The costs are calculated successfully; set the cost properties and
		// report a success as a condition.
		properties[PerCPUCoreCostProperty] = clusterv1beta1.PropertyValue{
			Value:           fmt.Sprintf(CostPrecisionTemplate, perCPUCost),
			ObservationTime: metav1.Now(),
		}
		properties[PerGBMemoryCostProperty] = clusterv1beta1.PropertyValue{
			Value:           fmt.Sprintf(CostPrecisionTemplate, perGBMemoryCost),
			ObservationTime: metav1.Now(),
		}
		conds = append(conds, metav1.Condition{
			Type:    CostPropertiesCollectionSucceededCondType,
			Status:  metav1.ConditionTrue,
			Reason:  CostPropertiesCollectionSucceededReason,
			Message: CostPropertiesCollectionSucceededMsg,
		})
	}

	return conds
}

// collectAvailableResource collects the available resource information.
func (p *PropertyProvider) collectAvailableResource(_ context.Context, usage *clusterv1beta1.ResourceUsage) {
	if p.podTracker == nil {
		// No pod tracker has been set; but the property provider has been configured to collect
		// available resource information. Normally this should never occur.
		_ = controller.NewUnexpectedBehaviorError(fmt.Errorf("no pod tracker has been set, but the property provider has been configured to collect available resource information"))
	}

	requested := p.podTracker.TotalRequested()
	allocatable := usage.Allocatable
	available := make(corev1.ResourceList)
	for rn := range allocatable {
		left := allocatable[rn].DeepCopy()
		// In some unlikely scenarios, it could happen that, due to unavoidable
		// inconsistencies in the data collection process, the total value of a specific
		// requested resource exceeds that of the allocatable resource, as observed by
		// the property provider; for example, the node tracker might fail to track a node
		// in time yet the some pods have been assigned to the pod and gets tracked by
		// the pod tracker. In such cases, the property provider will report a zero
		// value for the resource; and this occurrence should get fixed in the next (few)
		// property collection iterations.
		if left.Cmp(requested[rn]) > 0 {
			left.Sub(requested[rn])
		} else {
			left = resource.Quantity{}
		}
		available[rn] = left
	}
	usage.Available = available
}

// autoDiscoverRegionAndSetupTrackers auto-discovers the region of the AKS cluster.
func (p *PropertyProvider) autoDiscoverRegionAndSetupTrackers(ctx context.Context, c client.Reader) (*string, error) {
	klog.V(2).Info("Auto-discover region for the Azure property provider")
	// Auto-discover the region by listing the nodes.
	nodeList := &corev1.NodeList{}
	// List only one node to reduce performance impact (if supported).
	//
	// By default an AKS cluster always has at least one node; all nodes should be in the same
	// region and has the topology label set.
	req, err := labels.NewRequirement(corev1.LabelTopologyRegion, selection.Exists, []string{})
	if err != nil {
		// This should never happen.
		err := fmt.Errorf("failed to create a label requirement: %w", err)
		klog.Error(err)
		return nil, err
	}
	listOptions := client.ListOptions{
		LabelSelector: labels.NewSelector().Add(*req),
		Limit:         1,
	}
	if err := c.List(ctx, nodeList, &listOptions); err != nil {
		err := fmt.Errorf("failed to list nodes with the region label: %w", err)
		klog.Error(err)
		return nil, err
	}

	// If no nodes are found, return an error.
	if len(nodeList.Items) == 0 {
		err := fmt.Errorf("no nodes found with the region label")
		klog.Error(err)
		return nil, err
	}

	// Extract the region from the first node via the region label.
	node := nodeList.Items[0]
	nodeRegion, found := node.Labels[corev1.LabelTopologyRegion]
	if !found {
		// The region label is absent; normally this should never occur.
		err := fmt.Errorf("region label is absent on node %s", node.Name)
		klog.Error(err)
		return nil, err
	}
	klog.V(2).InfoS("Auto-discovered region for the Azure property provider", "region", nodeRegion)

	return &nodeRegion, nil
}

// New returns a new Azure property provider using the default pricing provider, which is,
// at this moment, an AKS Karpenter pricing client.
//
// If the region is unspecified at the time when this function is called, the provider
// will attempt to auto-discover the region of its host cluster when the Start method is
// called.
func New(
	region *string,
	isCostCollectionEnabled, isAvailableResourcesCollectionEnabled bool,
) propertyprovider.PropertyProvider {
	return &PropertyProvider{
		region: region,
		// Use the default names.
		nodeControllerName:                    "azure-property-provider-node-watcher",
		podControllerName:                     "azure-property-provider-pod-watcher",
		isCostCollectionEnabled:               isCostCollectionEnabled,
		isAvailableResourcesCollectionEnabled: isAvailableResourcesCollectionEnabled,
	}
}

// NewWithPricingProvider returns a new Azure property provider with the given
// pricing provider.
//
// This is mostly used for allow plugging in of alternate pricing providers (one that
// does not use the Karpenter client), and for testing purposes.
func NewWithPricingProvider(
	pp trackers.PricingProvider,
	nodeControllerName, podControllerName string,
	isCostCollectionEnabled, isAvailableResourcesCollectionEnabled bool,
) propertyprovider.PropertyProvider {
	return &PropertyProvider{
		nodeTracker:                           trackers.NewNodeTracker(pp),
		nodeControllerName:                    nodeControllerName,
		podControllerName:                     podControllerName,
		isCostCollectionEnabled:               isCostCollectionEnabled,
		isAvailableResourcesCollectionEnabled: isAvailableResourcesCollectionEnabled,
	}
}
