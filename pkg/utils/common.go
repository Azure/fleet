/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package utils

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"math/big"
	"strings"
	"time"

	"github.com/pkg/errors"
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
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	workv1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
)

const (
	FleetSystemNamespace = "fleet-system"

	NamespaceNameFormat = "fleet-member-%s"

	RoleNameFormat = "fleet-role-%s"

	RoleBindingNameFormat = "fleet-rolebinding-%s"

	PlacementFieldManagerName = "work-api-agent"

	WorkNameFormat = "work-%s"
)

const (
	// LabelFleetObj is a label key indicate the resource is created by the fleet
	LabelFleetObj      = "kubernetes.azure.com/managed-by"
	LabelFleetObjValue = "fleet"

	// LabelWorkPlacementName is used to indicate which placement created the work.
	// This label aims to enable different work objects to be managed by different placement.
	LabelWorkPlacementName = "work.fleet.azure.com/placement-name"

	// AnnotationPlacementList is used to store all the placements that select this resource.
	// This annotation aims to enable identify the placements that need to notified when a resource
	// is changed.
	AnnotationPlacementList = "work.fleet.azure.com/placement-list"
	PlacementListSep        = ";"

	// PlacementFinalizer is used to make sure that we handle the deleting of an already placed resource
	PlacementFinalizer = "work.fleet.azure.com/placement-protection"
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
		APIGroups: []string{"networking.fleet.azure.com"},
		Resources: []string{"*"},
	}
	// LeaseRule Leases permissions are required for leader election of hub controller manager in member cluster.
	LeaseRule = rbacv1.PolicyRule{
		Verbs:     []string{"create", "get", "list", "update"},
		APIGroups: []string{"coordination.k8s.io"},
		Resources: []string{"leases"},
	}
)

var (
	ClusterResourcePlacementGVR = schema.GroupVersionResource{
		Group:    fleetv1alpha1.GroupVersion.Group,
		Version:  fleetv1alpha1.GroupVersion.Version,
		Resource: fleetv1alpha1.ClusterResourcePlacementResource,
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

	WorkGVK = schema.GroupVersionKind{
		Group:   workv1alpha1.GroupVersion.Group,
		Version: workv1alpha1.GroupVersion.Version,
		Kind:    workv1alpha1.WorkKind,
	}

	WorkGVR = schema.GroupVersionResource{
		Group:   workv1alpha1.GroupVersion.Group,
		Version: workv1alpha1.GroupVersion.Version,
		// TODO: use the const after it's checked in
		Resource: "works",
	}

	ServiceGVR = schema.GroupVersionResource{
		Group:    corev1.GroupName,
		Version:  corev1.SchemeGroupVersion.Version,
		Resource: "services",
	}
)

func RandSecureInt(limit int64) int64 {
	nBig, err := rand.Int(rand.Reader, big.NewInt(limit))
	if err != nil {
		log.Println(err)
	}
	return nBig.Int64()
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

// ContextForChannel derives a child context from a parent channel.
//
// The derived context's Done channel is closed when the returned cancel function
// is called or when the parent channel is closed, whichever happens first.
//
// Note the caller must *always* call the CancelFunc, otherwise resources may be leaked.
func ContextForChannel(parentCh <-chan struct{}) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		select {
		case <-parentCh:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
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
func ShouldPropagateObj(informerManager InformerManager, uObj *unstructured.Unstructured) (bool, error) {
	// TODO:  add more special handling for different resource kind
	switch uObj.GroupVersionKind() {
	case corev1.SchemeGroupVersion.WithKind("Secret"):
		// The secret, with type 'kubernetes.io/service-account-token', is created along with `ServiceAccount` should be
		// prevented from propagating.
		var secret corev1.Secret
		err := runtime.DefaultUnstructuredConverter.FromUnstructured(uObj.Object, &secret)
		if err != nil {
			return false, errors.Wrap(err, fmt.Sprintf(
				"failed to convert a secret object %s in namespace %s", uObj.GetName(), uObj.GetNamespace()))
		}
		if secret.Type == corev1.SecretTypeServiceAccountToken {
			return false, nil
		}
	case corev1.SchemeGroupVersion.WithKind("Endpoints"):
		// we assume that all endpoints with the same name of a service is created by the service controller
		var endpoint corev1.Endpoints
		err := runtime.DefaultUnstructuredConverter.FromUnstructured(uObj.Object, &endpoint)
		if err != nil {
			return false, errors.Wrap(err, fmt.Sprintf(
				"failed to convert an endpoint object %s in namespace %s", uObj.GetName(), uObj.GetNamespace()))
		}
		_, err = informerManager.Lister(ServiceGVR).ByNamespace(endpoint.GetNamespace()).Get(endpoint.GetName())
		if err != nil {
			if apierrors.IsNotFound(err) {
				// there is no service of the same name as the end point,
				// we assume that this endpoint is created by the user
				return true, nil
			}
			return false, errors.Wrap(err, fmt.Sprintf("failed to get the serviceo %s in namespace %s", uObj.GetName(), uObj.GetNamespace()))
		}
		// we find a service of the same name as the endpoint, we assume it's created by the service
		return false, nil
	case discoveryv1.SchemeGroupVersion.WithKind("EndpointSlice"):
		// all EndpointSlice created by the EndpointSlice controller has a managed by label
		var endpointSlice discoveryv1.EndpointSlice
		err := runtime.DefaultUnstructuredConverter.FromUnstructured(uObj.Object, &endpointSlice)
		if err != nil {
			return false, errors.Wrap(err, fmt.Sprintf(
				"failed to convert an endpointSlice object %s in namespace %s", uObj.GetName(), uObj.GetNamespace()))
		}
		if _, exist := endpointSlice.GetLabels()[discoveryv1.LabelManagedBy]; exist {
			// do not propagate hub cluster generated endpoint slice
			return false, nil
		}
	}
	return true, nil
}

// FindSelectedPlacements finds the placements which have selected this resource already
func FindSelectedPlacements(uObj *unstructured.Unstructured) ([]string, bool) {
	// see if this resource has been selected before
	placementList, exist := uObj.GetAnnotations()[AnnotationPlacementList]
	if !exist {
		klog.V(5).InfoS("Object is not selected by any placement", "resource", uObj.GetName())
		return nil, false
	}
	selectedPlacements := strings.Split(placementList, PlacementListSep)

	if !uObj.GetDeletionTimestamp().IsZero() {
		klog.V(3).InfoS("Object selected by placements is being deleted", "resource", uObj.GetName())
		if !controllerutil.ContainsFinalizer(uObj, PlacementFinalizer) {
			klog.Errorf("selected resource %s is being deleted without the placement finalizer", uObj.GetName())
		}
		// remove the finalizer and the placement annotation
		controllerutil.RemoveFinalizer(uObj, PlacementFinalizer)
		a := uObj.GetAnnotations()
		delete(a, AnnotationPlacementList)
		uObj.SetAnnotations(a)
		return selectedPlacements, true
	}
	return selectedPlacements, false
}

// AddPlacement accepts an unstructured and adds the newPlacement to its annotation if not present.
// It will also add the PlacementFinalizer no matter what
func AddPlacement(uObj *unstructured.Unstructured, newPlacement string) {
	anno := uObj.GetAnnotations()
	controllerutil.AddFinalizer(uObj, PlacementFinalizer)
	if len(anno) == 0 {
		klog.V(5).InfoS("the object is first selected by a placement", "resource", uObj.GetName(),
			"gvk", uObj.GroupVersionKind(), "placement", newPlacement)
		uObj.SetAnnotations(map[string]string{AnnotationPlacementList: newPlacement})
		return
	}
	placementList, exist := anno[AnnotationPlacementList]
	if !exist {
		anno[AnnotationPlacementList] = newPlacement
		uObj.SetAnnotations(anno)
		return
	}
	selectedPlacements := strings.Split(placementList, PlacementListSep)
	for _, e := range selectedPlacements {
		if e == newPlacement {
			return
		}
	}
	delete(anno, AnnotationPlacementList)
	anno[AnnotationPlacementList] = strings.Join(append(selectedPlacements, newPlacement), PlacementListSep)
	uObj.SetAnnotations(anno)
}

// RemovePlacement accepts an unstructured and removes the removed placement from its annotation if present.
// it will remove the PlacementFinalizer if there are no more placement select this
func RemovePlacement(uObj *unstructured.Unstructured, removedPlacement string) {
	anno := uObj.GetAnnotations()
	placementList, exist := anno[AnnotationPlacementList]
	if !exist {
		return
	}
	existingPlacements := strings.Split(placementList, PlacementListSep)
	for i := 0; i < len(existingPlacements); i++ {
		if existingPlacements[i] == removedPlacement {
			existingPlacements = append(existingPlacements[:i], existingPlacements[i+1:]...)
			i--
		}
	}
	delete(anno, AnnotationPlacementList)
	if len(existingPlacements) > 0 {
		anno[AnnotationPlacementList] = strings.Join(existingPlacements, PlacementListSep)
	} else {
		klog.V(5).InfoS("the object is no longer selected by any placement", "resource", uObj.GetName(),
			"gvk", uObj.GroupVersionKind(), "placement", removedPlacement)
		controllerutil.RemoveFinalizer(uObj, PlacementFinalizer)
	}
	uObj.SetAnnotations(anno)
}
