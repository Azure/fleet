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

// Package utils contains utility functions for the CRD installer.
package utils

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	// CRDInstallerLabelKey is the label key used to indicate that a CRD is managed by the installer.
	CRDInstallerLabelKey = "crd-installer.azurefleet.io/managed"
	// AddonManagerLabelKey is the label key used to indicate that a CRD is managed by the addon manager.
	AddonManagerLabelKey = "addonmanager.kubernetes.io/mode"
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

	isManagedByAddonManager, err := isCRDManagedByAddonManager(ctx, client, crd.Name)
	if err != nil {
		return err
	}
	if isManagedByAddonManager {
		return nil
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
		return nil
	})

	if err != nil {
		klog.ErrorS(err, "Failed to create or update CRD", "name", crd.Name, "operation", createOrUpdateRes)
		return err
	}

	klog.Infof("Successfully created/updated CRD %s", crd.Name)
	return nil
}

// isCRDManagedByAddonManager checks if a CRD is managed by the addon manager.
func isCRDManagedByAddonManager(ctx context.Context, client client.Client, crdName string) (bool, error) {
	var crd apiextensionsv1.CustomResourceDefinition
	if err := client.Get(ctx, types.NamespacedName{Name: crdName}, &crd); err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		} else {
			return false, fmt.Errorf("failed to get CRD %s: %w", crdName, err)
		}
	}

	labels := crd.GetLabels()
	if labels != nil {
		if _, exists := labels[AddonManagerLabelKey]; exists {
			klog.Infof("CRD %s is still managed by the addon manager, skipping installation", crd.Name)
			return true, nil
		}
	}
	return false, nil
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
