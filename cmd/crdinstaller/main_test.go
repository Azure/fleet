/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package main

import (
	"context"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	admv1 "k8s.io/api/admissionregistration/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"go.goms.io/fleet/cmd/crdinstaller/utils"
)

const (
	actualCRDPath = "../../config/crd/bases"
)

// TestInstallCRDs verifies that installCRDs installs exactly the CRDs collected for each mode, with the
// expected labels. The exact set of CRDs collected for each mode is verified in the utils package
// (TestCollectCRDFileNamesWithActualPath).
func TestInstallCRDs(t *testing.T) {
	if _, err := os.Stat(actualCRDPath); os.IsNotExist(err) {
		t.Skipf("Skipping test: %s does not exist", actualCRDPath)
	}

	scheme := runtime.NewScheme()
	if err := apiextensionsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("Failed to add apiextensions scheme: %v", err)
	}
	if err := admv1.AddToScheme(scheme); err != nil {
		t.Fatalf("Failed to add admissionregistration scheme: %v", err)
	}

	tests := []struct {
		name                      string
		crdPath                   string
		mode                      string
		enablePlacementPolicyAPIs bool
		wantAzureManagedLabel     string
		wantErr                   bool
	}{
		{
			name:                  "hub mode",
			crdPath:               actualCRDPath,
			mode:                  utils.ModeHub,
			wantAzureManagedLabel: utils.FleetLabelValue,
		},
		{
			name:                      "hub mode with placement policy APIs enabled",
			crdPath:                   actualCRDPath,
			mode:                      utils.ModeHub,
			enablePlacementPolicyAPIs: true,
			wantAzureManagedLabel:     utils.FleetLabelValue,
		},
		{
			name:                  "member mode",
			crdPath:               actualCRDPath,
			mode:                  utils.ModeMember,
			wantAzureManagedLabel: utils.FleetLabelValue,
		},
		{
			name:                      "member mode with placement policy APIs enabled",
			crdPath:                   actualCRDPath,
			mode:                      utils.ModeMember,
			enablePlacementPolicyAPIs: true,
			wantAzureManagedLabel:     utils.FleetLabelValue,
		},
		{
			name:                  "arcMember mode",
			crdPath:               actualCRDPath,
			mode:                  utils.ModeArcMember,
			wantAzureManagedLabel: utils.ModeArcMember,
		},
		{
			name:                      "arcMember mode with placement policy APIs enabled",
			crdPath:                   actualCRDPath,
			mode:                      utils.ModeArcMember,
			enablePlacementPolicyAPIs: true,
			wantAzureManagedLabel:     utils.ModeArcMember,
		},
		{
			name:                      "no CRDs found",
			crdPath:                   t.TempDir(),
			mode:                      utils.ModeHub,
			enablePlacementPolicyAPIs: true,
			wantErr:                   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

			collectedCRDs, err := utils.CollectCRDs(tt.crdPath, tt.mode, tt.enablePlacementPolicyAPIs, scheme)
			if err != nil {
				t.Fatalf("CollectCRDs() = %v, want no error", err)
			}
			wantCRDSpecs := make(map[string]apiextensionsv1.CustomResourceDefinitionSpec, len(collectedCRDs))
			for i := range collectedCRDs {
				wantCRDSpecs[collectedCRDs[i].Name] = collectedCRDs[i].Spec
			}

			err = installCRDs(ctx, fakeClient, tt.crdPath, tt.mode, tt.enablePlacementPolicyAPIs)
			if (err != nil) != tt.wantErr {
				t.Fatalf("installCRDs() = %v, wantErr %t", err, tt.wantErr)
			}

			crdList := &apiextensionsv1.CustomResourceDefinitionList{}
			if err := fakeClient.List(ctx, crdList); err != nil {
				t.Fatalf("List() = %v, want no error", err)
			}

			gotCRDSpecs := make(map[string]apiextensionsv1.CustomResourceDefinitionSpec, len(crdList.Items))
			wantLabels := map[string]string{
				utils.CRDInstallerLabelKey: "true",
				utils.AzureManagedLabelKey: tt.wantAzureManagedLabel,
			}
			for i := range crdList.Items {
				crd := &crdList.Items[i]
				gotCRDSpecs[crd.Name] = crd.Spec
				if diff := cmp.Diff(wantLabels, crd.Labels); diff != "" {
					t.Errorf("CRD %s labels mismatch (-want +got):\n%s", crd.Name, diff)
				}
			}
			if diff := cmp.Diff(wantCRDSpecs, gotCRDSpecs, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("installed CRDs mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
