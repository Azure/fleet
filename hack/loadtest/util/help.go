package util

import (
	"context"
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ClusterNames []string

func (i *ClusterNames) String() string {
	return "member cluster names: " + strings.Join(*i, ",")
}

func (i *ClusterNames) Set(value string) error {
	*i = append(*i, value)
	return nil
}

// applyObjectFromManifest returns a runtime object decoded from the file
func applyObjectFromManifest(ctx context.Context, hubClient client.Client, namespaceName string, relativeFilePath string) error {
	// Read files, create manifest
	rawByte, err := os.ReadFile(relativeFilePath)
	if err != nil {
		return err
	}
	json, err := yaml.ToJSON(rawByte)
	if err != nil {
		return err
	}
	obj := &unstructured.Unstructured{}
	err = obj.UnmarshalJSON(json)
	if err != nil {
		return err
	}
	obj.SetNamespace(namespaceName)
	return hubClient.Create(ctx, obj)
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
	if err := applyObjectFromManifest(ctx, hubClient, namespaceName, "hack/loadtest/manifests/test-rolebinding.yaml"); err != nil {
		return err
	}
	return nil
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
