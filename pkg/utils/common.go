/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package utils

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	workv1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils/controller"
	"go.goms.io/fleet/pkg/utils/informer"
)

const (
	kubePrefix            = "kube-"
	fleetPrefix           = "fleet-"
	FleetSystemNamespace  = fleetPrefix + "system"
	NamespaceNameFormat   = fleetPrefix + "member-%s"
	RoleNameFormat        = fleetPrefix + "role-%s"
	RoleBindingNameFormat = fleetPrefix + "rolebinding-%s"
)

const (
	// NetworkingGroupName is the group name of the fleet networking.
	NetworkingGroupName = "networking.fleet.azure.com"
)

const (
	PlacementFieldManagerName    = "cluster-placement-controller"
	MCControllerFieldManagerName = "member-cluster-controller"
)

const (
	// LabelFleetObj is a label key indicate the resource is created by the fleet.
	LabelFleetObj      = "kubernetes.azure.com/managed-by"
	LabelFleetObjValue = "fleet"

	// LabelWorkPlacementName is used to indicate which placement created the work.
	// This label aims to enable different work objects to be managed by different placement.
	LabelWorkPlacementName = "work.fleet.azure.com/placement-name"

	// MemberClusterFinalizer is used to make sure that we handle gc of all the member cluster resources on the hub cluster.
	MemberClusterFinalizer = "work.fleet.azure.com/membercluster-finalizer"

	// LastWorkUpdateTimeAnnotationKey is used to mark the last update time on a work object.
	LastWorkUpdateTimeAnnotationKey = "work.fleet.azure.com/last-update-time"
)

var (
	FleetRule = rbacv1.PolicyRule{
		Verbs:     []string{"*"},
		APIGroups: []string{fleetv1alpha1.GroupVersion.Group},
		Resources: []string{"*"},
	}
	EventRule = rbacv1.PolicyRule{
		Verbs:     []string{"get", "list", "update", "patch", "watch", "create"},
		APIGroups: []string{""},
		Resources: []string{"events"},
	}
	WorkRule = rbacv1.PolicyRule{
		Verbs:     []string{"*"},
		APIGroups: []string{workv1alpha1.GroupName},
		Resources: []string{"*"},
	}
	FleetNetworkRule = rbacv1.PolicyRule{
		Verbs:     []string{"*"},
		APIGroups: []string{NetworkingGroupName},
		Resources: []string{"*"},
	}
)

// Those are the GVR/GVK of the fleet related resources.
var (
	ClusterResourcePlacementGVR = schema.GroupVersionResource{
		Group:    fleetv1alpha1.GroupVersion.Group,
		Version:  fleetv1alpha1.GroupVersion.Version,
		Resource: fleetv1alpha1.ClusterResourcePlacementResource,
	}

	ClusterResourcePlacementGVK = schema.GroupVersionKind{
		Group:   fleetv1alpha1.GroupVersion.Group,
		Version: fleetv1alpha1.GroupVersion.Version,
		Kind:    "ClusterResourcePlacement",
	}

	NamespaceGVK = schema.GroupVersionKind{
		Group:   corev1.GroupName,
		Version: corev1.SchemeGroupVersion.Version,
		Kind:    "Namespace",
	}

	NamespaceGVR = schema.GroupVersionResource{
		Group:    corev1.GroupName,
		Version:  corev1.SchemeGroupVersion.Version,
		Resource: "namespaces",
	}

	MemberClusterGVR = schema.GroupVersionResource{
		Group:    fleetv1alpha1.GroupVersion.Group,
		Version:  fleetv1alpha1.GroupVersion.Version,
		Resource: fleetv1alpha1.MemberClusterResource,
	}

	MemberClusterGVK = schema.GroupVersionKind{
		Group:   fleetv1alpha1.GroupVersion.Group,
		Version: fleetv1alpha1.GroupVersion.Version,
		Kind:    fleetv1alpha1.MemberClusterKind,
	}

	WorkGVK = schema.GroupVersionKind{
		Group:   workv1alpha1.GroupVersion.Group,
		Version: workv1alpha1.GroupVersion.Version,
		Kind:    workv1alpha1.WorkKind,
	}

	WorkGVR = schema.GroupVersionResource{
		Group:    workv1alpha1.GroupVersion.Group,
		Version:  workv1alpha1.GroupVersion.Version,
		Resource: workv1alpha1.WorkResource,
	}

	ServiceGVR = schema.GroupVersionResource{
		Group:    corev1.GroupName,
		Version:  corev1.SchemeGroupVersion.Version,
		Resource: "services",
	}
)

// RandSecureInt returns a uniform random value in [1, max] or panic.
// Only use this in tests.
func RandSecureInt(limit int64) int64 {
	if limit <= 0 {
		panic("limit <= 0")
	}
	nBig, err := rand.Int(rand.Reader, big.NewInt(limit))
	if err != nil {
		panic(err)
	}
	return nBig.Int64() + 1
}

func RandStr() string {
	const length = 10 // specific size to avoid user passes in unreasonably large size, causing runtime error
	const letters = "0123456789abcdefghijklmnopqrstuvwxyz"
	ret := make([]byte, length)
	for i := 0; i < length; i++ {
		num, err := rand.Int(rand.Reader, big.NewInt(int64(len(letters))))
		if err != nil {
			return ""
		}
		ret[i] = letters[num.Int64()]
	}

	return string(ret)
}

// CheckCRDInstalled checks if the custom resource definition is installed
func CheckCRDInstalled(discoveryClient discovery.DiscoveryInterface, gvk schema.GroupVersionKind) error {
	startTime := time.Now()
	err := retry.OnError(retry.DefaultBackoff, func(err error) bool { return true }, func() error {
		resourceList, err := discoveryClient.ServerResourcesForGroupVersion(gvk.GroupVersion().String())
		if err != nil {
			return err
		}
		for _, r := range resourceList.APIResources {
			if r.Kind == gvk.Kind {
				return nil
			}
		}
		return fmt.Errorf("kind not found in group version resources")
	})

	if err != nil {
		klog.ErrorS(err, "Failed to find resources", "gvk", gvk, "waiting time", time.Since(startTime))
	}
	return err
}

// ShouldPropagateObj decides if one should propagate the object
func ShouldPropagateObj(informerManager informer.Manager, uObj *unstructured.Unstructured) (bool, error) {
	// TODO:  add more special handling for different resource kind
	switch uObj.GroupVersionKind() {
	case corev1.SchemeGroupVersion.WithKind("ConfigMap"):
		// Skip the built-in custom CA certificate created in the namespace
		if uObj.GetName() == "kube-root-ca.crt" {
			return false, nil
		}
	case corev1.SchemeGroupVersion.WithKind("ServiceAccount"):
		// Skip the default service account created in the namespace
		if uObj.GetName() == "default" {
			return false, nil
		}
	case corev1.SchemeGroupVersion.WithKind("Secret"):
		// The secret, with type 'kubernetes.io/service-account-token', is created along with `ServiceAccount` should be
		// prevented from propagating.
		var secret corev1.Secret
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(uObj.Object, &secret); err != nil {
			return false, controller.NewUnexpectedBehaviorError(fmt.Errorf("failed to convert a secret object %s in namespace %s: %w", uObj.GetName(), uObj.GetNamespace(), err))
		}
		if secret.Type == corev1.SecretTypeServiceAccountToken {
			return false, nil
		}
	case corev1.SchemeGroupVersion.WithKind("Endpoints"):
		// we assume that all endpoints with the same name of a service is created by the service controller
		if _, err := informerManager.Lister(ServiceGVR).ByNamespace(uObj.GetNamespace()).Get(uObj.GetName()); err != nil {
			if apierrors.IsNotFound(err) {
				// there is no service of the same name as the end point,
				// we assume that this endpoint is created by the user
				return true, nil
			}
			return false, controller.NewAPIServerError(true, fmt.Errorf("failed to get the service %s in namespace %s: %w", uObj.GetName(), uObj.GetNamespace(), err))
		}
		// we find a service of the same name as the endpoint, we assume it's created by the service
		return false, nil
	case discoveryv1.SchemeGroupVersion.WithKind("EndpointSlice"):
		// all EndpointSlice created by the EndpointSlice controller has a managed by label
		if _, exist := uObj.GetLabels()[discoveryv1.LabelManagedBy]; exist {
			// do not propagate hub cluster generated endpoint slice
			return false, nil
		}
	}
	return true, nil
}

// IsReservedNamespace indicates if an argued namespace is reserved.
func IsReservedNamespace(namespace string) bool {
	return strings.HasPrefix(namespace, fleetPrefix) || strings.HasPrefix(namespace, kubePrefix)
}

// ShouldPropagateNamespace decides if we should propagate the resources in the namespace.
func ShouldPropagateNamespace(namespace string, skippedNamespaces map[string]bool) bool {
	if IsReservedNamespace(namespace) {
		return false
	}

	if skippedNamespaces[namespace] {
		return false
	}
	return true
}

// ExtractNumOfClustersFromPolicySnapshot extracts the numOfClusters value from the annotations
// on a policy snapshot.
func ExtractNumOfClustersFromPolicySnapshot(policy *fleetv1beta1.ClusterSchedulingPolicySnapshot) (int, error) {
	numOfClustersStr, ok := policy.Annotations[fleetv1beta1.NumberOfClustersAnnotation]
	if !ok {
		return 0, fmt.Errorf("cannot find annotation %s", fleetv1beta1.NumberOfClustersAnnotation)
	}

	// Cast the annotation to an integer; throw an error if the cast cannot be completed or the value is negative.
	numOfClusters, err := strconv.Atoi(numOfClustersStr)
	if err != nil || numOfClusters < 0 {
		return 0, fmt.Errorf("invalid annotation %s: %s is not a valid count: %w", fleetv1beta1.NumberOfClustersAnnotation, numOfClustersStr, err)
	}

	return numOfClusters, nil
}

// ExtractSubindexFromClusterResourceSnapshot extracts the subindex value from the annotations from a clusterResourceSnapshot.
func ExtractSubindexFromClusterResourceSnapshot(snapshot *fleetv1beta1.ClusterResourceSnapshot) (doesExist bool, subindex int, err error) {
	subindexStr, ok := snapshot.Annotations[fleetv1beta1.SubindexOfResourceSnapshotAnnotation]
	if !ok {
		return false, -1, nil
	}
	subindex, err = strconv.Atoi(subindexStr)
	if err != nil || subindex < 0 {
		return true, -1, fmt.Errorf("invalid annotation %s: %s is invalid: %w", fleetv1beta1.SubindexOfResourceSnapshotAnnotation, subindexStr, err)
	}

	return true, subindex, nil
}

func ExtractObservedCRPGenerationFromPolicySnapshot(policy *fleetv1beta1.ClusterSchedulingPolicySnapshot) (int64, error) {
	crpGeneartionStr, ok := policy.Annotations[fleetv1beta1.CRPGenerationAnnotation]
	if !ok {
		return 0, fmt.Errorf("cannot find annotation %s", fleetv1beta1.CRPGenerationAnnotation)
	}

	// Cast the annotation to an integer; throw an error if the cast cannot be completed or the value is negative.
	observedCRPGeneration, err := strconv.Atoi(crpGeneartionStr)
	if err != nil || observedCRPGeneration < 0 {
		return 0, fmt.Errorf("invalid annotation %s: %s is not a valid value: %w", fleetv1beta1.CRPGenerationAnnotation, crpGeneartionStr, err)
	}

	return int64(observedCRPGeneration), nil
}
