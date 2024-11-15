/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package workapplier

import (
	"fmt"
	"time"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

const (
	workNameTemplate   = "work-%d"
	nsNameTemplate     = "ns-%d"
	deployNameTemplate = "deploy-%d"
)

const (
	eventuallyDuration = time.Second * 10
	eventuallyInternal = time.Second * 1
)

func createWorkObjectUsingDefaultApplyStrategyWithRegularNSAndDeploy(workName, nsName, deployName string) {
	// Prepare the NS object in JSON format.
	regularNS := ns.DeepCopy()
	regularNS.Name = nsName

	nsUnstructuredMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(regularNS)
	Expect(err).To(BeNil(), "Failed to convert the NS object to an unstructured object")
	nsUnstructured := &unstructured.Unstructured{Object: nsUnstructuredMap}
	nsJSON, err := nsUnstructured.MarshalJSON()
	Expect(err).To(BeNil(), "Failed to marshal the unstructured object to JSON")

	// Prepare the Deployment object in JSON format.
	regularDeploy := deploy.DeepCopy()
	regularDeploy.Namespace = nsName
	regularDeploy.Name = deployName

	deployUnstructuredMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(regularDeploy)
	Expect(err).To(BeNil(), "Failed to convert the Deployment object to an unstructured object")
	deployUnstructured := &unstructured.Unstructured{Object: deployUnstructuredMap}
	deployJSON, err := deployUnstructured.MarshalJSON()
	Expect(err).To(BeNil(), "Failed to marshal the unstructured object to JSON")

	// Create a new Work object.
	work := &fleetv1beta1.Work{
		ObjectMeta: metav1.ObjectMeta{
			Name:      workName,
			Namespace: memberReservedNSName,
		},
		Spec: fleetv1beta1.WorkSpec{
			Workload: fleetv1beta1.WorkloadTemplate{
				Manifests: []fleetv1beta1.Manifest{
					{
						RawExtension: runtime.RawExtension{
							Raw: nsJSON,
						},
					},
					{
						RawExtension: runtime.RawExtension{
							Raw: deployJSON,
						},
					},
				},
			},
		},
	}
	Expect(hubClient.Create(ctx, work)).To(Succeed())
}

func workFinalizerAddedActual(workName string) func() error {
	return func() error {
		// Retrieve the Work object.
		work := &fleetv1beta1.Work{}
		if err := hubClient.Get(ctx, client.ObjectKey{Name: workName, Namespace: memberReservedNSName}, work); err != nil {
			return fmt.Errorf("failed to retrieve the Work object: %w", err)
		}

		// Check that the cleanup finalizer has been added.
		if !controllerutil.ContainsFinalizer(work, fleetv1beta1.WorkFinalizer) {
			return fmt.Errorf("cleanup finalizer has not been added")
		}
		return nil
	}
}

func appliedWorkCreatedActual(workName string) func() error {
	return func() error {
		// Retrieve the AppliedWork object.
		appliedWork := &fleetv1beta1.AppliedWork{}
		if err := hubClient.Get(ctx, client.ObjectKey{Name: workName, Namespace: memberReservedNSName}, appliedWork); err != nil {
			return fmt.Errorf("failed to retrieve the AppliedWork object: %w", err)
		}

		wantAppliedWork := &fleetv1beta1.AppliedWork{
			ObjectMeta: metav1.ObjectMeta{
				Name: workName,
			},
			Spec: fleetv1beta1.AppliedWorkSpec{
				WorkName:      workName,
				WorkNamespace: memberReservedNSName,
			},
		}
		if diff := cmp.Diff(appliedWork, wantAppliedWork); diff != "" {
			return fmt.Errorf("appliedWork diff (-got +want):\n%s", diff)
		}
		return nil
	}
}

func regularNSObjectAppliedActual(nsName string) func() error {
	return func() error {
		// Retrieve the NS object.
		gotNS := &corev1.Namespace{}
		if err := hubClient.Get(ctx, client.ObjectKey{Name: nsName}, gotNS); err != nil {
			return fmt.Errorf("failed to retrieve the NS object: %w", err)
		}

		// Check that the NS object has been created as expected.

		// To ignore default values automatically, here the test suite rebuilds the objects.
		wantNS := ns.DeepCopy()
		wantNS.Name = nsName

		rebuiltGotNS := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: gotNS.Name,
			},
		}

		if diff := cmp.Diff(rebuiltGotNS, wantNS); diff != "" {
			return fmt.Errorf("namespace diff (-got +want):\n%s", diff)
		}
		return nil
	}
}

func regularDeploymentObjectAppliedActual(_ string) func() error {
	return func() error {
		return nil
	}
}

var _ = Describe("applying manifests", func() {
	Context("apply a new manifest (regular)", Ordered, func() {
		workName := fmt.Sprintf(workNameTemplate, GinkgoParallelProcess())
		nsName := fmt.Sprintf(nsNameTemplate, GinkgoParallelProcess())
		deployName := fmt.Sprintf(deployNameTemplate, GinkgoParallelProcess())

		BeforeAll(func() {
			// Ensure that no corresponding AppliedWork object has been created so far.

			// Ensure that no related manifests have been applied so far.

			// Create a new Work object.
			createWorkObjectUsingDefaultApplyStrategyWithRegularNSAndDeploy(workName, nsName, deployName)
		})

		It("should add cleanup finalizer to the Work object", func() {
			finalizerAddedActual := workFinalizerAddedActual(workName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to add cleanup finalizer to the Work object")
		})

		It("should prepare an AppliedWork object", func() {
			appliedWorkCreatedActual := appliedWorkCreatedActual(workName)
			Eventually(appliedWorkCreatedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to prepare an AppliedWork object")
		})

		It("should apply the manifests", func() {
			// Ensure that the NS object has been applied as expected.
			regularNSObjectAppliedActual := regularNSObjectAppliedActual(nsName)
			Eventually(regularNSObjectAppliedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to apply the namespace object")

			// Ensure that the Deployment object has been applied as expected.
		})

		It("can mark the deployment as available", func() {})

		It("should update the Work object status", func() {})

		It("should update the AppliedWork object status", func() {})

		AfterAll(func() {
			// Delete the Work object and related resources.

			// Ensure that all applied manifests have been removed.
		})
	})
})
