package utils

import (
	"context"
	"fmt"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
)

func (r *Runner) CleanUp(ctx context.Context) {
	wg := sync.WaitGroup{}

	// Run the producer.
	wg.Add(1)
	go func() {
		defer wg.Done()

		for i := 0; i < r.maxCRPToCreateCount; i++ {
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

				// Delete the CRPs.
				crp := placementv1beta1.ClusterResourcePlacement{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(placementNameFmt, resIdx),
					},
				}
				errAfterRetries := retry.OnError(r.retryOpsBackoff, func(err error) bool {
					return err != nil && !errors.IsNotFound(err)
				}, func() error {
					return r.hubClient.Delete(ctx, &crp)
				})
				if errAfterRetries != nil && !errors.IsNotFound(errAfterRetries) {
					fmt.Printf("worker %d: failed to delete CRP %s after retries: %v\n", workerIdx, fmt.Sprintf(placementNameFmt, resIdx), errAfterRetries)
					continue
				}

				// Wait until the CRP is deleted.
				errAfterRetries = retry.OnError(r.retryOpsBackoff, func(err error) bool {
					return err != nil && !errors.IsNotFound(err)
				}, func() error {
					crp := placementv1beta1.ClusterResourcePlacement{}
					err := r.hubClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(placementNameFmt, resIdx)}, &crp)
					if err == nil {
						return fmt.Errorf("CRP %s still exists", fmt.Sprintf(placementNameFmt, resIdx))
					}
					return err
				})
				if !errors.IsNotFound(errAfterRetries) {
					fmt.Printf("worker %d: failed to wait for CRP %s to be deleted after retries: %v\n", workerIdx, fmt.Sprintf(placementNameFmt, resIdx), errAfterRetries)
				} else {
					fmt.Printf("worker %d: deleted CRP %s\n", workerIdx, fmt.Sprintf(placementNameFmt, resIdx))
				}

				// Delete the namespace if it exists.
				namespace := corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf(nsNameFmt, resIdx),
					},
				}
				errAfterRetries = retry.OnError(r.retryOpsBackoff, func(err error) bool {
					return err != nil && !errors.IsNotFound(err)
				}, func() error {
					return r.hubClient.Delete(ctx, &namespace)
				})
				if errAfterRetries != nil && !errors.IsNotFound(errAfterRetries) {
					fmt.Printf("worker %d: failed to delete namespace %s after retries: %v\n", workerIdx, fmt.Sprintf(nsNameFmt, resIdx), errAfterRetries)
					continue
				}

				// Wait until the namespace is deleted.
				errAfterRetries = retry.OnError(r.retryOpsBackoff, func(err error) bool {
					return err != nil && !errors.IsNotFound(err)
				}, func() error {
					namespace := corev1.Namespace{}
					err := r.hubClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf(nsNameFmt, resIdx)}, &namespace)
					if err == nil {
						return fmt.Errorf("namespace %s still exists", fmt.Sprintf(nsNameFmt, resIdx))
					}
					return err
				})
				if errAfterRetries == nil || !errors.IsNotFound(errAfterRetries) {
					fmt.Printf("worker %d: failed to wait for namespace %s to be deleted after retries: %v\n", workerIdx, fmt.Sprintf(nsNameFmt, resIdx), errAfterRetries)
				} else {
					fmt.Printf("worker %d: deleted namespace %s\n", workerIdx, fmt.Sprintf(nsNameFmt, resIdx))
				}
			}
		}(i)
	}
	wg.Wait()

	// Cool down.
	time.Sleep(r.betweenStageCoolDownPeriod)
}
