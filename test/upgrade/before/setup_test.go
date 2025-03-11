/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package before

import (
	"context"
	"flag"
	"log"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
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

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1alpha1 "go.goms.io/fleet/apis/placement/v1alpha1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/controllers/work"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/condition"
	"go.goms.io/fleet/test/e2e/framework"
)

const (
	// The names of the hub cluster + the member clusters set up in this E2E test environment.
	//
	// Note that these names must match with those in `setup.sh`, with a prefix `kind-`.
	hubClusterName               = "kind-hub"
	memberCluster1EastProdName   = "kind-cluster-1"
	memberCluster2EastCanaryName = "kind-cluster-2"
	memberCluster3WestProdName   = "kind-cluster-3"

	// The names of the service accounts used by specific member clusters.
	//
	// Note that these names must also match those in `setup.sh`.
	memberCluster1EastProdSAName   = "fleet-member-agent-cluster-1"
	memberCluster2EastCanarySAName = "fleet-member-agent-cluster-2"
	memberCluster3WestProdSAName   = "fleet-member-agent-cluster-3"

	hubClusterSAName = "fleet-hub-agent"
	fleetSystemNS    = "fleet-system"

	kubeConfigPathEnvVarName = "KUBECONFIG"
)

const (
	// Do not bump this value unless you have a good reason. This is to safeguard any performance related regressions.
	eventuallyDuration = time.Second * 10
	// This is for eventually timeouts in the cluster setup steps.
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
	lessFuncConditionByType = func(a, b metav1.Condition) bool {
		return a.Type < b.Type
	}
	lessFuncPlacementStatusByClusterName = func(a, b placementv1beta1.ResourcePlacementStatus) bool {
		return a.ClusterName < b.ClusterName
	}
	lessFuncPlacementStatusByConditions = func(a, b placementv1beta1.ResourcePlacementStatus) bool {
		return len(a.Conditions) < len(b.Conditions)
	}

	ignoreObjectMetaAutoGeneratedFields    = cmpopts.IgnoreFields(metav1.ObjectMeta{}, "UID", "CreationTimestamp", "ResourceVersion", "Generation", "ManagedFields", "OwnerReferences")
	ignoreObjectMetaAnnotationField        = cmpopts.IgnoreFields(metav1.ObjectMeta{}, "Annotations")
	ignoreConditionObservedGenerationField = cmpopts.IgnoreFields(metav1.Condition{}, "ObservedGeneration")
	ignoreAgentStatusHeartbeatField        = cmpopts.IgnoreFields(clusterv1beta1.AgentStatus{}, "LastReceivedHeartbeat")
	ignoreNamespaceStatusField             = cmpopts.IgnoreFields(corev1.Namespace{}, "Status")
	ignoreJobSpecSelectorField             = cmpopts.IgnoreFields(batchv1.JobSpec{}, "Selector")
	ignorePodTemplateSpecObjectMetaField   = cmpopts.IgnoreFields(corev1.PodTemplateSpec{}, "ObjectMeta")
	ignoreJobStatusField                   = cmpopts.IgnoreFields(batchv1.Job{}, "Status")
	ignoreServiceStatusField               = cmpopts.IgnoreFields(corev1.Service{}, "Status")
	ignoreServiceSpecIPAndPolicyFields     = cmpopts.IgnoreFields(corev1.ServiceSpec{}, "ClusterIP", "ClusterIPs", "ExternalIPs", "SessionAffinity", "IPFamilies", "IPFamilyPolicy", "InternalTrafficPolicy")
	ignoreServicePortNodePortProtocolField = cmpopts.IgnoreFields(corev1.ServicePort{}, "NodePort", "Protocol")
	ignoreRPSClusterNameField              = cmpopts.IgnoreFields(placementv1beta1.ResourcePlacementStatus{}, "ClusterName")

	// Since Fleet agents v0.14.0 a minor reason string change was applied on the hub side that
	// affects CRP availability status reportings in the resource placement section when untrackable
	// resources are involved. This transformer is added to ensure that the compatibility test specs
	// can handle this string change smoothly.
	//
	// Note that the aforementioned change is hub side exclusive and is for informational purposes only.
	availableDueToUntrackableResCondAcyclicTransformer = cmpopts.AcyclicTransformer("AvailableDueToUntrackableResCond", func(cond metav1.Condition) metav1.Condition {
		transformedCond := cond.DeepCopy()
		if cond.Type == string(placementv1beta1.ResourcesAvailableConditionType) && cond.Reason == work.WorkNotTrackableReason {
			transformedCond.Reason = condition.WorkNotAvailabilityTrackableReason
		}
		return *transformedCond
	})

	crpStatusCmpOptions = cmp.Options{
		cmpopts.SortSlices(lessFuncConditionByType),
		cmpopts.SortSlices(lessFuncPlacementStatusByClusterName),
		cmpopts.SortSlices(utils.LessFuncResourceIdentifier),
		cmpopts.SortSlices(utils.LessFuncFailedResourcePlacements),
		utils.IgnoreConditionLTTAndMessageFields,
		availableDueToUntrackableResCondAcyclicTransformer,
		cmpopts.EquateEmpty(),
	}
	crpWithStuckRolloutStatusCmpOptions = cmp.Options{
		cmpopts.SortSlices(lessFuncConditionByType),
		cmpopts.SortSlices(lessFuncPlacementStatusByConditions),
		cmpopts.SortSlices(utils.LessFuncResourceIdentifier),
		cmpopts.SortSlices(utils.LessFuncFailedResourcePlacements),
		utils.IgnoreConditionLTTAndMessageFields,
		ignoreRPSClusterNameField,
		availableDueToUntrackableResCondAcyclicTransformer,
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

	RunSpecs(t, "Fleet Agent Upgrade Test Suite")
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

	memberCluster1EastProd = framework.NewCluster(memberCluster1EastProdName, memberCluster1EastProdSAName, scheme, nil)
	Expect(memberCluster1EastProd).NotTo(BeNil(), "Failed to initialize cluster object")
	framework.GetClusterClient(memberCluster1EastProd)
	memberCluster1EastProdClient = memberCluster1EastProd.KubeClient
	Expect(memberCluster1EastProdClient).NotTo(BeNil(), "Failed to initialize client for accessing Kubernetes cluster")

	memberCluster2EastCanary = framework.NewCluster(memberCluster2EastCanaryName, memberCluster2EastCanarySAName, scheme, nil)
	Expect(memberCluster2EastCanary).NotTo(BeNil(), "Failed to initialize cluster object")
	framework.GetClusterClient(memberCluster2EastCanary)
	memberCluster2EastCanaryClient = memberCluster2EastCanary.KubeClient
	Expect(memberCluster2EastCanaryClient).NotTo(BeNil(), "Failed to initialize client for accessing Kubernetes cluster")

	memberCluster3WestProd = framework.NewCluster(memberCluster3WestProdName, memberCluster3WestProdSAName, scheme, nil)
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
	})
}

func beforeSuiteForProcess1() {
	beforeSuiteForAllProcesses()

	setAllMemberClustersToJoin()
	checkIfAllMemberClustersHaveJoined()
}

var _ = SynchronizedBeforeSuite(beforeSuiteForProcess1, beforeSuiteForAllProcesses)

// For upgrade tests in the before stage, there is no need to tear down the test environment
// (i.e., no AfterSuite node).
