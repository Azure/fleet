/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package crdinstaller

import (
	"fmt"
	"time"

	"github.com/google/go-cmp/cmp"
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
	randomLabelKey  = "random-label.io"
)
const (
	eventuallyDuration = time.Minute * 1
	eventuallyInterval = time.Millisecond * 250
)

// This test verifies the behavior of the CRD installer when creating and updating CRDs.
// It ensures that the installer can create a CRD, update it with new fields, and handle ownership label correctly.
// The original CRD has 4 properties, and the updated CRD has a new property to simulate CRD upgrade.
var _ = Describe("Test CRD Installer, Create and Update CRD", Ordered, func() {
	It("should create original CRD", func() {
		crd, err := cmdCRDInstaller.GetCRDFromPath(originalCRDPath, scheme)
		Expect(err).NotTo(HaveOccurred(), "should get CRD from path %s", originalCRDPath)
		Expect(cmdCRDInstaller.InstallCRD(ctx, k8sClient, crd)).To(Succeed())
	})

	It("should verify original CRD installation", func() {
		ensureCRDExistsWithLabels(map[string]string{
			cmdCRDInstaller.CRDInstallerLabelKey: "true",
			cmdCRDInstaller.AzureManagedLabelKey: cmdCRDInstaller.FleetLabelValue,
		})
		crd := &apiextensionsv1.CustomResourceDefinition{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: crdName}, crd)).NotTo(HaveOccurred(), "CRD %s should be installed", crdName)
		spec := fetchSpecJSONSchemaProperties(crd)
		// Original CRD should have 4 properties defined in spec.
		Expect(len(spec.Properties)).Should(Equal(4), "CRD %s should have 4 properties defined in spec", crdName)
		Expect(spec.Properties["bar"].Type).Should(Equal("string"), "CRD %s should have 'bar' property of type string defined in properties", crdName)
		_, ok := spec.Properties["newField"]
		Expect(ok).To(BeFalse(), "CRD %s should not have 'newField' property defined in properties", crdName)
	})

	It("update the CRD to add a random label", func() {
		updateCRDLabels(crdName, map[string]string{randomLabelKey: "true"})
	})

	It("should update the CRD with new field in spec with crdinstaller label", func() {
		crd, err := cmdCRDInstaller.GetCRDFromPath(updatedCRDPath, scheme)
		Expect(err).NotTo(HaveOccurred(), "should get CRD from path %s", updatedCRDPath)
		Expect(cmdCRDInstaller.InstallCRD(ctx, k8sClient, crd)).To(Succeed())
	})

	It("should verify updated CRD", func() {
		// ensure we don't overwrite the random label.
		ensureCRDExistsWithLabels(map[string]string{
			randomLabelKey:                       "true",
			cmdCRDInstaller.CRDInstallerLabelKey: "true",
			cmdCRDInstaller.AzureManagedLabelKey: cmdCRDInstaller.FleetLabelValue,
		})
		crd := &apiextensionsv1.CustomResourceDefinition{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: crdName}, crd)).NotTo(HaveOccurred(), "CRD %s should be installed", crdName)
		spec := fetchSpecJSONSchemaProperties(crd)
		// Updated CRD should have 5 properties defined in spec.
		Expect(len(spec.Properties)).Should(Equal(5), "CRD %s should have 5 properties defined in spec", crdName)
		Expect(spec.Properties["bar"].Type).Should(Equal("string"), "CRD %s should have 'bar' property of type string defined in properties", crdName)
		_, ok := spec.Properties["newField"]
		Expect(ok).To(BeTrue(), "CRD %s should have 'newField' property defined in properties", crdName)
		Expect(spec.Properties["newField"].Type).Should(Equal("string"), "CRD %s should have 'newField' property of type string defined in properties", crdName)
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

func fetchSpecJSONSchemaProperties(crd *apiextensionsv1.CustomResourceDefinition) apiextensionsv1.JSONSchemaProps {
	Expect(len(crd.Spec.Versions)).Should(Equal(1), "CRD %s should have exactly one version", crdName)
	v1alpha1Version := crd.Spec.Versions[0]
	Expect(v1alpha1Version.Name).Should(Equal("v1alpha1"), "CRD %s should have version v1alpha1", crdName)
	Expect(v1alpha1Version.Schema).ShouldNot(BeNil(), "CRD %s should have a schema defined", crdName)
	Expect(v1alpha1Version.Schema.OpenAPIV3Schema).ShouldNot(BeNil(), "CRD %s should have OpenAPIV3Schema defined", crdName)
	Expect(v1alpha1Version.Schema.OpenAPIV3Schema.Properties["spec"]).ShouldNot(BeNil(), "CRD %s should have spec defined in Properties", crdName)
	return v1alpha1Version.Schema.OpenAPIV3Schema.Properties["spec"]
}

func ensureCRDExistsWithLabels(wantLabels map[string]string) {
	Eventually(func() error {
		crd := &apiextensionsv1.CustomResourceDefinition{}
		err := k8sClient.Get(ctx, types.NamespacedName{Name: crdName}, crd)
		if err != nil {
			return err
		}
		if diff := cmp.Diff(wantLabels, crd.GetLabels()); diff != "" {
			return fmt.Errorf("crd labels mismatch (-want, +got) :\n%s", diff)
		}
		return nil
	}, eventuallyDuration, eventuallyInterval).ShouldNot(HaveOccurred(), "CRD %s should exist with labels %v", crdName)
}
