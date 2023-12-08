package util

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/klog/v2"
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
func getFleetSize(crp v1beta1.ClusterResourcePlacement, clusterNames ClusterNames) (string, ClusterNames, error) {
	for _, status := range crp.Status.PlacementStatuses {
		if err := clusterNames.Set(status.ClusterName); err != nil {
			klog.ErrorS(err, "Failed to set clusterNames.")
			return "", nil, err
		}
	}
	return strconv.Itoa(len(clusterNames)), clusterNames, nil
}
func createCRP(crp *v1beta1.ClusterResourcePlacement, crpFile string, crpName string, nsName string) error {
	obj, err := readObjFromFile(fmt.Sprintf("hack/loadtest/%s", crpFile), nsName)
	if err != nil {
		klog.ErrorS(err, "Failed to read object from file.")
		return err
	}

	err = runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, crp)
	if err != nil {
		return err
	}

	crp.Name = crpName
	crp.Spec.ResourceSelectors = []v1beta1.ClusterResourceSelector{
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
	}
	return nil
}
