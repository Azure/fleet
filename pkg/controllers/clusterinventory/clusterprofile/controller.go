/*
Copyright 2025 The KubeFleet Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package clusterprofile features a controller to generate clusterprofile objects from MemberCluster.
package clusterprofile

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	clusterinventory "sigs.k8s.io/cluster-inventory-api/apis/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/propertyprovider"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/condition"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/controller"
)

const (
	// clusterProfileCleanupFinalizer is the finalizer added to a MemberCluster object if
	// a corresponding ClusterProfile object has been created.
	clusterProfileCleanupFinalizer = "kubernetes-fleet.io/cluster-profile-cleanup"

	// the list of reasons in the cluster profile status
	clusterNoStatusReason      = "MemberAgentReportedNoStatus"
	clusterHeartbeatLostReason = "MemberAgentHeartbeatLost"
	clusterHealthUnknownReason = "MemberAgentReportedNoHealthInfo"
	clusterUnHealthyReason     = "MemberClusterAPIServerUnhealthy"
	clusterHealthyReason       = "MemberClusterAPIServerHealthy"
)

// Reconciler reconciles a MemberCluster object and creates the corresponding ClusterProfile
// object in the designated namespace.
type Reconciler struct {
	client.Client
	ClusterProfileNamespace   string
	ClusterUnhealthyThreshold time.Duration
}

// Reconcile processes the MemberCluster object and creates the corresponding ClusterProfile object
// in the designated namespace.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	mcRef := klog.KRef(req.Namespace, req.Name)
	startTime := time.Now()
	klog.V(2).InfoS("Reconciliation starts (cluster profile controller)", "memberCluster", mcRef)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("Reconciliation ends (cluster profile controller)", "memberCluster", mcRef, "latency", latency)
	}()

	// Retrieve the MemberCluster object.
	mc := &clusterv1beta1.MemberCluster{}
	if err := r.Get(ctx, req.NamespacedName, mc); err != nil {
		if errors.IsNotFound(err) {
			klog.InfoS("Member cluster object is not found", "memberCluster", mcRef)
			// To address the case where a member cluster is deleted before its cluster profile is cleaned up
			// since we didn't put the logic in the member cluster controller
			// or a cluster profile has been created without the acknowledgment of this controller.
			if err = r.cleanupClusterProfile(ctx, req.Name); err != nil {
				klog.ErrorS(err, "Failed to clean up the cluster profile when the member cluster is already gone", "memberCluster", mcRef)
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
		}
		klog.ErrorS(err, "Failed to get member cluster", "memberCluster", mcRef)
		return ctrl.Result{}, err
	}

	// Check if the member cluster object has been marked for deletion.
	if mc.DeletionTimestamp != nil {
		klog.V(2).InfoS("Member cluster object is being deleted; remove the corresponding cluster profile", "memberCluster", mcRef)
		// Delete the corresponding ClusterProfile object.
		if err := r.cleanupClusterProfile(ctx, mc.Name); err != nil {
			klog.ErrorS(err, "Failed to clean up cluster profile when member cluster is marked for deletion", "memberCluster", mcRef)
			return ctrl.Result{}, err
		}

		// Remove the cleanup finalizer from the MemberCluster object.
		controllerutil.RemoveFinalizer(mc, clusterProfileCleanupFinalizer)
		if err := r.Update(ctx, mc); err != nil {
			klog.ErrorS(err, "Failed to remove cleanup finalizer", "memberCluster", mcRef)
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Check if the MemberCluster has joined.
	joinedCondition := meta.FindStatusCondition(mc.Status.Conditions, string(clusterv1beta1.ConditionTypeMemberClusterJoined))
	if !condition.IsConditionStatusTrue(joinedCondition, mc.Generation) {
		klog.V(2).InfoS("Member cluster has not joined; skip cluster profile reconciliation", "memberCluster", mcRef)
		return ctrl.Result{}, nil
	}

	// Check if the MemberCluster object has the cleanup finalizer; if not, add it.
	if !controllerutil.ContainsFinalizer(mc, clusterProfileCleanupFinalizer) {
		mc.Finalizers = append(mc.Finalizers, clusterProfileCleanupFinalizer)
		if err := r.Update(ctx, mc); err != nil {
			klog.ErrorS(err, "Failed to add cleanup finalizer", "memberCluster", mcRef)
			return ctrl.Result{}, err
		}
	}

	// Retrieve the corresponding ClusterProfile object. If the object does not exist, create it.
	cp := &clusterinventory.ClusterProfile{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: r.ClusterProfileNamespace,
			Name:      mc.Name,
		},
	}
	// Note that if the object already exists and its spec matches with the desired space, no
	// update op will be performed.
	createOrUpdateRes, err := controllerutil.CreateOrUpdate(ctx, r, cp, func() error {
		if !cp.CreationTimestamp.IsZero() {
			// log an unexpected error if the cluster profile content is modified behind our back
			if cp.Spec.DisplayName != mc.Name || cp.Labels == nil || cp.Labels[clusterinventory.LabelClusterManagerKey] != controller.ClusterManagerName {
				klog.ErrorS(controller.NewUnexpectedBehaviorError(fmt.Errorf("found an unexpected cluster profile: spec = `%+v`, label = `%+v`", cp.Spec, cp.Labels)),
					"Cluster profile is modified behind our back", "memberCluster", mcRef, "clusterProfile", klog.KObj(cp))
			}
		}
		// Set the spec.
		cp.Spec = clusterinventory.ClusterProfileSpec{
			ClusterManager: clusterinventory.ClusterManager{
				Name: controller.ClusterManagerName,
			},
			DisplayName: mc.Name,
		}
		// Set the labels.
		if cp.Labels == nil {
			cp.Labels = make(map[string]string)
		}
		cp.Labels[clusterinventory.LabelClusterManagerKey] = controller.ClusterManagerName
		return nil
	})
	if err != nil {
		klog.ErrorS(err, "Failed to create or update cluster profile", "memberCluster", mcRef, "clusterProfile", klog.KObj(cp), "operation", createOrUpdateRes)
		return ctrl.Result{}, err
	}
	klog.V(2).InfoS("Cluster profile object is created or updated", "memberCluster", mcRef, "clusterProfile", klog.KObj(cp), "operation", createOrUpdateRes)

	existingCP := cp.DeepCopy()
	// sync the cluster profile status/condition from the member cluster condition
	r.fillInClusterStatus(mc, cp)
	r.syncClusterProfileCondition(mc, cp)
	// skip status update if nothing changed
	if equality.Semantic.DeepEqual(existingCP.Status, cp.Status) {
		klog.V(2).InfoS("No need to update Cluster profile status", "memberCluster", mcRef, "clusterProfile", klog.KObj(cp))
		return ctrl.Result{}, nil
	}
	if err = r.Status().Update(ctx, cp); err != nil {
		klog.ErrorS(err, "Failed to update cluster profile status", "memberCluster", mcRef, "clusterProfile", klog.KObj(cp))
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// fillInClusterStatus fills in the ClusterProfile status fields from the MemberCluster status.
// Currently, it only fills in the Kubernetes version field.
func (r *Reconciler) fillInClusterStatus(mc *clusterv1beta1.MemberCluster, cp *clusterinventory.ClusterProfile) {
	clusterPropertyCondition := meta.FindStatusCondition(mc.Status.Conditions, string(clusterv1beta1.ConditionTypeClusterPropertyCollectionSucceeded))
	if !condition.IsConditionStatusTrue(clusterPropertyCondition, mc.Generation) {
		klog.V(3).InfoS("Cluster property collection has not succeeded; skip updating the cluster profile status", "memberCluster", klog.KObj(mc), "clusterProfile", klog.KObj(cp))
		return
	}

	k8sversion, exists := mc.Status.Properties[propertyprovider.K8sVersionProperty]
	if exists {
		klog.V(3).InfoS("Get Kubernetes version from member cluster status", "kubernetesVersion", k8sversion.Value, "clusterProfile", klog.KObj(cp))
		cp.Status.Version = clusterinventory.ClusterVersion{
			Kubernetes: k8sversion.Value,
		}
	}
	// Add the class access provider, we only have one so far
	cp.Status.AccessProviders = []clusterinventory.AccessProvider{
		{
			Name: controller.ClusterManagerName,
		},
	}
	// TODO throw and unexpected error if clusterEntryPoint is not found
	// We don't have a way to get it yet
	clusterEntry, exists := mc.Status.Properties[propertyprovider.ClusterEntryPointProperty]
	if exists {
		klog.V(3).InfoS("Get Kubernetes cluster entry point from member cluster status", "clusterEntryPoint", clusterEntry.Value, "clusterProfile", klog.KObj(cp))
		cp.Status.AccessProviders[0].Cluster.Server = clusterEntry.Value
	}
	// Get the CA Data
	certificateAuthorityData, exists := mc.Status.Properties[propertyprovider.ClusterCertificateAuthorityProperty]
	if exists {
		klog.V(3).InfoS("Get Kubernetes cluster certificate authority data from member cluster status", "clusterProfile", klog.KObj(cp))
		cp.Status.AccessProviders[0].Cluster.CertificateAuthorityData = []byte(certificateAuthorityData.Value)
	} else {
		// throw an alert
		_ = controller.NewUnexpectedBehaviorError(fmt.Errorf("cluster certificate authority data not found in member cluster %s status", mc.Name))
	}
}

// syncClusterProfileCondition syncs the ClusterProfile object's condition based on the MemberCluster object's condition.
func (r *Reconciler) syncClusterProfileCondition(mc *clusterv1beta1.MemberCluster, cp *clusterinventory.ClusterProfile) {
	// For simplicity reasons, for now only the health check condition is populated, using
	// Fleet member agent's API server health check result.
	var mcHealthCond *metav1.Condition
	var memberAgentLastHeartbeat *metav1.Time

	memberAgentStatus := mc.GetAgentStatus(clusterv1beta1.MemberAgent)
	if memberAgentStatus != nil {
		mcHealthCond = meta.FindStatusCondition(memberAgentStatus.Conditions, string(clusterv1beta1.AgentHealthy))
		memberAgentLastHeartbeat = &memberAgentStatus.LastReceivedHeartbeat
	}
	switch {
	case memberAgentStatus == nil:
		// The member agent hasn't reported its status yet.
		// Set the unknown health condition in the cluster profile status.
		meta.SetStatusCondition(&cp.Status.Conditions, metav1.Condition{
			Type:               clusterinventory.ClusterConditionControlPlaneHealthy,
			Status:             metav1.ConditionUnknown,
			Reason:             clusterNoStatusReason,
			ObservedGeneration: cp.Generation,
			Message:            "The Fleet member agent has not reported its status yet",
		})
	case mcHealthCond == nil:
		// The member agent has reported its status, but the health condition is missing.
		// Set the unknown health condition in the cluster profile status.
		meta.SetStatusCondition(&cp.Status.Conditions, metav1.Condition{
			Type:               clusterinventory.ClusterConditionControlPlaneHealthy,
			Status:             metav1.ConditionUnknown,
			Reason:             clusterHealthUnknownReason,
			ObservedGeneration: cp.Generation,
			Message:            "The Fleet member agent has reported its status, but the health condition is missing",
		})
	case memberAgentLastHeartbeat == nil || time.Since(memberAgentLastHeartbeat.Time) > r.ClusterUnhealthyThreshold:
		// The member agent has lost its heartbeat connection to the Fleet hub cluster.
		// Set the unknown health condition in the cluster profile status.
		meta.SetStatusCondition(&cp.Status.Conditions, metav1.Condition{
			Type:               clusterinventory.ClusterConditionControlPlaneHealthy,
			Status:             metav1.ConditionFalse,
			Reason:             clusterHeartbeatLostReason,
			ObservedGeneration: cp.Generation,
			Message:            "The Fleet member agent has lost its heartbeat connection to the Fleet hub cluster",
		})
	// TODO: Add the generation check after Fleet member agent handle the health condition appropriately.
	case mcHealthCond.Status == metav1.ConditionUnknown:
		// The health condition has not been updated.
		// Set the unknown health condition in the cluster profile status.
		meta.SetStatusCondition(&cp.Status.Conditions, metav1.Condition{
			Type:               clusterinventory.ClusterConditionControlPlaneHealthy,
			Status:             metav1.ConditionUnknown,
			Reason:             clusterHealthUnknownReason,
			ObservedGeneration: cp.Generation,
			Message:            "The Fleet member agent health check result is out of date or unknown",
		})
	case mcHealthCond.Status == metav1.ConditionFalse:
		// The member agent reports that the API server is unhealthy.
		// Set the false health condition in the cluster profile status.
		meta.SetStatusCondition(&cp.Status.Conditions, metav1.Condition{
			Type:               clusterinventory.ClusterConditionControlPlaneHealthy,
			Status:             metav1.ConditionFalse,
			Reason:             clusterUnHealthyReason,
			ObservedGeneration: cp.Generation,
			Message:            "The Fleet member agent reports that the API server is unhealthy",
		})
	default:
		// The member agent reports that the API server is healthy.
		// Set the true health condition in the cluster profile status.
		meta.SetStatusCondition(&cp.Status.Conditions, metav1.Condition{
			Type:               clusterinventory.ClusterConditionControlPlaneHealthy,
			Status:             metav1.ConditionTrue,
			Reason:             clusterHealthyReason,
			ObservedGeneration: cp.Generation,
			Message:            "The Fleet member agent reports that the API server is healthy",
		})
	}
}

// cleanupClusterProfile deletes the ClusterProfile object associated with a given MemberCluster object.
func (r *Reconciler) cleanupClusterProfile(ctx context.Context, clusterName string) error {
	cp := &clusterinventory.ClusterProfile{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: r.ClusterProfileNamespace,
			Name:      clusterName,
		},
	}
	klog.V(2).InfoS("delete the cluster profile", "memberCluster", clusterName, "clusterProfile", klog.KObj(cp))
	if err := r.Delete(ctx, cp); err != nil && !errors.IsNotFound(err) {
		klog.ErrorS(err, "Failed to delete the cluster profile", "memberCluster", clusterName, "clusterProfile", klog.KObj(cp))
		return err
	}
	return nil
}

// SetupWithManager sets up the controller with the controller manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).Named("clusterprofile-controller").
		For(&clusterv1beta1.MemberCluster{}).
		Watches(&clusterinventory.ClusterProfile{}, handler.Funcs{
			DeleteFunc: func(ctx context.Context, e event.DeleteEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
				klog.V(2).InfoS("Handling a clusterProfile delete event", "clusterProfile", klog.KObj(e.Object))
				q.Add(reconcile.Request{
					NamespacedName: types.NamespacedName{Name: e.Object.GetName()},
				})
			},
			UpdateFunc: func(ctx context.Context, e event.UpdateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
				if e.ObjectNew.GetGeneration() == e.ObjectOld.GetGeneration() {
					return
				}
				klog.V(2).InfoS("Handling a clusterProfil spec update event", "clusterProfile", klog.KObj(e.ObjectOld))
				q.Add(reconcile.Request{
					NamespacedName: types.NamespacedName{Name: e.ObjectOld.GetName()},
				})
			},
		}).
		Complete(r)
}
