package util

import (
	"context"
	"fmt"
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/klog/v2"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"go.goms.io/fleet/apis/placement/v1beta1"
)

type ClusterNames []string

func (i *ClusterNames) String() string {
	return "member cluster names: " + strings.Join(*i, ",")
}

func (i *ClusterNames) Set(value string) error {
	*i = append(*i, value)
	return nil
}

// applyObjectFromManifest apply an object from manifest
func applyObjectFromManifest(ctx context.Context, hubClient client.Client, namespaceName string, relativeFilePath string) error {
	obj, err := readObjFromFile(relativeFilePath, namespaceName)
	if err != nil {
		return err
	}
	return hubClient.Create(ctx, obj)
}

// deleteObjectFromManifest delete an object from manifest
func deleteObjectFromManifest(ctx context.Context, hubClient client.Client, namespaceName string, relativeFilePath string) error {
	obj, err := readObjFromFile(relativeFilePath, namespaceName)
	if err != nil {
		return err
	}
	return hubClient.Delete(ctx, obj)
}

// readObjFromFile returns a runtime object decoded from the file
func readObjFromFile(relativeFilePath string, namespaceName string) (*unstructured.Unstructured, error) {
	// Read files, create manifest
	rawByte, err := os.ReadFile(relativeFilePath)
	if err != nil {
		return nil, err
	}
	json, err := yaml.ToJSON(rawByte)
	if err != nil {
		return nil, err
	}
	obj := &unstructured.Unstructured{}
	err = obj.UnmarshalJSON(json)
	if err != nil {
		return nil, err
	}
	if len(namespaceName) != 0 {
		obj.SetNamespace(namespaceName)
	}
	return obj, nil
}

func ApplyClusterScopeManifests(ctx context.Context, hubClient client.Client) error {
	if err := applyObjectFromManifest(ctx, hubClient, "", "hack/loadtest/manifests/test_clonesets_crd.yaml"); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return err
		}
	}
	if err := applyObjectFromManifest(ctx, hubClient, "", "hack/loadtest/manifests/test_clusterrole.yaml"); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return err
		}
	}
	return nil
}

// applyTestManifests creates the test manifests in the hub cluster under a namespace
func applyTestManifests(ctx context.Context, hubClient client.Client, namespaceName string) error {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespaceName,
			Labels: map[string]string{
				labelKey: namespaceName,
			},
		},
	}
	if err := hubClient.Create(ctx, ns); err != nil {
		return err
	}
	if err := applyObjectFromManifest(ctx, hubClient, namespaceName, "hack/loadtest/manifests/test_pdb.yaml"); err != nil {
		return err
	}
	if err := applyObjectFromManifest(ctx, hubClient, namespaceName, "hack/loadtest/manifests/test-configmap.yaml"); err != nil {
		return err
	}
	if err := applyObjectFromManifest(ctx, hubClient, namespaceName, "hack/loadtest/manifests/test-secret.yaml"); err != nil {
		return err
	}
	if err := applyObjectFromManifest(ctx, hubClient, namespaceName, "hack/loadtest/manifests/test-service.yaml"); err != nil {
		return err
	}
	if err := applyObjectFromManifest(ctx, hubClient, namespaceName, "hack/loadtest/manifests/test-cloneset.yaml"); err != nil {
		return err
	}
	if err := applyObjectFromManifest(ctx, hubClient, namespaceName, "hack/loadtest/manifests/test-configmap-2.yaml"); err != nil {
		return err
	}
	if err := applyObjectFromManifest(ctx, hubClient, namespaceName, "hack/loadtest/manifests/test-role.yaml"); err != nil {
		return err
	}
	return applyObjectFromManifest(ctx, hubClient, namespaceName, "hack/loadtest/manifests/test-rolebinding.yaml")
}

// deleteTestManifests deletes the test manifests in the hub cluster under a namespace
func deleteTestManifests(ctx context.Context, hubClient client.Client, namespaceName string) error {
	if err := deleteObjectFromManifest(ctx, hubClient, namespaceName, "hack/loadtest/manifests/test_pdb.yaml"); err != nil {
		return err
	}
	if err := deleteObjectFromManifest(ctx, hubClient, namespaceName, "hack/loadtest/manifests/test-configmap.yaml"); err != nil {
		return err
	}
	if err := deleteObjectFromManifest(ctx, hubClient, namespaceName, "hack/loadtest/manifests/test-secret.yaml"); err != nil {
		return err
	}
	if err := deleteObjectFromManifest(ctx, hubClient, namespaceName, "hack/loadtest/manifests/test-service.yaml"); err != nil {
		return err
	}
	if err := deleteObjectFromManifest(ctx, hubClient, namespaceName, "hack/loadtest/manifests/test-cloneset.yaml"); err != nil {
		return err
	}
	if err := deleteObjectFromManifest(ctx, hubClient, namespaceName, "hack/loadtest/manifests/test-configmap-2.yaml"); err != nil {
		return err
	}
	if err := deleteObjectFromManifest(ctx, hubClient, namespaceName, "hack/loadtest/manifests/test-role.yaml"); err != nil {
		return err
	}
	return deleteObjectFromManifest(ctx, hubClient, namespaceName, "hack/loadtest/manifests/test-rolebinding.yaml")
}

func deleteNamespace(ctx context.Context, hubClient client.Client, namespaceName string) error {
	klog.InfoS("delete the namespace", "namespace", namespaceName)
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespaceName,
		},
	}
	return hubClient.Delete(ctx, ns)
}

func CleanupAll(hubClient client.Client) error {
	var crps v1beta1.ClusterResourcePlacementList
	if err := hubClient.List(context.Background(), &crps); err != nil {
		klog.ErrorS(err, "failed to list namespace")
		return err
	}
	for index, crp := range crps.Items {
		if err := hubClient.Delete(context.Background(), &crps.Items[index]); err != nil {
			klog.ErrorS(err, "failed to delete crp", "crp", crp.Name)
		}
	}

	var namespaces corev1.NamespaceList
	if err := hubClient.List(context.Background(), &namespaces); err != nil {
		klog.ErrorS(err, "failed to list namespace")
		return err
	}
	for index, ns := range namespaces.Items {
		if strings.HasPrefix(ns.Name, nsPrefix) {
			if err := hubClient.Delete(context.Background(), &namespaces.Items[index]); err != nil {
				klog.ErrorS(err, "failed to delete namespace", "namespace", ns.Name)
			}
		}
	}
	return nil
}

func executeStrategy(crp *v1beta1.ClusterResourcePlacement, obj *unstructured.Unstructured) error {
	strategy, foundType, err := unstructured.NestedString(obj.Object, "spec", "strategy", "type")
	if err != nil {
		klog.ErrorS(err, "Failed to get strategy.")
		return err
	}
	if !foundType {
		return nil
	}
	crp.Spec.Strategy.Type = v1beta1.RolloutStrategyType(strategy)

	rollingUpdate, found, err := unstructured.NestedMap(obj.Object, "spec", "strategy", "rollingUpdate")
	if err != nil {
		klog.ErrorS(err, "Failed to get RollingUpdate.")
		return err
	}
	if !found {
		klog.Info("RollingUpdate not found in file.")
		return nil
	}
	crp.Spec.Strategy.RollingUpdate = &v1beta1.RollingUpdateConfig{}

	if maxUnavailable, found := rollingUpdate["maxUnavailable"].(string); found {
		maxUnvailable := intstr.FromString(maxUnavailable)
		crp.Spec.Strategy.RollingUpdate.MaxUnavailable = &maxUnvailable
	}
	if maxSurge, found := rollingUpdate["maxSurge"].(string); found {
		maxSurge := intstr.FromString(maxSurge)
		crp.Spec.Strategy.RollingUpdate.MaxSurge = &maxSurge
	}
	if unavailablePeriodSeconds, found := rollingUpdate["unavailablePeriodSeconds"].(int64); found {
		crp.Spec.Strategy.RollingUpdate.UnavailablePeriodSeconds = pointer.Int(int(unavailablePeriodSeconds))
	}
	return nil
}

func executeRevisionHistoryLimit(crp *v1beta1.ClusterResourcePlacement, obj *unstructured.Unstructured) error {
	revisionLimit, found, err := unstructured.NestedInt64(obj.Object, "spec", "revisionHistoryLimit")
	if err != nil {
		klog.ErrorS(err, "Failed to get RevisionHistoryLimit.")
		return err
	}
	if !found {
		return nil
	}
	crp.Spec.RevisionHistoryLimit = pointer.Int32(int32(revisionLimit))
	return nil
}

func executePickFixedPolicy(crp *v1beta1.ClusterResourcePlacement, obj *unstructured.Unstructured, clusterNames ClusterNames) (ClusterNames, error) {
	names, found, err := unstructured.NestedStringSlice(obj.Object, "spec", "policy", "clusterNames")
	if err != nil {
		klog.ErrorS(err, "Failed to get cluster names.")
		return clusterNames, err
	}
	if !found {
		klog.Infof("Cluster Names not found in file.")
		return clusterNames, nil
	}

	for _, name := range names {
		if err := clusterNames.Set(name); err != nil {
			klog.ErrorS(err, "Error setting cluster name: %v")
			return clusterNames, err
		}
	}

	crp.Spec.Policy = &v1beta1.PlacementPolicy{
		PlacementType: v1beta1.PickFixedPlacementType,
		ClusterNames:  names,
	}
	return clusterNames, nil
}

func applyCRP(crp *v1beta1.ClusterResourcePlacement, crpFile string, nsName string, clusterNames ClusterNames) error {
	obj, err := readObjFromFile(fmt.Sprintf("hack/loadtest/%s", crpFile), nsName)
	if err != nil {
		klog.ErrorS(err, "Failed to read object from file.")
		return err
	}

	placementType, found, err := unstructured.NestedString(obj.Object, "spec", "policy", "placementType")
	if err != nil {
		klog.ErrorS(err, "Failed to get placement type.")
		return err
	}
	if !found {
		placementType = "default"
	}

	switch placementType {
	case "PickFixed":
		if clusterNames, err = executePickFixedPolicy(crp, obj, clusterNames); err != nil {
			return err
		}
	default:
		for i := 1; i <= 4; i++ {
			if err := clusterNames.Set(fmt.Sprintf("cluster-%d", i)); err != nil {
				klog.ErrorS(err, "Error setting cluster name: %v")
				return err
			}
		}
	}

	if err := executeStrategy(crp, obj); err != nil {
		return err
	}
	
	return executeRevisionHistoryLimit(crp, obj)
}
