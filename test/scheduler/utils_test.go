/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package tests

// This file features utilities used in the test suites.

import (
	"strconv"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/scheduler/clustereligibilitychecker"
	"go.goms.io/fleet/pkg/scheduler/framework"
	"go.goms.io/fleet/pkg/scheduler/framework/plugins/clusteraffinity"
	"go.goms.io/fleet/pkg/scheduler/framework/plugins/clustereligibility"
	"go.goms.io/fleet/pkg/scheduler/framework/plugins/sameplacementaffinity"
	"go.goms.io/fleet/pkg/scheduler/framework/plugins/topologyspreadconstraints"
)

// This file features some utilities used in the test suites.

const (
	crpNameTemplate = "crp-%d"

	policySnapshotNameTemplate = "%s-policy-snapshot-%d"

	policyHash = "policy-hash"

	bindingNamePlaceholder = "binding"
)

var (
	defaultResourceSelectors = []fleetv1beta1.ClusterResourceSelector{
		{
			Group:   "core",
			Kind:    "Namespace",
			Version: "v1",
			Name:    "work",
		},
	}

	nilScoreByCluster = map[string]*fleetv1beta1.ClusterScore{}
)

var (
	lessFuncBinding = func(binding1, binding2 fleetv1beta1.ClusterResourceBinding) bool {
		return binding1.Spec.TargetCluster < binding2.Spec.TargetCluster
	}
	lessFuncClusterDecision = func(decision1, decision2 fleetv1beta1.ClusterDecision) bool {
		return decision1.ClusterName < decision2.ClusterName
	}

	ignoreClusterDecisionReasonField          = cmpopts.IgnoreFields(fleetv1beta1.ClusterDecision{}, "Reason")
	ignoreObjectMetaNameField                 = cmpopts.IgnoreFields(metav1.ObjectMeta{}, "Name")
	ignoreObjectMetaAutoGeneratedFields       = cmpopts.IgnoreFields(metav1.ObjectMeta{}, "UID", "CreationTimestamp", "ResourceVersion", "Generation", "ManagedFields")
	ignoreResourceBindingTypeMetaField        = cmpopts.IgnoreFields(fleetv1beta1.ClusterResourceBinding{}, "TypeMeta")
	ignoreConditionTimeReasonAndMessageFields = cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime", "Reason", "Message")

	ignoreResourceBindingFields = []cmp.Option{
		ignoreResourceBindingTypeMetaField,
		ignoreObjectMetaNameField,
		ignoreObjectMetaAutoGeneratedFields,
		ignoreClusterDecisionReasonField,
		cmpopts.SortSlices(lessFuncBinding),
	}
)

func buildSchedulerFramework(ctrlMgr manager.Manager, clusterEligibilityChecker *clustereligibilitychecker.ClusterEligibilityChecker) framework.Framework {
	// Create a new profile.
	profile := framework.NewProfile(defaultProfileName)

	// Register the plugins.
	clusterAffinityPlugin := clusteraffinity.New()
	clustereligibilityPlugin := clustereligibility.New()
	samePlacementAffinityPlugin := sameplacementaffinity.New()
	topologyspreadconstraintsPlugin := topologyspreadconstraints.New()
	profile.
		// Register cluster affinity plugin.
		WithPreFilterPlugin(&clusterAffinityPlugin).
		WithFilterPlugin(&clusterAffinityPlugin).
		WithPreScorePlugin(&clusterAffinityPlugin).
		WithScorePlugin(&clusterAffinityPlugin).
		// Register cluster eligibility plugin.
		WithFilterPlugin(&clustereligibilityPlugin).
		// Register same placement affinity plugin.
		WithFilterPlugin(&samePlacementAffinityPlugin).
		WithScorePlugin(&samePlacementAffinityPlugin).
		// Register topology spread constraints plugin.
		WithPostBatchPlugin(&topologyspreadconstraintsPlugin).
		WithPreFilterPlugin(&topologyspreadconstraintsPlugin).
		WithFilterPlugin(&topologyspreadconstraintsPlugin).
		WithPreScorePlugin(&topologyspreadconstraintsPlugin).
		WithScorePlugin(&topologyspreadconstraintsPlugin)

	// Create a scheduler framework.
	return framework.NewFramework(profile, ctrlMgr, framework.WithClusterEligibilityChecker(clusterEligibilityChecker))
}

func buildK8sAPIConfigFrom(restCfg *rest.Config) []byte {
	clusterName := "default-cluster"
	contextName := "default-context"
	userName := "admin"

	clusters := make(map[string]*clientcmdapi.Cluster)
	clusters[clusterName] = &clientcmdapi.Cluster{
		Server:                   restCfg.Host,
		CertificateAuthorityData: restCfg.CAData,
	}

	contexts := make(map[string]*clientcmdapi.Context)
	contexts[contextName] = &clientcmdapi.Context{
		Cluster:  clusterName,
		AuthInfo: userName,
	}

	authInfos := make(map[string]*clientcmdapi.AuthInfo)
	authInfos[userName] = &clientcmdapi.AuthInfo{
		ClientCertificateData: restCfg.CertData,
		ClientKeyData:         restCfg.KeyData,
	}

	apiCfg := &clientcmdapi.Config{
		Kind:           "Config",
		APIVersion:     "v1",
		Clusters:       clusters,
		Contexts:       contexts,
		CurrentContext: contextName,
		AuthInfos:      authInfos,
	}

	apiCfgBytes, err := clientcmd.Write(*apiCfg)
	Expect(err).To(BeNil(), "Failed to write API config")
	return apiCfgBytes
}

func loadRestConfigFrom(apiCfgBytes []byte) *rest.Config {
	apiCfg, err := clientcmd.Load(apiCfgBytes)
	Expect(err).To(BeNil(), "Failed to load API config")

	restCfg, err := clientcmd.NewDefaultClientConfig(*apiCfg, &clientcmd.ConfigOverrides{}).ClientConfig()
	Expect(err).To(BeNil(), "Failed to load REST config")

	return restCfg
}

func updatePickedFixedCRPWithNewTargetClustersAndRefreshSnapshots(crpName string, targetClusters []string, oldPolicySnapshotName, newPolicySnapshotName string) {
	// Update the CRP.
	crp := &fleetv1beta1.ClusterResourcePlacement{}
	Expect(hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp)).To(Succeed(), "Failed to get CRP")

	policy := crp.Spec.Policy.DeepCopy()
	policy.ClusterNames = targetClusters
	crp.Spec.Policy = policy
	Expect(hubClient.Update(ctx, crp)).To(Succeed(), "Failed to update CRP")

	crpGeneration := crp.Generation

	// Mark the old policy snapshot as inactive.
	policySnapshot := &fleetv1beta1.ClusterSchedulingPolicySnapshot{}
	Expect(hubClient.Get(ctx, types.NamespacedName{Name: oldPolicySnapshotName}, policySnapshot)).To(Succeed(), "Failed to get policy snapshot")
	policySnapshot.Labels[fleetv1beta1.IsLatestSnapshotLabel] = strconv.FormatBool(false)
	Expect(hubClient.Update(ctx, policySnapshot)).To(Succeed(), "Failed to update policy snapshot")

	// Create a new policy snapshot.
	policySnapshot = &fleetv1beta1.ClusterSchedulingPolicySnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name: newPolicySnapshotName,
			Labels: map[string]string{
				fleetv1beta1.IsLatestSnapshotLabel: strconv.FormatBool(true),
				fleetv1beta1.CRPTrackingLabel:      crpName,
			},
			Annotations: map[string]string{
				fleetv1beta1.CRPGenerationAnnotation: strconv.FormatInt(crpGeneration, 10),
			},
		},
		Spec: fleetv1beta1.SchedulingPolicySnapshotSpec{
			Policy:     policy,
			PolicyHash: []byte(policyHash),
		},
	}
	Expect(hubClient.Create(ctx, policySnapshot)).To(Succeed(), "Failed to create policy snapshot")
}

func clearUnscheduledBindings() {
	// List all bindings.
	bindingList := &fleetv1beta1.ClusterResourceBindingList{}
	Expect(hubClient.List(ctx, bindingList)).To(Succeed(), "Failed to list bindings")

	// Delete all unscheduled bindings.
	for idx := range bindingList.Items {
		binding := bindingList.Items[idx]
		if binding.Spec.State == fleetv1beta1.BindingStateUnscheduled {
			Expect(hubClient.Delete(ctx, &binding)).To(Succeed(), "Failed to delete binding")

			Eventually(func() error {
				err := hubClient.Get(ctx, types.NamespacedName{Name: binding.Name}, &fleetv1beta1.ClusterResourceBinding{})
				if errors.IsNotFound(err) {
					return nil
				}

				return err
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to delete binding")
		}
	}
}

func clearPolicySnapshots() {
	// List all policy snapshots.
	policySnapshotList := &fleetv1beta1.ClusterSchedulingPolicySnapshotList{}
	Expect(hubClient.List(ctx, policySnapshotList)).To(Succeed(), "Failed to list policy snapshots")

	// Delete all policy snapshots.
	for idx := range policySnapshotList.Items {
		policySnapshot := policySnapshotList.Items[idx]
		Expect(hubClient.Delete(ctx, &policySnapshot)).To(Succeed(), "Failed to delete policy snapshot")
		Eventually(func() error {
			err := hubClient.Get(ctx, types.NamespacedName{Name: policySnapshot.Name}, &fleetv1beta1.ClusterSchedulingPolicySnapshot{})
			if errors.IsNotFound(err) {
				return nil
			}

			return err
		}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to delete policy snapshot")
	}
}

func ensureCRPDeletion(crpName string) {
	// Retrieve the CRP.
	crp := &fleetv1beta1.ClusterResourcePlacement{}
	Expect(hubClient.Get(ctx, types.NamespacedName{Name: crpName}, crp)).To(Succeed(), "Failed to get CRP")

	// Remove all finalizers from the CRP.
	crp.Finalizers = []string{}
	Expect(hubClient.Update(ctx, crp)).To(Succeed(), "Failed to update CRP")

	// Ensure that the CRP is deleted.
	Eventually(func() error {
		err := hubClient.Get(ctx, types.NamespacedName{Name: crpName}, &fleetv1beta1.ClusterResourcePlacement{})
		if errors.IsNotFound(err) {
			return nil
		}

		return err
	}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to delete CRP")
}
