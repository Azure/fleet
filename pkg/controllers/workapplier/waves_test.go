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
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"

	"github.com/google/go-cmp/cmp"
	fleetv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
)

// TestOrganizeBundlesIntoProcessingWaves tests the organizeBundlesIntoProcessingWaves function.
func TestOrganizeBundlesIntoProcessingWaves(t *testing.T) {
	work := &fleetv1beta1.Work{
		ObjectMeta: metav1.ObjectMeta{
			Name: workName,
		},
	}
	workRef := klog.KObj(work)

	testCases := []struct {
		name      string
		bundles   []*manifestProcessingBundle
		wantWaves []*bundleProcessingWave
	}{
		{
			name: "single bundle (known resource) into single wave",
			bundles: []*manifestProcessingBundle{
				{
					// Note: the IDs are added here purely for identification reasons; they are
					// not consistent with other parts of the bundle, and do not reflect
					// how the work applier actually sees bundles in an actual processing run.
					//
					// The same applies to other test cases in this spec.
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 0,
					},
					// Note: the version part does not matter, as Kubernetes requires that one
					// resource must be uniquely identified by the combo of its API group, resource type,
					// namespace (if applicable), and name, but not the version. The information
					// here is added for completeness reasons.
					//
					// The same applies to other tests cases in this spec.
					gvr: &schema.GroupVersionResource{
						Group:    "",
						Version:  "v1",
						Resource: "namespaces",
					},
				},
			},
			wantWaves: []*bundleProcessingWave{
				{
					num: 0,
					bundles: []*manifestProcessingBundle{
						{
							id: &fleetv1beta1.WorkResourceIdentifier{
								Ordinal: 0,
							},
							gvr: &schema.GroupVersionResource{
								Group:    "",
								Version:  "v1",
								Resource: "namespaces",
							},
						},
					},
				},
			},
		},
		{
			// Normally this test case will never occur; it is added for completeness reasons.
			name: "bundle with decoding errors (no GVR)",
			bundles: []*manifestProcessingBundle{
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 1,
					},
					gvr: &schema.GroupVersionResource{
						Group:    "",
						Version:  "v1",
						Resource: "namespaces",
					},
				},
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 2,
					},
					gvr: nil,
				},
			},
			wantWaves: []*bundleProcessingWave{
				{
					num: 0,
					bundles: []*manifestProcessingBundle{
						{
							id: &fleetv1beta1.WorkResourceIdentifier{
								Ordinal: 1,
							},
							gvr: &schema.GroupVersionResource{
								Group:    "",
								Version:  "v1",
								Resource: "namespaces",
							},
						},
					},
				},
			},
		},
		{
			name: "bundle with decoding errors (invalid JSON)",
			bundles: []*manifestProcessingBundle{
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 0,
					},
					gvr: &schema.GroupVersionResource{
						Group:    "",
						Version:  "v1",
						Resource: "namespaces",
					},
				},
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 1,
					},
					gvr:                  &schema.GroupVersionResource{},
					applyOrReportDiffErr: fmt.Errorf("failed to unmarshal JSON"),
				},
			},
			wantWaves: []*bundleProcessingWave{
				{
					num: 0,
					bundles: []*manifestProcessingBundle{
						{
							id: &fleetv1beta1.WorkResourceIdentifier{
								Ordinal: 0,
							},
							gvr: &schema.GroupVersionResource{
								Group:    "",
								Version:  "v1",
								Resource: "namespaces",
							},
						},
					},
				},
			},
		},
		{
			name: "bundle with decoding errors (unregistered API)",
			bundles: []*manifestProcessingBundle{
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 0,
					},
					gvr: &schema.GroupVersionResource{
						Group:    "",
						Version:  "v1",
						Resource: "namespaces",
					},
				},
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 1,
					},
					gvr:                  &schema.GroupVersionResource{},
					applyOrReportDiffErr: fmt.Errorf("failed to find GVR from member cluster client REST mapping"),
				},
			},
			wantWaves: []*bundleProcessingWave{
				{
					num: 0,
					bundles: []*manifestProcessingBundle{
						{
							id: &fleetv1beta1.WorkResourceIdentifier{
								Ordinal: 0,
							},
							gvr: &schema.GroupVersionResource{
								Group:    "",
								Version:  "v1",
								Resource: "namespaces",
							},
						},
					},
				},
			},
		},
		{
			name: "bundle with unknown resource type",
			bundles: []*manifestProcessingBundle{
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 0,
					},
					gvr: &schema.GroupVersionResource{
						Group:    "",
						Version:  "v1",
						Resource: "namespaces",
					},
				},
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 1,
					},
					gvr: &schema.GroupVersionResource{
						Group:    "",
						Version:  "v1",
						Resource: "placeholders",
					},
				},
			},
			wantWaves: []*bundleProcessingWave{
				{
					num: 0,
					bundles: []*manifestProcessingBundle{
						{
							id: &fleetv1beta1.WorkResourceIdentifier{
								Ordinal: 0,
							},
							gvr: &schema.GroupVersionResource{
								Group:    "",
								Version:  "v1",
								Resource: "namespaces",
							},
						},
					},
				},
				{
					num: lastWave,
					bundles: []*manifestProcessingBundle{
						{
							id: &fleetv1beta1.WorkResourceIdentifier{
								Ordinal: 1,
							},
							gvr: &schema.GroupVersionResource{
								Group:    "",
								Version:  "v1",
								Resource: "placeholders",
							},
						},
					},
				},
			},
		},
		{
			name: "bundle with known resource type but different API group",
			bundles: []*manifestProcessingBundle{
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 0,
					},
					gvr: &schema.GroupVersionResource{
						Group:    "",
						Version:  "v1",
						Resource: "namespaces",
					},
				},
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 1,
					},
					gvr: &schema.GroupVersionResource{
						Group:    "dummy",
						Version:  "v1",
						Resource: "deployments",
					},
				},
			},
			wantWaves: []*bundleProcessingWave{
				{
					num: 0,
					bundles: []*manifestProcessingBundle{
						{
							id: &fleetv1beta1.WorkResourceIdentifier{
								Ordinal: 0,
							},
							gvr: &schema.GroupVersionResource{
								Group:    "",
								Version:  "v1",
								Resource: "namespaces",
							},
						},
					},
				},
				{
					num: lastWave,
					bundles: []*manifestProcessingBundle{
						{
							id: &fleetv1beta1.WorkResourceIdentifier{
								Ordinal: 1,
							},
							gvr: &schema.GroupVersionResource{
								Group:    "dummy",
								Version:  "v1",
								Resource: "deployments",
							},
						},
					},
				},
			},
		},
		{
			name: "bundle with unknown resource type from unknown API group",
			bundles: []*manifestProcessingBundle{
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 0,
					},
					gvr: &schema.GroupVersionResource{
						Group:    "",
						Version:  "v1",
						Resource: "namespaces",
					},
				},
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 1,
					},
					gvr: &schema.GroupVersionResource{
						Group:    "dummy",
						Version:  "v10",
						Resource: "placeholders",
					},
				},
			},
			wantWaves: []*bundleProcessingWave{
				{
					num: 0,
					bundles: []*manifestProcessingBundle{
						{
							id: &fleetv1beta1.WorkResourceIdentifier{
								Ordinal: 0,
							},
							gvr: &schema.GroupVersionResource{
								Group:    "",
								Version:  "v1",
								Resource: "namespaces",
							},
						},
					},
				},
				{
					num: lastWave,
					bundles: []*manifestProcessingBundle{
						{
							id: &fleetv1beta1.WorkResourceIdentifier{
								Ordinal: 1,
							},
							gvr: &schema.GroupVersionResource{
								Group:    "dummy",
								Version:  "v10",
								Resource: "placeholders",
							},
						},
					},
				},
			},
		},
		{
			name: "mixed",
			// The bundles below feature all known resource types from known API groups
			// (in reverse order by wave number), plus unknown resources and bundles with errors.
			bundles: []*manifestProcessingBundle{
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 0,
					},
					gvr: &schema.GroupVersionResource{
						Group:    "admissionregistration.k8s.io",
						Version:  "v1",
						Resource: "mutatingwebhookconfigurations",
					},
				},
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 1,
					},
					gvr: &schema.GroupVersionResource{
						Group:    "admissionregistration.k8s.io",
						Version:  "v1",
						Resource: "validatingwebhookconfigurations",
					},
				},
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 2,
					},
					gvr: &schema.GroupVersionResource{
						Group:    "apiregistration.k8s.io",
						Version:  "v1",
						Resource: "apiservices",
					},
				},
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 3,
					},
					gvr: &schema.GroupVersionResource{
						Group:    "networking.k8s.io",
						Version:  "v1",
						Resource: "ingresses",
					},
				},
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 4,
					},
					gvr: &schema.GroupVersionResource{
						Group:    "batch",
						Version:  "v1",
						Resource: "cronjobs",
					},
				},
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 5,
					},
					gvr: &schema.GroupVersionResource{
						Group:    "batch",
						Version:  "v1",
						Resource: "jobs",
					},
				},
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 6,
					},
					gvr: &schema.GroupVersionResource{
						Group:    "autoscaling",
						Version:  "v1",
						Resource: "horizontalpodautoscalers",
					},
				},
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 7,
					},
					gvr: &schema.GroupVersionResource{
						Group:    "apps",
						Version:  "v1",
						Resource: "statefulsets",
					},
				},
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 8,
					},
					gvr: &schema.GroupVersionResource{
						Group:    "apps",
						Version:  "v1",
						Resource: "deployments",
					},
				},
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 9,
					},
					gvr: &schema.GroupVersionResource{
						Group:    "apps",
						Version:  "v1",
						Resource: "replicasets",
					},
				},
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 10,
					},
					gvr: &schema.GroupVersionResource{
						Group:    "",
						Version:  "v1",
						Resource: "replicationcontrollers",
					},
				},
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 11,
					},
					gvr: &schema.GroupVersionResource{
						Group:    "",
						Version:  "v1",
						Resource: "pods",
					},
				},
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 12,
					},
					gvr: &schema.GroupVersionResource{
						Group:    "apps",
						Version:  "v1",
						Resource: "daemonsets",
					},
				},
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 13,
					},
					gvr: &schema.GroupVersionResource{
						Group:    "",
						Version:  "v1",
						Resource: "services",
					},
				},
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 14,
					},
					gvr: &schema.GroupVersionResource{
						Group:    "rbac.authorization.k8s.io",
						Version:  "v1",
						Resource: "rolebindings",
					},
				},
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 15,
					},
					gvr: &schema.GroupVersionResource{
						Group:    "rbac.authorization.k8s.io",
						Version:  "v1",
						Resource: "clusterrolebindings",
					},
				},
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 16,
					},
					gvr: &schema.GroupVersionResource{
						Group:    "rbac.authorization.k8s.io",
						Version:  "v1",
						Resource: "roles",
					},
				},
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 17,
					},
					gvr: &schema.GroupVersionResource{
						Group:    "rbac.authorization.k8s.io",
						Version:  "v1",
						Resource: "clusterroles",
					},
				},
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 18,
					},
					gvr: &schema.GroupVersionResource{
						Group:    "networking.k8s.io",
						Version:  "v1",
						Resource: "ingressclasses",
					},
				},
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 19,
					},
					gvr: &schema.GroupVersionResource{
						Group:    "apiextensions.k8s.io",
						Version:  "v1",
						Resource: "customresourcedefinitions",
					},
				},
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 20,
					},
					gvr: &schema.GroupVersionResource{
						Group:    "",
						Version:  "v1",
						Resource: "persistentvolumeclaims",
					},
				},
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 21,
					},
					gvr: &schema.GroupVersionResource{
						Group:    "",
						Version:  "v1",
						Resource: "persistentvolumes",
					},
				},
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 22,
					},
					gvr: &schema.GroupVersionResource{
						Group:    "storage.k8s.io",
						Version:  "v1",
						Resource: "storageclasses",
					},
				},
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 23,
					},
					gvr: &schema.GroupVersionResource{
						Group:    "",
						Version:  "v1",
						Resource: "configmaps",
					},
				},
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 24,
					},
					gvr: &schema.GroupVersionResource{
						Group:    "",
						Version:  "v1",
						Resource: "secrets",
					},
				},
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 25,
					},
					gvr: &schema.GroupVersionResource{
						Group:    "",
						Version:  "v1",
						Resource: "serviceaccounts",
					},
				},
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 26,
					},
					gvr: &schema.GroupVersionResource{
						Group:    "policy",
						Version:  "v1",
						Resource: "poddisruptionbudgets",
					},
				},
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 27,
					},
					gvr: &schema.GroupVersionResource{
						Group:    "policy",
						Version:  "v1",
						Resource: "podsecuritypolicies",
					},
				},
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 28,
					},
					gvr: &schema.GroupVersionResource{
						Group:    "",
						Version:  "v1",
						Resource: "limitranges",
					},
				},
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 29,
					},
					gvr: &schema.GroupVersionResource{
						Group:    "",
						Version:  "v1",
						Resource: "resourcequotas",
					},
				},
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 30,
					},
					gvr: &schema.GroupVersionResource{
						Group:    "networking.k8s.io",
						Version:  "v1",
						Resource: "networkpolicies",
					},
				},
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 31,
					},
					gvr: &schema.GroupVersionResource{
						Group:    "scheduling.k8s.io",
						Version:  "v1",
						Resource: "priorityclasses",
					},
				},
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 32,
					},
					gvr: &schema.GroupVersionResource{
						Group:    "dummy",
						Version:  "v10",
						Resource: "placeholders",
					},
				},
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 33,
					},
					gvr: &schema.GroupVersionResource{
						Group:    "",
						Version:  "v1",
						Resource: "namespaces",
					},
				},
				{
					id: &fleetv1beta1.WorkResourceIdentifier{
						Ordinal: 34,
					},
					gvr:                  &schema.GroupVersionResource{},
					applyOrReportDiffErr: fmt.Errorf("failed to find GVR from member cluster client REST mapping"),
				},
			},
			wantWaves: []*bundleProcessingWave{
				{
					num: 0,
					bundles: []*manifestProcessingBundle{
						{
							id: &fleetv1beta1.WorkResourceIdentifier{
								Ordinal: 31,
							},
							gvr: &schema.GroupVersionResource{
								Group:    "scheduling.k8s.io",
								Version:  "v1",
								Resource: "priorityclasses",
							},
						},
						{
							id: &fleetv1beta1.WorkResourceIdentifier{
								Ordinal: 33,
							},
							gvr: &schema.GroupVersionResource{
								Group:    "",
								Version:  "v1",
								Resource: "namespaces",
							},
						},
					},
				},
				{
					num: 1,
					bundles: []*manifestProcessingBundle{
						{
							id: &fleetv1beta1.WorkResourceIdentifier{
								Ordinal: 18,
							},
							gvr: &schema.GroupVersionResource{
								Group:    "networking.k8s.io",
								Version:  "v1",
								Resource: "ingressclasses",
							},
						},
						{
							id: &fleetv1beta1.WorkResourceIdentifier{
								Ordinal: 19,
							},
							gvr: &schema.GroupVersionResource{
								Group:    "apiextensions.k8s.io",
								Version:  "v1",
								Resource: "customresourcedefinitions",
							},
						},
						{
							id: &fleetv1beta1.WorkResourceIdentifier{
								Ordinal: 20,
							},
							gvr: &schema.GroupVersionResource{
								Group:    "",
								Version:  "v1",
								Resource: "persistentvolumeclaims",
							},
						},
						{
							id: &fleetv1beta1.WorkResourceIdentifier{
								Ordinal: 21,
							},
							gvr: &schema.GroupVersionResource{
								Group:    "",
								Version:  "v1",
								Resource: "persistentvolumes",
							},
						},
						{
							id: &fleetv1beta1.WorkResourceIdentifier{
								Ordinal: 22,
							},
							gvr: &schema.GroupVersionResource{
								Group:    "storage.k8s.io",
								Version:  "v1",
								Resource: "storageclasses",
							},
						},
						{
							id: &fleetv1beta1.WorkResourceIdentifier{
								Ordinal: 23,
							},
							gvr: &schema.GroupVersionResource{
								Group:    "",
								Version:  "v1",
								Resource: "configmaps",
							},
						},
						{
							id: &fleetv1beta1.WorkResourceIdentifier{
								Ordinal: 24,
							},
							gvr: &schema.GroupVersionResource{
								Group:    "",
								Version:  "v1",
								Resource: "secrets",
							},
						},
						{
							id: &fleetv1beta1.WorkResourceIdentifier{
								Ordinal: 25,
							},
							gvr: &schema.GroupVersionResource{
								Group:    "",
								Version:  "v1",
								Resource: "serviceaccounts",
							},
						},
						{
							id: &fleetv1beta1.WorkResourceIdentifier{
								Ordinal: 26,
							},
							gvr: &schema.GroupVersionResource{
								Group:    "policy",
								Version:  "v1",
								Resource: "poddisruptionbudgets",
							},
						},
						{
							id: &fleetv1beta1.WorkResourceIdentifier{
								Ordinal: 27,
							},
							gvr: &schema.GroupVersionResource{
								Group:    "policy",
								Version:  "v1",
								Resource: "podsecuritypolicies",
							},
						},
						{
							id: &fleetv1beta1.WorkResourceIdentifier{
								Ordinal: 28,
							},
							gvr: &schema.GroupVersionResource{
								Group:    "",
								Version:  "v1",
								Resource: "limitranges",
							},
						},
						{
							id: &fleetv1beta1.WorkResourceIdentifier{
								Ordinal: 29,
							},
							gvr: &schema.GroupVersionResource{
								Group:    "",
								Version:  "v1",
								Resource: "resourcequotas",
							},
						},
						{
							id: &fleetv1beta1.WorkResourceIdentifier{
								Ordinal: 30,
							},
							gvr: &schema.GroupVersionResource{
								Group:    "networking.k8s.io",
								Version:  "v1",
								Resource: "networkpolicies",
							},
						},
					},
				},
				{
					num: 2,
					bundles: []*manifestProcessingBundle{
						{
							id: &fleetv1beta1.WorkResourceIdentifier{
								Ordinal: 16,
							},
							gvr: &schema.GroupVersionResource{
								Group:    "rbac.authorization.k8s.io",
								Version:  "v1",
								Resource: "roles",
							},
						},
						{
							id: &fleetv1beta1.WorkResourceIdentifier{
								Ordinal: 17,
							},
							gvr: &schema.GroupVersionResource{
								Group:    "rbac.authorization.k8s.io",
								Version:  "v1",
								Resource: "clusterroles",
							},
						},
					},
				},
				{
					num: 3,
					bundles: []*manifestProcessingBundle{
						{
							id: &fleetv1beta1.WorkResourceIdentifier{
								Ordinal: 14,
							},
							gvr: &schema.GroupVersionResource{
								Group:    "rbac.authorization.k8s.io",
								Version:  "v1",
								Resource: "rolebindings",
							},
						},
						{
							id: &fleetv1beta1.WorkResourceIdentifier{
								Ordinal: 15,
							},
							gvr: &schema.GroupVersionResource{
								Group:    "rbac.authorization.k8s.io",
								Version:  "v1",
								Resource: "clusterrolebindings",
							},
						},
					},
				},
				{
					num: 4,
					bundles: []*manifestProcessingBundle{
						{
							id: &fleetv1beta1.WorkResourceIdentifier{
								Ordinal: 3,
							},
							gvr: &schema.GroupVersionResource{
								Group:    "networking.k8s.io",
								Version:  "v1",
								Resource: "ingresses",
							},
						},
						{
							id: &fleetv1beta1.WorkResourceIdentifier{
								Ordinal: 4,
							},
							gvr: &schema.GroupVersionResource{
								Group:    "batch",
								Version:  "v1",
								Resource: "cronjobs",
							},
						},
						{
							id: &fleetv1beta1.WorkResourceIdentifier{
								Ordinal: 5,
							},
							gvr: &schema.GroupVersionResource{
								Group:    "batch",
								Version:  "v1",
								Resource: "jobs",
							},
						},
						{
							id: &fleetv1beta1.WorkResourceIdentifier{
								Ordinal: 6,
							},
							gvr: &schema.GroupVersionResource{
								Group:    "autoscaling",
								Version:  "v1",
								Resource: "horizontalpodautoscalers",
							},
						},
						{
							id: &fleetv1beta1.WorkResourceIdentifier{
								Ordinal: 7,
							},
							gvr: &schema.GroupVersionResource{
								Group:    "apps",
								Version:  "v1",
								Resource: "statefulsets",
							},
						},
						{
							id: &fleetv1beta1.WorkResourceIdentifier{
								Ordinal: 8,
							},
							gvr: &schema.GroupVersionResource{
								Group:    "apps",
								Version:  "v1",
								Resource: "deployments",
							},
						},
						{
							id: &fleetv1beta1.WorkResourceIdentifier{
								Ordinal: 9,
							},
							gvr: &schema.GroupVersionResource{
								Group:    "apps",
								Version:  "v1",
								Resource: "replicasets",
							},
						},
						{
							id: &fleetv1beta1.WorkResourceIdentifier{
								Ordinal: 10,
							},
							gvr: &schema.GroupVersionResource{
								Group:    "",
								Version:  "v1",
								Resource: "replicationcontrollers",
							},
						},
						{
							id: &fleetv1beta1.WorkResourceIdentifier{
								Ordinal: 11,
							},
							gvr: &schema.GroupVersionResource{
								Group:    "",
								Version:  "v1",
								Resource: "pods",
							},
						},
						{
							id: &fleetv1beta1.WorkResourceIdentifier{
								Ordinal: 12,
							},
							gvr: &schema.GroupVersionResource{
								Group:    "apps",
								Version:  "v1",
								Resource: "daemonsets",
							},
						},
						{
							id: &fleetv1beta1.WorkResourceIdentifier{
								Ordinal: 13,
							},
							gvr: &schema.GroupVersionResource{
								Group:    "",
								Version:  "v1",
								Resource: "services",
							},
						},
					},
				},
				{
					num: 5,
					bundles: []*manifestProcessingBundle{
						{
							id: &fleetv1beta1.WorkResourceIdentifier{
								Ordinal: 0,
							},
							gvr: &schema.GroupVersionResource{
								Group:    "admissionregistration.k8s.io",
								Version:  "v1",
								Resource: "mutatingwebhookconfigurations",
							},
						},
						{
							id: &fleetv1beta1.WorkResourceIdentifier{
								Ordinal: 1,
							},
							gvr: &schema.GroupVersionResource{
								Group:    "admissionregistration.k8s.io",
								Version:  "v1",
								Resource: "validatingwebhookconfigurations",
							},
						},
						{
							id: &fleetv1beta1.WorkResourceIdentifier{
								Ordinal: 2,
							},
							gvr: &schema.GroupVersionResource{
								Group:    "apiregistration.k8s.io",
								Version:  "v1",
								Resource: "apiservices",
							},
						},
					},
				},
				{
					num: lastWave,
					bundles: []*manifestProcessingBundle{
						{
							id: &fleetv1beta1.WorkResourceIdentifier{
								Ordinal: 32,
							},
							gvr: &schema.GroupVersionResource{
								Group:    "dummy",
								Version:  "v10",
								Resource: "placeholders",
							},
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			waves := organizeBundlesIntoProcessingWaves(tc.bundles, workRef)
			if diff := cmp.Diff(
				waves, tc.wantWaves,
				cmp.AllowUnexported(manifestProcessingBundle{}, bundleProcessingWave{}),
			); diff != "" {
				t.Errorf("organized waves mismatch (-got, +want):\n%s", diff)
			}
		})
	}
}
