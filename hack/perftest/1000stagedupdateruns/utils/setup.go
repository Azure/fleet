package utils

import (
	"fmt"
	"sync/atomic"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
)

const (
	nsNameFmt        = "work-%d"
	configMapNameFmt = "data-%d"
	deployNameFmt    = "app-%d"

	placementNameFmt = "crp-%d"

	stagedUpdateRunNameFmt = "run-%d"
)

const (
	commonStagedUpdateRunStrategyName = "staging-canary-prod"

	envLabelKey          = "env"
	stagingEnvLabelValue = "staging"
	canaryEnvLabelValue  = "canary"
	prodEnvLabelValue    = "prod"

	randomUUIDLabelKey = "random-uuid"
)

type latencyTrackAttempt struct {
	latency time.Duration
	resIdx  int
}

type Runner struct {
	hubClient client.Client

	resourceSetupWorkerCount   int
	longPollingWorkerCount     int
	betweenStageCoolDownPeriod time.Duration
	longPollingCoolDownPeriod  time.Duration

	maxConcurrencyPerStage string
	maxCRPToUpdateCount    int

	retryOpsBackoff wait.Backoff

	toDeleteChan                   chan int
	toPatchResourcesChan           chan int
	toCreateStagedUpdateRunsChan   chan int
	toLongPollStagedUpdateRunsChan chan int
	toTrackLatencyChan             chan latencyTrackAttempt

	resourcesPatchedCount            atomic.Int32
	longPollingStagedUpdateRunsCount atomic.Int32
	completedStagedUpdateRunCount    atomic.Int32

	stagedUpdateRunCompletionLatencyByRunName map[string]time.Duration
}

func New(
	concurrentWorkerCount, longPollingWorkerCount int,
	betweenStageCoolDownPeriod, longPollingCoolDownPeriod time.Duration,
	maxConcurrencyPerStage string,
	maxCRPToUpdateCount int,
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
		hubClient:                                 hubClient,
		resourceSetupWorkerCount:                  concurrentWorkerCount,
		longPollingWorkerCount:                    longPollingWorkerCount,
		betweenStageCoolDownPeriod:                betweenStageCoolDownPeriod,
		longPollingCoolDownPeriod:                 longPollingCoolDownPeriod,
		maxConcurrencyPerStage:                    maxConcurrencyPerStage,
		maxCRPToUpdateCount:                       maxCRPToUpdateCount,
		retryOpsBackoff:                           retryOpsBackoff,
		toDeleteChan:                              make(chan int, 20),
		toPatchResourcesChan:                      make(chan int, maxCRPToUpdateCount),
		toCreateStagedUpdateRunsChan:              make(chan int, 20),
		toLongPollStagedUpdateRunsChan:            make(chan int, maxCRPToUpdateCount),
		toTrackLatencyChan:                        make(chan latencyTrackAttempt, maxCRPToUpdateCount+1),
		stagedUpdateRunCompletionLatencyByRunName: make(map[string]time.Duration, maxCRPToUpdateCount),
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
