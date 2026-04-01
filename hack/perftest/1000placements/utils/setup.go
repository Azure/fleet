package utils

import (
	"fmt"
	"os/exec"
	"sync/atomic"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"

	"k8s.io/apimachinery/pkg/util/wait"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
)

const (
	nsNameFmt        = "work-%d"
	configMapNameFmt = "data-%d"
	deployNameFmt    = "app-%d"

	placementNameFmt = "crp-%d"
)

const (
	placementGroupLabelKey = "placement-group"
)

type latencyTrackAttempt struct {
	latency time.Duration
	resIdx  int
}

type Runner struct {
	runName   string
	hubClient client.Client

	resourceSetupWorkerCount       int
	longPollingWorkerCount         int
	betweenStageCoolDownPeriod     time.Duration
	resourceCreationCoolDownPeriod time.Duration
	longPollingCoolDownPeriod      time.Duration

	configMapDataByteCount int

	maxCRPToCreateCount int

	triggerPtsForMemProfileDumping map[int]bool
	pProfEndpoint                  string

	retryOpsBackoff wait.Backoff

	toDeleteChan             chan int
	toCreateResourcesChan    chan int
	toCreatePlacementsChan   chan int
	toLongPollPlacementsChan chan int
	toTrackLatencyChan       chan latencyTrackAttempt

	longPollingPlacementsCount atomic.Int32
	completedPlacementCount    atomic.Int32

	placementCompletionLatencyByName map[string]time.Duration
}

func New(
	runName string,
	resourceSetupWorkerCount, longPollingWorkerCount int,
	betweenStageCoolDownPeriod, resourceCreationCoolDownPeriod, longPollingCoolDownPeriod time.Duration,
	configMapDataByteCount, maxCRPToCreateCount int,
	triggerPtsForMemProfileDumping map[int]bool,
	pProfEndpoint string,
	retryOpsBackoff wait.Backoff,
) *Runner {
	// Set up the K8s client for the hub cluster.
	hubClusterConfig := ctrl.GetConfigOrDie()
	hubClusterConfig.QPS = 200
	hubClusterConfig.Burst = 400
	hubClient, err := client.New(hubClusterConfig, client.Options{
		Scheme: scheme.Scheme,
	})
	if err != nil {
		panic(fmt.Sprintf("Failed to create hub client: %v", err))
	}

	return &Runner{
		runName:                          runName,
		hubClient:                        hubClient,
		resourceSetupWorkerCount:         resourceSetupWorkerCount,
		longPollingWorkerCount:           longPollingWorkerCount,
		betweenStageCoolDownPeriod:       betweenStageCoolDownPeriod,
		resourceCreationCoolDownPeriod:   resourceCreationCoolDownPeriod,
		longPollingCoolDownPeriod:        longPollingCoolDownPeriod,
		configMapDataByteCount:           configMapDataByteCount,
		maxCRPToCreateCount:              maxCRPToCreateCount,
		triggerPtsForMemProfileDumping:   triggerPtsForMemProfileDumping,
		pProfEndpoint:                    pProfEndpoint,
		retryOpsBackoff:                  retryOpsBackoff,
		toDeleteChan:                     make(chan int, 20),
		toCreateResourcesChan:            make(chan int, maxCRPToCreateCount),
		toCreatePlacementsChan:           make(chan int, 20),
		toLongPollPlacementsChan:         make(chan int, maxCRPToCreateCount),
		toTrackLatencyChan:               make(chan latencyTrackAttempt, maxCRPToCreateCount+1),
		placementCompletionLatencyByName: make(map[string]time.Duration, maxCRPToCreateCount),
	}
}

func init() {
	// Set up the scheme.
	if err := placementv1beta1.AddToScheme(scheme.Scheme); err != nil {
		panic(fmt.Sprintf("Failed to add placement v1beta1 APIs to the scheme: %v", err))
	}
	if err := corev1.AddToScheme(scheme.Scheme); err != nil {
		panic(fmt.Sprintf("Failed to add core v1 APIs to the scheme: %v", err))
	}
	if err := appsv1.AddToScheme(scheme.Scheme); err != nil {
		panic(fmt.Sprintf("Failed to add apps v1 APIs to the scheme: %v", err))
	}
}

func (r *Runner) CoolDown() {
	time.Sleep(r.betweenStageCoolDownPeriod)
}

func (r *Runner) RetrievePProfProfile(retrievalIdx int) {
	cmd := exec.Command("curl", "-o", fmt.Sprintf("pprof_%s_%d.out", r.runName, retrievalIdx), r.pProfEndpoint) // #nosec G204
	if err := cmd.Run(); err != nil {
		fmt.Printf("Failed to run curl command to retrieve pprof data: %v\n", err)
		return
	}
}
