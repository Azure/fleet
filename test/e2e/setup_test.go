/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package e2e

import (
	"context"
	"flag"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	clusterinventory "sigs.k8s.io/cluster-inventory-api/apis/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	fleetnetworkingv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/propertyprovider/azure/trackers"
	"go.goms.io/fleet/pkg/utils"
	testv1alpha1 "go.goms.io/fleet/test/apis/v1alpha1"
	"go.goms.io/fleet/test/e2e/framework"
)

const (
	// The names of the hub cluster + the member clusters set up in this E2E test environment.
	//
	// Note that these names must match with those in `setup.sh`, with a prefix `kind-`.
	hubClusterName                = "kind-hub"
	memberCluster1EastProdName    = "kind-cluster-1"
	memberCluster2EastCanaryName  = "kind-cluster-2"
	memberCluster3WestProdName    = "kind-cluster-3"
	memberCluster4UnhealthyName   = "kind-unhealthy-cluster"
	memberCluster5LeftName        = "kind-left-cluster"
	memberCluster6NonExistentName = "kind-non-existent-cluster"

	// The names of the service accounts used by specific member clusters.
	//
	// Note that these names must also match those in `setup.sh`.
	memberCluster1EastProdSAName   = "fleet-member-agent-cluster-1"
	memberCluster2EastCanarySAName = "fleet-member-agent-cluster-2"
	memberCluster3WestProdSAName   = "fleet-member-agent-cluster-3"

	hubClusterSAName = "fleet-hub-agent"
	fleetSystemNS    = "fleet-system"

	kubeConfigPathEnvVarName            = "KUBECONFIG"
	propertyProviderEnvVarName          = "PROPERTY_PROVIDER"
	azurePropertyProviderEnvVarValue    = "azure"
	fleetClusterResourceIDAnnotationKey = "fleet.azure.com/cluster-resource-id"
	fleetLocationAnnotationKey          = "fleet.azure.com/location"
)

const (
	// Do not bump this value unless you have a good reason. This is to safeguard any performance related regressions.
	eventuallyDuration = time.Second * 10
	// this is for workload related test cases
	workloadEventuallyDuration = time.Second * 45
	// This is for cluster setup.
	longEventuallyDuration = time.Minute * 2
	eventuallyInterval     = time.Millisecond * 250
	consistentlyDuration   = time.Second * 15
	consistentlyInterval   = time.Millisecond * 500
)

var (
	ctx    = context.Background()
	scheme = runtime.NewScheme()
	once   = sync.Once{}

	hubCluster               *framework.Cluster
	memberCluster1EastProd   *framework.Cluster
	memberCluster2EastCanary *framework.Cluster
	memberCluster3WestProd   *framework.Cluster

	hubClient                      client.Client
	impersonateHubClient           client.Client
	memberCluster1EastProdClient   client.Client
	memberCluster2EastCanaryClient client.Client
	memberCluster3WestProdClient   client.Client

	allMemberClusters     []*framework.Cluster
	allMemberClusterNames = []string{}
)

var (
	regionLabelName = "region"
	regionEast      = "east"
	regionWest      = "west"
	regionSouth     = "south"
	envLabelName    = "env"
	envProd         = "prod"
	envCanary       = "canary"
	clusterID1      = "test-cluster-id-1"
	clusterID2      = "test-cluster-id-2"
	clusterID3      = "test-cluster-id-3"

	labelsByClusterName = map[string]map[string]string{
		memberCluster1EastProdName: {
			regionLabelName: regionEast,
			envLabelName:    envProd,
		},
		memberCluster2EastCanaryName: {
			regionLabelName: regionEast,
			envLabelName:    envCanary,
		},
		memberCluster3WestProdName: {
			regionLabelName: regionWest,
			envLabelName:    envProd,
		},
	}
	annotationsByClusterName = map[string]map[string]string{
		memberCluster1EastProdName: {
			fleetClusterResourceIDAnnotationKey: clusterID1,
		},
		memberCluster2EastCanaryName: {
			fleetClusterResourceIDAnnotationKey: clusterID2,
		},
		memberCluster3WestProdName: {
			fleetClusterResourceIDAnnotationKey: clusterID3,
		},
	}

	taintTolerationMap = map[string]map[string]string{
		memberCluster1EastProdName: {
			regionLabelName: regionEast,
		},
		memberCluster2EastCanaryName: {
			regionLabelName: regionWest,
		},
		memberCluster3WestProdName: {
			regionLabelName: regionSouth,
		},
	}
)

var (
	drainBinaryPath    = filepath.Join("../../", "hack", "tools", "bin", "kubectl-draincluster")
	uncordonBinaryPath = filepath.Join("../../", "hack", "tools", "bin", "kubectl-uncordoncluster")
)

var (
	isAzurePropertyProviderEnabled = (os.Getenv(propertyProviderEnvVarName) == azurePropertyProviderEnvVarValue)

	// Note that the region information below is used only for the Azure property provider to
	// calculate costs (if applicable), which is different from the region label set above.
	//
	// The information should match with the AKS regions specified in the setup script.
	memberCluster1AKSRegion = "westus"
	memberCluster2AKSRegion = "northeurope"
	memberCluster3AKSRegion = "eastasia"
)

var (
	lessFuncCondition = func(a, b metav1.Condition) bool {
		return a.Type < b.Type
	}
	lessFuncPlacementStatus = func(a, b placementv1beta1.ResourcePlacementStatus) bool {
		return a.ClusterName < b.ClusterName
	}
	lessFuncPlacementStatusByConditions = func(a, b placementv1beta1.ResourcePlacementStatus) bool {
		return len(a.Conditions) < len(b.Conditions)
	}

	ignoreObjectMetaAutoGeneratedFields         = cmpopts.IgnoreFields(metav1.ObjectMeta{}, "UID", "CreationTimestamp", "ResourceVersion", "Generation", "ManagedFields", "OwnerReferences")
	ignoreObjectMetaAutoGenExceptOwnerRefFields = cmpopts.IgnoreFields(metav1.ObjectMeta{}, "UID", "CreationTimestamp", "ResourceVersion", "Generation", "ManagedFields")
	ignoreObjectMetaAnnotationField             = cmpopts.IgnoreFields(metav1.ObjectMeta{}, "Annotations")
	ignoreConditionObservedGenerationField      = cmpopts.IgnoreFields(metav1.Condition{}, "ObservedGeneration")

	ignoreConditionReasonField                                  = cmpopts.IgnoreFields(metav1.Condition{}, "Reason")
	ignoreAgentStatusHeartbeatField                             = cmpopts.IgnoreFields(clusterv1beta1.AgentStatus{}, "LastReceivedHeartbeat")
	ignoreNamespaceStatusField                                  = cmpopts.IgnoreFields(corev1.Namespace{}, "Status")
	ignoreNamespaceSpecField                                    = cmpopts.IgnoreFields(corev1.Namespace{}, "Spec")
	ignoreClusterNameField                                      = cmpopts.IgnoreFields(placementv1beta1.ResourcePlacementStatus{}, "ClusterName")
	ignoreMemberClusterJoinAndPropertyProviderStartedConditions = cmpopts.IgnoreSliceElements(func(c metav1.Condition) bool {
		return c.Type == string(clusterv1beta1.ConditionTypeMemberClusterReadyToJoin) ||
			c.Type == string(clusterv1beta1.ConditionTypeMemberClusterJoined) ||
			// The property provider started condition is omitted as it will only be added once
			// by the Fleet agent unless the agent restarts. In normal operations, this is the
			// expected behavior; however, for E2E tests, it is often that E2E tests are re-run
			// without stripping down the test environment, which may cause this condition to
			// disappear from the status of the MemberCluster object.
			c.Type == string(clusterv1beta1.ConditionTypeClusterPropertyProviderStarted)
	})
	ignoreTimeTypeFields                            = cmpopts.IgnoreTypes(time.Time{}, metav1.Time{})
	ignoreCRPStatusDriftedPlacementsTimestampFields = cmpopts.IgnoreFields(placementv1beta1.DriftedResourcePlacement{}, "ObservationTime", "FirstDriftedObservedTime")
	ignoreCRPStatusDiffedPlacementsTimestampFields  = cmpopts.IgnoreFields(placementv1beta1.DiffedResourcePlacement{}, "ObservationTime", "FirstDiffedObservedTime")

	crpStatusCmpOptions = cmp.Options{
		cmpopts.SortSlices(lessFuncCondition),
		cmpopts.SortSlices(lessFuncPlacementStatus),
		cmpopts.SortSlices(utils.LessFuncResourceIdentifier),
		cmpopts.SortSlices(utils.LessFuncFailedResourcePlacements),
		cmpopts.SortSlices(utils.LessFuncDiffedResourcePlacements),
		cmpopts.SortSlices(utils.LessFuncDriftedResourcePlacements),
		utils.IgnoreConditionLTTAndMessageFields,
		ignoreCRPStatusDriftedPlacementsTimestampFields,
		ignoreCRPStatusDiffedPlacementsTimestampFields,
		cmpopts.EquateEmpty(),
	}

	// We don't sort ResourcePlacementStatus by their name since we don't know which cluster will become unavailable first,
	// prompting the rollout to be blocked for remaining clusters.
	safeRolloutCRPStatusCmpOptions = cmp.Options{
		cmpopts.SortSlices(lessFuncCondition),
		cmpopts.SortSlices(lessFuncPlacementStatusByConditions),
		cmpopts.SortSlices(utils.LessFuncResourceIdentifier),
		cmpopts.SortSlices(utils.LessFuncFailedResourcePlacements),
		utils.IgnoreConditionLTTAndMessageFields,
		ignoreClusterNameField,
		cmpopts.EquateEmpty(),
	}

	updateRunStatusCmpOption = cmp.Options{
		utils.IgnoreConditionLTTAndMessageFields,
		cmpopts.IgnoreFields(placementv1beta1.StageUpdatingStatus{}, "StartTime", "EndTime"),
		cmpopts.EquateEmpty(),
	}
)

// TestMain sets up the E2E test environment.
func TestMain(m *testing.M) {
	// Add custom APIs to the scheme.
	if err := clusterv1beta1.AddToScheme(scheme); err != nil {
		log.Fatalf("failed to add custom APIs (cluster) to the runtime scheme: %v", err)
	}
	if err := placementv1alpha1.AddToScheme(scheme); err != nil {
		log.Fatalf("failed to add custom APIs (placement v1alpha1) to the runtime scheme: %v", err)
	}
	if err := placementv1beta1.AddToScheme(scheme); err != nil {
		log.Fatalf("failed to add custom APIs (placement) to the runtime scheme: %v", err)
	}
	if err := fleetnetworkingv1alpha1.AddToScheme(scheme); err != nil {
		log.Fatalf("failed to add custom APIs (networking) to the runtime scheme: %v", err)
	}
	if err := testv1alpha1.AddToScheme(scheme); err != nil {
		log.Fatalf("failed to add custom APIs (test) to the runtime scheme: %v", err)
	}

	// Add built-in APIs and extensions to the scheme.
	if err := k8sscheme.AddToScheme(scheme); err != nil {
		log.Fatalf("failed to add built-in APIs to the runtime scheme: %v", err)
	}
	if err := apiextensionsv1.AddToScheme(scheme); err != nil {
		log.Fatalf("failed to add API extensions to the runtime scheme: %v", err)
	}
	if err := clusterinventory.AddToScheme(scheme); err != nil {
		log.Fatalf("failed to add cluster inventory APIs to the runtime scheme: %v", err)
	}

	os.Exit(m.Run())
}

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Fleet E2E Test Suite (with v1beta1 APIs)")
}

func beforeSuiteForAllProcesses() {
	// Set up the logger.
	logger := zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true))
	klog.SetLogger(logger)
	ctrllog.SetLogger(logger)
	By("Setup klog")
	fs := flag.NewFlagSet("klog", flag.ContinueOnError)
	klog.InitFlags(fs)
	Expect(fs.Parse([]string{"--v", "5", "-add_dir_header", "true"})).Should(Succeed())

	// Check if the required environment variable, which specifies the path to kubeconfig file, has been set.
	Expect(os.Getenv(kubeConfigPathEnvVarName)).NotTo(BeEmpty(), "Required environment variable KUBECONFIG is not set")

	// Initialize the cluster objects and their clients.
	hubCluster = framework.NewCluster(hubClusterName, "", scheme, nil)
	Expect(hubCluster).NotTo(BeNil(), "Failed to initialize cluster object")
	framework.GetClusterClient(hubCluster)
	hubClient = hubCluster.KubeClient
	Expect(hubClient).NotTo(BeNil(), "Failed to initialize client for accessing Kubernetes cluster")
	impersonateHubClient = hubCluster.ImpersonateKubeClient
	Expect(impersonateHubClient).NotTo(BeNil(), "Failed to initialize impersonate client for accessing Kubernetes cluster")

	var pricingProvider1 trackers.PricingProvider
	if isAzurePropertyProviderEnabled {
		pricingProvider1 = trackers.NewAKSKarpenterPricingClient(ctx, memberCluster1AKSRegion)
	}
	memberCluster1EastProd = framework.NewCluster(memberCluster1EastProdName, memberCluster1EastProdSAName, scheme, pricingProvider1)
	Expect(memberCluster1EastProd).NotTo(BeNil(), "Failed to initialize cluster object")
	framework.GetClusterClient(memberCluster1EastProd)
	memberCluster1EastProdClient = memberCluster1EastProd.KubeClient
	Expect(memberCluster1EastProdClient).NotTo(BeNil(), "Failed to initialize client for accessing Kubernetes cluster")

	var pricingProvider2 trackers.PricingProvider
	if isAzurePropertyProviderEnabled {
		pricingProvider2 = trackers.NewAKSKarpenterPricingClient(ctx, memberCluster2AKSRegion)
	}
	memberCluster2EastCanary = framework.NewCluster(memberCluster2EastCanaryName, memberCluster2EastCanarySAName, scheme, pricingProvider2)
	Expect(memberCluster2EastCanary).NotTo(BeNil(), "Failed to initialize cluster object")
	framework.GetClusterClient(memberCluster2EastCanary)
	memberCluster2EastCanaryClient = memberCluster2EastCanary.KubeClient
	Expect(memberCluster2EastCanaryClient).NotTo(BeNil(), "Failed to initialize client for accessing Kubernetes cluster")

	var pricingProvider3 trackers.PricingProvider
	if isAzurePropertyProviderEnabled {
		pricingProvider3 = trackers.NewAKSKarpenterPricingClient(ctx, memberCluster3AKSRegion)
	}
	memberCluster3WestProd = framework.NewCluster(memberCluster3WestProdName, memberCluster3WestProdSAName, scheme, pricingProvider3)
	Expect(memberCluster3WestProd).NotTo(BeNil(), "Failed to initialize cluster object")
	framework.GetClusterClient(memberCluster3WestProd)
	memberCluster3WestProdClient = memberCluster3WestProd.KubeClient
	Expect(memberCluster3WestProdClient).NotTo(BeNil(), "Failed to initialize client for accessing kubernetes cluster")

	allMemberClusters = []*framework.Cluster{memberCluster1EastProd, memberCluster2EastCanary, memberCluster3WestProd}
	once.Do(func() {
		// Set these arrays only once; this is necessary as for the first spawned Ginkgo process,
		// the `beforeSuiteForAllProcesses` function is called twice.
		for i := range allMemberClusters {
			allMemberClusterNames = append(allMemberClusterNames, allMemberClusters[i].ClusterName)
		}

		// Build drain binary
		buildCmd := exec.Command("go", "build", "-o", drainBinaryPath, filepath.Join("../../", "tools", "draincluster"))
		output, err := buildCmd.CombinedOutput()
		Expect(err).ToNot(HaveOccurred(), "Failed to drain cluster: %v\nOutput: %s", err, string(output))

		// Build uncordon binary
		buildCmd = exec.Command("go", "build", "-o", uncordonBinaryPath, filepath.Join("../../", "tools", "uncordoncluster"))
		output, err = buildCmd.CombinedOutput()
		Expect(err).ToNot(HaveOccurred(), "Failed to uncordon cluster: %v\nOutput: %s", err, string(output))
	})
}

func beforeSuiteForProcess1() {
	beforeSuiteForAllProcesses()

	setAllMemberClustersToJoin()
	checkIfAllMemberClustersHaveJoined()
	checkIfAzurePropertyProviderIsWorking()

	// Simulate that member cluster 4 become unhealthy, and member cluster 5 has left the fleet.
	//
	// Note that these clusters are not real kind clusters.
	setupInvalidClusters()
	createResourcesForFleetGuardRail()
	createTestResourceCRD()
}

var _ = SynchronizedBeforeSuite(beforeSuiteForProcess1, beforeSuiteForAllProcesses)

var _ = SynchronizedAfterSuite(func() {}, func() {
	deleteResourcesForFleetGuardRail()
	deleteTestResourceCRD()
	setAllMemberClustersToLeave()
	checkIfAllMemberClustersHaveLeft()
	cleanupInvalidClusters()
	// Cleanup tool binaries.
	Expect(os.Remove(drainBinaryPath)).Should(Succeed(), "Failed to remove drain binary")
	Expect(os.Remove(uncordonBinaryPath)).Should(Succeed(), "Failed to remove uncordon binary")
})
