/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package e2e

import (
	"context"
	"embed"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	workapi "sigs.k8s.io/work-api/pkg/apis/v1alpha1"

	"go.goms.io/fleet/test/e2e/framework"
)

var (
	//go:embed manifests
	testManifestFiles embed.FS
)

type manifestDetails struct {
	Manifest workapi.Manifest
	GVK      *schema.GroupVersionKind
	GVR      *schema.GroupVersionResource
	ObjMeta  metav1.ObjectMeta
}

func createWorkObj(workName string, workNamespace string, manifestDetails []manifestDetails) *workapi.Work {
	work := &workapi.Work{
		ObjectMeta: metav1.ObjectMeta{
			Name:      workName,
			Namespace: workNamespace,
		},
	}

	for _, detail := range manifestDetails {
		work.Spec.Workload.Manifests = append(work.Spec.Workload.Manifests, detail.Manifest)
	}

	return work
}

func createWork(work *workapi.Work, hubCluster *framework.Cluster) error {
	return hubCluster.KubeClient.Create(context.Background(), work)
}

func decodeUnstructured(manifest workapi.Manifest) (*unstructured.Unstructured, error) {
	unstructuredObj := &unstructured.Unstructured{}
	err := unstructuredObj.UnmarshalJSON(manifest.Raw)

	return unstructuredObj, err
}

func deleteWorkResource(work *workapi.Work, hubCluster *framework.Cluster) error {
	return hubCluster.KubeClient.Delete(context.Background(), work)
}

func retrieveAppliedWork(appliedWorkName string, memberCluster *framework.Cluster) (*workapi.AppliedWork, error) {
	retrievedAppliedWork := workapi.AppliedWork{}
	err := memberCluster.KubeClient.Get(context.Background(), types.NamespacedName{Name: appliedWorkName}, &retrievedAppliedWork)
	if err != nil {
		return &retrievedAppliedWork, err
	}

	return &retrievedAppliedWork, nil
}

func retrieveWork(workNamespace string, workName string, hubCluster *framework.Cluster) (*workapi.Work, error) {
	workRetrieved := workapi.Work{}
	err := hubCluster.KubeClient.Get(context.Background(), types.NamespacedName{Namespace: workNamespace, Name: workName}, &workRetrieved)
	if err != nil {
		println("err still exists")
		println(err.Error())
		return nil, err
	}
	return &workRetrieved, nil
}

func updateWork(work *workapi.Work, hubCluster *framework.Cluster) (*workapi.Work, error) {
	err := hubCluster.KubeClient.Update(context.Background(), work)
	if err != nil {
		return nil, err
	}

	updatedWork, err := retrieveWork(work.Namespace, work.Name, hubCluster)
	if err != nil {
		return nil, err
	}
	return updatedWork, err
}

func getWorkName(length int) string {
	return "work" + rand.String(length)
}
