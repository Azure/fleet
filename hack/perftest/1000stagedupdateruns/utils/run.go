package utils

import (
	"context"
	"fmt"
	"sync"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
)

func (r *Runner) CreateStagedUpdateRuns(ctx context.Context) {
	wg := sync.WaitGroup{}

	// Run the producer.
	wg.Add(1)
	go func() {
		defer wg.Done()

		for i := 0; i < r.maxCRPToUpdateCount; i++ {
			select {
			case r.toCreateStagedUpdateRunsChan <- i:
			case <-ctx.Done():
				close(r.toCreateStagedUpdateRunsChan)
				return
			}
		}

		close(r.toCreateStagedUpdateRunsChan)
	}()

	// Run the workers to create staged update runs.
	for i := 0; i < r.resourceSetupWorkerCount; i++ {
		wg.Add(1)

		go func(workerIdx int) {
			defer wg.Done()

			for {
				// Read from the channel.
				var resIdx int
				var readOk bool
				select {
				case resIdx, readOk = <-r.toCreateStagedUpdateRunsChan:
					if !readOk {
						fmt.Printf("worker %d exits\n", workerIdx)
						return
					}
				case <-ctx.Done():
					return
				}

				// Create the staged update run.
				stagedUpdateRun := placementv1beta1.ClusterStagedUpdateRun{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(stagedUpdateRunNameFmt, resIdx),
					},
					Spec: placementv1beta1.UpdateRunSpec{
						PlacementName:            fmt.Sprintf(placementNameFmt, resIdx),
						StagedUpdateStrategyName: commonStagedUpdateRunStrategyName,
						State:                    placementv1beta1.StateRun,
					},
				}
				errAfterRetries := retry.OnError(r.retryOpsBackoff, func(err error) bool {
					return err != nil && !errors.IsAlreadyExists(err)
				}, func() error {
					return r.hubClient.Create(ctx, &stagedUpdateRun)
				})
				if errAfterRetries != nil && !errors.IsAlreadyExists(errAfterRetries) {
					fmt.Printf("worker %d: failed to create staged update run run-%d after retries: %v\n", workerIdx, resIdx, errAfterRetries)
					continue
				}
				fmt.Printf("worker %d: successfully created staged update run run-%d\n", workerIdx, resIdx)

				// Submit the run for long polling.
				r.toLongPollStagedUpdateRunsChan <- resIdx
				r.longPollingStagedUpdateRunsCount.Add(1)
			}
		}(i)
	}

	wg.Wait()
}
