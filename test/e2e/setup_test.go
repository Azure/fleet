/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package e2e

import (
	"context"
	"fmt"
	"log"
	"os"
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
	runtime "k8s.io/apimachinery/pkg/runtime"
	k8sscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/test/e2e/framework"
)

const (
	// The names of the hub cluster + the member clusters set up in this E2E test environment.
	//
	// Note that these names must match with those in `setup.sh`, with a prefix `kind-`.
	hubClusterName     = "kind-hub"
	memberCluster1Name = "kind-cluster-1"
	memberCluster2Name = "kind-cluster-2"
	memberCluster3Name = "kind-cluster-3"
	memberCluster4Name = "kind-unhealthy-cluster"
	memberCluster5Name = "kind-left-cluster"
	memberCluster6Name = "kind-non-existent-cluster"

	hubClusterSAName = "hub-agent-sa"
	fleetSystemNS    = "fleet-system"

	kubeConfigPathEnvVarName = "KUBECONFIG"
)

const (
	eventuallyDuration = time.Second * 40
	eventuallyInterval = time.Second * 5
)

var (
	ctx    = context.Background()
	scheme = runtime.NewScheme()
	once   = sync.Once{}

	hubCluster     *framework.Cluster
	memberCluster1 *framework.Cluster
	memberCluster2 *framework.Cluster
	memberCluster3 *framework.Cluster

	hubClient            client.Client
	memberCluster1Client client.Client
	memberCluster2Client client.Client
	memberCluster3Client client.Client

	allMemberClusters     []*framework.Cluster
	allMemberClusterNames = []string{}
)

var (
	regionLabelName   = "region"
	regionLabelValue1 = "east"
	regionLabelValue2 = "west"
	envLabelName      = "env"
	envLabelValue1    = "prod"
	envLabelValue2    = "canary"

	labelsByClusterName = map[string]map[string]string{
		memberCluster1Name: {
			regionLabelName: regionLabelValue1,
			envLabelName:    envLabelValue1,
		},
		memberCluster2Name: {
			regionLabelName: regionLabelValue1,
			envLabelName:    envLabelValue2,
		},
		memberCluster3Name: {
			regionLabelName: regionLabelValue2,
			envLabelName:    envLabelValue1,
		},
	}
)

var (
	lessFuncCondition = func(a, b metav1.Condition) bool {
		return a.Type < b.Type
	}
	lessFuncPlacementStatus = func(a, b placementv1beta1.ResourcePlacementStatus) bool {
		return a.ClusterName < b.ClusterName
	}

	resourceIdentifierStringFormat = "%s/%s/%s/%s/%s"
	lessFuncResourceIdentifier     = func(a, b placementv1beta1.ResourceIdentifier) bool {
		aStr := fmt.Sprintf(resourceIdentifierStringFormat, a.Group, a.Version, a.Kind, a.Namespace, a.Name)
		bStr := fmt.Sprintf(resourceIdentifierStringFormat, b.Group, b.Version, b.Kind, b.Namespace, b.Name)
		return aStr < bStr
	}

	ignoreObjectMetaAutoGeneratedFields      = cmpopts.IgnoreFields(metav1.ObjectMeta{}, "UID", "CreationTimestamp", "ResourceVersion", "Generation", "ManagedFields", "OwnerReferences")
	ignoreObjectMetaAnnotationField          = cmpopts.IgnoreFields(metav1.ObjectMeta{}, "Annotations")
	ignoreConditionObservedGenerationField   = cmpopts.IgnoreFields(metav1.Condition{}, "ObservedGeneration")
	ignoreConditionLTTReasonAndMessageFields = cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime", "Reason", "Message")
	ignoreAgentStatusHeartbeatField          = cmpopts.IgnoreFields(clusterv1beta1.AgentStatus{}, "LastReceivedHeartbeat")
	ignoreNamespaceStatusField               = cmpopts.IgnoreFields(corev1.Namespace{}, "Status")

	crpStatusCmpOptions = cmp.Options{
		cmpopts.SortSlices(lessFuncCondition),
		cmpopts.SortSlices(lessFuncPlacementStatus),
		cmpopts.SortSlices(lessFuncResourceIdentifier),
		ignoreConditionLTTReasonAndMessageFields,
		cmpopts.EquateEmpty(),
	}
)

// TestMain sets up the E2E test environment.
func TestMain(m *testing.M) {
	// Add custom APIs to the scheme.
	if err := clusterv1beta1.AddToScheme(scheme); err != nil {
		log.Fatalf("failed to add custom APIs (cluster) to the runtime scheme: %v", err)
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

	os.Exit(m.Run())
}

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Fleet E2E Test Suite (with v1beta1 APIs)")
}

func beforeSuiteForAllProcesses() {
	klog.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	// Check if the required environment variable, which specifies the path to kubeconfig file, has been set.
	Expect(os.Getenv(kubeConfigPathEnvVarName)).NotTo(BeEmpty(), "Required environment variable KUBECONFIG is not set")

	// Initialize the cluster objects and their clients.
	hubCluster = framework.NewCluster(hubClusterName, scheme)
	Expect(hubCluster).NotTo(BeNil(), "Failed to initialize cluster object")
	framework.GetClusterClient(hubCluster)
	hubClient = hubCluster.KubeClient
	Expect(hubClient).NotTo(BeNil(), "Failed to initialize client for accessing Kubernetes cluster")

	memberCluster1 = framework.NewCluster(memberCluster1Name, scheme)
	Expect(memberCluster1).NotTo(BeNil(), "Failed to initialize cluster object")
	framework.GetClusterClient(memberCluster1)
	memberCluster1Client = memberCluster1.KubeClient
	Expect(memberCluster1Client).NotTo(BeNil(), "Failed to initialize client for accessing Kubernetes cluster")

	memberCluster2 = framework.NewCluster(memberCluster2Name, scheme)
	Expect(memberCluster2).NotTo(BeNil(), "Failed to initialize cluster object")
	framework.GetClusterClient(memberCluster2)
	memberCluster2Client = memberCluster2.KubeClient
	Expect(memberCluster2Client).NotTo(BeNil(), "Failed to initialize client for accessing Kubernetes cluster")

	memberCluster3 = framework.NewCluster(memberCluster3Name, scheme)
	Expect(memberCluster3).NotTo(BeNil(), "Failed to initialize cluster object")
	framework.GetClusterClient(memberCluster3)
	memberCluster3Client = memberCluster3.KubeClient
	Expect(memberCluster3Client).NotTo(BeNil(), "Failed to initialize client for accessing kubernetes cluster")

	allMemberClusters = []*framework.Cluster{memberCluster1, memberCluster2, memberCluster3}
	once.Do(func() {
		// Set these arrays only once; this is necessary as for the first spawned Ginkgo process,
		// the `beforeSuiteForAllProcesses` function is called twice.
		for _, cluster := range allMemberClusters {
			allMemberClusterNames = append(allMemberClusterNames, cluster.ClusterName)
		}
	})
}

func beforeSuiteForProcess1() {
	beforeSuiteForAllProcesses()

	setAllMemberClustersToJoin()
	checkIfAllMemberClustersHaveJoined()

	// Simulate that member cluster 4 become unhealthy, and member cluster 5 has left the fleet.
	//
	// Note that these clusters are not real kind clusters.
	setupInvalidClusters()
}

var _ = SynchronizedBeforeSuite(beforeSuiteForProcess1, beforeSuiteForAllProcesses)

var _ = SynchronizedAfterSuite(func() {}, func() {
	setAllMemberClustersToLeave()
	checkIfAllMemberClustersHaveLeft()

	cleanupInvalidClusters()
})
