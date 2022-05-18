/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package memberinternalmembercluster

import (
	"context"
	"sync"
	"time"

	"github.com/pkg/errors"
	v12 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"go.goms.io/fleet/apis"
	"go.goms.io/fleet/pkg/controllers/common"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
)

// TODO (mng): move `util` pkg to `controller/util`

// Reconciler reconciles a InternalMemberCluster object in the member cluster.
type Reconciler struct {
	hubClient                 client.Client
	memberClient              client.Client
	restMapper                meta.RESTMapper
	recorder                  record.EventRecorder
	internalMemberClusterChan chan<- fleetv1alpha1.ClusterState
	membershipChan            <-chan fleetv1alpha1.ClusterState
	membershipState           fleetv1alpha1.ClusterState
	membershipStateLock       sync.RWMutex
}

// NewReconciler creates a new reconciler for the internal membership CR
func NewReconciler(hubClient client.Client, memberClient client.Client, restMapper meta.RESTMapper,
	internalMemberClusterChan chan<- fleetv1alpha1.ClusterState,
	membershipChan <-chan fleetv1alpha1.ClusterState) *Reconciler {
	return &Reconciler{
		hubClient:                 hubClient,
		memberClient:              memberClient,
		restMapper:                restMapper,
		internalMemberClusterChan: internalMemberClusterChan,
		membershipChan:            membershipChan,
	}
}

type ClusterUsage struct {
	AllocatableCPU    resource.Quantity
	AllocatableMemory resource.Quantity
	CapacityCPU       resource.Quantity
	CapacityMemory    resource.Quantity
}

// Reconcile event reasons.
const (
	errJoiningInternalMemberCluster = "error joining internal member cluster"
	errNoHeartbeatReceived          = "no heartbeat received"
)

const (
	reasonInternalMemberClusterJoined            = "InternalMemberClusterJoined"
	reasonInternalMemberClusterJoinUnknown       = "InternalMemberClusterJoinUnknown"
	reasonInternalMemberClusterHeartbeatReceived = "InternalMemberClusterHeartbeatReceived"
	reasonInternalMemberClusterHeartbeatUnknown  = "InternalMemberClusterHeartbeatUnknown"
)

func checkHeartbeatReceived(nodes v12.NodeList, clusterHeartbeat int32) bool {
	for _, node := range nodes.Items {
		// check if node is master node https://v1-20.docs.kubernetes.io/docs/setup/release/notes/#urgent-upgrade-notes
		if _, ok := node.Labels["node-role.kubernetes.io/control-plane"]; ok {
			clusterHeartbeatInNanoSecond := time.Duration(clusterHeartbeat * 1000000000)
			if len(node.Status.Conditions) > 0 &&
				node.Status.Conditions[0].LastHeartbeatTime.Add(clusterHeartbeatInNanoSecond).After(time.Now()) {
				return true
			}
		}
	}
	return false
}

func getClusterUsage(nodes v12.NodeList) ClusterUsage {
	currentClusterUsage := &ClusterUsage{}
	for _, node := range nodes.Items {
		currentClusterUsage.CapacityCPU.Add(*(node.Status.Capacity.Cpu()))
		currentClusterUsage.CapacityMemory.Add(*(node.Status.Capacity.Memory()))
		currentClusterUsage.AllocatableCPU.Add(*(node.Status.Allocatable.Cpu()))
		currentClusterUsage.AllocatableMemory.Add(*(node.Status.Allocatable.Memory()))
	}
	return *(currentClusterUsage)
}

func (r *Reconciler) markInternalMemberClusterJoinSucceed(internalMemberCluster apis.ConditionedObj) {
	klog.InfoS("mark internal member cluster join succeed",
		"namespace", internalMemberCluster.GetNamespace(), "internal member cluster", internalMemberCluster.GetName())
	r.recorder.Event(internalMemberCluster, v12.EventTypeNormal, reasonInternalMemberClusterJoined, "internal member cluster join succeed")
	joinSucceedCondition := v1.Condition{
		Type:               fleetv1alpha1.ConditionTypeInternalMemberClusterJoin,
		Status:             v1.ConditionTrue,
		Reason:             reasonInternalMemberClusterJoined,
		ObservedGeneration: internalMemberCluster.GetGeneration(),
	}
	internalMemberCluster.SetConditions(joinSucceedCondition, common.ReconcileSuccessCondition())
}

func (r *Reconciler) markInternalMemberClusterJoinUnknown(internalMemberCluster apis.ConditionedObj, err error) {
	klog.InfoS("mark internal member cluster join unknown",
		"namespace", internalMemberCluster.GetNamespace(), "internal member cluster", internalMemberCluster.GetName())
	if err != nil {
		r.recorder.Event(internalMemberCluster, v12.EventTypeWarning, reasonInternalMemberClusterJoinUnknown, "internal member cluster join unknown")
	} else {
		r.recorder.Event(internalMemberCluster, v12.EventTypeNormal, reasonInternalMemberClusterJoinUnknown, "internal member cluster join unknown")
	}
	var errMsg string
	if err != nil {
		errMsg = err.Error()
	}
	joinUnknownCondition := v1.Condition{
		Type:    fleetv1alpha1.ConditionTypeInternalMemberClusterJoin,
		Status:  v1.ConditionUnknown,
		Reason:  reasonInternalMemberClusterJoinUnknown,
		Message: errMsg,
	}
	if err != nil {
		internalMemberCluster.SetConditions(joinUnknownCondition, common.ReconcileErrorCondition(err))
	} else {
		internalMemberCluster.SetConditions(joinUnknownCondition, common.ReconcileSuccessCondition())
	}
}

func (r *Reconciler) markInternalMemberClusterHeartbeatReceived(internalMemberCluster apis.ConditionedObj) {
	klog.InfoS("mark internal member cluster heartbeat received",
		"namespace", internalMemberCluster.GetNamespace(), "internalMemberCluster", internalMemberCluster.GetName())
	r.recorder.Event(internalMemberCluster, v12.EventTypeNormal, reasonInternalMemberClusterHeartbeatReceived, "internal member cluster heartbeat received")
	hearbeatReceivedCondition := v1.Condition{
		Type:               fleetv1alpha1.ConditionTypeInternalMemberClusterHeartbeat,
		Status:             v1.ConditionTrue,
		Reason:             reasonInternalMemberClusterHeartbeatReceived,
		ObservedGeneration: internalMemberCluster.GetGeneration(),
	}
	internalMemberCluster.SetConditions(hearbeatReceivedCondition, common.ReconcileSuccessCondition())
}

func (r *Reconciler) markInternalMemberClusterHeartbeatUnknown(internalMemberCluster apis.ConditionedObj, err error) {
	klog.InfoS("mark internal member cluster heartbeat unknown",
		"namespace", internalMemberCluster.GetNamespace(), "internalMemberCluster", internalMemberCluster.GetName())
	if err != nil {
		r.recorder.Event(internalMemberCluster, v12.EventTypeWarning, reasonInternalMemberClusterHeartbeatUnknown, "internal member cluster heartbeat unknown")
	} else {
		r.recorder.Event(internalMemberCluster, v12.EventTypeNormal, reasonInternalMemberClusterHeartbeatUnknown, "internal member cluster heartbeat unknown")
	}
	var errMsg string
	if err != nil {
		errMsg = err.Error()
	}
	heartbeatUnknownCondition := v1.Condition{
		Type:    fleetv1alpha1.ConditionTypeInternalMemberClusterHeartbeat,
		Status:  v1.ConditionUnknown,
		Reason:  reasonInternalMemberClusterHeartbeatUnknown,
		Message: errMsg,
	}
	if err != nil {
		internalMemberCluster.SetConditions(heartbeatUnknownCondition, common.ReconcileErrorCondition(err))
	} else {
		internalMemberCluster.SetConditions(heartbeatUnknownCondition, common.ReconcileSuccessCondition())
	}
}

//+kubebuilder:rbac:groups=fleet.azure.com,resources=internalmemberclusters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=fleet.azure.com,resources=internalmemberclusters/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=fleet.azure.com,resources=internalmemberclusters/finalizers,verbs=update

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	var cluster fleetv1alpha1.InternalMemberCluster
	if err := r.hubClient.Get(ctx, req.NamespacedName, &cluster); err != nil {
		return ctrl.Result{}, errors.Wrap(client.IgnoreNotFound(err), "error getting internal member cluster")
	}

	if cluster.Spec.State == fleetv1alpha1.ClusterStateJoin {
		return r.joinInternalMemberCluster(ctx, cluster)
	}

	return ctrl.Result{}, nil
}

func (r *Reconciler) getMembershipClusterState() fleetv1alpha1.ClusterState {
	r.membershipStateLock.RLock()
	defer r.membershipStateLock.RUnlock()
	return r.membershipState
}

//joinInternalMemberCluster carries two operations:
//1. Gets current cluster usage.
//2. Updates the associated InternalMemberCluster Custom Resource with current cluster usage and marks it as Joined.
func (r *Reconciler) joinInternalMemberCluster(ctx context.Context, internalMemberCluster fleetv1alpha1.InternalMemberCluster) (ctrl.Result, error) {
	membershipState := r.getMembershipClusterState()

	if membershipState == fleetv1alpha1.ClusterStateJoin {
		// Get current cluster usage.
		kubeConfig := ctrl.GetConfigOrDie()
		clientSet, err := kubernetes.NewForConfig(kubeConfig)
		if err != nil {
			klog.ErrorS(err, errJoiningInternalMemberCluster)
			r.markInternalMemberClusterJoinUnknown(&internalMemberCluster, err)
			r.markInternalMemberClusterHeartbeatUnknown(&internalMemberCluster, err)
			return ctrl.Result{}, errors.Wrap(err, errJoiningInternalMemberCluster)
		}

		nodes, err := clientSet.CoreV1().Nodes().List(ctx, v1.ListOptions{})
		if err != nil {
			klog.ErrorS(err, errJoiningInternalMemberCluster)
			r.markInternalMemberClusterJoinUnknown(&internalMemberCluster, err)
			r.markInternalMemberClusterHeartbeatUnknown(&internalMemberCluster, err)
			return ctrl.Result{}, errors.Wrap(err, errJoiningInternalMemberCluster)
		}

		updatedClusterUsage := getClusterUsage(*(nodes))
		heartbeatReceived := checkHeartbeatReceived(*(nodes), internalMemberCluster.Spec.HeartbeatPeriodSeconds)

		// Updates the associated InternalMemberCluster Custom Resource with current cluster usage and marks it as Joined.
		if heartbeatReceived {
			internalMemberCluster.Status.Capacity.Cpu().Set(updatedClusterUsage.CapacityCPU.Value())
			internalMemberCluster.Status.Capacity.Memory().Set(updatedClusterUsage.CapacityMemory.Value())
			internalMemberCluster.Status.Allocatable.Cpu().Set(updatedClusterUsage.AllocatableCPU.Value())
			internalMemberCluster.Status.Allocatable.Memory().Set(updatedClusterUsage.AllocatableMemory.Value())

			r.markInternalMemberClusterJoinSucceed(&internalMemberCluster)
			r.markInternalMemberClusterHeartbeatReceived(&internalMemberCluster)
		} else {
			r.markInternalMemberClusterJoinUnknown(&internalMemberCluster, errors.New(errNoHeartbeatReceived))
			r.markInternalMemberClusterHeartbeatUnknown(&internalMemberCluster, errors.New(errNoHeartbeatReceived))
		}

		heartbeatBackoff := wait.Backoff{
			Duration: time.Duration(internalMemberCluster.Spec.HeartbeatPeriodSeconds),
		}
		err = retry.OnError(heartbeatBackoff,
			func(err error) bool {
				return true
			},
			func() error {
				err := r.hubClient.Update(ctx, &internalMemberCluster)
				if err != nil {
					klog.Error(err.Error())
					klog.InfoS("retry update internal member cluster on hub cluster")
				}
				return err
			},
		)
		if err == nil {
			r.markInternalMemberClusterJoinSucceed(&internalMemberCluster)
			r.markInternalMemberClusterHeartbeatReceived(&internalMemberCluster)
		} else {
			r.markInternalMemberClusterJoinUnknown(&internalMemberCluster, errors.New(errNoHeartbeatReceived))
			r.markInternalMemberClusterHeartbeatUnknown(&internalMemberCluster, errors.New(errNoHeartbeatReceived))
		}
		return ctrl.Result{}, err
	}

	r.markInternalMemberClusterJoinUnknown(&internalMemberCluster, nil)
	err := r.hubClient.Update(ctx, &internalMemberCluster)
	return ctrl.Result{RequeueAfter: time.Minute}, errors.Wrap(err, "error marking internal member cluster as unknown")
}

func (r *Reconciler) watchMembershipChan() {
	for membershipState := range r.membershipChan {
		klog.InfoS("membership state has changed", "membershipState", membershipState)
		r.membershipStateLock.Lock()
		r.membershipState = membershipState
		r.membershipStateLock.Unlock()
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.recorder = mgr.GetEventRecorderFor("MemberShipController")
	go r.watchMembershipChan()
	return ctrl.NewControllerManagedBy(mgr).
		For(&fleetv1alpha1.InternalMemberCluster{}).
		Complete(r)
}
