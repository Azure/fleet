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

package crdinstaller

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/types"

	cmdCRDInstaller "go.goms.io/fleet/cmd/crdinstaller/utils"
)

const (
	crdName         = "testresources.test.kubernetes-fleet.io"
	originalCRDPath = "./original_crds/test.kubernetes-fleet.io_testresources.yaml"
	updatedCRDPath  = "./updated_crds/test.kubernetes-fleet.io_testresources.yaml"
)
const (
	eventuallyDuration = time.Minute * 1
	eventuallyInterval = time.Millisecond * 250
)

var _ = Describe("Test CRD Installer, Create and Update CRD", func() {
	It("should create original CRD", func() {
		Expect(cmdCRDInstaller.InstallCRD(ctx, k8sClient, originalCRDPath)).To(Succeed())
	})

	It("should verify original CRD installation", func() {
		crd := &apiextensionsv1.CustomResourceDefinition{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: crdName}, crd)).NotTo(HaveOccurred(), "CRD %s should be installed", crdName)
		Expect(len(crd.Labels)).Should(Equal(1), "CRD %s should have 1 label defined", crdName)
		Expect(crd.Labels["crd-installer.kubernetes-fleet.io/managed"]).Should(Equal("true"), "CRD %s should have addonmanager label set to Reconcile", crdName)

		v1alpha1Version := crd.Spec.Versions[0]
		Expect(v1alpha1Version.Name).Should(Equal("v1alpha1"), "CRD %s should have version v1alpha1", crdName)
		Expect(v1alpha1Version.Schema).ShouldNot(BeNil(), "CRD %s should have a schema defined", crdName)
		Expect(v1alpha1Version.Schema.OpenAPIV3Schema).ShouldNot(BeNil(), "CRD %s should have OpenAPIV3Schema defined", crdName)
		Expect(v1alpha1Version.Schema.OpenAPIV3Schema.Properties["spec"]).ShouldNot(BeNil(), "CRD %s should have spec defined in Properties", crdName)
		// Original CRD should have 4 properties defined in spec.
		Expect(len(v1alpha1Version.Schema.OpenAPIV3Schema.Properties["spec"].Properties)).Should(Equal(4), "CRD %s should have 4 properties defined in spec", crdName)
		Expect(v1alpha1Version.Schema.OpenAPIV3Schema.Properties["spec"].Properties["bar"].Type).Should(Equal("string"), "CRD %s should have 'bar' property of type string defined in properties", crdName)
		_, ok := v1alpha1Version.Schema.OpenAPIV3Schema.Properties["spec"].Properties["newField"]
		Expect(ok).To(BeFalse(), "CRD %s should not have 'newField' property defined in properties", crdName)
	})

	It("update the CRD to have the addonmanger ownership label", func() {
		updateCRDLabels(crdName, map[string]string{"addonmanager.kubernetes.io/mode": "Reconcile"})
	})

	It("should not update the CRD with new fields with addonmanager ownership label", func() {
		Expect(cmdCRDInstaller.InstallCRD(ctx, k8sClient, updatedCRDPath)).To(Succeed())
	})

	It("should verify original CRD still exists, because it's owned by addonmanager", func() {
		crd := &apiextensionsv1.CustomResourceDefinition{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: crdName}, crd)).NotTo(HaveOccurred(), "CRD %s should be installed", crdName)
		Expect(len(crd.Labels)).Should(Equal(1), "CRD %s should have 1 label defined", crdName)
		Expect(crd.Labels["addonmanager.kubernetes.io/mode"]).Should(Equal("Reconcile"), "CRD %s should have addonmanager label set to Reconcile", crdName)

		v1alpha1Version := crd.Spec.Versions[0]
		Expect(v1alpha1Version.Name).Should(Equal("v1alpha1"), "CRD %s should have version v1alpha1", crdName)
		Expect(v1alpha1Version.Schema).ShouldNot(BeNil(), "CRD %s should have a schema defined", crdName)
		Expect(v1alpha1Version.Schema.OpenAPIV3Schema).ShouldNot(BeNil(), "CRD %s should have OpenAPIV3Schema defined", crdName)
		Expect(v1alpha1Version.Schema.OpenAPIV3Schema.Properties["spec"]).ShouldNot(BeNil(), "CRD %s should have spec defined in Properties", crdName)
		// Original CRD should still have 4 properties defined in spec.
		Expect(len(v1alpha1Version.Schema.OpenAPIV3Schema.Properties["spec"].Properties)).Should(Equal(4), "CRD %s should have 4 properties defined in spec", crdName)
		Expect(v1alpha1Version.Schema.OpenAPIV3Schema.Properties["spec"].Properties["bar"].Type).Should(Equal("string"), "CRD %s should have 'bar' property of type string defined in properties", crdName)
		_, ok := v1alpha1Version.Schema.OpenAPIV3Schema.Properties["spec"].Properties["newField"]
		Expect(ok).To(BeFalse(), "CRD %s should not have 'newField' property defined in properties", crdName)
	})

	It("update the CRD to remove addonmanager owenership label", func() {
		updateCRDLabels(crdName, map[string]string{"random-label.io": "true"})
	})

	It("should update the CRD with new field in spec with crdinstaller label", func() {
		Expect(cmdCRDInstaller.InstallCRD(ctx, k8sClient, updatedCRDPath)).To(Succeed())
	})

	It("should verify updated CRD", func() {
		crd := &apiextensionsv1.CustomResourceDefinition{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: crdName}, crd)).NotTo(HaveOccurred(), "CRD %s should be installed", crdName)
		Expect(len(crd.Labels)).Should(Equal(2), "CRD %s should have 1 label defined", crdName)
		Expect(crd.Labels["random-label.io"]).Should(Equal("true"), "CRD %s should have random label still set", crdName)
		Expect(crd.Labels["crd-installer.kubernetes-fleet.io/managed"]).Should(Equal("true"), "CRD %s should have addonmanager label set to Reconcile", crdName)

		v1alpha1Version := crd.Spec.Versions[0]
		Expect(v1alpha1Version.Name).Should(Equal("v1alpha1"), "CRD %s should have version v1alpha1", crdName)
		Expect(v1alpha1Version.Schema).ShouldNot(BeNil(), "CRD %s should have a schema defined", crdName)
		Expect(v1alpha1Version.Schema.OpenAPIV3Schema).ShouldNot(BeNil(), "CRD %s should have OpenAPIV3Schema defined", crdName)
		Expect(v1alpha1Version.Schema.OpenAPIV3Schema.Properties["spec"]).ShouldNot(BeNil(), "CRD %s should have spec defined in Properties", crdName)
		// Updated CRD should have 5 properties defined in spec.
		Expect(len(v1alpha1Version.Schema.OpenAPIV3Schema.Properties["spec"].Properties)).Should(Equal(5), "CRD %s should have 5 properties defined in spec", crdName)
		Expect(v1alpha1Version.Schema.OpenAPIV3Schema.Properties["spec"].Properties["bar"].Type).Should(Equal("string"), "CRD %s should have 'bar' property of type string defined in properties", crdName)
		_, ok := v1alpha1Version.Schema.OpenAPIV3Schema.Properties["spec"].Properties["newField"]
		Expect(ok).To(BeTrue(), "CRD %s should have 'newField' property defined in properties", crdName)
		Expect(v1alpha1Version.Schema.OpenAPIV3Schema.Properties["spec"].Properties["newField"].Type).Should(Equal("string"), "CRD %s should have 'newField' property of type string defined in properties", crdName)
	})
})

func updateCRDLabels(crdName string, labels map[string]string) {
	Eventually(func() error {
		var crd apiextensionsv1.CustomResourceDefinition
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: crdName}, &crd); err != nil {
			return err
		}
		crd.Labels = labels
		return k8sClient.Update(ctx, &crd)
	}, eventuallyDuration, eventuallyInterval).ShouldNot(HaveOccurred(), "CRD %s should have addonmanager label", crdName)
}
