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
	"time"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
)

const (
	FleetSystemNamespace  = "fleet-system"
	NamespaceNameFormat   = "fleet-member-%s"
	RoleNameFormat        = "fleet-role-%s"
	RoleBindingNameFormat = "fleet-rolebinding-%s"
)

// Condition types.
const (
	// ConditionTypeReady resources are believed to be ready to handle work.
	ConditionTypeReady string = "Ready"

	// ConditionTypeSynced resources are believed to be in sync with the
	// Kubernetes resources that manage their lifecycle.
	ConditionTypeSynced string = "Synced"
)

// Reasons a resource is or is not synced.
const (
	ReasonReconcileSuccess string = "ReconcileSuccess"
	ReasonReconcileError   string = "ReconcileError"
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
	FleetNetworkRule = rbacv1.PolicyRule{
		Verbs:     []string{"*"},
		APIGroups: []string{"networking.fleet.azure.com"},
		Resources: []string{"*"},
	}
)

// ReconcileErrorCondition returns a condition indicating that we encountered an
// error while reconciling the resource.
func ReconcileErrorCondition(err error) metav1.Condition {
	return metav1.Condition{
		Type:    ConditionTypeSynced,
		Status:  metav1.ConditionFalse,
		Reason:  ReasonReconcileError,
		Message: err.Error(),
	}
}

// ReconcileSuccessCondition returns a condition indicating that we successfully reconciled the resource
func ReconcileSuccessCondition() metav1.Condition {
	return metav1.Condition{
		Type:   ConditionTypeSynced,
		Status: metav1.ConditionTrue,
		Reason: ReasonReconcileSuccess,
	}
}

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
