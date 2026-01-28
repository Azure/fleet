/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package main

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"go.goms.io/fleet/cmd/crdinstaller/utils"
)

var lessFunc = func(s1, s2 string) bool {
	return s1 < s2
}

func TestCleanupCRDs(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := apiextensionsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("Failed to add apiextensions scheme: %v", err)
	}

	// Create test CRDs with mode labels.
	hubCRD1 := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "memberclusters.cluster.kubernetes-fleet.io",
			Labels: map[string]string{
				utils.CRDInstallerLabelKey:  "true",
				utils.CRDInstallerModeLabel: "hub",
			},
		},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "cluster.kubernetes-fleet.io",
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural:   "memberclusters",
				Singular: "membercluster",
				Kind:     "MemberCluster",
			},
			Scope: apiextensionsv1.ClusterScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{Name: "v1beta1", Served: true, Storage: true, Schema: &apiextensionsv1.CustomResourceValidation{
					OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{Type: "object"},
				}},
			},
		},
	}

	hubCRD2 := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "clusterprofiles.multicluster.x-k8s.io",
			Labels: map[string]string{
				utils.CRDInstallerLabelKey:  "true",
				utils.CRDInstallerModeLabel: "hub",
			},
		},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "multicluster.x-k8s.io",
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural:   "clusterprofiles",
				Singular: "clusterprofile",
				Kind:     "ClusterProfile",
			},
			Scope: apiextensionsv1.ClusterScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{Name: "v1alpha1", Served: true, Storage: true, Schema: &apiextensionsv1.CustomResourceValidation{
					OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{Type: "object"},
				}},
			},
		},
	}

	memberCRD := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "appliedworks.placement.kubernetes-fleet.io",
			Labels: map[string]string{
				utils.CRDInstallerLabelKey:  "true",
				utils.CRDInstallerModeLabel: "member",
			},
		},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "placement.kubernetes-fleet.io",
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural:   "appliedworks",
				Singular: "appliedwork",
				Kind:     "AppliedWork",
			},
			Scope: apiextensionsv1.NamespaceScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{Name: "v1beta1", Served: true, Storage: true, Schema: &apiextensionsv1.CustomResourceValidation{
					OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{Type: "object"},
				}},
			},
		},
	}

	unmanagedCRD := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "unmanaged.example.com",
			// No managed label or mode label
		},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "example.com",
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural:   "unmanageds",
				Singular: "unmanaged",
				Kind:     "Unmanaged",
			},
			Scope: apiextensionsv1.NamespaceScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{Name: "v1", Served: true, Storage: true, Schema: &apiextensionsv1.CustomResourceValidation{
					OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{Type: "object"},
				}},
			},
		},
	}

	tests := []struct {
		name              string
		mode              string
		existingCRDs      []apiextensionsv1.CustomResourceDefinition
		wantRemainingCRDs []string
		wantError         bool
	}{
		{
			name: "hub mode - deletes only hub-labeled CRDs",
			mode: "hub",
			existingCRDs: []apiextensionsv1.CustomResourceDefinition{
				*hubCRD1,
				*hubCRD2,
				*memberCRD,
				*unmanagedCRD,
			},
			// After hub cleanup: member CRD and unmanaged CRD should remain
			wantRemainingCRDs: []string{
				"appliedworks.placement.kubernetes-fleet.io",
				"unmanaged.example.com",
			},
			wantError: false,
		},
		{
			name: "member mode - deletes only member-labeled CRDs",
			mode: "member",
			existingCRDs: []apiextensionsv1.CustomResourceDefinition{
				*hubCRD1,
				*memberCRD,
				*unmanagedCRD,
			},
			// After member cleanup: hub CRD and unmanaged CRD should remain
			wantRemainingCRDs: []string{
				"memberclusters.cluster.kubernetes-fleet.io",
				"unmanaged.example.com",
			},
			wantError: false,
		},
		{
			name:              "no CRDs to cleanup",
			mode:              "hub",
			existingCRDs:      []apiextensionsv1.CustomResourceDefinition{*unmanagedCRD},
			wantRemainingCRDs: []string{"unmanaged.example.com"},
			wantError:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Build fake client with existing CRDs.
			objs := make([]runtime.Object, len(tt.existingCRDs))
			for i := range tt.existingCRDs {
				objs[i] = &tt.existingCRDs[i]
			}
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(objs...).
				Build()

			// Run cleanup.
			err := cleanupCRDs(context.Background(), fakeClient, tt.mode)
			if (err != nil) != tt.wantError {
				t.Errorf("cleanupCRDs() error = %v, wantError %v", err, tt.wantError)
				return
			}

			// Verify remaining CRDs.
			var remainingCRDs apiextensionsv1.CustomResourceDefinitionList
			if err := fakeClient.List(context.Background(), &remainingCRDs); err != nil {
				t.Fatalf("Failed to list remaining CRDs: %v", err)
			}

			gotNames := make([]string, len(remainingCRDs.Items))
			for i, crd := range remainingCRDs.Items {
				gotNames[i] = crd.Name
			}

			if diff := cmp.Diff(tt.wantRemainingCRDs, gotNames, cmpopts.SortSlices(lessFunc)); diff != "" {
				t.Errorf("Remaining CRDs mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
