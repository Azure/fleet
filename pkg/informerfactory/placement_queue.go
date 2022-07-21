package informerfactory

import (
	"context"
	"encoding/json"
	"fmt"
	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/work-api/pkg/apis/v1alpha1"
	"time"
)

type PlacementQueue struct {
	cf         InformerFactory
	client     client.Client
	scheme     *runtime.Scheme
	tasks      workqueue.RateLimitingInterface
	stopCh     <-chan struct{}
	numworkers int
}

func NewPlacementQueue(cf InformerFactory, client client.Client, scheme *runtime.Scheme, stopCh <-chan struct{}) *PlacementQueue {
	return &PlacementQueue{
		cf:     cf,
		tasks:  workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "placement-queue"),
		client: client,
		scheme: scheme,
		stopCh: stopCh,
	}
}

func (d *PlacementQueue) Deliver(placement *fleetv1alpha1.ClusterResourcePlacement, workload runtime.Object, gvk schema.GroupVersionKind) {
	workloadAccessor, err := meta.Accessor(workload)
	if err != nil {
		klog.Errorf("failed to deliver to placement task queue, failed to get %+v accessor: err %+v", workload, err)
		return
	}
	key := fmt.Sprintf("%s/%s/%s", gvk.Kind, workloadAccessor.GetNamespace(), workloadAccessor.GetName())
	task := taskItem{
		placement:   placement,
		resourceKey: key,
	}
	d.tasks.Add(task)
	klog.Infof("%s deliverered to placement task queue: due to %s change", task.placement.Name, key)
}

func (d *PlacementQueue) Run() {
	// work on its tasks queue
	go wait.Until(func() {
		klog.Infof("Starting processing placement task queue...")
		for d.processTask() {
		}
	}, 0, d.stopCh)

	// ensure task queue is shutdown when stopch closes
	go func() {
		<-d.stopCh
		d.tasks.ShutDown()
	}()
}

type taskItem struct {
	placement   *fleetv1alpha1.ClusterResourcePlacement
	resourceKey string
}

func (d *PlacementQueue) processTask() bool {
	task, quit := d.tasks.Get()
	if quit {
		klog.Infof("deliverer task wq shutting down...")
		return false
	}
	defer d.tasks.Done(task)

	item, ok := task.(taskItem)
	if !ok {
		klog.Errorf("not ClusterResourcePlacement, got %+v", item)
		return true
	}
	klog.Infof("creating work crd based from crp %s, due to resource %s changed", item.placement.Name, item.resourceKey)
	// add work crd
	crpKey := types.NamespacedName{Namespace: item.placement.Namespace,
		Name: item.placement.Name}
	allResources, allTargetedClusters, err := d.createOrUpdateWorkSpec(crpKey)
	if err != nil {
		klog.Errorf("!!failed to update work cr %s, err: %+v", crpKey.String(), err)
		d.tasks.AddRateLimited(task)
		return true
	}
	err = d.updatePlacementStatus(crpKey, allResources, allTargetedClusters)
	if err != nil {
		klog.Errorf("!!failed to update crp status %s, err: %+v", crpKey.String(), err)
		d.tasks.AddRateLimited(task)
		return true
	}
	return true
}

func (d *PlacementQueue) createOrUpdateWorkSpec(crpNamespaceName types.NamespacedName) ([]interface{}, []string, error) {
	var memberClusterNames []string
	allResources := make([]interface{}, 0)
	crp := &fleetv1alpha1.ClusterResourcePlacement{}
	err := d.client.Get(context.TODO(), crpNamespaceName, crp)
	if err != nil {
		if apierrors.IsNotFound(err) {
			klog.Infof("crp %s has been deleted, ignore", crpNamespaceName)
			return allResources, memberClusterNames, nil
		}
		return allResources, memberClusterNames, err
	}
	memberClusterNames, err = selectClusters(crp, d.client)
	if err != nil {
		return allResources, memberClusterNames, err
	}

	for _, memberClusterName := range memberClusterNames {
		resources, err := listAllResourcesFromResourceSelectors(d.cf, crp.Spec.ResourceSelectors)
		allResources = append(allResources, resources...)
		if err != nil {
			return allResources, memberClusterNames, err
		}
		manifests := make([]v1alpha1.Manifest, 0)
		for i := range resources {
			unstructuredObj := resources[i].(*unstructured.Unstructured)
			rawContent, _ := json.Marshal(unstructuredObj)
			manifests = append(manifests, v1alpha1.Manifest{
				RawExtension: runtime.RawExtension{Raw: rawContent},
			})
		}
		workCRObjectKey := client.ObjectKey{Name: crp.Name, Namespace: getWorkCRNamespace(memberClusterName, crp.Name)}

		curWorkCR := &v1alpha1.Work{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: workCRObjectKey.Namespace,
				Name:      workCRObjectKey.Name,
			},
		}
		err = d.client.Get(context.TODO(), workCRObjectKey, curWorkCR)
		if err != nil && !apierrors.IsNotFound(err) {
			return allResources, memberClusterNames, err
		}

		// 1. make sure namespace is created
		ns := &v1.Namespace{}
		ns.Name = workCRObjectKey.Namespace
		err = d.client.Get(context.TODO(), types.NamespacedName{Name: workCRObjectKey.Namespace}, ns)
		if err != nil {
			if apierrors.IsNotFound(err) {
				// create one
				err = d.client.Create(context.TODO(), ns)
				if err != nil {
					klog.Errorf("failed to create namespace %s: err %+v", ns.Name, err)
					return allResources, memberClusterNames, err
				}
			} else {
				klog.Errorf("failed to get namespace %s: err %+v", ns.Name, err)
				return allResources, memberClusterNames, err
			}
		}

		desiredWorkCR := &v1alpha1.Work{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: getWorkCRNamespace(memberClusterName, crp.Name),
				Name:      crp.Name,
			},
			Spec: v1alpha1.WorkSpec{
				Workload: v1alpha1.WorkloadTemplate{
					Manifests: manifests,
				},
			},
		}
		err = controllerutil.SetControllerReference(crp, desiredWorkCR, d.scheme)
		if err != nil {
			return allResources, memberClusterNames, err
		}
		opResult, err := controllerutil.CreateOrUpdate(context.TODO(),
			d.client, curWorkCR, WorkSpecMutator(curWorkCR, desiredWorkCR))
		if err != nil {
			return allResources, memberClusterNames, err
		}

		klog.Infof("updated work spec %s/%s with %d manifests: %+v", desiredWorkCR.Namespace, desiredWorkCR.Name, len(manifests), opResult)
	}
	return allResources, memberClusterNames, nil
}

func (d *PlacementQueue) updatePlacementStatus(crpNamespaceName types.NamespacedName, allResource []interface{}, allTargetedClusters []string) error {
	crp := &fleetv1alpha1.ClusterResourcePlacement{}
	err := d.client.Get(context.TODO(), crpNamespaceName, crp)
	if err != nil {
		if apierrors.IsNotFound(err) {
			klog.Infof("crp %s has been deleted, ignore", crpNamespaceName)
			return nil
		}
		return err
	}
	crp.Status.TargetClusters = allTargetedClusters
	selectedResources := make([]fleetv1alpha1.ResourceIdentifier, 0)
	for _, res := range allResource {
		accessor, _ := meta.Accessor(res)
		obj, ok := res.(runtime.Object)
		if !ok || obj == nil {
			continue
		}
		gvk := obj.GetObjectKind().GroupVersionKind()
		selectedResources = append(selectedResources, fleetv1alpha1.ResourceIdentifier{
			Group:     gvk.Group,
			Version:   gvk.Version,
			Kind:      gvk.Kind,
			Name:      accessor.GetName(),
			Namespace: accessor.GetNamespace(),
		})
	}
	crp.Status.SelectedResources = selectedResources
	crp.Status.Conditions = []metav1.Condition{
		{
			Status:             metav1.ConditionTrue,
			LastTransitionTime: metav1.Time{Time: time.Now()},
			Type:               "AbleToSyncResources",
			Reason:             "StatusSynced",
			Message:            fmt.Sprintf("synced %d resources from %+v member clusters", len(allResource), allTargetedClusters),
		},
	}
	return d.client.Status().Update(context.TODO(), crp)
}

func WorkSpecMutator(curWork *v1alpha1.Work, desiredWork *v1alpha1.Work) controllerutil.MutateFn {
	return func() error {
		if curWork.ObjectMeta.CreationTimestamp.IsZero() {
			*curWork = *desiredWork
			return nil
		}
		curWork.Spec = desiredWork.Spec
		return nil
	}
}

func getWorkCRNamespace(memberClusterName, crpName string) string {
	return fmt.Sprintf("fleet-%s", memberClusterName)
}
