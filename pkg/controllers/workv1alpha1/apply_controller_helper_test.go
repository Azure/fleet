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

package workv1alpha1

import (
	"context"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	workv1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"
)

// createWorkWithManifest creates a work given a manifest
func createWorkWithManifest(workNamespace string, manifest runtime.Object) *workv1alpha1.Work {
	manifestCopy := manifest.DeepCopyObject()
	newWork := workv1alpha1.Work{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "work-" + utilrand.String(5),
			Namespace: workNamespace,
		},
		Spec: workv1alpha1.WorkSpec{
			Workload: workv1alpha1.WorkloadTemplate{
				Manifests: []workv1alpha1.Manifest{
					{
						RawExtension: runtime.RawExtension{Object: manifestCopy},
					},
				},
			},
		},
	}
	return &newWork
}

// verifyAppliedConfigMap verifies that the applied CM is the same as the CM we want to apply
func verifyAppliedConfigMap(cm *corev1.ConfigMap) *corev1.ConfigMap {
	var appliedCM corev1.ConfigMap
	Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: cm.GetName(), Namespace: cm.GetNamespace()}, &appliedCM)).Should(Succeed())

	By("Check the config map label")
	Expect(cmp.Diff(appliedCM.Labels, cm.Labels)).Should(BeEmpty())

	By("Check the config map annotation value")
	Expect(len(appliedCM.Annotations)).Should(Equal(len(cm.Annotations) + 2)) // we added 2 more annotations
	for key := range cm.Annotations {
		Expect(appliedCM.Annotations[key]).Should(Equal(cm.Annotations[key]))
	}
	Expect(appliedCM.Annotations[manifestHashAnnotation]).ShouldNot(BeEmpty())
	Expect(appliedCM.Annotations[lastAppliedConfigAnnotation]).ShouldNot(BeEmpty())

	By("Check the config map data")
	Expect(cmp.Diff(appliedCM.Data, cm.Data)).Should(BeEmpty())
	return &appliedCM
}

// waitForWorkToApply waits for a work to be applied
func waitForWorkToApply(workName, workNS string) *workv1alpha1.Work {
	var resultWork workv1alpha1.Work
	Eventually(func() bool {
		err := k8sClient.Get(context.Background(), types.NamespacedName{Name: workName, Namespace: workNS}, &resultWork)
		if err != nil {
			return false
		}
		applyCond := meta.FindStatusCondition(resultWork.Status.Conditions, ConditionTypeApplied)
		if applyCond == nil || applyCond.Status != metav1.ConditionTrue || applyCond.ObservedGeneration != resultWork.Generation {
			return false
		}
		for _, manifestCondition := range resultWork.Status.ManifestConditions {
			if !meta.IsStatusConditionTrue(manifestCondition.Conditions, ConditionTypeApplied) {
				return false
			}
		}
		return true
	}, timeout, interval).Should(BeTrue())
	return &resultWork
}

// waitForWorkToBeHandled waits for a work to have a finalizer
func waitForWorkToBeHandled(workName, workNS string) *workv1alpha1.Work {
	var resultWork workv1alpha1.Work
	Eventually(func() bool {
		err := k8sClient.Get(context.Background(), types.NamespacedName{Name: workName, Namespace: workNS}, &resultWork)
		if err != nil {
			return false
		}
		return controllerutil.ContainsFinalizer(&resultWork, workFinalizer)
	}, timeout, interval).Should(BeTrue())
	return &resultWork
}
