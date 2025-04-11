/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/test/e2e/framework"
)

var _ = FDescribe("Drain cluster", Ordered, Serial, func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())
	var drainClusters, noDrainClusters []*framework.Cluster
	var noDrainClusterNames []string
	var drainBinaryPath, uncordonBinaryPath string

	BeforeAll(func() {
		drainClusters = []*framework.Cluster{memberCluster1EastProd}
		noDrainClusters = []*framework.Cluster{memberCluster2EastCanary, memberCluster3WestProd}
		noDrainClusterNames = []string{memberCluster2EastCanaryName, memberCluster3WestProdName}

		// Build drain binary
		drainBinaryPath = filepath.Join("../../", "hack", "tools", "bin", "kubectl-draincluster")
		buildCmd := exec.Command("go", "build", "-o", drainBinaryPath, filepath.Join("../../", "tools", "draincluster", "main.go"))
		output, err := buildCmd.CombinedOutput()
		Expect(err).ToNot(HaveOccurred(), "Failed to drain cluster: %v\nOutput: %s", err, string(output))

		// Build uncordon binary
		uncordonBinaryPath = filepath.Join("../../", "hack", "tools", "bin", "kubectl-uncordoncluster")
		buildCmd = exec.Command("go", "build", "-o", uncordonBinaryPath, filepath.Join("../../", "tools", "uncordoncluster", "main.go"))
		_, err = buildCmd.CombinedOutput()
		Expect(err).ToNot(HaveOccurred(), "Failed to uncordon cluster: %v\nOutput: %s", err, string(output))

		By("creating work resources")
		createWorkResources()

		// Create the CRP.
		crp := &placementv1beta1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
				// Add a custom finalizer; this would allow us to better observe
				// the behavior of the controllers.
				Finalizers: []string{customDeletionBlockerFinalizer},
			},
			Spec: placementv1beta1.ClusterResourcePlacementSpec{
				Policy: &placementv1beta1.PlacementPolicy{
					PlacementType: placementv1beta1.PickAllPlacementType,
				},
				ResourceSelectors: workResourceSelector(),
			},
		}
		Expect(hubClient.Create(ctx, crp)).To(Succeed(), "Failed to create CRP %s", crpName)
	})

	AfterAll(func() {
		ensureCRPAndRelatedResourcesDeleted(crpName, allMemberClusters)
		// Cleanup binary
		Expect(os.Remove(drainBinaryPath)).Should(Succeed(), "Failed to remove drain binary")
		Expect(os.Remove(uncordonBinaryPath)).Should(Succeed(), "Failed to remove uncordon binary")
	})

	It("should update cluster resource placement status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should place resources on all available member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)

	It("drain cluster using binary", func() {
		cmd := exec.Command(drainBinaryPath,
			"--hubClusterContext", hubClusterName,
			"--clusterName", memberCluster1EastProdName)
		output, err := cmd.CombinedOutput()
		Expect(err).ToNot(HaveOccurred(), "Failed to drain cluster: %v\nOutput: %s", err, string(output))
	})

	It("should ensure no resources exist on drained clusters", func() {
		for _, cluster := range drainClusters {
			resourceRemovedActual := workNamespaceRemovedFromClusterActual(cluster)
			Eventually(resourceRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to check if resources doesn't exist on member cluster")
		}
	})

	It("should update cluster resource placement status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), noDrainClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should place resources on the selected clusters with no taint", func() {
		for _, cluster := range noDrainClusters {
			resourcePlacedActual := workNamespaceAndConfigMapPlacedOnClusterActual(cluster)
			Eventually(resourcePlacedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to place resources on the selected clusters")
		}
	})

	It("uncordon cluster using binary", func() {
		cmd := exec.Command(uncordonBinaryPath,
			"--hubClusterContext", hubClusterName,
			"--clusterName", memberCluster1EastProdName)
		output, err := cmd.CombinedOutput()
		Expect(err).ToNot(HaveOccurred(), "Failed to uncordon cluster: %v\nOutput: %s", err, string(output))
	})

	It("should update cluster resource placement status as expected", func() {
		crpStatusUpdatedActual := crpStatusUpdatedActual(workResourceIdentifiers(), allMemberClusterNames, nil, "0")
		Eventually(crpStatusUpdatedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update cluster resource placement status as expected")
	})

	It("should place resources on the all available member clusters", checkIfPlacedWorkResourcesOnAllMemberClusters)
})
