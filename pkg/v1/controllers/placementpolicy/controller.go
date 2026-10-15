/*
Copyright 2026 The KubeFleet Authors.

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

// Package placementpolicy implements the FEP-0001 placement policy controller, which reconciles
// the PlacementPolicy and ClusterPlacementPolicy API objects (placement.kubefleet.dev API group):
// it resolves cluster selectors against the current member cluster inventory and reports
// scheduling status on the policy objects.
package placementpolicy

import (
	"context"
	"time"

	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	runtime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	kfplacementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/scheduler/clustereligibilitychecker"
	kferrors "github.com/kubefleet-dev/kubefleet/pkg/utils/errors"
)

const (
	// unfulfilledRequeueAfter is the wait before re-evaluating a policy that has not reached
	// all of its desired cluster counts; it backstops cluster eligibility transitions that do
	// not surface as watch events (e.g., a member agent heartbeat going stale).
	unfulfilledRequeueAfter = 2 * time.Minute
	// fulfilledRequeueAfter is the wait before re-evaluating a fulfilled policy, for the same
	// backstop reason: a cluster that silently stops heartbeating emits no event, yet it must
	// eventually stop counting toward fulfillment. Set to half the eligibility checker's
	// 5-minute heartbeat timeout so the worst-case detection latency stays near the timeout
	// itself (timeout + one requeue period, ~7.5 minutes) instead of doubling it.
	fulfilledRequeueAfter = 150 * time.Second
)

// Reconciler reconciles PlacementPolicy and ClusterPlacementPolicy objects.
//
// One Reconciler instance serves both APIs: requests for cluster-scoped ClusterPlacementPolicy
// objects carry no namespace, which is how the two are told apart.
type Reconciler struct {
	client.Client

	// uncachedReader reads directly from the API server; it gates the claim cleanup
	// finalizer release, where a stale cache read could orphan a just-created claim.
	uncachedReader client.Reader

	eligibility eligibilityChecker
}

// NewReconciler returns a Reconciler that judges cluster fulfillment with the scheduler's
// standard cluster eligibility gate.
func NewReconciler(c client.Client, uncachedReader client.Reader) *Reconciler {
	return &Reconciler{
		Client:         c,
		uncachedReader: uncachedReader,
		eligibility:    clustereligibilitychecker.New(),
	}
}

// Reconcile runs a single reconciliation round for a PlacementPolicy or ClusterPlacementPolicy object.
func (r *Reconciler) Reconcile(ctx context.Context, req runtime.Request) (runtime.Result, error) {
	startTime := time.Now()
	klog.V(2).InfoS("Placement policy reconciliation starts", "placementPolicy", req.NamespacedName)
	defer func() {
		latency := time.Since(startTime).Milliseconds()
		klog.V(2).InfoS("Placement policy reconciliation ends", "placementPolicy", req.NamespacedName, "latency", latency)
	}()

	policy, err := r.fetchPolicy(ctx, req)
	if err != nil {
		if errors.IsNotFound(err) {
			klog.V(4).InfoS("Ignoring not-found placement policy", "placementPolicy", req.NamespacedName)
			// The policy is gone for good; stop reporting metrics for it.
			forgetPolicyMetrics(req.Namespace, req.Name)
			return runtime.Result{}, nil
		}
		klog.ErrorS(err, "Failed to get the placement policy object", append(kferrors.Args(err), "placementPolicy", req.NamespacedName)...)
		return runtime.Result{}, err
	}
	if !policy.GetDeletionTimestamp().IsZero() {
		// Cluster claims cannot be owner-referenced by the policy (cross-scope), so cleanup
		// is finalizer-driven.
		return runtime.Result{}, r.cleanupClaims(ctx, policy)
	}

	memberClusters := &clusterv1beta1.MemberClusterList{}
	if err := r.List(ctx, memberClusters); err != nil {
		wrappedErr := kferrors.NewAPIServerError(err, "failed to list member clusters", true, "placementPolicy", req.NamespacedName)
		klog.ErrorS(wrappedErr, "Failed to list member clusters", kferrors.Args(wrappedErr)...)
		return runtime.Result{}, wrappedErr
	}
	var mostRecentClusterCreation metav1.Time
	for i := range memberClusters.Items {
		if ts := memberClusters.Items[i].CreationTimestamp; ts.After(mostRecentClusterCreation.Time) {
			mostRecentClusterCreation = ts
		}
	}

	outcomes, err := evaluateSelectors(policy.GetSpec(), memberClusters.Items, r.eligibility)
	if err != nil {
		// Selector evaluation fails only on invalid selector contents (e.g., an operator
		// applied to a key it does not support); retrying cannot help until the spec changes,
		// so the error surfaces on the status instead of the reconcile loop. Outstanding
		// claims are deliberately left untouched: their specs reflect the last valid
		// selectors.
		klog.ErrorS(err, "Failed to evaluate the cluster selectors", "placementPolicy", req.NamespacedName)
		return runtime.Result{}, r.updateStatus(ctx, policy, nil, nil, invalidSelectorsCondition(policy.GetGeneration(), err))
	}

	activeClaims, claimErr := r.reconcileClaims(ctx, policy, outcomes, mostRecentClusterCreation)
	// The scheduling status is written even when claim reconciliation failed partway, but the
	// claim count is published only from a completed round: a failed round returns whatever it
	// had counted when it stopped, which would misreport the claims that the rest of the round
	// never reached. A nil count leaves the last completed round's value standing; the retry
	// corrects it.
	reportedClaims := &activeClaims
	if claimErr != nil {
		reportedClaims = nil
	}
	if err := r.updateStatus(ctx, policy, outcomes, reportedClaims, scheduledCondition(policy.GetGeneration(), outcomes)); err != nil {
		return runtime.Result{}, err
	}
	if claimErr != nil {
		klog.ErrorS(claimErr, "Failed to reconcile the cluster claims", append(kferrors.Args(claimErr), "placementPolicy", req.NamespacedName)...)
		return runtime.Result{}, claimErr
	}

	requeueAfter := fulfilledRequeueAfter
	for i := range outcomes {
		if !outcomes[i].satisfiedInFull() {
			requeueAfter = unfulfilledRequeueAfter
			break
		}
	}
	return runtime.Result{RequeueAfter: requeueAfter}, nil
}

// updateStatus writes the scheduling outcome onto the policy status, skipping the API call when
// nothing has changed. A nil outcome list clears the cluster counts (used when the selectors
// cannot be evaluated at all); a nil activeClaims leaves the current claim count untouched.
func (r *Reconciler) updateStatus(ctx context.Context, policy kfplacementv1alpha1.PlacementPolicyAccessor, outcomes []selectorOutcome, activeClaims *int32, scheduledCond metav1.Condition) error {
	status := policy.GetStatus()
	observedStatus := status.DeepCopy()

	if outcomes == nil {
		status.DesiredClusters = nil
		status.ScheduledClusters = nil
	} else {
		desired, scheduled := aggregateCounts(outcomes)
		status.DesiredClusters = &desired
		status.ScheduledClusters = &scheduled
	}
	if activeClaims != nil {
		status.ActiveClusterClaims = activeClaims
	}
	meta.SetStatusCondition(&status.Conditions, scheduledCond)

	reportPolicyMetrics(policy, status, scheduledCond)

	if apiequality.Semantic.DeepEqual(observedStatus, status) {
		return nil
	}
	if err := r.Status().Update(ctx, policy); err != nil {
		wrappedErr := kferrors.NewAPIServerError(err, "failed to update the placement policy status", false, "placementPolicy", client.ObjectKeyFromObject(policy))
		klog.ErrorS(wrappedErr, "Failed to update the placement policy status", kferrors.Args(wrappedErr)...)
		return wrappedErr
	}
	return nil
}

// fetchPolicy retrieves the policy object for the given request; requests without a namespace
// concern the cluster-scoped ClusterPlacementPolicy API.
//
// The read is categorized as an API server error, which stays recognizable to errors.IsNotFound:
// it resolves through the wrap, so the caller can still tell a deleted policy from a failed read.
func (r *Reconciler) fetchPolicy(ctx context.Context, req runtime.Request) (kfplacementv1alpha1.PlacementPolicyAccessor, error) {
	var policy kfplacementv1alpha1.PlacementPolicyAccessor = &kfplacementv1alpha1.PlacementPolicy{}
	if req.Namespace == "" {
		policy = &kfplacementv1alpha1.ClusterPlacementPolicy{}
	}
	if err := r.Get(ctx, req.NamespacedName, policy); err != nil {
		return nil, kferrors.NewAPIServerError(err, "failed to get the placement policy", true, "placementPolicy", req.NamespacedName)
	}
	return policy, nil
}

// SetupWithManagerForPlacementPolicy registers the reconciler with the manager for the
// namespaced PlacementPolicy API. The reconciler lists cluster claims through a cache field index,
// so fieldindexers.SetupWithHubAgentControllerManager must have run before the manager starts.
func (r *Reconciler) SetupWithManagerForPlacementPolicy(mgr runtime.Manager) error {
	return runtime.NewControllerManagedBy(mgr).
		Named("placement-policy-controller").
		For(&kfplacementv1alpha1.PlacementPolicy{}).
		Watches(
			&clusterv1beta1.MemberCluster{},
			handler.EnqueueRequestsFromMapFunc(r.mapMemberClusterToPlacementPolicies),
			builder.WithPredicates(memberClusterSchedulingRelevantChanges()),
		).
		Watches(
			&kfplacementv1alpha1.ClusterClaim{},
			handler.EnqueueRequestsFromMapFunc(r.mapClaimToPlacementPolicy),
		).
		Complete(r)
}

// SetupWithManagerForClusterPlacementPolicy registers the reconciler with the manager for the
// cluster-scoped ClusterPlacementPolicy API. Like SetupWithManagerForPlacementPolicy, it relies on
// the cache field indexes of fieldindexers.SetupWithHubAgentControllerManager.
func (r *Reconciler) SetupWithManagerForClusterPlacementPolicy(mgr runtime.Manager) error {
	return runtime.NewControllerManagedBy(mgr).
		Named("cluster-placement-policy-controller").
		For(&kfplacementv1alpha1.ClusterPlacementPolicy{}).
		Watches(
			&clusterv1beta1.MemberCluster{},
			handler.EnqueueRequestsFromMapFunc(r.mapMemberClusterToClusterPlacementPolicies),
			builder.WithPredicates(memberClusterSchedulingRelevantChanges()),
		).
		Watches(
			&kfplacementv1alpha1.ClusterClaim{},
			handler.EnqueueRequestsFromMapFunc(r.mapClaimToClusterPlacementPolicy),
		).
		Complete(r)
}
