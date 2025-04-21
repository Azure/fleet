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

package work

import (
	"context"
	"fmt"

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

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/condition"
)

// createWorkWithManifest creates a work given a manifest
func createWorkWithManifest(workNamespace string, manifest runtime.Object) *fleetv1beta1.Work {
	manifestCopy := manifest.DeepCopyObject()
	newWork := fleetv1beta1.Work{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "work-" + utilrand.String(5),
			Namespace: workNamespace,
		},
		Spec: fleetv1beta1.WorkSpec{
			Workload: fleetv1beta1.WorkloadTemplate{
				Manifests: []fleetv1beta1.Manifest{
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
	Expect(appliedCM.Annotations[fleetv1beta1.ManifestHashAnnotation]).ShouldNot(BeEmpty())
	Expect(appliedCM.Annotations[fleetv1beta1.LastAppliedConfigAnnotation]).ShouldNot(BeEmpty())

	By("Check the config map data")
	Expect(cmp.Diff(appliedCM.Data, cm.Data)).Should(BeEmpty())
	return &appliedCM
}

// waitForWorkToApply waits for a work to be applied
func waitForWorkToApply(workName, workNS string) *fleetv1beta1.Work {
	var resultWork fleetv1beta1.Work
	Eventually(func() bool {
		err := k8sClient.Get(context.Background(), types.NamespacedName{Name: workName, Namespace: workNS}, &resultWork)
		if err != nil {
			return false
		}
		applyCond := meta.FindStatusCondition(resultWork.Status.Conditions, fleetv1beta1.WorkConditionTypeApplied)
		if applyCond == nil || applyCond.Status != metav1.ConditionTrue || applyCond.ObservedGeneration != resultWork.Generation {
			By(fmt.Sprintf("applyCond not true: %v", applyCond))
			return false
		}
		for _, manifestCondition := range resultWork.Status.ManifestConditions {
			if !meta.IsStatusConditionTrue(manifestCondition.Conditions, fleetv1beta1.WorkConditionTypeApplied) {
				By(fmt.Sprintf("manifest applyCond not true %v : %v", manifestCondition.Identifier, manifestCondition.Conditions))
				return false
			}
		}
		return true
	}, timeout, interval).Should(BeTrue())
	return &resultWork
}

// waitForWorkToAvailable waits for a work to have an available condition to be true
func waitForWorkToBeAvailable(workName, workNS string) *fleetv1beta1.Work {
	var resultWork fleetv1beta1.Work
	Eventually(func() bool {
		err := k8sClient.Get(context.Background(), types.NamespacedName{Name: workName, Namespace: workNS}, &resultWork)
		if err != nil {
			return false
		}
		availCond := meta.FindStatusCondition(resultWork.Status.Conditions, fleetv1beta1.WorkConditionTypeAvailable)
		if !condition.IsConditionStatusTrue(availCond, resultWork.Generation) {
			By(fmt.Sprintf("availCond not true: %v", availCond))
			return false
		}
		for _, manifestCondition := range resultWork.Status.ManifestConditions {
			if !meta.IsStatusConditionTrue(manifestCondition.Conditions, fleetv1beta1.WorkConditionTypeAvailable) {
				By(fmt.Sprintf("manifest availCond not true %v : %v", manifestCondition.Identifier, manifestCondition.Conditions))
				return false
			}
		}
		return true
	}, timeout, interval).Should(BeTrue())
	return &resultWork
}

// waitForWorkToBeHandled waits for a work to have a finalizer
func waitForWorkToBeHandled(workName, workNS string) *fleetv1beta1.Work {
	var resultWork fleetv1beta1.Work
	Eventually(func() bool {
		err := k8sClient.Get(context.Background(), types.NamespacedName{Name: workName, Namespace: workNS}, &resultWork)
		if err != nil {
			return false
		}
		return controllerutil.ContainsFinalizer(&resultWork, fleetv1beta1.WorkFinalizer)
	}, timeout, interval).Should(BeTrue())
	return &resultWork
}
