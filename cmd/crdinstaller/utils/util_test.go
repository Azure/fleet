/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package utils contains utility functions for the CRD installer.
package utils

import (
	"context"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var lessFunc = func(s1, s2 string) bool {
	return s1 < s2
}

// Test using the actual config/crd/bases directory.
func TestCollectCRDFileNamesWithActualPath(t *testing.T) {
	// Use a path relative to the project root when running with make local-unit-test
	const realCRDPath = "config/crd/bases"

	// Skip this test if the directory doesn't exist.
	if _, err := os.Stat(realCRDPath); os.IsNotExist(err) {
		// Try the original path (for when tests are run from the package directory).
		const fallbackPath = "../../../config/crd/bases"
		if _, fallBackPathErr := os.Stat(fallbackPath); os.IsNotExist(fallBackPathErr) {
			t.Skipf("Skipping test: neither %s nor %s exist", realCRDPath, fallbackPath)
		} else {
			// Use the fallback path if it exists.
			runTest(t, fallbackPath)
		}
	} else {
		// Use the primary path if it exists.
		runTest(t, realCRDPath)
	}
}

// runTest contains the actual test logic, separated to allow running with different paths.
func runTest(t *testing.T, crdPath string) {
	tests := []struct {
		name           string
		mode           string
		wantedCRDNames []string
		wantError      bool
	}{
		{
			name: "hub mode v1beta1 with actual directory",
			mode: ModeHub,
			wantedCRDNames: []string{
				"memberclusters.cluster.kubernetes-fleet.io",
				"internalmemberclusters.cluster.kubernetes-fleet.io",
				"approvalrequests.placement.kubernetes-fleet.io",
				"clusterapprovalrequests.placement.kubernetes-fleet.io",
				"clusterresourcebindings.placement.kubernetes-fleet.io",
				"clusterresourceenvelopes.placement.kubernetes-fleet.io",
				"clusterresourceplacements.placement.kubernetes-fleet.io",
				"clusterresourceplacementstatuses.placement.kubernetes-fleet.io",
				"clusterresourceoverrides.placement.kubernetes-fleet.io",
				"clusterresourceoverridesnapshots.placement.kubernetes-fleet.io",
				"clusterresourceplacementdisruptionbudgets.placement.kubernetes-fleet.io",
				"clusterresourceplacementevictions.placement.kubernetes-fleet.io",
				"clusterresourcesnapshots.placement.kubernetes-fleet.io",
				"clusterschedulingpolicysnapshots.placement.kubernetes-fleet.io",
				"clusterstagedupdateruns.placement.kubernetes-fleet.io",
				"clusterstagedupdatestrategies.placement.kubernetes-fleet.io",
				"resourcebindings.placement.kubernetes-fleet.io",
				"resourceenvelopes.placement.kubernetes-fleet.io",
				"resourceoverrides.placement.kubernetes-fleet.io",
				"resourceoverridesnapshots.placement.kubernetes-fleet.io",
				"resourceplacements.placement.kubernetes-fleet.io",
				"resourcesnapshots.placement.kubernetes-fleet.io",
				"schedulingpolicysnapshots.placement.kubernetes-fleet.io",
				"stagedupdateruns.placement.kubernetes-fleet.io",
				"stagedupdatestrategies.placement.kubernetes-fleet.io",
				"works.placement.kubernetes-fleet.io",
				"clusterprofiles.multicluster.x-k8s.io",
			},
			wantError: false,
		},
		{
			name: "member mode v1beta1 with actual directory",
			mode: ModeMember,
			wantedCRDNames: []string{
				"appliedworks.placement.kubernetes-fleet.io",
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		scheme := runtime.NewScheme()
		if err := apiextensionsv1.AddToScheme(scheme); err != nil {
			t.Fatalf("Failed to add apiextensions scheme: %v", err)
		}
		t.Run(tt.name, func(t *testing.T) {
			// Call the function.
			gotCRDs, err := CollectCRDs(crdPath, tt.mode, scheme)
			if (err != nil) != tt.wantError {
				t.Errorf("collectCRDs() error = %v, wantError %v", err, tt.wantError)
			}
			gotCRDNames := make([]string, len(gotCRDs))
			for i, crd := range gotCRDs {
				gotCRDNames[i] = crd.Name
			}
			// Sort the names for comparison.
			if diff := cmp.Diff(tt.wantedCRDNames, gotCRDNames, cmpopts.SortSlices(lessFunc)); diff != "" {
				t.Errorf("CRD names mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestInstallCRD(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := apiextensionsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("Failed to add apiextensions scheme: %v", err)
	}

	testCRD := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test.example.com",
		},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "example.com",
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{
					Name:    "v1",
					Served:  true,
					Storage: true,
					Schema: &apiextensionsv1.CustomResourceValidation{
						OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
							Type: "object",
						},
					},
				},
			},
			Scope: apiextensionsv1.NamespaceScoped,
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural:   "tests",
				Singular: "test",
				Kind:     "Test",
			},
		},
	}

	tests := []struct {
		name      string
		crd       *apiextensionsv1.CustomResourceDefinition
		mode      string
		wantError bool
	}{
		{
			name:      "successful CRD installation with member mode",
			crd:       testCRD,
			mode:      ModeMember,
			wantError: false,
		},
		{
			name:      "successful CRD installation with arcMember mode",
			crd:       testCRD,
			mode:      ModeArcMember,
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
			err := InstallCRD(context.Background(), fakeClient, tt.crd, tt.mode)

			if tt.wantError {
				if err == nil {
					t.Errorf("InstallCRD() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("InstallCRD() unexpected error: %v", err)
				return
			}

			var installedCRD apiextensionsv1.CustomResourceDefinition
			err = fakeClient.Get(context.Background(), types.NamespacedName{Name: tt.crd.Name}, &installedCRD)
			if err != nil {
				t.Errorf("Failed to get installed CRD: %v", err)
				return
			}

			if installedCRD.Labels[CRDInstallerLabelKey] != "true" {
				t.Errorf("Expected CRD label %s to be 'true', got %q", CRDInstallerLabelKey, installedCRD.Labels[CRDInstallerLabelKey])
			}

			if installedCRD.Labels[AzureManagedLabelKey] != FleetLabelValue {
				t.Errorf("Expected CRD label %s to be %q, got %q", AzureManagedLabelKey, FleetLabelValue, installedCRD.Labels[AzureManagedLabelKey])
			}

			if tt.mode == ModeArcMember {
				if installedCRD.Labels[ArcInstallationKey] != "true" {
					t.Errorf("Expected CRD label %s to be 'true', got %q", ArcInstallationKey, installedCRD.Labels[ArcInstallationKey])
				}
			} else {
				if _, exists := installedCRD.Labels[ArcInstallationKey]; exists {
					t.Errorf("Expected CRD label %s to not exist for non-ARC installation", ArcInstallationKey)
				}
			}

			if diff := cmp.Diff(tt.crd.Spec, installedCRD.Spec); diff != "" {
				t.Errorf("CRD spec mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestInstall(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := apiextensionsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("Failed to add apiextensions scheme: %v", err)
	}

	testCRD := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test.example.com",
		},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "example.com",
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{
					Name:    "v1",
					Served:  true,
					Storage: true,
					Schema: &apiextensionsv1.CustomResourceValidation{
						OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
							Type: "object",
						},
					},
				},
			},
			Scope: apiextensionsv1.NamespaceScoped,
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural:   "tests",
				Singular: "test",
				Kind:     "Test",
			},
		},
	}

	tests := []struct {
		name      string
		obj       client.Object
		mutFunc   func() error
		wantError bool
	}{
		{
			name: "successful install with mutation",
			obj:  testCRD,
			mutFunc: func() error {
				if testCRD.Labels == nil {
					testCRD.Labels = make(map[string]string)
				}
				testCRD.Labels["test"] = "value"
				return nil
			},
			wantError: false,
		},
		{
			name: "successful install without mutation",
			obj: &apiextensionsv1.CustomResourceDefinition{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test2.example.com",
				},
				Spec: testCRD.Spec,
			},
			mutFunc: func() error {
				return nil
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
			err := install(context.Background(), fakeClient, tt.obj, tt.mutFunc)

			if tt.wantError {
				if err == nil {
					t.Errorf("install() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("install() unexpected error: %v", err)
				return
			}

			var installed apiextensionsv1.CustomResourceDefinition
			err = fakeClient.Get(context.Background(), types.NamespacedName{Name: tt.obj.GetName()}, &installed)
			if err != nil {
				t.Errorf("Failed to get installed object: %v", err)
				return
			}

			if tt.mutFunc != nil && tt.obj.GetName() == "test.example.com" {
				if installed.Labels["test"] != "value" {
					t.Errorf("Expected label 'test' to be 'value', got %q", installed.Labels["test"])
				}
			}
		})
	}
}
