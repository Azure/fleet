/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

// Package utils contains utility functions for the CRD installer.
package utils

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	// CRDInstallerLabelKey is the label key used to indicate that a CRD is managed by the installer.
	CRDInstallerLabelKey = "crd-installer.azurefleet.io/managed"
	// AzureManagedLabelKey is the label key used to indicate that a CRD is managed by an azure resource.
	AzureManagedLabelKey = "kubernetes.azure.com/managedby"
	// FleetLabelValue is the value for the AzureManagedLabelKey indicating management by Fleet.
	FleetLabelValue = "fleet"
)

var (
	hubCRDs = map[string]bool{
		"cluster.kubernetes-fleet.io_memberclusters.yaml":                              true,
		"cluster.kubernetes-fleet.io_internalmemberclusters.yaml":                      true,
		"placement.kubernetes-fleet.io_clusterapprovalrequests.yaml":                   true,
		"placement.kubernetes-fleet.io_clusterresourcebindings.yaml":                   true,
		"placement.kubernetes-fleet.io_clusterresourceenvelopes.yaml":                  true,
		"placement.kubernetes-fleet.io_clusterresourceplacements.yaml":                 true,
		"placement.kubernetes-fleet.io_clusterresourceoverrides.yaml":                  true,
		"placement.kubernetes-fleet.io_clusterresourceoverridesnapshots.yaml":          true,
		"placement.kubernetes-fleet.io_clusterresourceplacementdisruptionbudgets.yaml": true,
		"placement.kubernetes-fleet.io_clusterresourceplacementevictions.yaml":         true,
		"placement.kubernetes-fleet.io_clusterresourcesnapshots.yaml":                  true,
		"placement.kubernetes-fleet.io_clusterschedulingpolicysnapshots.yaml":          true,
		"placement.kubernetes-fleet.io_clusterstagedupdateruns.yaml":                   true,
		"placement.kubernetes-fleet.io_clusterstagedupdatestrategies.yaml":             true,
		"placement.kubernetes-fleet.io_resourceenvelopes.yaml":                         true,
		"placement.kubernetes-fleet.io_resourceoverrides.yaml":                         true,
		"placement.kubernetes-fleet.io_resourceoverridesnapshots.yaml":                 true,
		"placement.kubernetes-fleet.io_works.yaml":                                     true,
		"multicluster.x-k8s.io_clusterprofiles.yaml":                                   true,
	}
	memberCRDs = map[string]bool{
		"placement.kubernetes-fleet.io_appliedworks.yaml": true,
	}
)

// InstallCRD installs a Custom Resource Definition (CRD) from the specified path.
func InstallCRD(ctx context.Context, client client.Client, path string) error {
	klog.V(2).Infof("Installing CRD from: %s", path)

	// Read and parse CRD file.
	crdBytes, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read CRD file %s: %w", path, err)
	}

	// Create decoder for converting raw bytes to Go types.
	codecFactory := serializer.NewCodecFactory(client.Scheme())
	decoder := codecFactory.UniversalDeserializer()

	// Decode YAML into a structured CRD object.
	obj, gvk, err := decoder.Decode(crdBytes, nil, nil)
	if err != nil {
		return fmt.Errorf("failed to decode CRD from %s: %w", path, err)
	}

	// Type assertion to make sure we have a CRD.
	crd, ok := obj.(*apiextensionsv1.CustomResourceDefinition)
	if !ok {
		return fmt.Errorf("unexpected type from %s, expected %s but got %s", path, gvk, apiextensionsv1.SchemeGroupVersion.WithKind("CustomResourceDefinition"))
	}

	existingCRD := apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: crd.Name,
		},
	}

	createOrUpdateRes, err := controllerutil.CreateOrUpdate(ctx, client, &existingCRD, func() error {
		// Copy spec from our decoded CRD to the object we're creating/updating.
		existingCRD.Spec = crd.Spec

		// Add an additional ownership label to indicate this CRD is managed by the installer.
		if existingCRD.Labels == nil {
			existingCRD.Labels = make(map[string]string)
		}
		// Ensure the label for management by the installer is set.
		existingCRD.Labels[CRDInstallerLabelKey] = "true"
		// Also set the Azure managed label to indicate this is managed by Fleet,
		// needed for clean up for CRD by kube-addon-manager.
		existingCRD.Labels[AzureManagedLabelKey] = FleetLabelValue
		return nil
	})

	if err != nil {
		klog.ErrorS(err, "Failed to create or update CRD", "name", crd.Name, "operation", createOrUpdateRes)
		return err
	}

	klog.Infof("Successfully created/updated CRD %s", crd.Name)
	return nil
}

// CollectCRDFileNames collects CRD filenames from the specified path based on the mode either hub/member.
func CollectCRDFileNames(crdPath, mode string) (map[string]bool, error) {
	// Set of CRD filenames to install based on mode.
	crdFilesToInstall := make(map[string]bool)

	// Walk through the CRD directory and collect filenames.
	if err := filepath.WalkDir(crdPath, func(path string, d fs.DirEntry, err error) error {
		// Handle errors from WalkDir.
		if err != nil {
			return err
		}

		// Skip directories.
		if d.IsDir() {
			return nil
		}

		// Only process yaml files.
		if filepath.Ext(path) != ".yaml" && filepath.Ext(path) != ".yml" {
			return nil
		}

		// Process based on mode.
		filename := filepath.Base(path)
		isHubCRD := hubCRDs[filename]
		isMemberCRD := memberCRDs[filename]

		switch mode {
		case "hub":
			if isHubCRD {
				crdFilesToInstall[path] = true
			}
		case "member":
			if isMemberCRD {
				crdFilesToInstall[path] = true
			}
		}

		return nil
	}); err != nil {
		return nil, fmt.Errorf("failed to walk CRD directory: %w", err)
	}

	return crdFilesToInstall, nil
}
