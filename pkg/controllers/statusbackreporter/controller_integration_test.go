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

package statusbackreporter

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
	testutilsactuals "github.com/kubefleet-dev/kubefleet/test/utils/actuals"
	testutilsresource "github.com/kubefleet-dev/kubefleet/test/utils/resource"
)

const (
	// The linter in use mistakenly recognizes some of the names as potential hardcoded credentials;
	// as a result, gosec linter warnings are suppressed for these variables.
	crpWorkNameTemplate = "%s-work-%s" //nolint:gosec
	nsNameTemplate      = "ns-%s"
	crpNameTemplate     = "crp-%s"

	deployName = "app"

	workOrManifestAppliedReason  = "MarkedAsApplied"
	workOrManifestAppliedMessage = "the object is marked as applied"
	deployAvailableReason        = "MarkedAsAvailable"
	deployAvailableMessage       = "the object is marked as available"
)

const (
	eventuallyDuration = time.Second * 10
	eventuallyInterval = time.Second * 1
)

var (
	nsTemplate = corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Namespace",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: nsName,
		},
	}
)

// createWorkObject creates a new Work object with the given work name/namespace, placement object name/namespace, report back strategy, and raw manifest JSONs.
func createWorkObject(workName, memberClusterReservedNSName, placementObjName, placementObjNSName string, reportBackStrategy *placementv1beta1.ReportBackStrategy, rawManifestJSON ...[]byte) {
	work := testutilsresource.WorkObjectForTest(workName, memberClusterReservedNSName, placementObjName, placementObjNSName, nil, reportBackStrategy, rawManifestJSON...)
	Expect(hubClient.Create(ctx, work)).To(Succeed())
}

func marshalK8sObjJSON(obj runtime.Object) []byte {
	json, err := testutilsresource.MarshalRuntimeObjToJSONForTest(obj)
	Expect(err).To(BeNil(), "Failed to marshal the k8s object to JSON")
	return json
}

func prepareStatusWrapperData(obj runtime.Object) ([]byte, error) {
	unstructuredObjMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, fmt.Errorf("failed to convert to unstructured object: %w", err)
	}
	unstructuredObj := &unstructured.Unstructured{Object: unstructuredObjMap}
	statusBackReportingWrapper := make(map[string]interface{})
	statusBackReportingWrapper["apiVersion"] = unstructuredObj.GetAPIVersion()
	statusBackReportingWrapper["kind"] = unstructuredObj.GetKind()
	statusBackReportingWrapper["status"] = unstructuredObj.Object["status"]
	statusBackReportingWrapperData, err := json.Marshal(statusBackReportingWrapper)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal status back-reporting wrapper data: %w", err)
	}
	return statusBackReportingWrapperData, nil
}

func ensureWorkObjectDeletion(workName string) {
	// Retrieve the Work object.
	work := &placementv1beta1.Work{
		ObjectMeta: metav1.ObjectMeta{
			Name:      workName,
			Namespace: memberReservedNSName,
		},
	}
	Expect(hubClient.Delete(ctx, work)).To(Succeed(), "Failed to delete the Work object")

	workObjRemovedActual := testutilsactuals.WorkObjectRemovedActual(ctx, hubClient, workName, memberReservedNSName)
	Eventually(workObjRemovedActual, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove work object")
}

var _ = Describe("back-reporting status", func() {
	Context("back-report status for deployments (CRP)", Ordered, func() {
		crpName := fmt.Sprintf(crpNameTemplate, utils.RandStr())
		workName := fmt.Sprintf(crpWorkNameTemplate, crpName, utils.RandStr())
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, utils.RandStr())

		var ns *corev1.Namespace
		var deploy *appsv1.Deployment
		var now metav1.Time

		BeforeAll(func() {
			now = metav1.Now().Rfc3339Copy()

			// Create the namespace.
			ns = nsTemplate.DeepCopy()
			nsJSON := marshalK8sObjJSON(ns)
			ns.Name = nsName
			Expect(hubClient.Create(ctx, ns)).To(Succeed())

			// Create the deployment.
			deploy = &appsv1.Deployment{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Deployment",
					APIVersion: "apps/v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      deployName,
					Namespace: nsName,
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: ptr.To(int32(1)),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "nginx",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "nginx",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "nginx",
									Image: "nginx",
									Ports: []corev1.ContainerPort{
										{
											ContainerPort: 80,
										},
									},
								},
							},
						},
					},
				},
			}
			deployJSON := marshalK8sObjJSON(deploy)
			Expect(hubClient.Create(ctx, deploy)).To(Succeed())

			// Create the CRP.
			crp := &placementv1beta1.ClusterResourcePlacement{
				ObjectMeta: metav1.ObjectMeta{
					Name: crpName,
				},
				Spec: placementv1beta1.PlacementSpec{
					ResourceSelectors: []placementv1beta1.ResourceSelectorTerm{
						{
							Group:   "",
							Version: "v1",
							Kind:    "Namespace",
							Name:    nsName,
						},
					},
					Policy: &placementv1beta1.PlacementPolicy{
						PlacementType: placementv1beta1.PickFixedPlacementType,
						ClusterNames: []string{
							cluster1,
						},
					},
					Strategy: placementv1beta1.RolloutStrategy{
						ReportBackStrategy: &placementv1beta1.ReportBackStrategy{
							Type:        placementv1beta1.ReportBackStrategyTypeMirror,
							Destination: ptr.To(placementv1beta1.ReportBackDestinationOriginalResource),
						},
					},
				},
			}
			Expect(hubClient.Create(ctx, crp)).To(Succeed())

			// Create the Work object.
			reportBackStrategy := &placementv1beta1.ReportBackStrategy{
				Type:        placementv1beta1.ReportBackStrategyTypeMirror,
				Destination: ptr.To(placementv1beta1.ReportBackDestinationOriginalResource),
			}
			createWorkObject(workName, memberReservedNSName, crpName, "", reportBackStrategy, nsJSON, deployJSON)
		})

		It("can update CRP status", func() {
			Eventually(func() error {
				crp := &placementv1beta1.ClusterResourcePlacement{}
				if err := hubClient.Get(ctx, client.ObjectKey{Name: crpName}, crp); err != nil {
					return fmt.Errorf("failed to retrieve CRP object: %w", err)
				}

				crp.Status = placementv1beta1.PlacementStatus{
					SelectedResources: []placementv1beta1.ResourceIdentifier{
						{
							Group:   "",
							Version: "v1",
							Kind:    "Namespace",
							Name:    nsName,
						},
						{
							Group:     "apps",
							Version:   "v1",
							Kind:      "Deployment",
							Name:      deployName,
							Namespace: nsName,
						},
					},
				}
				if err := hubClient.Status().Update(ctx, crp); err != nil {
					return fmt.Errorf("failed to update CRP status: %w", err)
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).To(Succeed(), "Failed to update CRP status")
		})

		It("can update work status", func() {
			Eventually(func() error {
				work := &placementv1beta1.Work{}
				if err := hubClient.Get(ctx, client.ObjectKey{Namespace: memberReservedNSName, Name: workName}, work); err != nil {
					return fmt.Errorf("failed to retrieve work object: %w", err)
				}

				deployWithStatus := deploy.DeepCopy()
				deployWithStatus.Status = appsv1.DeploymentStatus{
					ObservedGeneration:  deploy.Generation,
					Replicas:            1,
					UpdatedReplicas:     1,
					AvailableReplicas:   1,
					ReadyReplicas:       1,
					UnavailableReplicas: 0,
					Conditions: []appsv1.DeploymentCondition{
						{
							Type:               appsv1.DeploymentAvailable,
							Status:             corev1.ConditionTrue,
							LastUpdateTime:     now,
							LastTransitionTime: now,
							Reason:             deployAvailableReason,
							Message:            deployAvailableMessage,
						},
					},
				}

				statusBackReportingWrapperData, err := prepareStatusWrapperData(deployWithStatus)
				if err != nil {
					return fmt.Errorf("failed to prepare status wrapper data: %w", err)
				}

				work.Status = placementv1beta1.WorkStatus{
					Conditions: []metav1.Condition{
						{
							Type:               placementv1beta1.WorkConditionTypeApplied,
							Status:             metav1.ConditionTrue,
							Reason:             workOrManifestAppliedReason,
							Message:            workOrManifestAppliedMessage,
							ObservedGeneration: 1,
							LastTransitionTime: now,
						},
					},
					ManifestConditions: []placementv1beta1.ManifestCondition{
						{
							Identifier: placementv1beta1.WorkResourceIdentifier{
								Ordinal:   0,
								Group:     "",
								Version:   "v1",
								Kind:      "Namespace",
								Resource:  "namespaces",
								Namespace: "",
								Name:      nsName,
							},
							Conditions: []metav1.Condition{
								{
									Type:               placementv1beta1.WorkConditionTypeApplied,
									Status:             metav1.ConditionTrue,
									Reason:             workOrManifestAppliedReason,
									Message:            workOrManifestAppliedMessage,
									ObservedGeneration: 1,
									LastTransitionTime: now,
								},
							},
						},
						{
							Identifier: placementv1beta1.WorkResourceIdentifier{
								Ordinal:   1,
								Group:     "apps",
								Version:   "v1",
								Kind:      "Deployment",
								Resource:  "deployments",
								Namespace: nsName,
								Name:      deployName,
							},
							Conditions: []metav1.Condition{
								{
									Type:               placementv1beta1.WorkConditionTypeApplied,
									Status:             metav1.ConditionTrue,
									Reason:             workOrManifestAppliedReason,
									Message:            workOrManifestAppliedMessage,
									ObservedGeneration: 1,
									LastTransitionTime: now,
								},
							},
							BackReportedStatus: &placementv1beta1.BackReportedStatus{
								ObservedStatus: runtime.RawExtension{
									Raw: statusBackReportingWrapperData,
								},
								ObservationTime: now,
							},
						},
					},
				}
				if err := hubClient.Status().Update(ctx, work); err != nil {
					return fmt.Errorf("failed to update Work object status: %w", err)
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to update Work object status")
		})

		It("should back-report status to original resource", func() {
			wantDeployStatus := appsv1.DeploymentStatus{
				ObservedGeneration:  deploy.Generation,
				Replicas:            1,
				UpdatedReplicas:     1,
				AvailableReplicas:   1,
				ReadyReplicas:       1,
				UnavailableReplicas: 0,
				Conditions: []appsv1.DeploymentCondition{
					{
						Type:               appsv1.DeploymentAvailable,
						Status:             corev1.ConditionTrue,
						LastUpdateTime:     now,
						LastTransitionTime: now,
						Reason:             deployAvailableReason,
						Message:            deployAvailableMessage,
					},
				},
			}

			Eventually(func() error {
				deploy := &appsv1.Deployment{}
				if err := hubClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: deployName}, deploy); err != nil {
					return fmt.Errorf("failed to retrieve Deployment object: %w", err)
				}

				if diff := cmp.Diff(deploy.Status, wantDeployStatus); diff != "" {
					return fmt.Errorf("deploy status diff (-got, +want):\n%s", diff)
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to back-report status")
		})

		AfterAll(func() {
			// Delete the Work object.
			ensureWorkObjectDeletion(workName)

			// Delete the Deployment object.
			Eventually(func() error {
				deploy := &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: nsName,
						Name:      deployName,
					},
				}
				if err := hubClient.Delete(ctx, deploy); err != nil && !errors.IsNotFound(err) {
					return fmt.Errorf("failed to delete Deployment object: %w", err)
				}
				if err := hubClient.Get(ctx, client.ObjectKey{Name: deployName, Namespace: nsName}, deploy); err != nil && !errors.IsNotFound(err) {
					return fmt.Errorf("Deployment object still exists or an unexpected error occurred: %w", err)
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to remove Deployment object")

			// The environment prepared by the envtest package does not support namespace
			// deletion; consequently this test suite would not attempt to verify its deletion.
		})
	})
})
