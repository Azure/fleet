/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package utils

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"time"

	appv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	workv1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"

	fleetnetworkingv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils/condition"
	"go.goms.io/fleet/pkg/utils/controller"
	"go.goms.io/fleet/pkg/utils/informer"
)

const (
	kubePrefix             = "kube-"
	fleetPrefix            = "fleet-"
	FleetSystemNamespace   = fleetPrefix + "system"
	NamespaceNameFormat    = fleetPrefix + "member-%s"
	RoleNameFormat         = fleetPrefix + "role-%s"
	RoleBindingNameFormat  = fleetPrefix + "rolebinding-%s"
	ValidationPathFmt      = "/validate-%s-%s-%s"
	lessGroupsStringFormat = "groups: %v"
	moreGroupsStringFormat = "groups: [%s, %s, %s,......]"
)

const (
	// NetworkingGroupName is the group name of the fleet networking.
	NetworkingGroupName = "networking.fleet.azure.com"

	DeploymentKind  = "Deployment"
	DaemonSetKind   = "DaemonSet"
	StatefulSetKind = "StatefulSet"
	ConfigMapKind   = "ConfigMap"
	ServiceKind     = "Service"
	NamespaceKind   = "Namespace"
	JobKind         = "Job"
)

const (
	PlacementFieldManagerName           = "cluster-placement-controller"
	MCControllerFieldManagerName        = "member-cluster-controller"
	OverrideControllerFieldManagerName  = "override-controller"
	UpdateRunControllerFieldManagerName = "cluster-staged-update-run-controller"
)

// TODO(ryanzhang): move this to the api directory
const (
	// LabelFleetObj is a label key indicate the resource is created by the fleet.
	LabelFleetObj      = "kubernetes.azure.com/managed-by"
	LabelFleetObjValue = "fleet"

	// LabelWorkPlacementName is used to indicate which placement created the work.
	// This label aims to enable different work objects to be managed by different placement.
	LabelWorkPlacementName = "work.fleet.azure.com/placement-name"

	// LastWorkUpdateTimeAnnotationKey is used to mark the last update time on a work object.
	LastWorkUpdateTimeAnnotationKey = "work.fleet.azure.com/last-update-time"

	// ResourceIdentifierStringFormat is the format of the resource identifier string.
	ResourceIdentifierStringFormat = "%s/%s/%s/%s/%s"

	// ResourceIdentifierWithEnvelopeIdentifierStringFormat is the format of the resource identifier string with envelope identifier.
	ResourceIdentifierWithEnvelopeIdentifierStringFormat = "%s/%s/%s/%s/%s/%s/%s/%s"

	// FleetAnnotationPrefix is the prefix used to annotate fleet member cluster resources.
	FleetAnnotationPrefix = "fleet.azure.com"
)

var (
	FleetRule = rbacv1.PolicyRule{
		Verbs:     []string{"*"},
		APIGroups: []string{fleetv1alpha1.GroupVersion.Group},
		Resources: []string{"*"},
	}
	FleetClusterRule = rbacv1.PolicyRule{
		Verbs:     []string{"*"},
		APIGroups: []string{clusterv1beta1.GroupVersion.Group},
		Resources: []string{"*"},
	}
	FleetPlacementRule = rbacv1.PolicyRule{
		Verbs:     []string{"*"},
		APIGroups: []string{placementv1beta1.GroupVersion.Group},
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
	ClusterResourcePlacementV1Alpha1GVK = schema.GroupVersionKind{
		Group:   fleetv1alpha1.GroupVersion.Group,
		Version: fleetv1alpha1.GroupVersion.Version,
		Kind:    "ClusterResourcePlacement",
	}

	ClusterResourcePlacementV1Alpha1GVR = schema.GroupVersionResource{
		Group:    fleetv1alpha1.GroupVersion.Group,
		Version:  fleetv1alpha1.GroupVersion.Version,
		Resource: fleetv1alpha1.ClusterResourcePlacementResource,
	}

	ClusterResourcePlacementGVR = schema.GroupVersionResource{
		Group:    placementv1beta1.GroupVersion.Group,
		Version:  placementv1beta1.GroupVersion.Version,
		Resource: placementv1beta1.ClusterResourcePlacementResource,
	}

	ClusterResourcePlacementMetaGVK = metav1.GroupVersionKind{
		Group:   placementv1beta1.GroupVersion.Group,
		Version: placementv1beta1.GroupVersion.Version,
		Kind:    placementv1beta1.ClusterResourcePlacementKind,
	}

	ConfigMapGVK = schema.GroupVersionKind{
		Group:   corev1.GroupName,
		Version: corev1.SchemeGroupVersion.Version,
		Kind:    ConfigMapKind,
	}

	CRDMetaGVK = metav1.GroupVersionKind{
		Group:   apiextensionsv1.SchemeGroupVersion.Group,
		Version: apiextensionsv1.SchemeGroupVersion.Version,
		Kind:    "CustomResourceDefinition",
	}

	EndpointSliceExportMetaGVK = metav1.GroupVersionKind{
		Group:   fleetnetworkingv1alpha1.GroupVersion.Group,
		Version: fleetnetworkingv1alpha1.GroupVersion.Version,
		Kind:    "EndpointSliceExport",
	}

	EndpointSliceImportMetaGVK = metav1.GroupVersionKind{
		Group:   fleetnetworkingv1alpha1.GroupVersion.Group,
		Version: fleetnetworkingv1alpha1.GroupVersion.Version,
		Kind:    "EndpointSliceImport",
	}

	EventMetaGVK = metav1.GroupVersionKind{
		Group:   corev1.SchemeGroupVersion.Group,
		Version: corev1.SchemeGroupVersion.Version,
		Kind:    "Event",
	}

	IMCV1Alpha1MetaGVK = metav1.GroupVersionKind{
		Group:   fleetv1alpha1.GroupVersion.Group,
		Version: fleetv1alpha1.GroupVersion.Version,
		Kind:    "InternalMemberCluster",
	}

	InternalServiceExportMetaGVK = metav1.GroupVersionKind{
		Group:   fleetnetworkingv1alpha1.GroupVersion.Group,
		Version: fleetnetworkingv1alpha1.GroupVersion.Version,
		Kind:    "InternalServiceExport",
	}

	InternalServiceImportMetaGVK = metav1.GroupVersionKind{
		Group:   fleetnetworkingv1alpha1.GroupVersion.Group,
		Version: fleetnetworkingv1alpha1.GroupVersion.Version,
		Kind:    "InternalServiceImport",
	}

	IMCMetaGVK = metav1.GroupVersionKind{
		Group:   clusterv1beta1.GroupVersion.Group,
		Version: clusterv1beta1.GroupVersion.Version,
		Kind:    "InternalMemberCluster",
	}

	MCV1Alpha1MetaGVK = metav1.GroupVersionKind{
		Group:   fleetv1alpha1.GroupVersion.Group,
		Version: fleetv1alpha1.GroupVersion.Version,
		Kind:    "MemberCluster",
	}

	MCV1Alpha1GVK = schema.GroupVersionKind{
		Group:   fleetv1alpha1.GroupVersion.Group,
		Version: fleetv1alpha1.GroupVersion.Version,
		Kind:    fleetv1alpha1.MemberClusterKind,
	}

	MCV1Alpha1GVR = schema.GroupVersionResource{
		Group:    fleetv1alpha1.GroupVersion.Group,
		Version:  fleetv1alpha1.GroupVersion.Version,
		Resource: fleetv1alpha1.MemberClusterResource,
	}

	MCMetaGVK = metav1.GroupVersionKind{
		Group:   clusterv1beta1.GroupVersion.Group,
		Version: clusterv1beta1.GroupVersion.Version,
		Kind:    "MemberCluster",
	}

	NamespaceMetaGVK = metav1.GroupVersionKind{
		Group:   corev1.GroupName,
		Version: corev1.SchemeGroupVersion.Version,
		Kind:    NamespaceKind,
	}

	NamespaceGVK = schema.GroupVersionKind{
		Group:   corev1.GroupName,
		Version: corev1.SchemeGroupVersion.Version,
		Kind:    NamespaceKind,
	}

	NamespaceGVR = schema.GroupVersionResource{
		Group:    corev1.GroupName,
		Version:  corev1.SchemeGroupVersion.Version,
		Resource: "namespaces",
	}

	PodMetaGVK = metav1.GroupVersionKind{
		Group:   corev1.SchemeGroupVersion.Group,
		Version: corev1.SchemeGroupVersion.Version,
		Kind:    "Pod",
	}

	RoleMetaGVK = metav1.GroupVersionKind{
		Group:   rbacv1.SchemeGroupVersion.Group,
		Version: rbacv1.SchemeGroupVersion.Version,
		Kind:    "Role",
	}

	RoleBindingMetaGVK = metav1.GroupVersionKind{
		Group:   rbacv1.SchemeGroupVersion.Group,
		Version: rbacv1.SchemeGroupVersion.Version,
		Kind:    "RoleBinding",
	}

	ServiceGVR = schema.GroupVersionResource{
		Group:    corev1.GroupName,
		Version:  corev1.SchemeGroupVersion.Version,
		Resource: "services",
	}

	WorkV1Alpha1MetaGVK = metav1.GroupVersionKind{
		Group:   workv1alpha1.GroupVersion.Group,
		Version: workv1alpha1.GroupVersion.Version,
		Kind:    "Work",
	}

	WorkV1Alpha1GVK = schema.GroupVersionKind{
		Group:   workv1alpha1.GroupVersion.Group,
		Version: workv1alpha1.GroupVersion.Version,
		Kind:    workv1alpha1.WorkKind,
	}

	WorkV1Alpha1GVR = schema.GroupVersionResource{
		Group:    workv1alpha1.GroupVersion.Group,
		Version:  workv1alpha1.GroupVersion.Version,
		Resource: workv1alpha1.WorkResource,
	}

	WorkMetaGVK = metav1.GroupVersionKind{
		Group:   placementv1beta1.GroupVersion.Group,
		Version: placementv1beta1.GroupVersion.Version,
		Kind:    "Work",
	}

	ClusterResourceOverrideSnapshotKind = schema.GroupVersionKind{
		Group:   placementv1alpha1.GroupVersion.Group,
		Version: placementv1alpha1.GroupVersion.Version,
		Kind:    placementv1alpha1.ClusterResourceOverrideSnapshotKind,
	}

	ResourceOverrideSnapshotKind = schema.GroupVersionKind{
		Group:   placementv1alpha1.GroupVersion.Group,
		Version: placementv1alpha1.GroupVersion.Version,
		Kind:    placementv1alpha1.ResourceOverrideSnapshotKind,
	}

	DeploymentGVR = schema.GroupVersionResource{
		Group:    appv1.GroupName,
		Version:  appv1.SchemeGroupVersion.Version,
		Resource: "deployments",
	}

	DeploymentGVK = schema.GroupVersionKind{
		Group:   appv1.GroupName,
		Version: appv1.SchemeGroupVersion.Version,
		Kind:    DeploymentKind,
	}

	DaemonSettGVR = schema.GroupVersionResource{
		Group:    appv1.GroupName,
		Version:  appv1.SchemeGroupVersion.Version,
		Resource: "daemonsets",
	}

	StatefulSettGVR = schema.GroupVersionResource{
		Group:    appv1.GroupName,
		Version:  appv1.SchemeGroupVersion.Version,
		Resource: "statefulsets",
	}

	JobGVR = schema.GroupVersionResource{
		Group:    batchv1.GroupName,
		Version:  batchv1.SchemeGroupVersion.Version,
		Resource: "jobs",
	}

	ConfigMapGVR = schema.GroupVersionResource{
		Group:    corev1.GroupName,
		Version:  corev1.SchemeGroupVersion.Version,
		Resource: string(corev1.ResourceConfigMaps),
	}

	SecretGVR = schema.GroupVersionResource{
		Group:    corev1.GroupName,
		Version:  corev1.SchemeGroupVersion.Version,
		Resource: string(corev1.ResourceSecrets),
	}

	RoleGVR = schema.GroupVersionResource{
		Group:    rbacv1.GroupName,
		Version:  rbacv1.SchemeGroupVersion.Version,
		Resource: "roles",
	}

	ClusterRoleGVR = schema.GroupVersionResource{
		Group:    rbacv1.GroupName,
		Version:  rbacv1.SchemeGroupVersion.Version,
		Resource: "clusterroles",
	}

	ClusterRoleGVK = schema.GroupVersionKind{
		Group:   rbacv1.GroupName,
		Version: rbacv1.SchemeGroupVersion.Version,
		Kind:    "ClusterRole",
	}

	RoleBindingGVR = schema.GroupVersionResource{
		Group:    rbacv1.GroupName,
		Version:  rbacv1.SchemeGroupVersion.Version,
		Resource: "rolebindings",
	}

	ClusterRoleBindingGVR = schema.GroupVersionResource{
		Group:    rbacv1.GroupName,
		Version:  rbacv1.SchemeGroupVersion.Version,
		Resource: "clusterrolebindings",
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
	case corev1.SchemeGroupVersion.WithKind(ConfigMapKind):
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

// GenerateGroupString generates a string which prints groups in which a user belongs,
// it compresses the string to just display three groups if length of groups is more than 10.
func GenerateGroupString(groups []string) string {
	var groupString string
	if len(groups) > 10 {
		groupString = fmt.Sprintf(moreGroupsStringFormat, groups[0], groups[1], groups[2])
	} else {
		groupString = fmt.Sprintf(lessGroupsStringFormat, groups)
	}
	return groupString
}

// LessFuncResourceIdentifier is a less function for sorting resource identifiers
var LessFuncResourceIdentifier = func(a, b placementv1beta1.ResourceIdentifier) bool {
	aStr := fmt.Sprintf(ResourceIdentifierStringFormat, a.Group, a.Version, a.Kind, a.Namespace, a.Name)
	bStr := fmt.Sprintf(ResourceIdentifierStringFormat, b.Group, b.Version, b.Kind, b.Namespace, b.Name)
	return aStr < bStr
}

// LessFuncFailedResourcePlacements is a less function for sorting failed resource placements
var LessFuncFailedResourcePlacements = func(a, b placementv1beta1.FailedResourcePlacement) bool {
	var aStr, bStr string
	if a.Envelope != nil {
		aStr = fmt.Sprintf(ResourceIdentifierWithEnvelopeIdentifierStringFormat, a.Group, a.Version, a.Kind, a.Namespace, a.Name, a.Envelope.Type, a.Envelope.Namespace, a.Envelope.Name)
	} else {
		aStr = fmt.Sprintf(ResourceIdentifierStringFormat, a.Group, a.Version, a.Kind, a.Namespace, a.Name)
	}
	if b.Envelope != nil {
		bStr = fmt.Sprintf(ResourceIdentifierWithEnvelopeIdentifierStringFormat, b.Group, b.Version, b.Kind, b.Namespace, b.Name, b.Envelope.Type, b.Envelope.Namespace, b.Envelope.Name)
	} else {
		bStr = fmt.Sprintf(ResourceIdentifierStringFormat, b.Group, b.Version, b.Kind, b.Namespace, b.Name)

	}
	return aStr < bStr
}

func IsFailedResourcePlacementsEqual(oldFailedResourcePlacements, newFailedResourcePlacements []placementv1beta1.FailedResourcePlacement) bool {
	if len(oldFailedResourcePlacements) != len(newFailedResourcePlacements) {
		return false
	}
	sort.Slice(oldFailedResourcePlacements, func(i, j int) bool {
		return LessFuncFailedResourcePlacements(oldFailedResourcePlacements[i], oldFailedResourcePlacements[j])
	})
	sort.Slice(newFailedResourcePlacements, func(i, j int) bool {
		return LessFuncFailedResourcePlacements(newFailedResourcePlacements[i], newFailedResourcePlacements[j])
	})
	for i := range oldFailedResourcePlacements {
		oldFailedResourcePlacement := oldFailedResourcePlacements[i]
		newFailedResourcePlacement := newFailedResourcePlacements[i]
		if !equality.Semantic.DeepEqual(oldFailedResourcePlacement.ResourceIdentifier, newFailedResourcePlacement.ResourceIdentifier) {
			return false
		}
		if !condition.EqualCondition(&oldFailedResourcePlacement.Condition, &newFailedResourcePlacement.Condition) {
			return false
		}
	}
	return true
}

// LessFuncDriftedResourcePlacements is a less function for sorting drifted resource placements
var LessFuncDriftedResourcePlacements = func(a, b placementv1beta1.DriftedResourcePlacement) bool {
	var aStr, bStr string
	if a.Envelope != nil {
		aStr = fmt.Sprintf(ResourceIdentifierWithEnvelopeIdentifierStringFormat, a.Group, a.Version, a.Kind, a.Namespace, a.Name, a.Envelope.Type, a.Envelope.Namespace, a.Envelope.Name)
	} else {
		aStr = fmt.Sprintf(ResourceIdentifierStringFormat, a.Group, a.Version, a.Kind, a.Namespace, a.Name)
	}
	if b.Envelope != nil {
		bStr = fmt.Sprintf(ResourceIdentifierWithEnvelopeIdentifierStringFormat, b.Group, b.Version, b.Kind, b.Namespace, b.Name, b.Envelope.Type, b.Envelope.Namespace, b.Envelope.Name)
	} else {
		bStr = fmt.Sprintf(ResourceIdentifierStringFormat, b.Group, b.Version, b.Kind, b.Namespace, b.Name)

	}
	return aStr < bStr
}

func IsDriftedResourcePlacementsEqual(oldDriftedResourcePlacements, newDriftedResourcePlacements []placementv1beta1.DriftedResourcePlacement) bool {
	if len(oldDriftedResourcePlacements) != len(newDriftedResourcePlacements) {
		return false
	}
	sort.Slice(oldDriftedResourcePlacements, func(i, j int) bool {
		return LessFuncDriftedResourcePlacements(oldDriftedResourcePlacements[i], oldDriftedResourcePlacements[j])
	})
	sort.Slice(newDriftedResourcePlacements, func(i, j int) bool {
		return LessFuncDriftedResourcePlacements(newDriftedResourcePlacements[i], newDriftedResourcePlacements[j])
	})
	for i := range oldDriftedResourcePlacements {
		oldDriftedResourcePlacement := oldDriftedResourcePlacements[i]
		newDriftedResourcePlacement := newDriftedResourcePlacements[i]
		if !equality.Semantic.DeepEqual(oldDriftedResourcePlacement.ResourceIdentifier, newDriftedResourcePlacement.ResourceIdentifier) {
			return false
		}
		if !equality.Semantic.DeepEqual(oldDriftedResourcePlacement.ObservationTime, newDriftedResourcePlacement.ObservationTime) {
			return false
		}
		if oldDriftedResourcePlacement.TargetClusterObservedGeneration != newDriftedResourcePlacement.TargetClusterObservedGeneration {
			return false
		}
		if !equality.Semantic.DeepEqual(oldDriftedResourcePlacement.FirstDriftedObservedTime, newDriftedResourcePlacement.FirstDriftedObservedTime) {
			return false
		}
		for j := range oldDriftedResourcePlacement.ObservedDrifts {
			if !equality.Semantic.DeepEqual(oldDriftedResourcePlacement.ObservedDrifts[j], newDriftedResourcePlacement.ObservedDrifts[j]) {
				return false
			}
		}
	}
	return true
}

// LessFuncDiffedResourcePlacements is a less function for sorting diffed resource placements
var LessFuncDiffedResourcePlacements = func(a, b placementv1beta1.DiffedResourcePlacement) bool {
	var aStr, bStr string
	if a.Envelope != nil {
		aStr = fmt.Sprintf(ResourceIdentifierWithEnvelopeIdentifierStringFormat, a.Group, a.Version, a.Kind, a.Namespace, a.Name, a.Envelope.Type, a.Envelope.Namespace, a.Envelope.Name)
	} else {
		aStr = fmt.Sprintf(ResourceIdentifierStringFormat, a.Group, a.Version, a.Kind, a.Namespace, a.Name)
	}
	if b.Envelope != nil {
		bStr = fmt.Sprintf(ResourceIdentifierWithEnvelopeIdentifierStringFormat, b.Group, b.Version, b.Kind, b.Namespace, b.Name, b.Envelope.Type, b.Envelope.Namespace, b.Envelope.Name)
	} else {
		bStr = fmt.Sprintf(ResourceIdentifierStringFormat, b.Group, b.Version, b.Kind, b.Namespace, b.Name)

	}
	return aStr < bStr
}

func IsDiffedResourcePlacementsEqual(oldDiffedResourcePlacements, newDiffedResourcePlacements []placementv1beta1.DiffedResourcePlacement) bool {
	if len(oldDiffedResourcePlacements) != len(newDiffedResourcePlacements) {
		return false
	}
	sort.Slice(oldDiffedResourcePlacements, func(i, j int) bool {
		return LessFuncDiffedResourcePlacements(oldDiffedResourcePlacements[i], oldDiffedResourcePlacements[j])
	})
	sort.Slice(newDiffedResourcePlacements, func(i, j int) bool {
		return LessFuncDiffedResourcePlacements(newDiffedResourcePlacements[i], newDiffedResourcePlacements[j])
	})
	for i := range oldDiffedResourcePlacements {
		oldDiffedResourcePlacement := oldDiffedResourcePlacements[i]
		newDiffedResourcePlacement := newDiffedResourcePlacements[i]
		if !equality.Semantic.DeepEqual(oldDiffedResourcePlacement.ResourceIdentifier, newDiffedResourcePlacement.ResourceIdentifier) {
			return false
		}
		if !equality.Semantic.DeepEqual(oldDiffedResourcePlacement.ObservationTime, newDiffedResourcePlacement.ObservationTime) {
			return false
		}
		if oldDiffedResourcePlacement.TargetClusterObservedGeneration != newDiffedResourcePlacement.TargetClusterObservedGeneration {
			return false
		}
		if !equality.Semantic.DeepEqual(oldDiffedResourcePlacement.FirstDiffedObservedTime, newDiffedResourcePlacement.FirstDiffedObservedTime) {
			return false
		}
		for j := range oldDiffedResourcePlacement.ObservedDiffs {
			if !equality.Semantic.DeepEqual(oldDiffedResourcePlacement.ObservedDiffs[j], newDiffedResourcePlacement.ObservedDiffs[j]) {
				return false
			}
		}
	}
	return true
}

// IsFleetAnnotationPresent returns true if a key with fleet prefix is present in the annotations map.
func IsFleetAnnotationPresent(annotations map[string]string) bool {
	for k := range annotations {
		if strings.HasPrefix(k, FleetAnnotationPrefix) {
			return true
		}
	}
	return false
}
