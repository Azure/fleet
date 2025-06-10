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

	cmdCRDInstaller "go.goms.io/fleet/cmd/crdinstaller/utils"
)

var _ = Describe("Test CRD Installer", func() {
	It("should install CRDs and create a CRD instance", func() {
		Expect(cmdCRDInstaller.InstallCRD(ctx, k8sClient, "./original_crds/test.kubernetes-fleet.io_testresources.yaml")).To(Succeed())
	})
})
