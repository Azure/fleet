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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/types"
	
	cmdCRDInstaller "go.goms.io/fleet/cmd/crdinstaller/utils"
)

var _ = Describe("Test CRD Installer", func() {
	It("should install CRDs and create a CRD instance", func() {
		Expect(cmdCRDInstaller.InstallCRD(ctx, k8sClient, "./original_crds/test.kubernetes-fleet.io_testresources.yaml")).To(Succeed())
	})

	It("should verify CRD installation", func() {
		crdName := "testresources.test.kubernetes-fleet.io"
		crd := &apiextensionsv1.CustomResourceDefinition{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: crdName}, crd)).NotTo(HaveOccurred(), "CRD %s should be installed", crdName)

		v1alpha1Version := crd.Spec.Versions[0]
		Expect(v1alpha1Version.Name).Should(Equal("v1alpha1"), "CRD %s should have version v1alpha1", crdName)

		Expect(v1alpha1Version.Schema.OpenAPIV3Schema.Properties["spec"].Properties["properties"].Properties["bar"]).ShouldNot(BeNil(), "CRD %s should have 'bar' property in spec", crdName)
	})
})
