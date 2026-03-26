package utils

import (
	"context"
	"fmt"
	"sync"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
)

func (r *Runner) CleanUp(ctx context.Context) {
	r.cleanUpStrategy(ctx)
	r.cleanUpRuns(ctx)
}

func (r *Runner) cleanUpStrategy(ctx context.Context) {
	stagedUpdateRunStrategy := &placementv1beta1.ClusterStagedUpdateStrategy{
		ObjectMeta: metav1.ObjectMeta{
			Name: commonStagedUpdateRunStrategyName,
		},
	}

	errAfterRetries := retry.OnError(r.retryOpsBackoff, func(err error) bool {
		return err != nil && !errors.IsNotFound(err)
	}, func() error {
		return r.hubClient.Delete(ctx, stagedUpdateRunStrategy)
	})
	if errAfterRetries != nil && !errors.IsNotFound(errAfterRetries) {
		fmt.Printf("failed to delete staged update run strategy %s after retries: %v\n", commonStagedUpdateRunStrategyName, errAfterRetries)
	}
}

func (r *Runner) cleanUpRuns(ctx context.Context) {
	wg := sync.WaitGroup{}

	// Run the producer.
	wg.Add(1)
	go func() {
		defer wg.Done()

		for i := 0; i < r.maxCRPToUpdateCount; i++ {
			select {
			case r.toDeleteChan <- i:
			case <-ctx.Done():
				close(r.toDeleteChan)
				return
			}
		}

		close(r.toDeleteChan)
	}()

	// Run the workers.
	for i := 0; i < r.resourceSetupWorkerCount; i++ {
		wg.Add(1)
		go func(workerIdx int) {
			defer wg.Done()

			for {
				// Read from the channel.
				var resIdx int
				var readOk bool
				select {
				case resIdx, readOk = <-r.toDeleteChan:
					if !readOk {
						fmt.Printf("worker %d exits\n", workerIdx)
						return
					}
				case <-ctx.Done():
					return
				}

				// Delete the staged update run.
				stagedUpdateRun := &placementv1beta1.ClusterStagedUpdateRun{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(stagedUpdateRunNameFmt, resIdx),
					},
				}
				errAfterRetries := retry.OnError(r.retryOpsBackoff, func(err error) bool {
					return err != nil && !errors.IsNotFound(err)
				}, func() error {
					return r.hubClient.Delete(ctx, stagedUpdateRun)
				})
				if errAfterRetries != nil && !errors.IsNotFound(errAfterRetries) {
					fmt.Printf("worker %d: failed to delete staged update run %s after retries: %v\n", workerIdx, stagedUpdateRun.Name, errAfterRetries)
					continue
				}

				// Wait until the staged update run is deleted.
				errAfterRetries = retry.OnError(r.retryOpsBackoff, func(err error) bool {
					return err != nil && !errors.IsNotFound(err)
				}, func() error {
					stagedUpdateRun := &placementv1beta1.ClusterStagedUpdateRun{}
					err := r.hubClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(stagedUpdateRunNameFmt, resIdx)}, stagedUpdateRun)
					if err == nil {
						return fmt.Errorf("staged update run %s still exists", stagedUpdateRun.Name)
					}
					return err
				})
				if errAfterRetries == nil || !errors.IsNotFound(errAfterRetries) {
					fmt.Printf("worker %d: failed to wait for staged update run %s to be deleted after retries: %v\n", workerIdx, stagedUpdateRun.Name, errAfterRetries)
				} else {
					fmt.Printf("worker %d: deleted staged update run %s\n", workerIdx, stagedUpdateRun.Name)
				}
			}
		}(i)
	}
	wg.Wait()
}
