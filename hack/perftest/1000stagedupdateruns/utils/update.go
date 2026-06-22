package utils

import (
	"context"
	"fmt"
	"sync"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/util/retry"
)

func (r *Runner) UpdateResources(ctx context.Context) {
	wg := sync.WaitGroup{}

	// Run the producer.
	wg.Add(1)
	go func() {
		defer wg.Done()

		for i := 0; i < r.maxCRPToUpdateCount; i++ {
			select {
			case r.toPatchResourcesChan <- i:
			case <-ctx.Done():
				close(r.toPatchResourcesChan)
				return
			}
		}

		close(r.toPatchResourcesChan)
	}()

	// Run the workers to add a label to each existing resource.
	for i := 0; i < r.resourceSetupWorkerCount; i++ {
		wg.Add(1)
		go func(workerIdx int) {
			defer wg.Done()

			for {
				// Read from the channel.
				var resIdx int
				var readOk bool
				select {
				case resIdx, readOk = <-r.toPatchResourcesChan:
					if !readOk {
						fmt.Printf("worker %d exits\n", workerIdx)
						return
					}
				case <-ctx.Done():
					return
				}

				if err := r.updateNS(ctx, workerIdx, resIdx); err != nil {
					fmt.Printf("worker %d: failed to update namespace work-%d: %v\n", workerIdx, resIdx, err)
					continue
				}

				if err := r.updateConfigMap(ctx, workerIdx, resIdx); err != nil {
					fmt.Printf("worker %d: failed to update configmap data-%d: %v\n", workerIdx, resIdx, err)
					continue
				}

				if err := r.updateDeploy(ctx, workerIdx, resIdx); err != nil {
					fmt.Printf("worker %d: failed to update deployment deploy-%d: %v\n", workerIdx, resIdx, err)
					continue
				}

				fmt.Printf("worker %d: successfully updated resources group of idx %d\n", workerIdx, resIdx)

				r.resourcesPatchedCount.Add(1)
			}
		}(i)
	}

	wg.Wait()

	// Do a sanity check report.
	fmt.Printf("patched %d out of %d resources in total\n", r.resourcesPatchedCount.Load(), r.maxCRPToUpdateCount)
}

func (r *Runner) updateNS(ctx context.Context, workerIdx, resIdx int) error {
	nsName := fmt.Sprintf(nsNameFmt, resIdx)

	// Update the namespace with the new label.
	errAfterRetries := retry.OnError(r.retryOpsBackoff, func(err error) bool {
		return err != nil
	}, func() error {
		ns := &corev1.Namespace{}
		if err := r.hubClient.Get(ctx, types.NamespacedName{Name: nsName}, ns); err != nil {
			return fmt.Errorf("failed to get namespace %s before update: %w", nsName, err)
		}

		labels := ns.GetLabels()
		if labels == nil {
			labels = make(map[string]string)
		}
		labels[randomUUIDLabelKey] = string(uuid.NewUUID())
		ns.SetLabels(labels)

		if err := r.hubClient.Update(ctx, ns); err != nil {
			return fmt.Errorf("failed to update namespace %s: %w", nsName, err)
		}
		return nil
	})
	return errAfterRetries
}

func (r *Runner) updateConfigMap(ctx context.Context, workerIdx, resIdx int) error {
	cmName := fmt.Sprintf(configMapNameFmt, resIdx)
	cmNamespace := fmt.Sprintf(nsNameFmt, resIdx)

	// Update the configmap with the new label.
	errAfterRetries := retry.OnError(r.retryOpsBackoff, func(err error) bool {
		return err != nil
	}, func() error {
		cm := &corev1.ConfigMap{}
		if err := r.hubClient.Get(ctx, types.NamespacedName{Name: cmName, Namespace: cmNamespace}, cm); err != nil {
			return fmt.Errorf("failed to get configmap %s in namespace %s before update: %w", cmName, cmNamespace, err)
		}

		labels := cm.GetLabels()
		if labels == nil {
			labels = make(map[string]string)
		}
		labels[randomUUIDLabelKey] = string(uuid.NewUUID())
		cm.SetLabels(labels)

		if err := r.hubClient.Update(ctx, cm); err != nil {
			return fmt.Errorf("failed to update configmap %s in namespace %s: %w", cmName, cmNamespace, err)
		}
		return nil
	})
	return errAfterRetries
}

func (r *Runner) updateDeploy(ctx context.Context, workerIdx, resIdx int) error {
	deployName := fmt.Sprintf(deployNameFmt, resIdx)
	deployNamespace := fmt.Sprintf(nsNameFmt, resIdx)

	// Update the deployment with the new label.
	errAfterRetries := retry.OnError(r.retryOpsBackoff, func(err error) bool {
		return err != nil
	}, func() error {
		deploy := &appsv1.Deployment{}
		if err := r.hubClient.Get(ctx, types.NamespacedName{Name: deployName, Namespace: deployNamespace}, deploy); err != nil {
			return fmt.Errorf("failed to get deployment %s in namespace %s before update: %w", deployName, deployNamespace, err)
		}

		labels := deploy.GetLabels()
		if labels == nil {
			labels = make(map[string]string)
		}
		labels[randomUUIDLabelKey] = string(uuid.NewUUID())
		deploy.SetLabels(labels)

		if err := r.hubClient.Update(ctx, deploy); err != nil {
			return fmt.Errorf("failed to update deployment %s in namespace %s: %w", deployName, deployNamespace, err)
		}
		return nil
	})
	return errAfterRetries
}
