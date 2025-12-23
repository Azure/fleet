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

package workapplier

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"time"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	storagev1 "k8s.io/api/storage/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/condition"
	testutilsactuals "github.com/kubefleet-dev/kubefleet/test/utils/actuals"
)

// Note (chenyu1): all test cases in this file use a separate test environment
// (same hub cluster, different fleet member reserved namespace, different
// work applier instance) from the other integration tests. This is needed
// to (relatively speaking) reliably verify the wave-based parallel processing
// in the work applier.

var _ = Describe("parallel processing with waves", func() {
	Context("single wave", Ordered, func() {
		workName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())

		pcName := "priority-class-1"

		BeforeAll(func() {
			// Prepare a NS object.
			regularNS := ns.DeepCopy()
			regularNS.Name = nsName
			regularNSJSON := marshalK8sObjJSON(regularNS)

			// Prepare a PriorityClass object.
			regularPC := &schedulingv1.PriorityClass{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "scheduling.k8s.io/v1",
					Kind:       "PriorityClass",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: pcName,
				},
				Value:         1000,
				GlobalDefault: false,
				Description:   "Experimental priority class",
			}
			regularPCJSON := marshalK8sObjJSON(regularPC)

			// Create a new Work object with all the manifest JSONs.
			createWorkObject(workName, memberReservedNSName3, nil, nil, regularNSJSON, regularPCJSON)
		})

		// For simplicity reasons, this test case will skip some of the regular apply op result verification
		// (finalizer check, AppliedWork object check, etc.), as they have been repeatedly verified in different
		// test cases under similar conditions.

		It("should update the Work object status", func() {
			// Prepare the status information.
			workConds := []metav1.Condition{
				{
					Type:   fleetv1beta1.WorkConditionTypeApplied,
					Status: metav1.ConditionTrue,
					Reason: condition.WorkAllManifestsAppliedReason,
				},
				{
					Type:   fleetv1beta1.WorkConditionTypeAvailable,
					Status: metav1.ConditionTrue,
					Reason: condition.WorkAllManifestsAvailableReason,
				},
			}
			manifestConds := []fleetv1beta1.ManifestCondition{
				{
					Identifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:  0,
						Group:    "",
						Version:  "v1",
						Kind:     "Namespace",
						Resource: "namespaces",
						Name:     nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:               fleetv1beta1.WorkConditionTypeApplied,
							Status:             metav1.ConditionTrue,
							Reason:             string(ApplyOrReportDiffResTypeApplied),
							ObservedGeneration: 0,
						},
						{
							Type:               fleetv1beta1.WorkConditionTypeAvailable,
							Status:             metav1.ConditionTrue,
							Reason:             string(AvailabilityResultTypeAvailable),
							ObservedGeneration: 0,
						},
					},
				},
				{
					Identifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:  1,
						Group:    "scheduling.k8s.io",
						Version:  "v1",
						Kind:     "PriorityClass",
						Resource: "priorityclasses",
						Name:     pcName,
					},
					Conditions: []metav1.Condition{
						{
							Type:               fleetv1beta1.WorkConditionTypeApplied,
							Status:             metav1.ConditionTrue,
							Reason:             string(ApplyOrReportDiffResTypeApplied),
							ObservedGeneration: 1,
						},
						{
							Type:               fleetv1beta1.WorkConditionTypeAvailable,
							Status:             metav1.ConditionTrue,
							Reason:             string(AvailabilityResultTypeAvailable),
							ObservedGeneration: 1,
						},
					},
				},
			}

			workStatusUpdatedActual := workStatusUpdated(memberReservedNSName3, workName, workConds, manifestConds, nil, nil)
			// Considering the presence of fixed delay in the parallelizer, the test case here
			// uses a longer timeout and interval.
			Eventually(workStatusUpdatedActual, eventuallyDuration*2, eventuallyInterval*2).Should(Succeed(), "Failed to update work status")
		})

		It("should create resources in parallel", func() {
			// The work applier in use for this environment is set to wait for a fixed delay between each
			// parallelizer call. If the parallelization is set up correctly, resources in the same wave
			// should have very close creation timestamps, while the creation timestamps between resources
			// in different waves should have a consistent gap (roughly the fixed delay).

			placedNS := &corev1.Namespace{}
			Eventually(memberClient3.Get(ctx, types.NamespacedName{Name: nsName}, placedNS), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to get the placed Namespace")

			placedPC := &schedulingv1.PriorityClass{}
			Eventually(memberClient3.Get(ctx, types.NamespacedName{Name: pcName}, placedPC), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to get the placed PriorityClass")

			gap := placedPC.CreationTimestamp.Sub(placedNS.CreationTimestamp.Time)
			// The two objects belong to the same wave; the creation timestamps should be very close.
			Expect(gap).To(BeNumerically("<=", time.Second), "The creation time gap between resources in the same wave is greater than or equal to the fixed delay")
		})

		AfterAll(func() {
			// Delete the Work object and related resources.
			deleteWorkObject(workName, memberReservedNSName3)

			// Remove the PriorityClass object if it still exists.
			Eventually(func() error {
				pc := &schedulingv1.PriorityClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: pcName,
					},
				}
				return memberClient3.Delete(ctx, pc)
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the PriorityClass object")

			// Ensure that the AppliedWork object has been removed.
			appliedWorkRemovedActual := appliedWorkRemovedActual(memberClient3, workName)
			Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the AppliedWork object")

			workRemovedActual := testutilsactuals.WorkObjectRemovedActual(ctx, hubClient, workName, memberReservedNSName3)
			Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the Work object")
			// The environment prepared by the envtest package does not support namespace
			// deletion; consequently this test suite would not attempt to verify its deletion.
		})
	})

	Context("two consecutive waves", Ordered, func() {
		workName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())

		BeforeAll(func() {
			// Prepare a NS object.
			regularNS := ns.DeepCopy()
			regularNS.Name = nsName
			regularNSJSON := marshalK8sObjJSON(regularNS)

			// Prepare a ConfigMap object.
			regularCM := configMap.DeepCopy()
			regularCM.Namespace = nsName
			regularCMJSON := marshalK8sObjJSON(regularCM)

			// Create a new Work object with all the manifest JSONs.
			createWorkObject(workName, memberReservedNSName3, nil, nil, regularNSJSON, regularCMJSON)
		})

		// For simplicity reasons, this test case will skip some of the regular apply op result verification
		// (finalizer check, AppliedWork object check, etc.), as they have been repeatedly verified in different
		// test cases under similar conditions.

		It("should update the Work object status", func() {
			// Prepare the status information.
			workConds := []metav1.Condition{
				{
					Type:   fleetv1beta1.WorkConditionTypeApplied,
					Status: metav1.ConditionTrue,
					Reason: condition.WorkAllManifestsAppliedReason,
				},
				{
					Type:   fleetv1beta1.WorkConditionTypeAvailable,
					Status: metav1.ConditionTrue,
					Reason: condition.WorkAllManifestsAvailableReason,
				},
			}
			manifestConds := []fleetv1beta1.ManifestCondition{
				{
					Identifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:  0,
						Group:    "",
						Version:  "v1",
						Kind:     "Namespace",
						Resource: "namespaces",
						Name:     nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:               fleetv1beta1.WorkConditionTypeApplied,
							Status:             metav1.ConditionTrue,
							Reason:             string(ApplyOrReportDiffResTypeApplied),
							ObservedGeneration: 0,
						},
						{
							Type:               fleetv1beta1.WorkConditionTypeAvailable,
							Status:             metav1.ConditionTrue,
							Reason:             string(AvailabilityResultTypeAvailable),
							ObservedGeneration: 0,
						},
					},
				},
				{
					Identifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:   1,
						Group:     "",
						Version:   "v1",
						Kind:      "ConfigMap",
						Resource:  "configmaps",
						Name:      configMapName,
						Namespace: nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:               fleetv1beta1.WorkConditionTypeApplied,
							Status:             metav1.ConditionTrue,
							Reason:             string(ApplyOrReportDiffResTypeApplied),
							ObservedGeneration: 0,
						},
						{
							Type:               fleetv1beta1.WorkConditionTypeAvailable,
							Status:             metav1.ConditionTrue,
							Reason:             string(AvailabilityResultTypeAvailable),
							ObservedGeneration: 0,
						},
					},
				},
			}

			workStatusUpdatedActual := workStatusUpdated(memberReservedNSName3, workName, workConds, manifestConds, nil, nil)
			// Considering the presence of fixed delay in the parallelizer, the test case here
			// uses a longer timeout and interval.
			Eventually(workStatusUpdatedActual, eventuallyDuration*2, eventuallyInterval*2).Should(Succeed(), "Failed to update work status")
		})

		It("should create resources in waves", func() {
			// The work applier in use for this environment is set to wait for a fixed delay between each
			// parallelizer call. If the parallelization is set up correctly, resources in the same wave
			// should have very close creation timestamps, while the creation timestamps between resources
			// in different waves should have a consistent gap (roughly the fixed delay).

			placedNS := &corev1.Namespace{}
			Eventually(memberClient3.Get(ctx, types.NamespacedName{Name: nsName}, placedNS), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to get the placed Namespace")

			placedCM := &corev1.ConfigMap{}
			Eventually(memberClient3.Get(ctx, types.NamespacedName{Namespace: nsName, Name: configMapName}, placedCM), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to get the placed ConfigMap")

			gap := placedCM.CreationTimestamp.Sub(placedNS.CreationTimestamp.Time)
			Expect(gap).To(BeNumerically(">=", parallelizerFixedDelay), "The creation time gap between resources in different waves is less than the fixed delay")
			Expect(gap).To(BeNumerically("<", parallelizerFixedDelay*2), "The creation time gap between resources in different waves is at least twice as large as the fixed delay")
		})

		AfterAll(func() {
			// Delete the Work object and related resources.
			deleteWorkObject(workName, memberReservedNSName3)

			// Remove the ConfigMap object if it still exists.
			cmRemovedActual := regularConfigMapRemovedActual(nsName, configMapName)
			Eventually(cmRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the ConfigMap object")

			// Ensure that the AppliedWork object has been removed.
			appliedWorkRemovedActual := appliedWorkRemovedActual(memberClient3, workName)
			Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the AppliedWork object")

			workRemovedActual := testutilsactuals.WorkObjectRemovedActual(ctx, hubClient, workName, memberReservedNSName3)
			Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the Work object")
			// The environment prepared by the envtest package does not support namespace
			// deletion; consequently this test suite would not attempt to verify its deletion.
		})
	})

	Context("two non-consecutive waves", Ordered, func() {
		workName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())

		roleName := "role-1"

		BeforeAll(func() {
			// Prepare a NS object.
			regularNS := ns.DeepCopy()
			regularNS.Name = nsName
			regularNSJSON := marshalK8sObjJSON(regularNS)

			// Prepare a Role object.
			regularRole := &rbacv1.Role{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "rbac.authorization.k8s.io/v1",
					Kind:       "Role",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      roleName,
					Namespace: nsName,
				},
				Rules: []rbacv1.PolicyRule{},
			}
			regularRoleJSON := marshalK8sObjJSON(regularRole)

			// Create a new Work object with all the manifest JSONs.
			createWorkObject(workName, memberReservedNSName3, nil, nil, regularNSJSON, regularRoleJSON)
		})

		// For simplicity reasons, this test case will skip some of the regular apply op result verification
		// (finalizer check, AppliedWork object check, etc.), as they have been repeatedly verified in different
		// test cases under similar conditions.

		It("should update the Work object status", func() {
			// Prepare the status information.
			workConds := []metav1.Condition{
				{
					Type:   fleetv1beta1.WorkConditionTypeApplied,
					Status: metav1.ConditionTrue,
					Reason: condition.WorkAllManifestsAppliedReason,
				},
				{
					Type:   fleetv1beta1.WorkConditionTypeAvailable,
					Status: metav1.ConditionTrue,
					Reason: condition.WorkAllManifestsAvailableReason,
				},
			}
			manifestConds := []fleetv1beta1.ManifestCondition{
				{
					Identifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:  0,
						Group:    "",
						Version:  "v1",
						Kind:     "Namespace",
						Resource: "namespaces",
						Name:     nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:               fleetv1beta1.WorkConditionTypeApplied,
							Status:             metav1.ConditionTrue,
							Reason:             string(ApplyOrReportDiffResTypeApplied),
							ObservedGeneration: 0,
						},
						{
							Type:               fleetv1beta1.WorkConditionTypeAvailable,
							Status:             metav1.ConditionTrue,
							Reason:             string(AvailabilityResultTypeAvailable),
							ObservedGeneration: 0,
						},
					},
				},
				{
					Identifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:   1,
						Group:     "rbac.authorization.k8s.io",
						Version:   "v1",
						Kind:      "Role",
						Resource:  "roles",
						Name:      roleName,
						Namespace: nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:               fleetv1beta1.WorkConditionTypeApplied,
							Status:             metav1.ConditionTrue,
							Reason:             string(ApplyOrReportDiffResTypeApplied),
							ObservedGeneration: 0,
						},
						{
							Type:               fleetv1beta1.WorkConditionTypeAvailable,
							Status:             metav1.ConditionTrue,
							Reason:             string(AvailabilityResultTypeAvailable),
							ObservedGeneration: 0,
						},
					},
				},
			}

			workStatusUpdatedActual := workStatusUpdated(memberReservedNSName3, workName, workConds, manifestConds, nil, nil)
			// Considering the presence of fixed delay in the parallelizer, the test case here
			// uses a longer timeout and interval.
			Eventually(workStatusUpdatedActual, eventuallyDuration*2, eventuallyInterval*2).Should(Succeed(), "Failed to update work status")
		})

		It("should create resources in waves", func() {
			// The work applier in use for this environment is set to wait for a fixed delay between each
			// parallelizer call. If the parallelization is set up correctly, resources in the same wave
			// should have very close creation timestamps, while the creation timestamps between resources
			// in different waves should have a consistent gap (roughly the fixed delay).

			placedNS := &corev1.Namespace{}
			Eventually(memberClient3.Get(ctx, types.NamespacedName{Name: nsName}, placedNS), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to get the placed Namespace")

			placedRole := &rbacv1.Role{}
			Eventually(memberClient3.Get(ctx, types.NamespacedName{Namespace: nsName, Name: roleName}, placedRole), eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to get the placed Role")

			gap := placedRole.CreationTimestamp.Sub(placedNS.CreationTimestamp.Time)
			Expect(gap).To(BeNumerically(">=", parallelizerFixedDelay), "The creation time gap between resources in different waves is less than the fixed delay")
			Expect(gap).To(BeNumerically("<", parallelizerFixedDelay*2), "The creation time gap between resources in different waves is at least twice as large as the fixed delay")
		})

		AfterAll(func() {
			// Delete the Work object and related resources.
			deleteWorkObject(workName, memberReservedNSName3)

			// Remove the Role object if it still exists.
			Eventually(func() error {
				cr := &rbacv1.Role{
					ObjectMeta: metav1.ObjectMeta{
						Name:      roleName,
						Namespace: nsName,
					},
				}
				if err := memberClient3.Delete(ctx, cr); err != nil && !errors.IsNotFound(err) {
					return fmt.Errorf("failed to deleete the role object: %w", err)
				}

				// Check that the Role object has been deleted.
				if err := memberClient3.Get(ctx, types.NamespacedName{Namespace: nsName, Name: roleName}, cr); !errors.IsNotFound(err) {
					return fmt.Errorf("role object still exists or an unexpected error occurred: %w", err)
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the Role object")

			// Ensure that the AppliedWork object has been removed.
			appliedWorkRemovedActual := appliedWorkRemovedActual(memberClient3, workName)
			Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the AppliedWork object")

			workRemovedActual := testutilsactuals.WorkObjectRemovedActual(ctx, hubClient, workName, memberReservedNSName3)
			Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the Work object")
			// The environment prepared by the envtest package does not support namespace
			// deletion; consequently this test suite would not attempt to verify its deletion.
		})
	})

	Context("all waves", Ordered, func() {
		//workName := fmt.Sprintf(workNameTemplate, utils.RandStr())
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())

		// The array below includes objects of all known resource types for waved
		// processing, plus a few objects of unknown resource types.
		objectsOfVariousResourceTypes := []client.Object{
			// Wave 0 objects.
			// Namespace object is created separately.
			&schedulingv1.PriorityClass{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "scheduling.k8s.io/v1",
					Kind:       "PriorityClass",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("pc-%s", utils.RandStr()),
				},
				Value: 1000,
			},
			// Wave 1 objects.
			&networkingv1.NetworkPolicy{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "networking.k8s.io/v1",
					Kind:       "NetworkPolicy",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("np-%s", utils.RandStr()),
					Namespace: nsName,
				},
				Spec: networkingv1.NetworkPolicySpec{
					PodSelector: metav1.LabelSelector{},
				},
			},
			&corev1.ResourceQuota{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "ResourceQuota",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("rq-%s", utils.RandStr()),
					Namespace: nsName,
				},
				Spec: corev1.ResourceQuotaSpec{},
			},
			&corev1.LimitRange{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "LimitRange",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("lr-%s", utils.RandStr()),
					Namespace: nsName,
				},
				Spec: corev1.LimitRangeSpec{},
			},
			&policyv1.PodDisruptionBudget{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "policy/v1",
					Kind:       "PodDisruptionBudget",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("pdb-%s", utils.RandStr()),
					Namespace: nsName,
				},
				Spec: policyv1.PodDisruptionBudgetSpec{
					Selector: &metav1.LabelSelector{},
				},
			},
			&corev1.ServiceAccount{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "ServiceAccount",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("sa-%s", utils.RandStr()),
					Namespace: nsName,
				},
			},
			&corev1.Secret{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Secret",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("secret-%s", utils.RandStr()),
					Namespace: nsName,
				},
				Type: corev1.SecretTypeOpaque,
			},
			&corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "ConfigMap",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("cm-%s", utils.RandStr()),
					Namespace: nsName,
				},
			},
			&storagev1.StorageClass{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "storage.k8s.io/v1",
					Kind:       "StorageClass",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("sc-%s", utils.RandStr()),
				},
				Provisioner: "kubernetes.io/no-provisioner",
			},
			&corev1.PersistentVolume{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "PersistentVolume",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("pv-%s", utils.RandStr()),
				},
				Spec: corev1.PersistentVolumeSpec{
					Capacity: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse("1Gi"),
					},
					AccessModes: []corev1.PersistentVolumeAccessMode{
						corev1.ReadWriteOnce,
					},
					PersistentVolumeSource: corev1.PersistentVolumeSource{
						HostPath: &corev1.HostPathVolumeSource{
							Path: "/mnt/data",
						},
					},
				},
			},
			&corev1.PersistentVolumeClaim{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "PersistentVolumeClaim",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("pvc-%s", utils.RandStr()),
					Namespace: nsName,
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{
						corev1.ReadWriteOnce,
					},
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("1Gi"),
						},
					},
				},
			},
			&apiextensionsv1.CustomResourceDefinition{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "apiextensions.k8s.io/v1",
					Kind:       "CustomResourceDefinition",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "bars.example.com",
				},
				Spec: apiextensionsv1.CustomResourceDefinitionSpec{
					Group: "example.com",
					Names: apiextensionsv1.CustomResourceDefinitionNames{
						Plural:   "bars",
						Kind:     "Bar",
						Singular: "bar",
					},
					Scope: apiextensionsv1.NamespaceScoped,
					Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
						Name:    "v1",
						Served:  true,
						Storage: true,
						Schema: &apiextensionsv1.CustomResourceValidation{
							OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
								Type: "object",
								Properties: map[string]apiextensionsv1.JSONSchemaProps{
									"spec": {
										Type: "object",
										Properties: map[string]apiextensionsv1.JSONSchemaProps{
											"placeholder": {
												Type: "string",
											},
										},
									},
								},
							},
						},
					}},
				},
			},
			&networkingv1.IngressClass{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "networking.k8s.io/v1",
					Kind:       "IngressClass",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("ic-%s", utils.RandStr()),
				},
				Spec: networkingv1.IngressClassSpec{
					Controller: "example.com/ingress-controller",
				},
			},
			// Wave 2 objects.
			&rbacv1.ClusterRole{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "rbac.authorization.k8s.io/v1",
					Kind:       "ClusterRole",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("cr-%s", utils.RandStr()),
				},
				Rules: []rbacv1.PolicyRule{},
			},
			&rbacv1.Role{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "rbac.authorization.k8s.io/v1",
					Kind:       "Role",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("role-%s", utils.RandStr()),
					Namespace: nsName,
				},
				Rules: []rbacv1.PolicyRule{},
			},
			// Wave 3 objects.
			&rbacv1.ClusterRoleBinding{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "rbac.authorization.k8s.io/v1",
					Kind:       "ClusterRoleBinding",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("crb-%s", utils.RandStr()),
				},
				Subjects: []rbacv1.Subject{},
				RoleRef: rbacv1.RoleRef{
					APIGroup: "rbac.authorization.k8s.io",
					Kind:     "ClusterRole",
					Name:     "dummy",
				},
			},
			&rbacv1.RoleBinding{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "rbac.authorization.k8s.io/v1",
					Kind:       "RoleBinding",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("rb-%s", utils.RandStr()),
					Namespace: nsName,
				},
				Subjects: []rbacv1.Subject{},
				RoleRef: rbacv1.RoleRef{
					APIGroup: "rbac.authorization.k8s.io",
					Kind:     "Role",
					Name:     "dummy",
				},
			},
			// Wave 4 objects.
			&corev1.Service{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Service",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("svc-%s", utils.RandStr()),
					Namespace: nsName,
				},
				Spec: corev1.ServiceSpec{
					Selector: map[string]string{},
					Ports: []corev1.ServicePort{
						{
							Name: "http",
							Port: 80,
						},
					},
				},
			},
			&appsv1.DaemonSet{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "apps/v1",
					Kind:       "DaemonSet",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("ds-%s", utils.RandStr()),
					Namespace: nsName,
				},
				Spec: appsv1.DaemonSetSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							dummyLabelKey: dummyLabelValue1,
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								dummyLabelKey: dummyLabelValue1,
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "busybox",
									Image: "busybox",
								},
							},
						},
					},
				},
			},
			&corev1.Pod{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Pod",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("pod-%s", utils.RandStr()),
					Namespace: nsName,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "busybox",
							Image: "busybox",
						},
					},
				},
			},
			&corev1.ReplicationController{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "ReplicationController",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("rc-%s", utils.RandStr()),
					Namespace: nsName,
				},
				Spec: corev1.ReplicationControllerSpec{
					Selector: map[string]string{
						dummyLabelKey: dummyLabelValue2,
					},
					Template: &corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								dummyLabelKey: dummyLabelValue2,
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "busybox",
									Image: "busybox",
								},
							},
						},
					},
				},
			},
			&appsv1.ReplicaSet{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "apps/v1",
					Kind:       "ReplicaSet",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("rs-%s", utils.RandStr()),
					Namespace: nsName,
				},
				Spec: appsv1.ReplicaSetSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							dummyLabelKey: dummyLabelValue3,
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								dummyLabelKey: dummyLabelValue3,
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "busybox",
									Image: "busybox",
								},
							},
						},
					},
				},
			},
			&appsv1.Deployment{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("deploy-%s", utils.RandStr()),
					Namespace: nsName,
				},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							dummyLabelKey: dummyLabelValue4,
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								dummyLabelKey: dummyLabelValue4,
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "busybox",
									Image: "busybox",
								},
							},
						},
					},
				},
			},
			&autoscalingv1.HorizontalPodAutoscaler{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "autoscaling/v1",
					Kind:       "HorizontalPodAutoscaler",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("hpa-%s", utils.RandStr()),
					Namespace: nsName,
				},
				Spec: autoscalingv1.HorizontalPodAutoscalerSpec{
					ScaleTargetRef: autoscalingv1.CrossVersionObjectReference{
						Kind: "Deployment",
						Name: "dummy",
					},
					MaxReplicas: 10,
					MinReplicas: ptr.To(int32(1)),
				},
			},
			&appsv1.StatefulSet{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "apps/v1",
					Kind:       "StatefulSet",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("sts-%s", utils.RandStr()),
					Namespace: nsName,
				},
				Spec: appsv1.StatefulSetSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							dummyLabelKey: dummyLabelValue5,
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								dummyLabelKey: dummyLabelValue5,
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "busybox",
									Image: "busybox",
								},
							},
						},
					},
				},
			},
			&batchv1.Job{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "batch/v1",
					Kind:       "Job",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("job-%s", utils.RandStr()),
					Namespace: nsName,
				},
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "busybox",
									Image: "busybox",
								},
							},
							RestartPolicy: corev1.RestartPolicyNever,
						},
					},
				},
			},
			&batchv1.CronJob{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "batch/v1",
					Kind:       "CronJob",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("cj-%s", utils.RandStr()),
					Namespace: nsName,
				},
				Spec: batchv1.CronJobSpec{
					JobTemplate: batchv1.JobTemplateSpec{
						Spec: batchv1.JobSpec{
							Template: corev1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{},
								Spec: corev1.PodSpec{
									Containers: []corev1.Container{
										{
											Name:  "busybox",
											Image: "busybox",
										},
									},
									RestartPolicy: corev1.RestartPolicyNever,
								},
							},
						},
					},
					Schedule: "*/1 * * * *",
				},
			},
			&networkingv1.Ingress{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "networking.k8s.io/v1",
					Kind:       "Ingress",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("ing-%s", utils.RandStr()),
					Namespace: nsName,
				},
				Spec: networkingv1.IngressSpec{
					Rules: []networkingv1.IngressRule{
						{
							Host: "example.com",
							IngressRuleValue: networkingv1.IngressRuleValue{
								HTTP: &networkingv1.HTTPIngressRuleValue{
									Paths: []networkingv1.HTTPIngressPath{
										{
											Path:     "/",
											PathType: ptr.To(networkingv1.PathTypePrefix),
											Backend: networkingv1.IngressBackend{
												Service: &networkingv1.IngressServiceBackend{
													Name: "placeholder",
													Port: networkingv1.ServiceBackendPort{
														Number: 80,
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			// Wave 5 objects.
			// The APIService object is not included due to setup complications.
			&admissionregistrationv1.ValidatingWebhookConfiguration{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "admissionregistration.k8s.io/v1",
					Kind:       "ValidatingWebhookConfiguration",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("vwc-%s", utils.RandStr()),
				},
				Webhooks: []admissionregistrationv1.ValidatingWebhook{},
			},
			&admissionregistrationv1.MutatingWebhookConfiguration{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "admissionregistration.k8s.io/v1",
					Kind:       "MutatingWebhookConfiguration",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("mwc-%s", utils.RandStr()),
				},
				Webhooks: []admissionregistrationv1.MutatingWebhook{},
			},
			// Unknown resource types (no wave assigned by default); should always get processed at last.
			&discoveryv1.EndpointSlice{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "discovery.k8s.io/v1",
					Kind:       "EndpointSlice",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("eps-%s", utils.RandStr()),
					Namespace: nsName,
				},
				AddressType: discoveryv1.AddressTypeIPv4,
				Endpoints:   []discoveryv1.Endpoint{},
			},
		}

		BeforeAll(func() {
			allManifestJSONByteArrs := make([][]byte, 0, len(objectsOfVariousResourceTypes)+1)

			// Prepare a NS object.
			regularNS := ns.DeepCopy()
			regularNS.Name = nsName
			regularNSJSON := marshalK8sObjJSON(regularNS)
			allManifestJSONByteArrs = append(allManifestJSONByteArrs, regularNSJSON)

			// Prepare all other objects.
			for idx := range objectsOfVariousResourceTypes {
				obj := objectsOfVariousResourceTypes[idx]
				allManifestJSONByteArrs = append(allManifestJSONByteArrs, marshalK8sObjJSON(obj))
			}
			// Shuffle the manifest JSONs.
			rand.Shuffle(len(allManifestJSONByteArrs), func(i, j int) {
				allManifestJSONByteArrs[i], allManifestJSONByteArrs[j] = allManifestJSONByteArrs[j], allManifestJSONByteArrs[i]
			})

			// Create a new Work object with all the manifest JSONs.
			createWorkObject(workName, memberReservedNSName3, nil, nil, allManifestJSONByteArrs...)
		})

		// For simplicity reasons, this test case will skip some of the regular apply op result verification
		// (finalizer check, AppliedWork object check, etc.), as they have been repeatedly verified in different
		// test cases under similar conditions.

		It("should update the Work object status", func() {
			Eventually(func() error {
				work := &fleetv1beta1.Work{}
				if err := hubClient.Get(ctx, client.ObjectKey{Name: workName, Namespace: memberReservedNSName3}, work); err != nil {
					return fmt.Errorf("failed to retrieve the Work object: %w", err)
				}

				// Compare only the work conditions for simplicity reasons.
				wantWorkConds := []metav1.Condition{
					{
						Type:   fleetv1beta1.WorkConditionTypeApplied,
						Status: metav1.ConditionTrue,
						Reason: condition.WorkAllManifestsAppliedReason,
					},
					{
						Type:   fleetv1beta1.WorkConditionTypeAvailable,
						Status: metav1.ConditionFalse,
						// In the current test environment some API objects will never become available.
						Reason: condition.WorkNotAllManifestsAvailableReason,
					},
				}
				for idx := range wantWorkConds {
					wantWorkConds[idx].ObservedGeneration = work.Generation
				}
				if diff := cmp.Diff(
					work.Status.Conditions, wantWorkConds,
					ignoreFieldConditionLTTMsg,
				); diff != "" {
					return fmt.Errorf("Work status conditions mismatch (-got, +want):\n%s", diff)
				}
				return nil
				// As each wave starts with a fixed delay, and this specific test case involves all the waves,
				// the test spec here uses a longer timeout and interval.
			}, eventuallyDuration*10, eventuallyInterval*5).Should(Succeed(), "Failed to update work status")
		})

		It("should process manifests in waves", func() {
			creationTimestampsPerWave := make(map[waveNumber][]metav1.Time, len(defaultWaveNumberByResourceType))
			for idx := range objectsOfVariousResourceTypes {
				obj := objectsOfVariousResourceTypes[idx]
				objGK := obj.GetObjectKind().GroupVersionKind().GroupKind()
				objVer := obj.GetObjectKind().GroupVersionKind().Version
				objResTyp, err := memberClient3.RESTMapper().RESTMapping(objGK, objVer)
				Expect(err).NotTo(HaveOccurred(), "Failed to get the resource type of an object")

				processedObj := obj.DeepCopyObject().(client.Object)
				Expect(memberClient3.Get(ctx, client.ObjectKey{Namespace: obj.GetNamespace(), Name: obj.GetName()}, processedObj)).To(Succeed(), "Failed to get a placed object")

				waveNum, ok := defaultWaveNumberByResourceType[objResTyp.Resource.Resource]
				if !ok {
					waveNum = lastWave
				}

				timestamps := creationTimestampsPerWave[waveNum]
				if timestamps == nil {
					timestamps = make([]metav1.Time, 0, 1)
				}
				timestamps = append(timestamps, processedObj.GetCreationTimestamp())
				creationTimestampsPerWave[waveNum] = timestamps
			}

			expectedWaveNums := []waveNumber{0, 1, 2, 3, 4, 5, lastWave}
			// Do a sanity check.
			Expect(len(creationTimestampsPerWave)).To(Equal(len(expectedWaveNums)), "The number of waves does not match the expectation")
			var observedLatestCreationTimestampInLastWave *metav1.Time
			for _, waveNum := range expectedWaveNums {
				By(fmt.Sprintf("checking wave %d", waveNum))

				timestamps := creationTimestampsPerWave[waveNum]
				// Do a sanity check.
				Expect(timestamps).NotTo(BeEmpty(), "No creation timestamps recorded for a wave")

				// Check that timestamps in the same wave are close enough.
				slices.SortFunc(timestamps, func(a, b metav1.Time) int {
					return a.Time.Compare(b.Time)
				})

				earliest := timestamps[0]
				latest := timestamps[len(timestamps)-1]
				gapWithinWave := latest.Sub(earliest.Time)
				// Normally all resources in the same wave should be created within a very short time window,
				// usually within a few tens of milliseconds; here the test spec uses a more forgiving threshold
				// of 2 seconds to avoid flakiness.
				Expect(gapWithinWave).To(BeNumerically("<", time.Second*2), "The creation time gap between resources in the same wave is larger than expected")

				if observedLatestCreationTimestampInLastWave != nil {
					// Check that the current wave is processed after the last wave with a fixed delay.
					gapBetweenWaves := earliest.Sub(observedLatestCreationTimestampInLastWave.Time)
					Expect(gapBetweenWaves).To(BeNumerically(">=", parallelizerFixedDelay), "The creation time gap between resources in different waves is less than the fixed delay")
					Expect(gapBetweenWaves).To(BeNumerically("<", parallelizerFixedDelay*2), "The creation time gap between resources in different waves is at least twice as large as the fixed delay")
				}

				observedLatestCreationTimestampInLastWave = &timestamps[len(timestamps)-1]
			}
		})

		AfterAll(func() {
			// Delete the Work object and related resources.
			deleteWorkObject(workName, memberReservedNSName3)

			// Remove all the placed objects if they still exist.
			for idx := range objectsOfVariousResourceTypes {
				objCopy := objectsOfVariousResourceTypes[idx].DeepCopyObject().(client.Object)
				gvk := objCopy.GetObjectKind().GroupVersionKind()
				switch {
				case gvk.Group == "" && gvk.Kind == "PersistentVolume":
					// Skip the PV resources as their deletion might get stuck in the test environment.
					// This in most cases should have no side effect as the tests do not reuse namespaces and
					// the resources have random suffixes in their names.
					continue
				case gvk.Group == "" && gvk.Kind == "PersistentVolumeClaim":
					// For the same reason as above, skip the PVC resources.
					continue
				case gvk.Group == "" && gvk.Kind == "ReplicationController":
					// For the same reason as above, skip the RC resources.
					continue
				case gvk.Group == "batch" && gvk.Kind == "Job":
					// For the same reason as above, skip the Job resources.
					continue
				}
				placeholder := objCopy.DeepCopyObject().(client.Object)

				Eventually(func() error {
					if err := memberClient3.Delete(ctx, objCopy); err != nil && !errors.IsNotFound(err) {
						return fmt.Errorf("failed to delete the object: %w", err)
					}

					// Check that the object has been deleted.
					if err := memberClient3.Get(ctx, client.ObjectKey{Namespace: objCopy.GetNamespace(), Name: objCopy.GetName()}, placeholder); !errors.IsNotFound(err) {
						return fmt.Errorf("object still exists or an unexpected error occurred: %w", err)
					}
					return nil
				}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove a placed object (idx: %d)", idx)
			}

			// Ensure that the AppliedWork object has been removed.
			appliedWorkRemovedActual := appliedWorkRemovedActual(memberClient3, workName)
			Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the AppliedWork object")

			workRemovedActual := testutilsactuals.WorkObjectRemovedActual(ctx, hubClient, workName, memberReservedNSName3)
			Eventually(workRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove the Work object")
			// The environment prepared by the envtest package does not support namespace
			// deletion; consequently this test suite would not attempt to verify its deletion.
		})
	})
})
