/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package workapplier

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

const (
	workNameTemplate   = "work-%s"
	nsNameTemplate     = "ns-%s"
	deployNameTemplate = "deploy-%s"
)

const (
	eventuallyDuration   = time.Second * 30
	eventuallyInternal   = time.Second * 1
	consistentlyDuration = time.Second * 5
	consistentlyInternal = time.Millisecond * 500
)

var (
	ignoreFieldObjectMetaAutoGenFields = cmpopts.IgnoreFields(metav1.ObjectMeta{}, "CreationTimestamp", "Generation", "ResourceVersion", "SelfLink", "UID", "ManagedFields")
	ignoreFieldAppliedWorkStatus       = cmpopts.IgnoreFields(fleetv1beta1.AppliedWork{}, "Status")
	ignoreFieldConditionLTTMsg         = cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime", "Message")
	ignoreDriftDetailsObsTime          = cmpopts.IgnoreFields(fleetv1beta1.DriftDetails{}, "ObservationTime", "FirstDriftedObservedTime")
	ignoreDiffDetailsObsTime           = cmpopts.IgnoreFields(fleetv1beta1.DiffDetails{}, "ObservationTime", "FirstDiffedObservedTime")

	lessFuncPatchDetail = func(a, b fleetv1beta1.PatchDetail) bool {
		return a.Path < b.Path
	}
)

// createWorkObject creates a new Work object with the given name, manifests, and apply strategy.
func createWorkObject(workName string, applyStrategy *fleetv1beta1.ApplyStrategy, rawManifestJSON ...[]byte) {
	manifests := make([]fleetv1beta1.Manifest, len(rawManifestJSON))
	for idx := range rawManifestJSON {
		manifests[idx] = fleetv1beta1.Manifest{
			RawExtension: runtime.RawExtension{
				Raw: rawManifestJSON[idx],
			},
		}
	}

	work := &fleetv1beta1.Work{
		ObjectMeta: metav1.ObjectMeta{
			Name:      workName,
			Namespace: memberReservedNSName,
		},
		Spec: fleetv1beta1.WorkSpec{
			Workload: fleetv1beta1.WorkloadTemplate{
				Manifests: manifests,
			},
			ApplyStrategy: applyStrategy,
		},
	}
	Expect(hubClient.Create(ctx, work)).To(Succeed())
}

func updateWorkObject(workName string, applyStrategy *fleetv1beta1.ApplyStrategy, rawManifestJSON ...[]byte) {
	manifests := make([]fleetv1beta1.Manifest, len(rawManifestJSON))
	for idx := range rawManifestJSON {
		manifests[idx] = fleetv1beta1.Manifest{
			RawExtension: runtime.RawExtension{
				Raw: rawManifestJSON[idx],
			},
		}
	}

	work := &fleetv1beta1.Work{}
	Expect(hubClient.Get(ctx, client.ObjectKey{Name: workName, Namespace: memberReservedNSName}, work)).To(Succeed())

	work.Spec.Workload.Manifests = manifests
	work.Spec.ApplyStrategy = applyStrategy
	Expect(hubClient.Update(ctx, work)).To(Succeed())
}

func marshalK8sObjJSON(obj runtime.Object) []byte {
	unstructuredObjMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	Expect(err).To(BeNil(), "Failed to convert the object to an unstructured object")
	unstructuredObj := &unstructured.Unstructured{Object: unstructuredObjMap}
	json, err := unstructuredObj.MarshalJSON()
	Expect(err).To(BeNil(), "Failed to marshal the unstructured object to JSON")
	return json
}

func workFinalizerAddedActual(workName string) func() error {
	return func() error {
		// Retrieve the Work object.
		work := &fleetv1beta1.Work{}
		if err := hubClient.Get(ctx, client.ObjectKey{Name: workName, Namespace: memberReservedNSName}, work); err != nil {
			return fmt.Errorf("failed to retrieve the Work object: %w", err)
		}

		// Check that the cleanup finalizer has been added.
		if !controllerutil.ContainsFinalizer(work, fleetv1beta1.WorkFinalizer) {
			return fmt.Errorf("cleanup finalizer has not been added")
		}
		return nil
	}
}

func appliedWorkCreatedActual(workName string) func() error {
	return func() error {
		// Retrieve the AppliedWork object.
		appliedWork := &fleetv1beta1.AppliedWork{}
		if err := memberClient.Get(ctx, client.ObjectKey{Name: workName, Namespace: memberReservedNSName}, appliedWork); err != nil {
			return fmt.Errorf("failed to retrieve the AppliedWork object: %w", err)
		}

		wantAppliedWork := &fleetv1beta1.AppliedWork{
			ObjectMeta: metav1.ObjectMeta{
				Name: workName,
			},
			Spec: fleetv1beta1.AppliedWorkSpec{
				WorkName:      workName,
				WorkNamespace: memberReservedNSName,
			},
		}
		if diff := cmp.Diff(
			appliedWork, wantAppliedWork,
			ignoreFieldObjectMetaAutoGenFields,
			ignoreFieldAppliedWorkStatus,
		); diff != "" {
			return fmt.Errorf("appliedWork diff (-got +want):\n%s", diff)
		}
		return nil
	}
}

func prepareAppliedWorkOwnerRef(workName string) *metav1.OwnerReference {
	// Retrieve the AppliedWork object.
	appliedWork := &fleetv1beta1.AppliedWork{}
	Expect(memberClient.Get(ctx, client.ObjectKey{Name: workName, Namespace: memberReservedNSName}, appliedWork)).To(Succeed(), "Failed to retrieve the AppliedWork object")

	// Prepare the expected OwnerReference.
	return &metav1.OwnerReference{
		APIVersion:         fleetv1beta1.GroupVersion.String(),
		Kind:               "AppliedWork",
		Name:               appliedWork.Name,
		UID:                appliedWork.GetUID(),
		BlockOwnerDeletion: ptr.To(false),
	}
}

func regularNSObjectAppliedActual(nsName string, appliedWorkOwnerRef *metav1.OwnerReference) func() error {
	return func() error {
		// Retrieve the NS object.
		gotNS := &corev1.Namespace{}
		if err := memberClient.Get(ctx, client.ObjectKey{Name: nsName}, gotNS); err != nil {
			return fmt.Errorf("failed to retrieve the NS object: %w", err)
		}

		// Check that the NS object has been created as expected.

		// To ignore default values automatically, here the test suite rebuilds the objects.
		wantNS := ns.DeepCopy()
		wantNS.TypeMeta = metav1.TypeMeta{}
		wantNS.Name = nsName
		wantNS.OwnerReferences = []metav1.OwnerReference{
			*appliedWorkOwnerRef,
		}

		rebuiltGotNS := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:            gotNS.Name,
				OwnerReferences: gotNS.OwnerReferences,
			},
		}

		if diff := cmp.Diff(rebuiltGotNS, wantNS); diff != "" {
			return fmt.Errorf("namespace diff (-got +want):\n%s", diff)
		}
		return nil
	}
}

func regularDeploymentObjectAppliedActual(nsName, deployName string, appliedWorkOwnerRef *metav1.OwnerReference) func() error {
	return func() error {
		// Retrieve the Deployment object.
		gotDeploy := &appsv1.Deployment{}
		if err := memberClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: deployName}, gotDeploy); err != nil {
			return fmt.Errorf("failed to retrieve the Deployment object: %w", err)
		}

		// Check that the Deployment object has been created as expected.

		// To ignore default values automatically, here the test suite rebuilds the objects.
		wantDeploy := deploy.DeepCopy()
		wantDeploy.TypeMeta = metav1.TypeMeta{}
		wantDeploy.Namespace = nsName
		wantDeploy.Name = deployName
		wantDeploy.OwnerReferences = []metav1.OwnerReference{
			*appliedWorkOwnerRef,
		}

		if len(gotDeploy.Spec.Template.Spec.Containers) != 1 {
			return fmt.Errorf("number of containers in the Deployment object, got %d, want %d", len(gotDeploy.Spec.Template.Spec.Containers), 1)
		}
		if len(gotDeploy.Spec.Template.Spec.Containers[0].Ports) != 1 {
			return fmt.Errorf("number of ports in the first container, got %d, want %d", len(gotDeploy.Spec.Template.Spec.Containers[0].Ports), 1)
		}
		rebuiltGotDeploy := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:       gotDeploy.Namespace,
				Name:            gotDeploy.Name,
				OwnerReferences: gotDeploy.OwnerReferences,
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: gotDeploy.Spec.Replicas,
				Selector: gotDeploy.Spec.Selector,
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app": gotDeploy.Spec.Template.ObjectMeta.Labels["app"],
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  gotDeploy.Spec.Template.Spec.Containers[0].Name,
								Image: gotDeploy.Spec.Template.Spec.Containers[0].Image,
								Ports: []corev1.ContainerPort{
									{
										ContainerPort: gotDeploy.Spec.Template.Spec.Containers[0].Ports[0].ContainerPort,
									},
								},
							},
						},
					},
				},
			},
		}
		if diff := cmp.Diff(rebuiltGotDeploy, wantDeploy); diff != "" {
			return fmt.Errorf("deployment diff (-got +want):\n%s", diff)
		}
		return nil
	}
}

func markDeploymentAsAvailable(nsName, deployName string) {
	// Retrieve the Deployment object.
	gotDeploy := &appsv1.Deployment{}
	Expect(memberClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: deployName}, gotDeploy)).To(Succeed(), "Failed to retrieve the Deployment object")

	// Mark the Deployment object as available.
	now := metav1.Now()
	requiredReplicas := int32(1)
	if gotDeploy.Spec.Replicas != nil {
		requiredReplicas = *gotDeploy.Spec.Replicas
	}
	gotDeploy.Status = appsv1.DeploymentStatus{
		ObservedGeneration:  gotDeploy.Generation,
		Replicas:            requiredReplicas,
		UpdatedReplicas:     requiredReplicas,
		ReadyReplicas:       requiredReplicas,
		AvailableReplicas:   requiredReplicas,
		UnavailableReplicas: 0,
		Conditions: []appsv1.DeploymentCondition{
			{
				Type:               appsv1.DeploymentAvailable,
				Status:             corev1.ConditionTrue,
				Reason:             "MarkedAsAvailable",
				Message:            "Deployment has been marked as available",
				LastUpdateTime:     now,
				LastTransitionTime: now,
			},
		},
	}
	Expect(memberClient.Status().Update(ctx, gotDeploy)).To(Succeed(), "Failed to mark the Deployment object as available")
}

func workStatusUpdated(
	workName string,
	workConds []metav1.Condition,
	manifestConds []fleetv1beta1.ManifestCondition,
	noLaterThanObservationTime *metav1.Time,
	noLaterThanFirstObservedTime *metav1.Time,
) func() error {
	return func() error {
		// Retrieve the Work object.
		work := &fleetv1beta1.Work{}
		if err := hubClient.Get(ctx, client.ObjectKey{Name: workName, Namespace: memberReservedNSName}, work); err != nil {
			return fmt.Errorf("failed to retrieve the Work object: %w", err)
		}

		// Prepare the expected Work object status.

		// Update the conditions with the observed generation.
		for idx := range workConds {
			workConds[idx].ObservedGeneration = work.Generation
		}
		for midx := range manifestConds {
			manifestCond := &manifestConds[midx]
			for cidx := range manifestCond.Conditions {
				manifestCond.Conditions[cidx].ObservedGeneration = work.Generation
			}
		}
		wantWorkStatus := fleetv1beta1.WorkStatus{
			Conditions:         workConds,
			ManifestConditions: manifestConds,
		}

		// Check that the Work object status has been updated as expected.
		if diff := cmp.Diff(
			work.Status, wantWorkStatus,
			ignoreFieldConditionLTTMsg,
			ignoreDiffDetailsObsTime, ignoreDriftDetailsObsTime,
			cmpopts.SortSlices(lessFuncPatchDetail),
		); diff != "" {
			return fmt.Errorf("work status diff (-got, +want):\n%s", diff)
		}

		// For each manifest condition, verify the timestamps.
		for idx := range work.Status.ManifestConditions {
			manifestCond := &work.Status.ManifestConditions[idx]
			if manifestCond.DriftDetails != nil {
				if noLaterThanObservationTime != nil && manifestCond.DriftDetails.ObservationTime.After(noLaterThanObservationTime.Time) {
					return fmt.Errorf("drift observation time is later than expected")
				}

				if noLaterThanFirstObservedTime != nil && manifestCond.DriftDetails.FirstDriftedObservedTime.After(noLaterThanFirstObservedTime.Time) {
					return fmt.Errorf("first drifted observation time is later than expected")
				}

				if manifestCond.DriftDetails.ObservationTime.After(manifestCond.DriftDetails.FirstDriftedObservedTime.Time) {
					return fmt.Errorf("drift observation time is later than first drifted observation time")
				}
			}

			if manifestCond.DiffDetails != nil {
				if noLaterThanObservationTime != nil && manifestCond.DiffDetails.ObservationTime.After(noLaterThanObservationTime.Time) {
					return fmt.Errorf("diff observation time is later than expected")
				}

				if noLaterThanFirstObservedTime != nil && manifestCond.DiffDetails.FirstDiffedObservedTime.After(noLaterThanFirstObservedTime.Time) {
					return fmt.Errorf("first diffed observation time is later than expected")
				}

				if manifestCond.DiffDetails.ObservationTime.After(manifestCond.DiffDetails.FirstDiffedObservedTime.Time) {
					return fmt.Errorf("diff observation time is later than first diffed observation time")
				}
			}
		}
		return nil
	}
}

func appliedWorkStatusUpdated(workName string, appliedResourceMeta []fleetv1beta1.AppliedResourceMeta) func() error {
	return func() error {
		// Retrieve the AppliedWork object.
		appliedWork := &fleetv1beta1.AppliedWork{}
		if err := memberClient.Get(ctx, client.ObjectKey{Name: workName, Namespace: memberReservedNSName}, appliedWork); err != nil {
			return fmt.Errorf("failed to retrieve the AppliedWork object: %w", err)
		}

		// Prepare the expected AppliedWork object status.
		wantAppliedWorkStatus := fleetv1beta1.AppliedWorkStatus{
			AppliedResources: appliedResourceMeta,
		}
		if diff := cmp.Diff(appliedWork.Status, wantAppliedWorkStatus); diff != "" {
			return fmt.Errorf("appliedWork status diff (-got, +want):\n%s", diff)
		}
		return nil
	}
}

func cleanupWorkObject(workName string) {
	// Retrieve the Work object.
	work := &fleetv1beta1.Work{
		ObjectMeta: metav1.ObjectMeta{
			Name:      workName,
			Namespace: memberReservedNSName,
		},
	}
	Expect(hubClient.Delete(ctx, work)).To(Succeed(), "Failed to delete the Work object")

	// Wait for the removal of the Work object.
	workRemovedActual := func() error {
		work := &fleetv1beta1.Work{}
		if err := hubClient.Get(ctx, client.ObjectKey{Name: workName, Namespace: memberReservedNSName}, work); !errors.IsNotFound(err) {
			return fmt.Errorf("work object still exists or an unexpected error occurred: %w", err)
		}
		return nil
	}
	Eventually(workRemovedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to remove the Work object")
}

func appliedWorkRemovedActual(workName string) func() error {
	return func() error {
		// Retrieve the AppliedWork object.
		appliedWork := &fleetv1beta1.AppliedWork{}
		if err := memberClient.Get(ctx, client.ObjectKey{Name: workName}, appliedWork); !errors.IsNotFound(err) {
			return fmt.Errorf("appliedWork object still exists or an unexpected error occurred: %w", err)
		}
		return nil
	}
}

func regularDeployRemovedActual(nsName, deployName string) func() error {
	return func() error {
		// Retrieve the Deployment object.
		deploy := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: nsName,
				Name:      deployName,
			},
		}
		if err := memberClient.Delete(ctx, deploy); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("failed to delete the Deployment object: %w", err)
		}

		if err := memberClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: deployName}, deploy); !errors.IsNotFound(err) {
			return fmt.Errorf("deployment object still exists or an unexpected error occurred: %w", err)
		}
		return nil
	}
}

func regularNSObjectNotAppliedActual(nsName string) func() error {
	return func() error {
		// Retrieve the NS object.
		ns := &corev1.Namespace{}
		if err := memberClient.Get(ctx, client.ObjectKey{Name: nsName}, ns); !errors.IsNotFound(err) {
			return fmt.Errorf("namespace object exists or an unexpected error occurred: %w", err)
		}
		return nil
	}
}

func regularDeployNotRemovedActual(nsName, deployName string) func() error {
	return func() error {
		// Retrieve the Deployment object.
		deploy := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: nsName,
				Name:      deployName,
			},
		}
		if err := memberClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: deployName}, deploy); err != nil {
			return fmt.Errorf("failed to retrieve the Deployment object: %w", err)
		}
		return nil
	}
}

func retrieveGeneratedNSObject(nsGenerateName string, expectedAppliedWorkOwnerRef *metav1.OwnerReference) (*corev1.Namespace, error) {
	// List all namespaces.
	nsList := &corev1.NamespaceList{}
	if err := memberClient.List(ctx, nsList); err != nil {
		return nil, fmt.Errorf("failed to list namespaces: %w", err)
	}

	// Filter out all the namespaces that match the generate name.
	matchingNSList := []corev1.Namespace{}
	for _, ns := range nsList.Items {
		nameMatched := strings.HasPrefix(ns.Name, nsGenerateName)

		ownerRefMatched := false
		if len(ns.OwnerReferences) == 1 && reflect.DeepEqual(ns.OwnerReferences[0], *expectedAppliedWorkOwnerRef) {
			ownerRefMatched = true
		}

		if nameMatched && ownerRefMatched {
			matchingNSList = append(matchingNSList, ns)
		}
	}

	if len(matchingNSList) != 1 {
		return nil, fmt.Errorf("number of matching namespaces, got %d, want %d", len(matchingNSList), 1)
	}
	return &matchingNSList[0], nil
}

func generatedNSObjectAppliedActual(nsGenerateName string, expectedAppliedWorkOwnerRef *metav1.OwnerReference) func() error {
	return func() error {
		matchedNS, err := retrieveGeneratedNSObject(nsGenerateName, expectedAppliedWorkOwnerRef)
		if err != nil {
			return fmt.Errorf("failed to retrieve the namespace with generate name: %w", err)
		}

		// To ignore default values automatically, here the test suite rebuilds the objects.
		wantNS := nsWithGenerateName.DeepCopy()
		wantNS.TypeMeta = metav1.TypeMeta{}
		wantNS.Name = matchedNS.Name
		// Once applied, the GenerateName field is cleared.
		wantNS.GenerateName = ""
		wantNS.OwnerReferences = []metav1.OwnerReference{
			*expectedAppliedWorkOwnerRef,
		}

		rebuiltGotNS := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:            matchedNS.Name,
				OwnerReferences: matchedNS.OwnerReferences,
			},
		}

		if diff := cmp.Diff(rebuiltGotNS, wantNS); diff != "" {
			return fmt.Errorf("generated namespace diff (-got +want):\n%s", diff)
		}
		return nil
	}
}

func retrieveGeneratedJobObject(nsName, jobGenerateName string, expectedAppliedWorkOwnerRef *metav1.OwnerReference) (*batchv1.Job, error) {
	// List all jobs.
	jobList := &batchv1.JobList{}
	if err := memberClient.List(ctx, jobList, &client.ListOptions{Namespace: nsName}); err != nil {
		return nil, fmt.Errorf("failed to list jobs: %w", err)
	}

	// Filter out all the jobs that match the generate name.
	matchingJobList := []batchv1.Job{}
	for _, job := range jobList.Items {
		nameMatched := strings.HasPrefix(job.Name, jobGenerateName)

		ownerRefMatched := false
		if len(job.OwnerReferences) == 1 && reflect.DeepEqual(job.OwnerReferences[0], *expectedAppliedWorkOwnerRef) {
			ownerRefMatched = true
		}

		if nameMatched && ownerRefMatched {
			matchingJobList = append(matchingJobList, job)
		}
	}

	if len(matchingJobList) != 1 {
		return nil, fmt.Errorf("number of matching jobs, got %d, want %d", len(matchingJobList), 1)
	}
	matchedJob := matchingJobList[0]
	return &matchedJob, nil
}

func generatedJobObjectAppliedActual(nsName, jobGenerateName string, expectedAppliedWorkOwnerRef *metav1.OwnerReference) func() error {
	return func() error {
		matchedJob, err := retrieveGeneratedJobObject(nsName, jobGenerateName, expectedAppliedWorkOwnerRef)
		if err != nil {
			return fmt.Errorf("failed to retrieve the job with generate name: %w", err)
		}

		// To ignore default values automatically, here the test suite rebuilds the objects.
		wantJob := jobWithGenerateName.DeepCopy()
		wantJob.TypeMeta = metav1.TypeMeta{}
		wantJob.Namespace = nsName
		// Once applied, the GenerateName field is cleared.
		wantJob.GenerateName = ""
		wantJob.Name = matchedJob.Name
		wantJob.OwnerReferences = []metav1.OwnerReference{
			*expectedAppliedWorkOwnerRef,
		}

		if len(matchedJob.Spec.Template.Spec.Containers) != 1 {
			return fmt.Errorf("number of containers in the Job object, got %d, want %d", len(matchedJob.Spec.Template.Spec.Containers), 1)
		}
		rebuiltGotJob := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:       nsName,
				Name:            matchedJob.Name,
				OwnerReferences: matchedJob.OwnerReferences,
			},
			Spec: batchv1.JobSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  matchedJob.Spec.Template.Spec.Containers[0].Name,
								Image: matchedJob.Spec.Template.Spec.Containers[0].Image,
							},
						},
						RestartPolicy: corev1.RestartPolicyNever,
					},
				},
			},
		}

		if diff := cmp.Diff(rebuiltGotJob, wantJob); diff != "" {
			return fmt.Errorf("generated job diff (-got +want):\n%s", diff)
		}

		return nil
	}
}

var _ = Describe("applying manifests", func() {
	Context("apply new manifests (regular)", Ordered, func() {
		workName := fmt.Sprintf(workNameTemplate, "a1")
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, "a1")
		deployName := fmt.Sprintf(deployNameTemplate, "a1")

		var appliedWorkOwnerRef *metav1.OwnerReference
		var regularNS *corev1.Namespace
		var regularDeploy *appsv1.Deployment

		BeforeAll(func() {
			// Prepare a NS object.
			regularNS = ns.DeepCopy()
			regularNS.Name = nsName
			regularNSJSON := marshalK8sObjJSON(regularNS)

			// Prepare a Deployment object.
			regularDeploy = deploy.DeepCopy()
			regularDeploy.Namespace = nsName
			regularDeploy.Name = deployName
			regularDeployJSON := marshalK8sObjJSON(regularDeploy)

			// Create a new Work object with all the manifest JSONs.
			createWorkObject(workName, nil, regularNSJSON, regularDeployJSON)
		})

		It("should add cleanup finalizer to the Work object", func() {
			finalizerAddedActual := workFinalizerAddedActual(workName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to add cleanup finalizer to the Work object")
		})

		It("should prepare an AppliedWork object", func() {
			appliedWorkCreatedActual := appliedWorkCreatedActual(workName)
			Eventually(appliedWorkCreatedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to prepare an AppliedWork object")

			appliedWorkOwnerRef = prepareAppliedWorkOwnerRef(workName)
		})

		It("should apply the manifests", func() {
			// Ensure that the NS object has been applied as expected.
			regularNSObjectAppliedActual := regularNSObjectAppliedActual(nsName, appliedWorkOwnerRef)
			Eventually(regularNSObjectAppliedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to apply the namespace object")

			Expect(memberClient.Get(ctx, client.ObjectKey{Name: nsName}, regularNS)).To(Succeed(), "Failed to retrieve the NS object")

			// Ensure that the Deployment object has been applied as expected.
			regularDeploymentObjectAppliedActual := regularDeploymentObjectAppliedActual(nsName, deployName, appliedWorkOwnerRef)
			Eventually(regularDeploymentObjectAppliedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to apply the deployment object")

			Expect(memberClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: deployName}, regularDeploy)).To(Succeed(), "Failed to retrieve the Deployment object")
		})

		It("can mark the deployment as available", func() {
			markDeploymentAsAvailable(nsName, deployName)
		})

		It("should update the Work object status", func() {
			// Prepare the status information.
			workConds := []metav1.Condition{
				{
					Type:   fleetv1beta1.WorkConditionTypeApplied,
					Status: metav1.ConditionTrue,
					Reason: string(ManifestProcessingApplyResultTypeApplied),
				},
				{
					Type:   fleetv1beta1.WorkConditionTypeAvailable,
					Status: metav1.ConditionTrue,
					Reason: string(ManifestProcessingAvailabilityResultTypeAvailable),
				},
			}
			manifestConds := []fleetv1beta1.ManifestCondition{
				{
					Identifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:  0,
						Group:    "",
						Version:  "v1",
						Kind:     "Namespace",
						Resource: "namespaces",
						Name:     nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:   fleetv1beta1.WorkConditionTypeApplied,
							Status: metav1.ConditionTrue,
							Reason: string(ManifestProcessingApplyResultTypeApplied),
						},
						{
							Type:   fleetv1beta1.WorkConditionTypeAvailable,
							Status: metav1.ConditionTrue,
							Reason: string(ManifestProcessingAvailabilityResultTypeAvailable),
						},
					},
				},
				{
					Identifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:   1,
						Group:     "apps",
						Version:   "v1",
						Kind:      "Deployment",
						Resource:  "deployments",
						Name:      deployName,
						Namespace: nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:   fleetv1beta1.WorkConditionTypeApplied,
							Status: metav1.ConditionTrue,
							Reason: string(ManifestProcessingApplyResultTypeApplied),
						},
						{
							Type:   fleetv1beta1.WorkConditionTypeAvailable,
							Status: metav1.ConditionTrue,
							Reason: string(ManifestProcessingAvailabilityResultTypeAvailable),
						},
					},
				},
			}

			workStatusUpdatedActual := workStatusUpdated(workName, workConds, manifestConds, nil, nil)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to update work status")
		})

		It("should update the AppliedWork object status", func() {
			// Prepare the status information.
			appliedResourceMeta := []fleetv1beta1.AppliedResourceMeta{
				{
					WorkResourceIdentifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:  0,
						Group:    "",
						Version:  "v1",
						Kind:     "Namespace",
						Resource: "namespaces",
						Name:     nsName,
					},
					UID: regularNS.UID,
				},
				{
					WorkResourceIdentifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:   1,
						Group:     "apps",
						Version:   "v1",
						Kind:      "Deployment",
						Resource:  "deployments",
						Name:      deployName,
						Namespace: nsName,
					},
					UID: regularDeploy.UID,
				},
			}

			appliedWorkStatusUpdatedActual := appliedWorkStatusUpdated(workName, appliedResourceMeta)
			Eventually(appliedWorkStatusUpdatedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to update appliedWork status")
		})

		AfterAll(func() {
			// Delete the Work object and related resources.
			cleanupWorkObject(workName)

			// Ensure that all applied manifests have been removed.
			appliedWorkRemovedActual := appliedWorkRemovedActual(workName)
			Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to remove the AppliedWork object")

			regularDeployRemovedActual := regularDeployRemovedActual(nsName, deployName)
			Eventually(regularDeployRemovedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to remove the deployment object")

			// The environment prepared by the envtest package does not support namespace
			// deletion; consequently this test suite would not attempt so verify its deletion.
		})
	})

	Context("apply new manifests (w/ generate name)", Ordered, func() {
		workName := fmt.Sprintf(workNameTemplate, "a2")
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, "a2")

		var appliedWorkOwnerRef *metav1.OwnerReference
		var regularNS *corev1.Namespace
		var generatedNS *corev1.Namespace
		var generatedJob *batchv1.Job

		BeforeAll(func() {
			// Prepare a NS object in JSON format.
			regularNS = ns.DeepCopy()
			regularNS.Name = nsName
			regularNSJSON := marshalK8sObjJSON(regularNS)

			// Prepare a NS object with generate name in JSON format.
			generatedNS = nsWithGenerateName.DeepCopy()
			generatedNSJSON := marshalK8sObjJSON(generatedNS)

			// Prepare a Job object in JSON format.
			generatedJob = jobWithGenerateName.DeepCopy()
			generatedJob.Namespace = nsName
			generatedJobJSON := marshalK8sObjJSON(generatedJob)

			// Create a new Work object with all the manifest JSONs.
			createWorkObject(workName, nil, regularNSJSON, generatedJobJSON, generatedNSJSON)
		})

		It("should add cleanup finalizer to the Work object", func() {
			finalizerAddedActual := workFinalizerAddedActual(workName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to add cleanup finalizer to the Work object")
		})

		It("should prepare an AppliedWork object", func() {
			appliedWorkCreatedActual := appliedWorkCreatedActual(workName)
			Eventually(appliedWorkCreatedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to prepare an AppliedWork object")

			appliedWorkOwnerRef = prepareAppliedWorkOwnerRef(workName)
		})

		It("should apply the manifests", func() {
			var err error

			// Ensure that the NS object has been applied as expected.
			regularNSObjectAppliedActual := regularNSObjectAppliedActual(nsName, appliedWorkOwnerRef)
			Eventually(regularNSObjectAppliedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to apply the namespace object")

			Expect(memberClient.Get(ctx, client.ObjectKey{Name: nsName}, regularNS)).To(Succeed(), "Failed to retrieve the NS object")

			// Ensure that the generated Job object has been applied as expected.
			generatedJobObjectAppliedActual := generatedJobObjectAppliedActual(nsName, jobGenerateName, appliedWorkOwnerRef)
			Eventually(generatedJobObjectAppliedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to apply the job object")

			generatedNS, err = retrieveGeneratedNSObject(nsGenerateName, appliedWorkOwnerRef)
			Expect(err).NotTo(HaveOccurred(), "Failed to retrieve the generated NS object")

			// Ensure that the generated NS object has been applied as expected.
			generatedNSObjectAppliedActual := generatedNSObjectAppliedActual(nsGenerateName, appliedWorkOwnerRef)
			Eventually(generatedNSObjectAppliedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to apply the namespace object")

			generatedJob, err = retrieveGeneratedJobObject(nsName, jobGenerateName, appliedWorkOwnerRef)
			Expect(err).NotTo(HaveOccurred(), "Failed to retrieve the generated Job object")
		})

		// Fleet does not know how to track an Job object's availability.

		It("should update the Work object status", func() {
			// Prepare the status information.
			workConds := []metav1.Condition{
				{
					Type:   fleetv1beta1.WorkConditionTypeApplied,
					Status: metav1.ConditionTrue,
					Reason: string(ManifestProcessingApplyResultTypeApplied),
				},
				{
					Type:   fleetv1beta1.WorkConditionTypeAvailable,
					Status: metav1.ConditionTrue,
					Reason: string(ManifestProcessingAvailabilityResultTypeAvailable),
				},
			}
			manifestConds := []fleetv1beta1.ManifestCondition{
				{
					Identifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:  0,
						Group:    "",
						Version:  "v1",
						Kind:     "Namespace",
						Resource: "namespaces",
						Name:     nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:   fleetv1beta1.WorkConditionTypeApplied,
							Status: metav1.ConditionTrue,
							Reason: string(ManifestProcessingApplyResultTypeApplied),
						},
						{
							Type:   fleetv1beta1.WorkConditionTypeAvailable,
							Status: metav1.ConditionTrue,
							Reason: string(ManifestProcessingAvailabilityResultTypeAvailable),
						},
					},
				},
				{
					Identifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:      1,
						Group:        "batch",
						Version:      "v1",
						Kind:         "Job",
						Resource:     "jobs",
						Name:         generatedJob.Name,
						Namespace:    nsName,
						GenerateName: jobGenerateName,
					},
					Conditions: []metav1.Condition{
						{
							Type:   fleetv1beta1.WorkConditionTypeApplied,
							Status: metav1.ConditionTrue,
							Reason: string(ManifestProcessingApplyResultTypeApplied),
						},
						{
							Type:   fleetv1beta1.WorkConditionTypeAvailable,
							Status: metav1.ConditionTrue,
							Reason: string(ManifestProcessingAvailabilityResultTypeNotTrackable),
						},
					},
				},
				{
					Identifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:      2,
						Group:        "",
						Version:      "v1",
						Kind:         "Namespace",
						Resource:     "namespaces",
						GenerateName: nsGenerateName,
						Name:         generatedNS.Name,
					},
					Conditions: []metav1.Condition{
						{
							Type:   fleetv1beta1.WorkConditionTypeApplied,
							Status: metav1.ConditionTrue,
							Reason: string(ManifestProcessingApplyResultTypeApplied),
						},
						{
							Type:   fleetv1beta1.WorkConditionTypeAvailable,
							Status: metav1.ConditionTrue,
							Reason: string(ManifestProcessingAvailabilityResultTypeAvailable),
						},
					},
				},
			}

			workStatusUpdatedActual := workStatusUpdated(workName, workConds, manifestConds, nil, nil)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to update work status")
		})

		It("should update the AppliedWork object status", func() {
			// Prepare the status information.
			appliedResourceMeta := []fleetv1beta1.AppliedResourceMeta{
				{
					WorkResourceIdentifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:  0,
						Group:    "",
						Version:  "v1",
						Kind:     "Namespace",
						Resource: "namespaces",
						Name:     nsName,
					},
					UID: regularNS.UID,
				},
				{
					WorkResourceIdentifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:      1,
						Group:        "batch",
						Version:      "v1",
						Kind:         "Job",
						Resource:     "jobs",
						Name:         generatedJob.Name,
						Namespace:    nsName,
						GenerateName: jobGenerateName,
					},
					UID: generatedJob.UID,
				},
				{
					WorkResourceIdentifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:      2,
						Group:        "",
						Version:      "v1",
						Kind:         "Namespace",
						Resource:     "namespaces",
						Name:         generatedNS.Name,
						GenerateName: nsGenerateName,
					},
					UID: generatedNS.UID,
				},
			}

			appliedWorkStatusUpdatedActual := appliedWorkStatusUpdated(workName, appliedResourceMeta)
			Eventually(appliedWorkStatusUpdatedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to update appliedWork status")
		})

		AfterAll(func() {})

	})
})

var _ = Describe("drift detection and takeover", func() {
	// Note that takeover does not concern objects with generate names, for obvious reasons.

	Context("take over pre-existing resources (take over if no diff, no diff present)", Ordered, func() {
		workName := fmt.Sprintf(workNameTemplate, "b1")
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, "b1")
		deployName := fmt.Sprintf(deployNameTemplate, "b1")

		var appliedWorkOwnerRef *metav1.OwnerReference
		var regularNS *corev1.Namespace
		var regularDeploy *appsv1.Deployment

		BeforeAll(func() {
			regularNS = ns.DeepCopy()
			regularNS.Name = nsName

			regularDeploy = deploy.DeepCopy()
			regularDeploy.Namespace = nsName
			regularDeploy.Name = deployName

			// Prepare the JSONs for the resources.
			regularNSJSON := marshalK8sObjJSON(regularNS)
			regularDeployJSON := marshalK8sObjJSON(regularDeploy)

			// Create the resources on the member cluster side.
			Expect(memberClient.Create(ctx, regularNS)).To(Succeed(), "Failed to create the NS object")
			Expect(memberClient.Create(ctx, regularDeploy)).To(Succeed(), "Failed to create the Deployment object")

			markDeploymentAsAvailable(nsName, deployName)

			// Create the Work object.
			applyStrategy := &fleetv1beta1.ApplyStrategy{
				ComparisonOption: fleetv1beta1.ComparisonOptionTypePartialComparison,
				WhenToTakeOver:   fleetv1beta1.WhenToTakeOverTypeIfNoDiff,
			}
			createWorkObject(workName, applyStrategy, regularNSJSON, regularDeployJSON)
		})

		It("should add cleanup finalizer to the Work object", func() {
			finalizerAddedActual := workFinalizerAddedActual(workName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to add cleanup finalizer to the Work object")
		})

		It("should prepare an AppliedWork object", func() {
			appliedWorkCreatedActual := appliedWorkCreatedActual(workName)
			Eventually(appliedWorkCreatedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to prepare an AppliedWork object")

			appliedWorkOwnerRef = prepareAppliedWorkOwnerRef(workName)
		})

		It("should apply the manifests", func() {
			// Ensure that the NS object has been applied as expected.
			regularNSObjectAppliedActual := regularNSObjectAppliedActual(nsName, appliedWorkOwnerRef)
			Eventually(regularNSObjectAppliedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to apply the namespace object")

			Expect(memberClient.Get(ctx, client.ObjectKey{Name: nsName}, regularNS)).To(Succeed(), "Failed to retrieve the NS object")

			// Ensure that the Deployment object has been applied as expected.
			regularDeploymentObjectAppliedActual := regularDeploymentObjectAppliedActual(nsName, deployName, appliedWorkOwnerRef)
			Eventually(regularDeploymentObjectAppliedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to apply the deployment object")

			Expect(memberClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: deployName}, regularDeploy)).To(Succeed(), "Failed to retrieve the Deployment object")
		})

		It("can mark the deployment as available", func() {
			markDeploymentAsAvailable(nsName, deployName)
		})

		It("should update the Work object status", func() {
			// Prepare the status information.
			workConds := []metav1.Condition{
				{
					Type:   fleetv1beta1.WorkConditionTypeApplied,
					Status: metav1.ConditionTrue,
					Reason: string(ManifestProcessingApplyResultTypeApplied),
				},
				{
					Type:   fleetv1beta1.WorkConditionTypeAvailable,
					Status: metav1.ConditionTrue,
					Reason: string(ManifestProcessingAvailabilityResultTypeAvailable),
				},
			}
			manifestConds := []fleetv1beta1.ManifestCondition{
				{
					Identifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:  0,
						Group:    "",
						Version:  "v1",
						Kind:     "Namespace",
						Resource: "namespaces",
						Name:     nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:   fleetv1beta1.WorkConditionTypeApplied,
							Status: metav1.ConditionTrue,
							Reason: string(ManifestProcessingApplyResultTypeApplied),
						},
						{
							Type:   fleetv1beta1.WorkConditionTypeAvailable,
							Status: metav1.ConditionTrue,
							Reason: string(ManifestProcessingAvailabilityResultTypeAvailable),
						},
					},
				},
				{
					Identifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:   1,
						Group:     "apps",
						Version:   "v1",
						Kind:      "Deployment",
						Resource:  "deployments",
						Name:      deployName,
						Namespace: nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:   fleetv1beta1.WorkConditionTypeApplied,
							Status: metav1.ConditionTrue,
							Reason: string(ManifestProcessingApplyResultTypeApplied),
						},
						{
							Type:   fleetv1beta1.WorkConditionTypeAvailable,
							Status: metav1.ConditionTrue,
							Reason: string(ManifestProcessingAvailabilityResultTypeAvailable),
						},
					},
				},
			}

			workStatusUpdatedActual := workStatusUpdated(workName, workConds, manifestConds, nil, nil)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to update work status")
		})

		It("should update the AppliedWork object status", func() {
			// Prepare the status information.
			appliedResourceMeta := []fleetv1beta1.AppliedResourceMeta{
				{
					WorkResourceIdentifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:  0,
						Group:    "",
						Version:  "v1",
						Kind:     "Namespace",
						Resource: "namespaces",
						Name:     nsName,
					},
					UID: regularNS.UID,
				},
				{
					WorkResourceIdentifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:   1,
						Group:     "apps",
						Version:   "v1",
						Kind:      "Deployment",
						Resource:  "deployments",
						Name:      deployName,
						Namespace: nsName,
					},
					UID: regularDeploy.UID,
				},
			}

			appliedWorkStatusUpdatedActual := appliedWorkStatusUpdated(workName, appliedResourceMeta)
			Eventually(appliedWorkStatusUpdatedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to update appliedWork status")
		})

		AfterAll(func() {
			// Delete the Work object and related resources.
			cleanupWorkObject(workName)

			// Ensure that all applied manifests have been removed.
			appliedWorkRemovedActual := appliedWorkRemovedActual(workName)
			Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to remove the AppliedWork object")

			regularDeployRemovedActual := regularDeployRemovedActual(nsName, deployName)
			Eventually(regularDeployRemovedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to remove the deployment object")

			// The environment prepared by the envtest package does not support namespace
			// deletion; consequently this test suite would not attempt so verify its deletion.
		})
	})

	Context("take over pre-existing resources (take over if no diff, with diff present, partial comparison)", Ordered, func() {
		workName := fmt.Sprintf(workNameTemplate, "b2")
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, "b2")
		deployName := fmt.Sprintf(deployNameTemplate, "b2")

		var appliedWorkOwnerRef *metav1.OwnerReference
		var regularNS *corev1.Namespace
		var regularDeploy *appsv1.Deployment

		BeforeAll(func() {
			regularNS = ns.DeepCopy()
			regularNS.Name = nsName

			regularDeploy = deploy.DeepCopy()
			regularDeploy.Namespace = nsName
			regularDeploy.Name = deployName

			// Prepare the JSONs for the resources.
			regularNSJSON := marshalK8sObjJSON(regularNS)
			regularDeployJSON := marshalK8sObjJSON(regularDeploy)

			// Make cluster specific changes.

			// Labels is not a managed field; with partial comparison this variance will be
			// ignored.
			regularNS.Labels = map[string]string{
				"foo": "bar",
			}
			// Replicas is a managed field; with partial comparison this variance will be noted.
			regularDeploy.Spec.Replicas = ptr.To(int32(2))

			// Create the resources on the member cluster side.
			Expect(memberClient.Create(ctx, regularNS)).To(Succeed(), "Failed to create the NS object")
			Expect(memberClient.Create(ctx, regularDeploy)).To(Succeed(), "Failed to create the Deployment object")

			markDeploymentAsAvailable(nsName, deployName)

			// Create the Work object.
			applyStrategy := &fleetv1beta1.ApplyStrategy{
				ComparisonOption: fleetv1beta1.ComparisonOptionTypePartialComparison,
				WhenToTakeOver:   fleetv1beta1.WhenToTakeOverTypeIfNoDiff,
			}
			createWorkObject(workName, applyStrategy, regularNSJSON, regularDeployJSON)
		})

		It("should add cleanup finalizer to the Work object", func() {
			finalizerAddedActual := workFinalizerAddedActual(workName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to add cleanup finalizer to the Work object")
		})

		It("should prepare an AppliedWork object", func() {
			appliedWorkCreatedActual := appliedWorkCreatedActual(workName)
			Eventually(appliedWorkCreatedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to prepare an AppliedWork object")

			appliedWorkOwnerRef = prepareAppliedWorkOwnerRef(workName)
		})

		It("should apply some manifests (while preserving diffs in unmanaged fields)", func() {
			// Verify that the object has been taken over, but all the unmanaged fields are
			// left alone.
			wantNS := ns.DeepCopy()
			wantNS.TypeMeta = metav1.TypeMeta{}
			wantNS.Name = nsName
			wantNS.Labels = map[string]string{
				"foo": "bar",
				// The label below is added by K8s itself (system-managed well-known label).
				"kubernetes.io/metadata.name": "ns-b2",
			}
			wantNS.OwnerReferences = []metav1.OwnerReference{
				*appliedWorkOwnerRef,
			}

			Eventually(func() error {
				// Retrieve the NS object.
				if err := memberClient.Get(ctx, client.ObjectKey{Name: nsName}, regularNS); err != nil {
					return fmt.Errorf("failed to retrieve the NS object: %w", err)
				}

				// To ignore default values automatically, here the test suite rebuilds the objects.
				rebuiltGotNS := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:            regularNS.Name,
						Labels:          regularNS.Labels,
						OwnerReferences: regularNS.OwnerReferences,
					},
				}

				if diff := cmp.Diff(rebuiltGotNS, wantNS); diff != "" {
					return fmt.Errorf("namespace diff (-got +want):\n%s", diff)
				}
				return nil
			}, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to take over the NS object")
		})

		It("should not take over some objects", func() {
			// Verify that the object has not been taken over.
			wantDeploy := deploy.DeepCopy()
			wantDeploy.TypeMeta = metav1.TypeMeta{}
			wantDeploy.Namespace = nsName
			wantDeploy.Name = deployName
			wantDeploy.Spec.Replicas = ptr.To(int32(2))

			Consistently(func() error {
				if err := memberClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: deployName}, regularDeploy); err != nil {
					return fmt.Errorf("failed to retrieve the Deployment object: %w", err)
				}

				if len(regularDeploy.Spec.Template.Spec.Containers) != 1 {
					return fmt.Errorf("number of containers in the Deployment object, got %d, want %d", len(regularDeploy.Spec.Template.Spec.Containers), 1)
				}
				if len(regularDeploy.Spec.Template.Spec.Containers[0].Ports) != 1 {
					return fmt.Errorf("number of ports in the first container, got %d, want %d", len(regularDeploy.Spec.Template.Spec.Containers[0].Ports), 1)
				}

				// To ignore default values automatically, here the test suite rebuilds the objects.
				rebuiltGotDeploy := &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Namespace:       regularDeploy.Namespace,
						Name:            regularDeploy.Name,
						OwnerReferences: regularDeploy.OwnerReferences,
					},
					Spec: appsv1.DeploymentSpec{
						Replicas: regularDeploy.Spec.Replicas,
						Selector: regularDeploy.Spec.Selector,
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: regularDeploy.Spec.Template.ObjectMeta.Labels,
							},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  regularDeploy.Spec.Template.Spec.Containers[0].Name,
										Image: regularDeploy.Spec.Template.Spec.Containers[0].Image,
										Ports: []corev1.ContainerPort{
											{
												ContainerPort: regularDeploy.Spec.Template.Spec.Containers[0].Ports[0].ContainerPort,
											},
										},
									},
								},
							},
						},
					},
				}

				if diff := cmp.Diff(rebuiltGotDeploy, wantDeploy); diff != "" {
					return fmt.Errorf("deployment diff (-got +want):\n%s", diff)
				}
				return nil
			}, consistentlyDuration, consistentlyInternal).Should(Succeed(), "Failed to leave the Deployment object alone")
		})

		It("should update the Work object status", func() {
			noLaterThanTimestamp := metav1.Now()

			// Prepare the status information.
			workConds := []metav1.Condition{
				{
					Type:   fleetv1beta1.WorkConditionTypeApplied,
					Status: metav1.ConditionFalse,
					Reason: notAllManifestsAppliedReason,
				},
				{
					Type:   fleetv1beta1.WorkConditionTypeAvailable,
					Status: metav1.ConditionFalse,
					Reason: notAllAppliedObjectsAvailableReason,
				},
			}
			manifestConds := []fleetv1beta1.ManifestCondition{
				{
					Identifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:  0,
						Group:    "",
						Version:  "v1",
						Kind:     "Namespace",
						Resource: "namespaces",
						Name:     nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:   fleetv1beta1.WorkConditionTypeApplied,
							Status: metav1.ConditionTrue,
							Reason: string(ManifestProcessingApplyResultTypeApplied),
						},
						{
							Type:   fleetv1beta1.WorkConditionTypeAvailable,
							Status: metav1.ConditionTrue,
							Reason: string(ManifestProcessingAvailabilityResultTypeAvailable),
						},
					},
				},
				{
					Identifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:   1,
						Group:     "apps",
						Version:   "v1",
						Kind:      "Deployment",
						Resource:  "deployments",
						Name:      deployName,
						Namespace: nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:   fleetv1beta1.WorkConditionTypeApplied,
							Status: metav1.ConditionFalse,
							Reason: string(ManifestProcessingApplyResultTypeFailedToTakeOver),
						},
					},
					DiffDetails: &fleetv1beta1.DiffDetails{
						ObservedInMemberClusterGeneration: regularDeploy.Generation,
						ObservedDiffs: []fleetv1beta1.PatchDetail{
							{
								Path:          "/spec/replicas",
								ValueInMember: "2",
								ValueInHub:    "1",
							},
						},
					},
				},
			}

			workStatusUpdatedActual := workStatusUpdated(workName, workConds, manifestConds, &noLaterThanTimestamp, &noLaterThanTimestamp)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to update work status")
		})

		It("should update the AppliedWork object status", func() {
			// Prepare the status information.
			appliedResourceMeta := []fleetv1beta1.AppliedResourceMeta{
				{
					WorkResourceIdentifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:  0,
						Group:    "",
						Version:  "v1",
						Kind:     "Namespace",
						Resource: "namespaces",
						Name:     nsName,
					},
					UID: regularNS.UID,
				},
			}

			appliedWorkStatusUpdatedActual := appliedWorkStatusUpdated(workName, appliedResourceMeta)
			Eventually(appliedWorkStatusUpdatedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to update appliedWork status")
		})

		AfterAll(func() {
			// Delete the Work object and related resources.
			cleanupWorkObject(workName)

			// Ensure that the AppliedWork object has been removed.
			appliedWorkRemovedActual := appliedWorkRemovedActual(workName)
			Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to remove the AppliedWork object")

			// Ensure that the Deployment object has been left alone.
			regularDeployNotRemovedActual := regularDeployNotRemovedActual(nsName, deployName)
			Consistently(regularDeployNotRemovedActual, consistentlyDuration, consistentlyInternal).Should(Succeed(), "Failed to remove the deployment object")

			// The environment prepared by the envtest package does not support namespace
			// deletion; consequently this test suite would not attempt so verify its deletion.
		})
	})

	Context("take over pre-existing resources (take over if no diff, with diff, full comparison)", Ordered, func() {
		workName := fmt.Sprintf(workNameTemplate, "b3")
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, "b3")
		deployName := fmt.Sprintf(deployNameTemplate, "b3")

		var regularNS *corev1.Namespace
		var regularDeploy *appsv1.Deployment

		BeforeAll(func() {
			regularNS = ns.DeepCopy()
			regularNS.Name = nsName

			regularDeploy = deploy.DeepCopy()
			regularDeploy.Namespace = nsName
			regularDeploy.Name = deployName

			// Prepare the JSONs for the resources.
			regularNSJSON := marshalK8sObjJSON(regularNS)
			regularDeployJSON := marshalK8sObjJSON(regularDeploy)

			// Make cluster specific changes.

			// Labels is not a managed field; with partial comparison this variance will be
			// ignored.
			regularNS.Labels = map[string]string{
				"foo": "bar",
			}
			// Replicas is a managed field; with partial comparison this variance will be noted.
			regularDeploy.Spec.Replicas = ptr.To(int32(2))

			// Create the resources on the member cluster side.
			Expect(memberClient.Create(ctx, regularNS)).To(Succeed(), "Failed to create the NS object")
			Expect(memberClient.Create(ctx, regularDeploy)).To(Succeed(), "Failed to create the Deployment object")

			markDeploymentAsAvailable(nsName, deployName)

			// Create the Work object.
			applyStrategy := &fleetv1beta1.ApplyStrategy{
				ComparisonOption: fleetv1beta1.ComparisonOptionTypeFullComparison,
				WhenToTakeOver:   fleetv1beta1.WhenToTakeOverTypeIfNoDiff,
			}
			createWorkObject(workName, applyStrategy, regularNSJSON, regularDeployJSON)
		})

		It("should add cleanup finalizer to the Work object", func() {
			finalizerAddedActual := workFinalizerAddedActual(workName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to add cleanup finalizer to the Work object")
		})

		It("should prepare an AppliedWork object", func() {
			appliedWorkCreatedActual := appliedWorkCreatedActual(workName)
			Eventually(appliedWorkCreatedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to prepare an AppliedWork object")
		})

		It("should not take over any object", func() {
			// Verify that the NS object has not been taken over.
			wantNS := ns.DeepCopy()
			wantNS.TypeMeta = metav1.TypeMeta{}
			wantNS.Name = nsName
			wantNS.Labels = map[string]string{
				"foo": "bar",
				// The label below is added by K8s itself (system-managed well-known label).
				"kubernetes.io/metadata.name": "ns-b3",
			}

			Consistently(func() error {
				// Retrieve the NS object.
				if err := memberClient.Get(ctx, client.ObjectKey{Name: nsName}, regularNS); err != nil {
					return fmt.Errorf("failed to retrieve the NS object: %w", err)
				}

				// To ignore default values automatically, here the test suite rebuilds the objects.
				rebuiltGotNS := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:            regularNS.Name,
						Labels:          regularNS.Labels,
						OwnerReferences: regularNS.OwnerReferences,
					},
				}

				if diff := cmp.Diff(rebuiltGotNS, wantNS); diff != "" {
					return fmt.Errorf("namespace diff (-got +want):\n%s", diff)
				}
				return nil
			}, consistentlyDuration, consistentlyInternal).Should(Succeed(), "Failed to take over the NS object")

			// Verify that the Deployment object has not been taken over.
			wantDeploy := deploy.DeepCopy()
			wantDeploy.TypeMeta = metav1.TypeMeta{}
			wantDeploy.Namespace = nsName
			wantDeploy.Name = deployName
			wantDeploy.Spec.Replicas = ptr.To(int32(2))

			Consistently(func() error {
				if err := memberClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: deployName}, regularDeploy); err != nil {
					return fmt.Errorf("failed to retrieve the Deployment object: %w", err)
				}

				if len(regularDeploy.Spec.Template.Spec.Containers) != 1 {
					return fmt.Errorf("number of containers in the Deployment object, got %d, want %d", len(regularDeploy.Spec.Template.Spec.Containers), 1)
				}
				if len(regularDeploy.Spec.Template.Spec.Containers[0].Ports) != 1 {
					return fmt.Errorf("number of ports in the first container, got %d, want %d", len(regularDeploy.Spec.Template.Spec.Containers[0].Ports), 1)
				}

				// To ignore default values automatically, here the test suite rebuilds the objects.
				rebuiltGotDeploy := &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Namespace:       regularDeploy.Namespace,
						Name:            regularDeploy.Name,
						OwnerReferences: regularDeploy.OwnerReferences,
					},
					Spec: appsv1.DeploymentSpec{
						Replicas: regularDeploy.Spec.Replicas,
						Selector: regularDeploy.Spec.Selector,
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: regularDeploy.Spec.Template.ObjectMeta.Labels,
							},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  regularDeploy.Spec.Template.Spec.Containers[0].Name,
										Image: regularDeploy.Spec.Template.Spec.Containers[0].Image,
										Ports: []corev1.ContainerPort{
											{
												ContainerPort: regularDeploy.Spec.Template.Spec.Containers[0].Ports[0].ContainerPort,
											},
										},
									},
								},
							},
						},
					},
				}

				if diff := cmp.Diff(rebuiltGotDeploy, wantDeploy); diff != "" {
					return fmt.Errorf("deployment diff (-got +want):\n%s", diff)
				}
				return nil
			}, consistentlyDuration, consistentlyInternal, "Failed to leave the Deployment object alone")
		})

		It("should update the Work object status", func() {
			noLaterThanTimestamp := metav1.Now()

			// Prepare the status information.
			workConds := []metav1.Condition{
				{
					Type:   fleetv1beta1.WorkConditionTypeApplied,
					Status: metav1.ConditionFalse,
					Reason: notAllManifestsAppliedReason,
				},
				{
					Type:   fleetv1beta1.WorkConditionTypeAvailable,
					Status: metav1.ConditionFalse,
					Reason: notAllAppliedObjectsAvailableReason,
				},
			}
			manifestConds := []fleetv1beta1.ManifestCondition{
				{
					Identifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:  0,
						Group:    "",
						Version:  "v1",
						Kind:     "Namespace",
						Resource: "namespaces",
						Name:     nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:   fleetv1beta1.WorkConditionTypeApplied,
							Status: metav1.ConditionFalse,
							Reason: string(ManifestProcessingApplyResultTypeFailedToTakeOver),
						},
					},
					DiffDetails: &fleetv1beta1.DiffDetails{
						ObservedInMemberClusterGeneration: regularNS.Generation,
						ObservedDiffs: []fleetv1beta1.PatchDetail{
							{
								Path:          "/metadata/labels/foo",
								ValueInMember: "bar",
							},
							// TO-DO (chenyu1): This is a namespace specific field; consider
							// if this should be added as an exception which allows ignoring
							// this diff automatically.
							{
								Path:          "/spec/finalizers",
								ValueInMember: "[kubernetes]",
							},
						},
					},
				},
				{
					Identifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:   1,
						Group:     "apps",
						Version:   "v1",
						Kind:      "Deployment",
						Resource:  "deployments",
						Name:      deployName,
						Namespace: nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:   fleetv1beta1.WorkConditionTypeApplied,
							Status: metav1.ConditionFalse,
							Reason: string(ManifestProcessingApplyResultTypeFailedToTakeOver),
						},
					},
					DiffDetails: &fleetv1beta1.DiffDetails{
						ObservedInMemberClusterGeneration: regularDeploy.Generation,
						ObservedDiffs: []fleetv1beta1.PatchDetail{
							{Path: "/spec/progressDeadlineSeconds", ValueInMember: "600"},
							{
								Path:          "/spec/replicas",
								ValueInMember: "2",
								ValueInHub:    "1",
							},
							{Path: "/spec/revisionHistoryLimit", ValueInMember: "10"},
							{
								Path:          "/spec/strategy/rollingUpdate",
								ValueInMember: "map[maxSurge:25% maxUnavailable:25%]",
							},
							{Path: "/spec/strategy/type", ValueInMember: "RollingUpdate"},
							{
								Path:          "/spec/template/spec/containers/0/imagePullPolicy",
								ValueInMember: "Always",
							},
							{Path: "/spec/template/spec/containers/0/ports/0/protocol", ValueInMember: "TCP"},
							{
								Path:          "/spec/template/spec/containers/0/terminationMessagePath",
								ValueInMember: "/dev/termination-log",
							},
							{
								Path:          "/spec/template/spec/containers/0/terminationMessagePolicy",
								ValueInMember: "File",
							},
							{Path: "/spec/template/spec/dnsPolicy", ValueInMember: "ClusterFirst"},
							{Path: "/spec/template/spec/restartPolicy", ValueInMember: "Always"},
							{Path: "/spec/template/spec/schedulerName", ValueInMember: "default-scheduler"},
							{Path: "/spec/template/spec/securityContext", ValueInMember: "map[]"},
							{Path: "/spec/template/spec/terminationGracePeriodSeconds", ValueInMember: "30"},
						},
					},
				},
			}

			workStatusUpdatedActual := workStatusUpdated(workName, workConds, manifestConds, &noLaterThanTimestamp, &noLaterThanTimestamp)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to update work status")
		})

		It("should update the AppliedWork object status", func() {
			// No object can be applied, hence no resource are bookkept in the AppliedWork object status.
			appliedWorkStatusUpdatedActual := appliedWorkStatusUpdated(workName, nil)
			Eventually(appliedWorkStatusUpdatedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to update appliedWork status")
		})

		AfterAll(func() {
			// Delete the Work object and related resources.
			cleanupWorkObject(workName)

			// Ensure that the AppliedWork object has been removed.
			appliedWorkRemovedActual := appliedWorkRemovedActual(workName)
			Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to remove the AppliedWork object")

			// Ensure that the Deployment object has been left alone.
			regularDeployNotRemovedActual := regularDeployNotRemovedActual(nsName, deployName)
			Consistently(regularDeployNotRemovedActual, consistentlyDuration, consistentlyInternal).Should(Succeed(), "Failed to remove the deployment object")

			// The environment prepared by the envtest package does not support namespace
			// deletion; consequently this test suite would not attempt so verify its deletion.
		})
	})

	Context("detect drifts (apply if no drift, drift occurred, partial comparison)", Ordered, func() {
		workName := fmt.Sprintf(workNameTemplate, "b6")
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, "b6")
		deployName := fmt.Sprintf(deployNameTemplate, "b6")

		var appliedWorkOwnerRef *metav1.OwnerReference
		var regularNS *corev1.Namespace
		var regularDeploy *appsv1.Deployment

		BeforeAll(func() {
			// Prepare a NS object.
			regularNS = ns.DeepCopy()
			regularNS.Name = nsName
			regularNSJSON := marshalK8sObjJSON(regularNS)

			// Prepare a Deployment object.
			regularDeploy = deploy.DeepCopy()
			regularDeploy.Namespace = nsName
			regularDeploy.Name = deployName
			regularDeployJSON := marshalK8sObjJSON(regularDeploy)

			// Create a new Work object with all the manifest JSONs and proper apply strategy.
			applyStrategy := &fleetv1beta1.ApplyStrategy{
				ComparisonOption: fleetv1beta1.ComparisonOptionTypePartialComparison,
				WhenToApply:      fleetv1beta1.WhenToApplyTypeIfNotDrifted,
			}
			createWorkObject(workName, applyStrategy, regularNSJSON, regularDeployJSON)
		})

		It("should add cleanup finalizer to the Work object", func() {
			finalizerAddedActual := workFinalizerAddedActual(workName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to add cleanup finalizer to the Work object")
		})

		It("should prepare an AppliedWork object", func() {
			appliedWorkCreatedActual := appliedWorkCreatedActual(workName)
			Eventually(appliedWorkCreatedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to prepare an AppliedWork object")

			appliedWorkOwnerRef = prepareAppliedWorkOwnerRef(workName)
		})

		It("should apply the manifests", func() {
			// Ensure that the NS object has been applied as expected.
			regularNSObjectAppliedActual := regularNSObjectAppliedActual(nsName, appliedWorkOwnerRef)
			Eventually(regularNSObjectAppliedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to apply the namespace object")

			Expect(memberClient.Get(ctx, client.ObjectKey{Name: nsName}, regularNS)).To(Succeed(), "Failed to retrieve the NS object")

			// Ensure that the Deployment object has been applied as expected.
			regularDeploymentObjectAppliedActual := regularDeploymentObjectAppliedActual(nsName, deployName, appliedWorkOwnerRef)
			Eventually(regularDeploymentObjectAppliedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to apply the deployment object")

			Expect(memberClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: deployName}, regularDeploy)).To(Succeed(), "Failed to retrieve the Deployment object")
		})

		It("can mark the deployment as available", func() {
			markDeploymentAsAvailable(nsName, deployName)
		})

		It("should update the Work object status", func() {
			// Prepare the status information.
			workConds := []metav1.Condition{
				{
					Type:   fleetv1beta1.WorkConditionTypeApplied,
					Status: metav1.ConditionTrue,
					Reason: string(ManifestProcessingApplyResultTypeApplied),
				},
				{
					Type:   fleetv1beta1.WorkConditionTypeAvailable,
					Status: metav1.ConditionTrue,
					Reason: string(ManifestProcessingAvailabilityResultTypeAvailable),
				},
			}
			manifestConds := []fleetv1beta1.ManifestCondition{
				{
					Identifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:  0,
						Group:    "",
						Version:  "v1",
						Kind:     "Namespace",
						Resource: "namespaces",
						Name:     nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:   fleetv1beta1.WorkConditionTypeApplied,
							Status: metav1.ConditionTrue,
							Reason: string(ManifestProcessingApplyResultTypeApplied),
						},
						{
							Type:   fleetv1beta1.WorkConditionTypeAvailable,
							Status: metav1.ConditionTrue,
							Reason: string(ManifestProcessingAvailabilityResultTypeAvailable),
						},
					},
				},
				{
					Identifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:   1,
						Group:     "apps",
						Version:   "v1",
						Kind:      "Deployment",
						Resource:  "deployments",
						Name:      deployName,
						Namespace: nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:   fleetv1beta1.WorkConditionTypeApplied,
							Status: metav1.ConditionTrue,
							Reason: string(ManifestProcessingApplyResultTypeApplied),
						},
						{
							Type:   fleetv1beta1.WorkConditionTypeAvailable,
							Status: metav1.ConditionTrue,
							Reason: string(ManifestProcessingAvailabilityResultTypeAvailable),
						},
					},
				},
			}

			workStatusUpdatedActual := workStatusUpdated(workName, workConds, manifestConds, nil, nil)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to update work status")
		})

		It("should update the AppliedWork object status", func() {
			// Prepare the status information.
			appliedResourceMeta := []fleetv1beta1.AppliedResourceMeta{
				{
					WorkResourceIdentifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:  0,
						Group:    "",
						Version:  "v1",
						Kind:     "Namespace",
						Resource: "namespaces",
						Name:     nsName,
					},
					UID: regularNS.UID,
				},
				{
					WorkResourceIdentifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:   1,
						Group:     "apps",
						Version:   "v1",
						Kind:      "Deployment",
						Resource:  "deployments",
						Name:      deployName,
						Namespace: nsName,
					},
					UID: regularDeploy.UID,
				},
			}

			appliedWorkStatusUpdatedActual := appliedWorkStatusUpdated(workName, appliedResourceMeta)
			Eventually(appliedWorkStatusUpdatedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to update appliedWork status")
		})

		It("can make changes to the objects", func() {
			// Use Eventually blocks to avoid conflicts.
			Eventually(func() error {
				// Retrieve the Deployment object.
				updatedDeploy := &appsv1.Deployment{}
				if err := memberClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: deployName}, updatedDeploy); err != nil {
					return fmt.Errorf("failed to retrieve the Deployment object: %w", err)
				}

				// Make changes to the Deployment object.
				updatedDeploy.Spec.Replicas = ptr.To(int32(2))

				// Update the Deployment object.
				if err := memberClient.Update(ctx, updatedDeploy); err != nil {
					return fmt.Errorf("failed to update the Deployment object: %w", err)
				}
				return nil
			}, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to update the Deployment object")

			Eventually(func() error {
				// Retrieve the NS object.
				updatedNS := &corev1.Namespace{}
				if err := memberClient.Get(ctx, client.ObjectKey{Name: nsName}, updatedNS); err != nil {
					return fmt.Errorf("failed to retrieve the NS object: %w", err)
				}

				// Make changes to the NS object.
				if updatedNS.Labels == nil {
					updatedNS.Labels = map[string]string{}
				}
				updatedNS.Labels["foo"] = "bar"

				// Update the NS object.
				if err := memberClient.Update(ctx, updatedNS); err != nil {
					return fmt.Errorf("failed to update the NS object: %w", err)
				}
				return nil
			}, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to update the NS object")
		})

		It("should continue to apply some manifest (while preserving drifts in unmanaged fields)", func() {
			// Verify that the object are still being applied, with the drifts in unmanaged fields
			// untouched.
			wantNS := ns.DeepCopy()
			wantNS.TypeMeta = metav1.TypeMeta{}
			wantNS.Name = nsName
			wantNS.Labels = map[string]string{
				"foo": "bar",
				// The label below is added by K8s itself (system-managed well-known label).
				"kubernetes.io/metadata.name": "ns-b6",
			}
			wantNS.OwnerReferences = []metav1.OwnerReference{
				*appliedWorkOwnerRef,
			}

			Consistently(func() error {
				// Retrieve the NS object.
				if err := memberClient.Get(ctx, client.ObjectKey{Name: nsName}, regularNS); err != nil {
					return fmt.Errorf("failed to retrieve the NS object: %w", err)
				}

				// To ignore default values automatically, here the test suite rebuilds the objects.
				rebuiltGotNS := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:            regularNS.Name,
						Labels:          regularNS.Labels,
						OwnerReferences: regularNS.OwnerReferences,
					},
				}

				if diff := cmp.Diff(rebuiltGotNS, wantNS); diff != "" {
					return fmt.Errorf("namespace diff (-got +want):\n%s", diff)
				}
				return nil
			}, consistentlyDuration, consistentlyInternal).Should(Succeed(), "Failed to take over the NS object")
		})

		It("should stop applying some objects", func() {
			// Verify that the changes in managed fields are not overwritten.
			wantDeploy := deploy.DeepCopy()
			wantDeploy.TypeMeta = metav1.TypeMeta{}
			wantDeploy.Namespace = nsName
			wantDeploy.Name = deployName
			wantDeploy.OwnerReferences = []metav1.OwnerReference{
				*appliedWorkOwnerRef,
			}
			wantDeploy.Spec.Replicas = ptr.To(int32(2))

			Consistently(func() error {
				if err := memberClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: deployName}, regularDeploy); err != nil {
					return fmt.Errorf("failed to retrieve the Deployment object: %w", err)
				}

				if len(regularDeploy.Spec.Template.Spec.Containers) != 1 {
					return fmt.Errorf("number of containers in the Deployment object, got %d, want %d", len(regularDeploy.Spec.Template.Spec.Containers), 1)
				}
				if len(regularDeploy.Spec.Template.Spec.Containers[0].Ports) != 1 {
					return fmt.Errorf("number of ports in the first container, got %d, want %d", len(regularDeploy.Spec.Template.Spec.Containers[0].Ports), 1)
				}

				// To ignore default values automatically, here the test suite rebuilds the objects.
				rebuiltGotDeploy := &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Namespace:       regularDeploy.Namespace,
						Name:            regularDeploy.Name,
						OwnerReferences: regularDeploy.OwnerReferences,
					},
					Spec: appsv1.DeploymentSpec{
						Replicas: regularDeploy.Spec.Replicas,
						Selector: regularDeploy.Spec.Selector,
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: regularDeploy.Spec.Template.ObjectMeta.Labels,
							},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  regularDeploy.Spec.Template.Spec.Containers[0].Name,
										Image: regularDeploy.Spec.Template.Spec.Containers[0].Image,
										Ports: []corev1.ContainerPort{
											{
												ContainerPort: regularDeploy.Spec.Template.Spec.Containers[0].Ports[0].ContainerPort,
											},
										},
									},
								},
							},
						},
					},
				}

				if diff := cmp.Diff(rebuiltGotDeploy, wantDeploy); diff != "" {
					return fmt.Errorf("deployment diff (-got +want):\n%s", diff)
				}
				return nil
			}, consistentlyDuration, consistentlyInternal).Should(Succeed(), "Failed to leave the Deployment object alone")
		})

		It("should update the Work object status", func() {
			// Shift the timestamp to account for drift detection delays.
			noLaterThanTimestamp := metav1.Time{
				Time: time.Now().Add(time.Second * 30),
			}

			// Prepare the status information.
			workConds := []metav1.Condition{
				{
					Type:   fleetv1beta1.WorkConditionTypeApplied,
					Status: metav1.ConditionFalse,
					Reason: notAllManifestsAppliedReason,
				},
				{
					Type:   fleetv1beta1.WorkConditionTypeAvailable,
					Status: metav1.ConditionFalse,
					Reason: notAllAppliedObjectsAvailableReason,
				},
			}
			manifestConds := []fleetv1beta1.ManifestCondition{
				{
					Identifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:  0,
						Group:    "",
						Version:  "v1",
						Kind:     "Namespace",
						Resource: "namespaces",
						Name:     nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:   fleetv1beta1.WorkConditionTypeApplied,
							Status: metav1.ConditionTrue,
							Reason: string(ManifestProcessingApplyResultTypeApplied),
						},
						{
							Type:   fleetv1beta1.WorkConditionTypeAvailable,
							Status: metav1.ConditionTrue,
							Reason: string(ManifestProcessingAvailabilityResultTypeAvailable),
						},
					},
				},
				{
					Identifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:   1,
						Group:     "apps",
						Version:   "v1",
						Kind:      "Deployment",
						Resource:  "deployments",
						Name:      deployName,
						Namespace: nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:   fleetv1beta1.WorkConditionTypeApplied,
							Status: metav1.ConditionFalse,
							Reason: string(ManifestProcessingApplyResultTypeFoundDrifts),
						},
					},
					DriftDetails: &fleetv1beta1.DriftDetails{
						ObservedInMemberClusterGeneration: regularDeploy.Generation,
						ObservedDrifts: []fleetv1beta1.PatchDetail{
							{
								Path:          "/spec/replicas",
								ValueInMember: "2",
								ValueInHub:    "1",
							},
						},
					},
				},
			}

			workStatusUpdatedActual := workStatusUpdated(workName, workConds, manifestConds, &noLaterThanTimestamp, &noLaterThanTimestamp)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to update work status")
		})

		It("should update the AppliedWork object status", func() {
			// Prepare the status information.
			appliedResourceMeta := []fleetv1beta1.AppliedResourceMeta{
				{
					WorkResourceIdentifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:  0,
						Group:    "",
						Version:  "v1",
						Kind:     "Namespace",
						Resource: "namespaces",
						Name:     nsName,
					},
					UID: regularNS.UID,
				},
			}

			appliedWorkStatusUpdatedActual := appliedWorkStatusUpdated(workName, appliedResourceMeta)
			Eventually(appliedWorkStatusUpdatedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to update appliedWork status")
		})

		AfterAll(func() {
			// Delete the Work object and related resources.
			cleanupWorkObject(workName)

			// Ensure that the AppliedWork object has been removed.
			appliedWorkRemovedActual := appliedWorkRemovedActual(workName)
			Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to remove the AppliedWork object")

			// Ensure that the Deployment object has been left alone.
			regularDeployNotRemovedActual := regularDeployNotRemovedActual(nsName, deployName)
			Consistently(regularDeployNotRemovedActual, consistentlyDuration, consistentlyInternal).Should(Succeed(), "Failed to remove the deployment object")

			// The environment prepared by the envtest package does not support namespace
			// deletion; consequently this test suite would not attempt so verify its deletion.
		})
	})

	// For simplicity reasons, this test case will only involve a NS object.
	Context("detect drifts (apply if no drift, drift occurred, full comparison)", Ordered, func() {
		workName := fmt.Sprintf(workNameTemplate, "b7")
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, "b7")

		var appliedWorkOwnerRef *metav1.OwnerReference
		var regularNS *corev1.Namespace

		BeforeAll(func() {
			// Prepare a NS object.
			regularNS = ns.DeepCopy()
			regularNS.Name = nsName
			regularNS.Spec.Finalizers = []corev1.FinalizerName{"kubernetes"}
			regularNSJSON := marshalK8sObjJSON(regularNS)

			// Create a new Work object with all the manifest JSONs and proper apply strategy.
			applyStrategy := &fleetv1beta1.ApplyStrategy{
				ComparisonOption: fleetv1beta1.ComparisonOptionTypeFullComparison,
				WhenToApply:      fleetv1beta1.WhenToApplyTypeIfNotDrifted,
			}
			createWorkObject(workName, applyStrategy, regularNSJSON)
		})

		It("should add cleanup finalizer to the Work object", func() {
			finalizerAddedActual := workFinalizerAddedActual(workName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to add cleanup finalizer to the Work object")
		})

		It("should prepare an AppliedWork object", func() {
			appliedWorkCreatedActual := appliedWorkCreatedActual(workName)
			Eventually(appliedWorkCreatedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to prepare an AppliedWork object")

			appliedWorkOwnerRef = prepareAppliedWorkOwnerRef(workName)
		})

		It("should apply the manifests", func() {
			// Ensure that the NS object has been applied as expected.
			regularNSObjectAppliedActual := regularNSObjectAppliedActual(nsName, appliedWorkOwnerRef)
			Eventually(regularNSObjectAppliedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to apply the namespace object")

			Expect(memberClient.Get(ctx, client.ObjectKey{Name: nsName}, regularNS)).To(Succeed(), "Failed to retrieve the NS object")
		})

		It("should update the Work object status", func() {
			// Prepare the status information.
			workConds := []metav1.Condition{
				{
					Type:   fleetv1beta1.WorkConditionTypeApplied,
					Status: metav1.ConditionTrue,
					Reason: string(ManifestProcessingApplyResultTypeApplied),
				},
				{
					Type:   fleetv1beta1.WorkConditionTypeAvailable,
					Status: metav1.ConditionTrue,
					Reason: string(ManifestProcessingAvailabilityResultTypeAvailable),
				},
			}
			manifestConds := []fleetv1beta1.ManifestCondition{
				{
					Identifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:  0,
						Group:    "",
						Version:  "v1",
						Kind:     "Namespace",
						Resource: "namespaces",
						Name:     nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:   fleetv1beta1.WorkConditionTypeApplied,
							Status: metav1.ConditionTrue,
							Reason: string(ManifestProcessingApplyResultTypeApplied),
						},
						{
							Type:   fleetv1beta1.WorkConditionTypeAvailable,
							Status: metav1.ConditionTrue,
							Reason: string(ManifestProcessingAvailabilityResultTypeAvailable),
						},
					},
				},
			}

			workStatusUpdatedActual := workStatusUpdated(workName, workConds, manifestConds, nil, nil)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to update work status")
		})

		It("should update the AppliedWork object status", func() {
			// Prepare the status information.
			appliedResourceMeta := []fleetv1beta1.AppliedResourceMeta{
				{
					WorkResourceIdentifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:  0,
						Group:    "",
						Version:  "v1",
						Kind:     "Namespace",
						Resource: "namespaces",
						Name:     nsName,
					},
					UID: regularNS.UID,
				},
			}

			appliedWorkStatusUpdatedActual := appliedWorkStatusUpdated(workName, appliedResourceMeta)
			Eventually(appliedWorkStatusUpdatedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to update appliedWork status")
		})

		It("can make changes to the objects", func() {
			Eventually(func() error {
				// Retrieve the NS object.
				updatedNS := &corev1.Namespace{}
				if err := memberClient.Get(ctx, client.ObjectKey{Name: nsName}, updatedNS); err != nil {
					return fmt.Errorf("failed to retrieve the NS object: %w", err)
				}

				// Make changes to the NS object.
				if updatedNS.Labels == nil {
					updatedNS.Labels = map[string]string{}
				}
				updatedNS.Labels["foo"] = "bar"

				// Update the NS object.
				if err := memberClient.Update(ctx, updatedNS); err != nil {
					return fmt.Errorf("failed to update the NS object: %w", err)
				}
				return nil
			}, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to update the NS object")
		})

		It("should stop applying some objects", func() {
			// Verify that the changes in unmanaged fields are not overwritten.
			wantNS := ns.DeepCopy()
			wantNS.TypeMeta = metav1.TypeMeta{}
			wantNS.Name = nsName
			wantNS.OwnerReferences = []metav1.OwnerReference{
				*appliedWorkOwnerRef,
			}
			wantNS.Labels = map[string]string{
				"foo": "bar",
				// The label below is added by K8s itself (system-managed well-known label).
				"kubernetes.io/metadata.name": "ns-b7",
			}

			Consistently(func() error {
				// Retrieve the NS object.
				if err := memberClient.Get(ctx, client.ObjectKey{Name: nsName}, regularNS); err != nil {
					return fmt.Errorf("failed to retrieve the NS object: %w", err)
				}

				// To ignore default values automatically, here the test suite rebuilds the objects.
				rebuiltGotNS := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:            regularNS.Name,
						Labels:          regularNS.Labels,
						OwnerReferences: regularNS.OwnerReferences,
					},
				}

				if diff := cmp.Diff(rebuiltGotNS, wantNS); diff != "" {
					return fmt.Errorf("namespace diff (-got +want):\n%s", diff)
				}
				return nil
			}, consistentlyDuration, consistentlyInternal).Should(Succeed(), "Failed to leave the NS object alone")
		})

		It("should update the Work object status", func() {
			// Shift the timestamp to account for drift detection delays.
			noLaterThanTimestamp := metav1.Time{
				Time: time.Now().Add(time.Second * 30),
			}

			// Prepare the status information.
			workConds := []metav1.Condition{
				{
					Type:   fleetv1beta1.WorkConditionTypeApplied,
					Status: metav1.ConditionFalse,
					Reason: notAllManifestsAppliedReason,
				},
				{
					Type:   fleetv1beta1.WorkConditionTypeAvailable,
					Status: metav1.ConditionFalse,
					Reason: notAllAppliedObjectsAvailableReason,
				},
			}
			manifestConds := []fleetv1beta1.ManifestCondition{
				{
					Identifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:  0,
						Group:    "",
						Version:  "v1",
						Kind:     "Namespace",
						Resource: "namespaces",
						Name:     nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:   fleetv1beta1.WorkConditionTypeApplied,
							Status: metav1.ConditionFalse,
							Reason: string(ManifestProcessingApplyResultTypeFoundDrifts),
						},
					},
					DriftDetails: &fleetv1beta1.DriftDetails{
						ObservedInMemberClusterGeneration: regularNS.Generation,
						ObservedDrifts: []fleetv1beta1.PatchDetail{
							{
								Path:          "/metadata/labels/foo",
								ValueInMember: "bar",
							},
						},
					},
				},
			}

			workStatusUpdatedActual := workStatusUpdated(workName, workConds, manifestConds, &noLaterThanTimestamp, &noLaterThanTimestamp)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to update work status")
		})

		It("should update the AppliedWork object status", func() {
			// No object can be applied, hence no resource are bookkept in the AppliedWork object status.
			appliedWorkStatusUpdatedActual := appliedWorkStatusUpdated(workName, nil)
			Eventually(appliedWorkStatusUpdatedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to update appliedWork status")
		})

		AfterAll(func() {
			// Delete the Work object and related resources.
			cleanupWorkObject(workName)

			// Ensure that the AppliedWork object has been removed.
			appliedWorkRemovedActual := appliedWorkRemovedActual(workName)
			Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to remove the AppliedWork object")

			// The environment prepared by the envtest package does not support namespace
			// deletion; consequently this test suite would not attempt so verify its deletion.
		})
	})

	// For simplicity reasons, this test case will only involve a NS object.
	Context("overwrite drifts (always apply, partial comparison)", Ordered, func() {
		workName := fmt.Sprintf(workNameTemplate, "b8")
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, "b8")

		var appliedWorkOwnerRef *metav1.OwnerReference
		var regularNS *corev1.Namespace

		BeforeAll(func() {
			// Prepare a NS object.
			regularNS = ns.DeepCopy()
			regularNS.Name = nsName
			regularNS.Labels = map[string]string{
				"foo": "bar",
			}
			regularNSJSON := marshalK8sObjJSON(regularNS)

			// Create a new Work object with all the manifest JSONs and proper apply strategy.
			applyStrategy := &fleetv1beta1.ApplyStrategy{
				ComparisonOption: fleetv1beta1.ComparisonOptionTypePartialComparison,
				WhenToApply:      fleetv1beta1.WhenToApplyTypeAlways,
			}
			createWorkObject(workName, applyStrategy, regularNSJSON)
		})

		It("should add cleanup finalizer to the Work object", func() {
			finalizerAddedActual := workFinalizerAddedActual(workName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to add cleanup finalizer to the Work object")
		})

		It("should prepare an AppliedWork object", func() {
			appliedWorkCreatedActual := appliedWorkCreatedActual(workName)
			Eventually(appliedWorkCreatedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to prepare an AppliedWork object")

			appliedWorkOwnerRef = prepareAppliedWorkOwnerRef(workName)
		})

		It("should apply the manifests", func() {
			// Ensure that the NS object has been applied as expected.
			regularNSObjectAppliedActual := regularNSObjectAppliedActual(nsName, appliedWorkOwnerRef)
			Eventually(regularNSObjectAppliedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to apply the namespace object")

			Expect(memberClient.Get(ctx, client.ObjectKey{Name: nsName}, regularNS)).To(Succeed(), "Failed to retrieve the NS object")
		})

		It("should update the Work object status", func() {
			// Prepare the status information.
			workConds := []metav1.Condition{
				{
					Type:   fleetv1beta1.WorkConditionTypeApplied,
					Status: metav1.ConditionTrue,
					Reason: string(ManifestProcessingApplyResultTypeApplied),
				},
				{
					Type:   fleetv1beta1.WorkConditionTypeAvailable,
					Status: metav1.ConditionTrue,
					Reason: string(ManifestProcessingAvailabilityResultTypeAvailable),
				},
			}
			manifestConds := []fleetv1beta1.ManifestCondition{
				{
					Identifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:  0,
						Group:    "",
						Version:  "v1",
						Kind:     "Namespace",
						Resource: "namespaces",
						Name:     nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:   fleetv1beta1.WorkConditionTypeApplied,
							Status: metav1.ConditionTrue,
							Reason: string(ManifestProcessingApplyResultTypeApplied),
						},
						{
							Type:   fleetv1beta1.WorkConditionTypeAvailable,
							Status: metav1.ConditionTrue,
							Reason: string(ManifestProcessingAvailabilityResultTypeAvailable),
						},
					},
				},
			}

			workStatusUpdatedActual := workStatusUpdated(workName, workConds, manifestConds, nil, nil)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to update work status")
		})

		It("should update the AppliedWork object status", func() {
			// Prepare the status information.
			appliedResourceMeta := []fleetv1beta1.AppliedResourceMeta{
				{
					WorkResourceIdentifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:  0,
						Group:    "",
						Version:  "v1",
						Kind:     "Namespace",
						Resource: "namespaces",
						Name:     nsName,
					},
					UID: regularNS.UID,
				},
			}

			appliedWorkStatusUpdatedActual := appliedWorkStatusUpdated(workName, appliedResourceMeta)
			Eventually(appliedWorkStatusUpdatedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to update appliedWork status")
		})

		It("can make changes to the objects", func() {
			Eventually(func() error {
				// Retrieve the NS object.
				updatedNS := &corev1.Namespace{}
				if err := memberClient.Get(ctx, client.ObjectKey{Name: nsName}, updatedNS); err != nil {
					return fmt.Errorf("failed to retrieve the NS object: %w", err)
				}

				// Make changes to the NS object.
				if updatedNS.Labels == nil {
					updatedNS.Labels = map[string]string{}
				}
				updatedNS.Labels["foo"] = "baz"

				// Update the NS object.
				if err := memberClient.Update(ctx, updatedNS); err != nil {
					return fmt.Errorf("failed to update the NS object: %w", err)
				}
				return nil
			}, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to update the NS object")
		})

		It("should continue to apply some manifest (while overwriting drifts in managed fields)", func() {
			// Verify that the object are still being applied, with the drifts in managed fields
			// overwritten.
			wantNS := ns.DeepCopy()
			wantNS.TypeMeta = metav1.TypeMeta{}
			wantNS.Name = nsName
			wantNS.Labels = map[string]string{
				"foo": "bar",
				// The label below is added by K8s itself (system-managed well-known label).
				"kubernetes.io/metadata.name": "ns-b8",
			}
			wantNS.OwnerReferences = []metav1.OwnerReference{
				*appliedWorkOwnerRef,
			}

			nsOverwrittenActual := func() error {
				// Retrieve the NS object.
				if err := memberClient.Get(ctx, client.ObjectKey{Name: nsName}, regularNS); err != nil {
					return fmt.Errorf("failed to retrieve the NS object: %w", err)
				}

				// To ignore default values automatically, here the test suite rebuilds the objects.
				rebuiltGotNS := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:            regularNS.Name,
						Labels:          regularNS.Labels,
						OwnerReferences: regularNS.OwnerReferences,
					},
				}

				if diff := cmp.Diff(rebuiltGotNS, wantNS); diff != "" {
					return fmt.Errorf("namespace diff (-got +want):\n%s", diff)
				}
				return nil
			}
			Eventually(nsOverwrittenActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to apply the NS object")
			Consistently(nsOverwrittenActual, consistentlyDuration, consistentlyInternal).Should(Succeed(), "Failed to apply the NS object")
		})

		It("should update the Work object status", func() {
			// Prepare the status information.
			workConds := []metav1.Condition{
				{
					Type:   fleetv1beta1.WorkConditionTypeApplied,
					Status: metav1.ConditionTrue,
					Reason: string(ManifestProcessingApplyResultTypeApplied),
				},
				{
					Type:   fleetv1beta1.WorkConditionTypeAvailable,
					Status: metav1.ConditionTrue,
					Reason: string(ManifestProcessingAvailabilityResultTypeAvailable),
				},
			}
			manifestConds := []fleetv1beta1.ManifestCondition{
				{
					Identifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:  0,
						Group:    "",
						Version:  "v1",
						Kind:     "Namespace",
						Resource: "namespaces",
						Name:     nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:   fleetv1beta1.WorkConditionTypeApplied,
							Status: metav1.ConditionTrue,
							Reason: string(ManifestProcessingApplyResultTypeApplied),
						},
						{
							Type:   fleetv1beta1.WorkConditionTypeAvailable,
							Status: metav1.ConditionTrue,
							Reason: string(ManifestProcessingAvailabilityResultTypeAvailable),
						},
					},
				},
			}

			workStatusUpdatedActual := workStatusUpdated(workName, workConds, manifestConds, nil, nil)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to update work status")
		})

		It("should update the AppliedWork object status", func() {
			// Prepare the status information.
			appliedResourceMeta := []fleetv1beta1.AppliedResourceMeta{
				{
					WorkResourceIdentifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:  0,
						Group:    "",
						Version:  "v1",
						Kind:     "Namespace",
						Resource: "namespaces",
						Name:     nsName,
					},
					UID: regularNS.UID,
				},
			}

			appliedWorkStatusUpdatedActual := appliedWorkStatusUpdated(workName, appliedResourceMeta)
			Eventually(appliedWorkStatusUpdatedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to update appliedWork status")
		})

		AfterAll(func() {
			// Delete the Work object and related resources.
			cleanupWorkObject(workName)

			// Ensure that the AppliedWork object has been removed.
			appliedWorkRemovedActual := appliedWorkRemovedActual(workName)
			Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to remove the AppliedWork object")

			// The environment prepared by the envtest package does not support namespace
			// deletion; consequently this test suite would not attempt so verify its deletion.
		})
	})

	// For simplicity reasons, this test case will only involve a NS object.
	Context("overwrite drifts (apply if no drift, drift occurred before manifest version bump, partial comparison)", Ordered, func() {
		workName := fmt.Sprintf(workNameTemplate, "b9")
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, "b9")

		var appliedWorkOwnerRef *metav1.OwnerReference
		var regularNS *corev1.Namespace

		BeforeAll(func() {
			// Prepare a NS object.
			regularNS = ns.DeepCopy()
			regularNS.Name = nsName
			regularNS.Labels = map[string]string{
				"foo": "bar",
			}

			// Create a new Work object with all the manifest JSONs and proper apply strategy.
			applyStrategy := &fleetv1beta1.ApplyStrategy{
				ComparisonOption: fleetv1beta1.ComparisonOptionTypePartialComparison,
				WhenToApply:      fleetv1beta1.WhenToApplyTypeIfNotDrifted,
			}
			createWorkObject(workName, applyStrategy, marshalK8sObjJSON(regularNS))
		})

		It("should add cleanup finalizer to the Work object", func() {
			finalizerAddedActual := workFinalizerAddedActual(workName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to add cleanup finalizer to the Work object")
		})

		It("should prepare an AppliedWork object", func() {
			appliedWorkCreatedActual := appliedWorkCreatedActual(workName)
			Eventually(appliedWorkCreatedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to prepare an AppliedWork object")

			appliedWorkOwnerRef = prepareAppliedWorkOwnerRef(workName)
		})

		It("should apply the manifests", func() {
			// Ensure that the NS object has been applied as expected.
			regularNSObjectAppliedActual := regularNSObjectAppliedActual(nsName, appliedWorkOwnerRef)
			Eventually(regularNSObjectAppliedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to apply the namespace object")

			Expect(memberClient.Get(ctx, client.ObjectKey{Name: nsName}, regularNS)).To(Succeed(), "Failed to retrieve the NS object")
		})

		It("should update the Work object status", func() {
			// Prepare the status information.
			workConds := []metav1.Condition{
				{
					Type:   fleetv1beta1.WorkConditionTypeApplied,
					Status: metav1.ConditionTrue,
					Reason: string(ManifestProcessingApplyResultTypeApplied),
				},
				{
					Type:   fleetv1beta1.WorkConditionTypeAvailable,
					Status: metav1.ConditionTrue,
					Reason: string(ManifestProcessingAvailabilityResultTypeAvailable),
				},
			}
			manifestConds := []fleetv1beta1.ManifestCondition{
				{
					Identifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:  0,
						Group:    "",
						Version:  "v1",
						Kind:     "Namespace",
						Resource: "namespaces",
						Name:     nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:   fleetv1beta1.WorkConditionTypeApplied,
							Status: metav1.ConditionTrue,
							Reason: string(ManifestProcessingApplyResultTypeApplied),
						},
						{
							Type:   fleetv1beta1.WorkConditionTypeAvailable,
							Status: metav1.ConditionTrue,
							Reason: string(ManifestProcessingAvailabilityResultTypeAvailable),
						},
					},
				},
			}

			workStatusUpdatedActual := workStatusUpdated(workName, workConds, manifestConds, nil, nil)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to update work status")
		})

		It("should update the AppliedWork object status", func() {
			// Prepare the status information.
			appliedResourceMeta := []fleetv1beta1.AppliedResourceMeta{
				{
					WorkResourceIdentifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:  0,
						Group:    "",
						Version:  "v1",
						Kind:     "Namespace",
						Resource: "namespaces",
						Name:     nsName,
					},
					UID: regularNS.UID,
				},
			}

			appliedWorkStatusUpdatedActual := appliedWorkStatusUpdated(workName, appliedResourceMeta)
			Eventually(appliedWorkStatusUpdatedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to update appliedWork status")
		})

		It("can make changes to the objects", func() {
			Eventually(func() error {
				// Retrieve the NS object.
				updatedNS := &corev1.Namespace{}
				if err := memberClient.Get(ctx, client.ObjectKey{Name: nsName}, updatedNS); err != nil {
					return fmt.Errorf("failed to retrieve the NS object: %w", err)
				}

				// Make changes to the NS object.
				if updatedNS.Labels == nil {
					updatedNS.Labels = map[string]string{}
				}
				updatedNS.Labels["foo"] = "baz"

				// Update the NS object.
				if err := memberClient.Update(ctx, updatedNS); err != nil {
					return fmt.Errorf("failed to update the NS object: %w", err)
				}
				return nil
			}, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to update the NS object")
		})

		It("should stop applying some objects", func() {
			// Verify that the changes in unmanaged fields are not overwritten.
			wantNS := ns.DeepCopy()
			wantNS.TypeMeta = metav1.TypeMeta{}
			wantNS.Name = nsName
			wantNS.OwnerReferences = []metav1.OwnerReference{
				*appliedWorkOwnerRef,
			}
			wantNS.Labels = map[string]string{
				"foo": "baz",
				// The label below is added by K8s itself (system-managed well-known label).
				"kubernetes.io/metadata.name": "ns-b9",
			}

			Consistently(func() error {
				// Retrieve the NS object.
				if err := memberClient.Get(ctx, client.ObjectKey{Name: nsName}, regularNS); err != nil {
					return fmt.Errorf("failed to retrieve the NS object: %w", err)
				}

				// To ignore default values automatically, here the test suite rebuilds the objects.
				rebuiltGotNS := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:            regularNS.Name,
						Labels:          regularNS.Labels,
						OwnerReferences: regularNS.OwnerReferences,
					},
				}

				if diff := cmp.Diff(rebuiltGotNS, wantNS); diff != "" {
					return fmt.Errorf("namespace diff (-got +want):\n%s", diff)
				}
				return nil
			}, consistentlyDuration, consistentlyInternal).Should(Succeed(), "Failed to leave the NS object alone")
		})

		It("should update the Work object status", func() {
			// Shift the timestamp to account for drift detection delays.
			noLaterThanTimestamp := metav1.Time{
				Time: time.Now().Add(time.Second * 30),
			}

			// Prepare the status information.
			workConds := []metav1.Condition{
				{
					Type:   fleetv1beta1.WorkConditionTypeApplied,
					Status: metav1.ConditionFalse,
					Reason: notAllManifestsAppliedReason,
				},
				{
					Type:   fleetv1beta1.WorkConditionTypeAvailable,
					Status: metav1.ConditionFalse,
					Reason: notAllAppliedObjectsAvailableReason,
				},
			}
			manifestConds := []fleetv1beta1.ManifestCondition{
				{
					Identifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:  0,
						Group:    "",
						Version:  "v1",
						Kind:     "Namespace",
						Resource: "namespaces",
						Name:     nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:   fleetv1beta1.WorkConditionTypeApplied,
							Status: metav1.ConditionFalse,
							Reason: string(ManifestProcessingApplyResultTypeFoundDrifts),
						},
					},
					DriftDetails: &fleetv1beta1.DriftDetails{
						ObservedInMemberClusterGeneration: regularNS.Generation,
						ObservedDrifts: []fleetv1beta1.PatchDetail{
							{
								Path:          "/metadata/labels/foo",
								ValueInMember: "baz",
								ValueInHub:    "bar",
							},
						},
					},
				},
			}

			workStatusUpdatedActual := workStatusUpdated(workName, workConds, manifestConds, &noLaterThanTimestamp, &noLaterThanTimestamp)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to update work status")
		})

		It("should update the AppliedWork object status", func() {
			// No object can be applied, hence no resource are bookkept in the AppliedWork object status.
			appliedWorkStatusUpdatedActual := appliedWorkStatusUpdated(workName, nil)
			Eventually(appliedWorkStatusUpdatedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to update appliedWork status")
		})

		It("can update the Work object", func() {
			// Prepare a NS object.
			regularNS = ns.DeepCopy()
			regularNS.Name = nsName
			regularNS.Labels = map[string]string{
				"foo": "quz",
			}

			// Create a new Work object with all the manifest JSONs and proper apply strategy.
			applyStrategy := &fleetv1beta1.ApplyStrategy{
				ComparisonOption: fleetv1beta1.ComparisonOptionTypePartialComparison,
				WhenToApply:      fleetv1beta1.WhenToApplyTypeIfNotDrifted,
			}
			updateWorkObject(workName, applyStrategy, marshalK8sObjJSON(regularNS))
		})

		It("should apply the new manifests and overwrite all drifts in managed fields", func() {
			// Verify that the new manifests are applied.
			wantNS := ns.DeepCopy()
			wantNS.TypeMeta = metav1.TypeMeta{}
			wantNS.Name = nsName
			wantNS.Labels = map[string]string{
				"foo": "quz",
				// The label below is added by K8s itself (system-managed well-known label).
				"kubernetes.io/metadata.name": "ns-b9",
			}
			wantNS.OwnerReferences = []metav1.OwnerReference{
				*appliedWorkOwnerRef,
			}

			Eventually(func() error {
				// Retrieve the NS object.
				if err := memberClient.Get(ctx, client.ObjectKey{Name: nsName}, regularNS); err != nil {
					return fmt.Errorf("failed to retrieve the NS object: %w", err)
				}

				// To ignore default values automatically, here the test suite rebuilds the objects.
				rebuiltGotNS := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:            regularNS.Name,
						Labels:          regularNS.Labels,
						OwnerReferences: regularNS.OwnerReferences,
					},
				}

				if diff := cmp.Diff(rebuiltGotNS, wantNS); diff != "" {
					return fmt.Errorf("namespace diff (-got +want):\n%s", diff)
				}
				return nil
			}, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to apply new manifests")
		})

		It("should update the Work object status", func() {
			// Prepare the status information.
			workConds := []metav1.Condition{
				{
					Type:   fleetv1beta1.WorkConditionTypeApplied,
					Status: metav1.ConditionTrue,
					Reason: string(ManifestProcessingApplyResultTypeApplied),
				},
				{
					Type:   fleetv1beta1.WorkConditionTypeAvailable,
					Status: metav1.ConditionTrue,
					Reason: string(ManifestProcessingAvailabilityResultTypeAvailable),
				},
			}
			manifestConds := []fleetv1beta1.ManifestCondition{
				{
					Identifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:  0,
						Group:    "",
						Version:  "v1",
						Kind:     "Namespace",
						Resource: "namespaces",
						Name:     nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:   fleetv1beta1.WorkConditionTypeApplied,
							Status: metav1.ConditionTrue,
							Reason: string(ManifestProcessingApplyResultTypeApplied),
						},
						{
							Type:   fleetv1beta1.WorkConditionTypeAvailable,
							Status: metav1.ConditionTrue,
							Reason: string(ManifestProcessingAvailabilityResultTypeAvailable),
						},
					},
				},
			}

			workStatusUpdatedActual := workStatusUpdated(workName, workConds, manifestConds, nil, nil)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to update work status")
		})

		It("should update the AppliedWork object status", func() {
			// Prepare the status information.
			appliedResourceMeta := []fleetv1beta1.AppliedResourceMeta{
				{
					WorkResourceIdentifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:  0,
						Group:    "",
						Version:  "v1",
						Kind:     "Namespace",
						Resource: "namespaces",
						Name:     nsName,
					},
					UID: regularNS.UID,
				},
			}

			appliedWorkStatusUpdatedActual := appliedWorkStatusUpdated(workName, appliedResourceMeta)
			Eventually(appliedWorkStatusUpdatedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to update appliedWork status")
		})

		AfterAll(func() {
			// Delete the Work object and related resources.
			cleanupWorkObject(workName)

			// Ensure that the AppliedWork object has been removed.
			appliedWorkRemovedActual := appliedWorkRemovedActual(workName)
			Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to remove the AppliedWork object")

			// The environment prepared by the envtest package does not support namespace
			// deletion; consequently this test suite would not attempt so verify its deletion.
		})
	})
})

var _ = Describe("report diff", func() {
	// For simplicity reasons, this test case will only involve a NS object.
	Context("report diff only (new object)", Ordered, func() {
		workName := fmt.Sprintf(workNameTemplate, "c1")
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, "c1")

		var regularNS *corev1.Namespace

		BeforeAll(func() {
			// Prepare a NS object.
			regularNS = ns.DeepCopy()
			regularNS.Name = nsName
			regularNSJSON := marshalK8sObjJSON(regularNS)

			// Create a new Work object with all the manifest JSONs and proper apply strategy.
			applyStrategy := &fleetv1beta1.ApplyStrategy{
				Type: fleetv1beta1.ApplyStrategyTypeReportDiff,
			}
			createWorkObject(workName, applyStrategy, regularNSJSON)
		})

		It("should add cleanup finalizer to the Work object", func() {
			finalizerAddedActual := workFinalizerAddedActual(workName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to add cleanup finalizer to the Work object")
		})

		It("should prepare an AppliedWork object", func() {
			appliedWorkCreatedActual := appliedWorkCreatedActual(workName)
			Eventually(appliedWorkCreatedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to prepare an AppliedWork object")

			appliedWorkOwnerRef = prepareAppliedWorkOwnerRef(workName)
		})

		It("should not apply the manifests", func() {
			// Ensure that the NS object has not been applied.
			regularNSObjectNotAppliedActual := regularNSObjectNotAppliedActual(nsName)
			Eventually(regularNSObjectNotAppliedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to avoid applying the namespace object")
		})

		It("should update the Work object status", func() {
			// Prepare the status information.
			workConds := []metav1.Condition{
				{
					Type:   fleetv1beta1.WorkConditionTypeApplied,
					Status: metav1.ConditionFalse,
					Reason: notAllManifestsAppliedReason,
				},
				{
					Type:   fleetv1beta1.WorkConditionTypeAvailable,
					Status: metav1.ConditionFalse,
					Reason: notAllAppliedObjectsAvailableReason,
				},
			}
			manifestConds := []fleetv1beta1.ManifestCondition{
				{
					Identifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:  0,
						Group:    "",
						Version:  "v1",
						Kind:     "Namespace",
						Resource: "namespaces",
						Name:     nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:   fleetv1beta1.WorkConditionTypeApplied,
							Status: metav1.ConditionFalse,
							Reason: string(ManifestProcessingApplyResultTypeFailedToReportDiff),
						},
					},
				},
			}

			workStatusUpdatedActual := workStatusUpdated(workName, workConds, manifestConds, nil, nil)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to update work status")
		})

		It("should update the AppliedWork object status", func() {
			// Prepare the status information.
			appliedWorkStatusUpdatedActual := appliedWorkStatusUpdated(workName, nil)
			Eventually(appliedWorkStatusUpdatedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to update appliedWork status")
		})

		AfterAll(func() {
			// Delete the Work object and related resources.
			cleanupWorkObject(workName)

			// Ensure that the AppliedWork object has been removed.
			appliedWorkRemovedActual := appliedWorkRemovedActual(workName)
			Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to remove the AppliedWork object")

			// The environment prepared by the envtest package does not support namespace
			// deletion; consequently this test suite would not attempt so verify its deletion.
		})
	})

	Context("report diff only (with diff present, diff disappears later, partial comparison)", Ordered, func() {
		workName := fmt.Sprintf(workNameTemplate, "c2")
		// The environment prepared by the envtest package does not support namespace
		// deletion; each test case would use a new namespace.
		nsName := fmt.Sprintf(nsNameTemplate, "c2")
		deployName := fmt.Sprintf(deployNameTemplate, "c2")

		var appliedWorkOwnerRef *metav1.OwnerReference
		var regularNS *corev1.Namespace
		var regularDeploy *appsv1.Deployment

		BeforeAll(func() {
			// Prepare a NS object.
			regularNS = ns.DeepCopy()
			regularNS.Name = nsName
			regularNSJSON := marshalK8sObjJSON(regularNS)

			// Prepare a Deployment object.
			regularDeploy = deploy.DeepCopy()
			regularDeploy.Namespace = nsName
			regularDeploy.Name = deployName
			regularDeployJSON := marshalK8sObjJSON(regularDeploy)

			// Create the objects first in the member cluster.
			Expect(memberClient.Create(ctx, regularNS)).To(Succeed(), "Failed to create the NS object")

			regularDeploy.Spec.Replicas = ptr.To(int32(2))
			Expect(memberClient.Create(ctx, regularDeploy)).To(Succeed(), "Failed to create the Deployment object")

			// Create a new Work object with all the manifest JSONs and proper apply strategy.
			applyStrategy := &fleetv1beta1.ApplyStrategy{
				ComparisonOption: fleetv1beta1.ComparisonOptionTypePartialComparison,
				Type:             fleetv1beta1.ApplyStrategyTypeReportDiff,
			}
			createWorkObject(workName, applyStrategy, regularNSJSON, regularDeployJSON)
		})

		It("should add cleanup finalizer to the Work object", func() {
			finalizerAddedActual := workFinalizerAddedActual(workName)
			Eventually(finalizerAddedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to add cleanup finalizer to the Work object")
		})

		It("should prepare an AppliedWork object", func() {
			appliedWorkCreatedActual := appliedWorkCreatedActual(workName)
			Eventually(appliedWorkCreatedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to prepare an AppliedWork object")

			appliedWorkOwnerRef = prepareAppliedWorkOwnerRef(workName)
		})

		It("should own the objects, but not apply any manifests", func() {
			// Verify that the Deployment manifest has not been applied, yet Fleet has assumed
			// its ownership.
			wantDeploy := deploy.DeepCopy()
			wantDeploy.TypeMeta = metav1.TypeMeta{}
			wantDeploy.Namespace = nsName
			wantDeploy.Name = deployName
			wantDeploy.OwnerReferences = []metav1.OwnerReference{
				*appliedWorkOwnerRef,
			}
			wantDeploy.Spec.Replicas = ptr.To(int32(2))

			deployOwnedButNotApplied := func() error {
				if err := memberClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: deployName}, regularDeploy); err != nil {
					return fmt.Errorf("failed to retrieve the Deployment object: %w", err)
				}

				if len(regularDeploy.Spec.Template.Spec.Containers) != 1 {
					return fmt.Errorf("number of containers in the Deployment object, got %d, want %d", len(regularDeploy.Spec.Template.Spec.Containers), 1)
				}
				if len(regularDeploy.Spec.Template.Spec.Containers[0].Ports) != 1 {
					return fmt.Errorf("number of ports in the first container, got %d, want %d", len(regularDeploy.Spec.Template.Spec.Containers[0].Ports), 1)
				}

				// To ignore default values automatically, here the test suite rebuilds the objects.
				rebuiltGotDeploy := &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Namespace:       regularDeploy.Namespace,
						Name:            regularDeploy.Name,
						OwnerReferences: regularDeploy.OwnerReferences,
					},
					Spec: appsv1.DeploymentSpec{
						Replicas: regularDeploy.Spec.Replicas,
						Selector: regularDeploy.Spec.Selector,
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: regularDeploy.Spec.Template.ObjectMeta.Labels,
							},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  regularDeploy.Spec.Template.Spec.Containers[0].Name,
										Image: regularDeploy.Spec.Template.Spec.Containers[0].Image,
										Ports: []corev1.ContainerPort{
											{
												ContainerPort: regularDeploy.Spec.Template.Spec.Containers[0].Ports[0].ContainerPort,
											},
										},
									},
								},
							},
						},
					},
				}

				if diff := cmp.Diff(rebuiltGotDeploy, wantDeploy); diff != "" {
					return fmt.Errorf("deployment diff (-got +want):\n%s", diff)
				}
				return nil
			}

			Eventually(deployOwnedButNotApplied, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to own the Deployment object without applying the manifest")
			Consistently(deployOwnedButNotApplied, consistentlyDuration, consistentlyInternal).Should(Succeed(), "Failed to own the Deployment object without applying the manifest")

			// Verify that Fleet has assumed ownership of the NS object.
			wantNS := ns.DeepCopy()
			wantNS.TypeMeta = metav1.TypeMeta{}
			wantNS.Name = nsName
			wantNS.OwnerReferences = []metav1.OwnerReference{
				*appliedWorkOwnerRef,
			}

			nsOwnedButNotApplied := func() error {
				if err := memberClient.Get(ctx, client.ObjectKey{Name: nsName}, regularNS); err != nil {
					return fmt.Errorf("failed to retrieve the NS object: %w", err)
				}

				// To ignore default values automatically, here the test suite rebuilds the objects.
				rebuiltGotNS := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:            regularNS.Name,
						OwnerReferences: regularNS.OwnerReferences,
					},
				}

				if diff := cmp.Diff(rebuiltGotNS, wantNS); diff != "" {
					return fmt.Errorf("namespace diff (-got +want):\n%s", diff)
				}
				return nil
			}
			Eventually(nsOwnedButNotApplied, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to own the NS object without applying the manifest")
			Consistently(nsOwnedButNotApplied, consistentlyDuration, consistentlyInternal).Should(Succeed(), "Failed to own the NS object without applying the manifest")
		})

		It("should update the Work object status", func() {
			noLaterThanTimestamp := metav1.Now()

			// Prepare the status information.
			workConds := []metav1.Condition{
				{
					Type:   fleetv1beta1.WorkConditionTypeApplied,
					Status: metav1.ConditionFalse,
					Reason: notAllManifestsAppliedReason,
				},
				{
					Type:   fleetv1beta1.WorkConditionTypeAvailable,
					Status: metav1.ConditionFalse,
					Reason: notAllAppliedObjectsAvailableReason,
				},
			}
			manifestConds := []fleetv1beta1.ManifestCondition{
				{
					Identifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:  0,
						Group:    "",
						Version:  "v1",
						Kind:     "Namespace",
						Resource: "namespaces",
						Name:     nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:   fleetv1beta1.WorkConditionTypeApplied,
							Status: metav1.ConditionTrue,
							Reason: string(ManifestProcessingApplyResultTypeNoDiffFound),
						},
						{
							Type:   fleetv1beta1.WorkConditionTypeAvailable,
							Status: metav1.ConditionTrue,
							Reason: string(ManifestProcessingAvailabilityResultTypeAvailable),
						},
					},
				},
				{
					Identifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:   1,
						Group:     "apps",
						Version:   "v1",
						Kind:      "Deployment",
						Resource:  "deployments",
						Name:      deployName,
						Namespace: nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:   fleetv1beta1.WorkConditionTypeApplied,
							Status: metav1.ConditionFalse,
							Reason: string(ManifestProcessingApplyResultTypeFoundDiffs),
						},
					},
					DiffDetails: &fleetv1beta1.DiffDetails{
						ObservedInMemberClusterGeneration: regularDeploy.Generation,
						ObservedDiffs: []fleetv1beta1.PatchDetail{
							{
								Path:          "/spec/replicas",
								ValueInMember: "2",
								ValueInHub:    "1",
							},
						},
					},
				},
			}

			workStatusUpdatedActual := workStatusUpdated(workName, workConds, manifestConds, &noLaterThanTimestamp, &noLaterThanTimestamp)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to update work status")
		})

		It("should update the AppliedWork object status", func() {
			// Prepare the status information.
			appliedResourceMeta := []fleetv1beta1.AppliedResourceMeta{
				{
					WorkResourceIdentifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:  0,
						Group:    "",
						Version:  "v1",
						Kind:     "Namespace",
						Resource: "namespaces",
						Name:     nsName,
					},
					UID: regularNS.UID,
				},
			}

			appliedWorkStatusUpdatedActual := appliedWorkStatusUpdated(workName, appliedResourceMeta)
			Eventually(appliedWorkStatusUpdatedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to update appliedWork status")
		})

		It("can make changes to the objects", func() {
			// Use Eventually blocks to avoid conflicts.
			Eventually(func() error {
				// Retrieve the Deployment object.
				updatedDeploy := &appsv1.Deployment{}
				if err := memberClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: deployName}, updatedDeploy); err != nil {
					return fmt.Errorf("failed to retrieve the Deployment object: %w", err)
				}

				// Make changes to the Deployment object.
				updatedDeploy.Spec.Replicas = ptr.To(int32(1))

				// Update the Deployment object.
				if err := memberClient.Update(ctx, updatedDeploy); err != nil {
					return fmt.Errorf("failed to update the Deployment object: %w", err)
				}
				return nil
			}, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to update the Deployment object")
		})

		It("can mark the deployment as available", func() {
			markDeploymentAsAvailable(nsName, deployName)
		})

		It("should update the Work object status", func() {
			// Shift the timestamp to account for drift/diff detection delays.
			noLaterThanTimestamp := metav1.Time{
				Time: time.Now().Add(time.Second * 30),
			}

			// Prepare the status information.
			workConds := []metav1.Condition{
				{
					Type:   fleetv1beta1.WorkConditionTypeApplied,
					Status: metav1.ConditionTrue,
					Reason: string(ManifestProcessingApplyResultTypeApplied),
				},
				{
					Type:   fleetv1beta1.WorkConditionTypeAvailable,
					Status: metav1.ConditionTrue,
					Reason: string(ManifestProcessingAvailabilityResultTypeAvailable),
				},
			}
			manifestConds := []fleetv1beta1.ManifestCondition{
				{
					Identifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:  0,
						Group:    "",
						Version:  "v1",
						Kind:     "Namespace",
						Resource: "namespaces",
						Name:     nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:   fleetv1beta1.WorkConditionTypeApplied,
							Status: metav1.ConditionTrue,
							Reason: string(ManifestProcessingApplyResultTypeNoDiffFound),
						},
						{
							Type:   fleetv1beta1.WorkConditionTypeAvailable,
							Status: metav1.ConditionTrue,
							Reason: string(ManifestProcessingAvailabilityResultTypeAvailable),
						},
					},
				},
				{
					Identifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:   1,
						Group:     "apps",
						Version:   "v1",
						Kind:      "Deployment",
						Resource:  "deployments",
						Name:      deployName,
						Namespace: nsName,
					},
					Conditions: []metav1.Condition{
						{
							Type:   fleetv1beta1.WorkConditionTypeApplied,
							Status: metav1.ConditionTrue,
							Reason: string(ManifestProcessingApplyResultTypeNoDiffFound),
						},
						{
							Type:   fleetv1beta1.WorkConditionTypeAvailable,
							Status: metav1.ConditionTrue,
							Reason: string(ManifestProcessingAvailabilityResultTypeAvailable),
						},
					},
				},
			}

			workStatusUpdatedActual := workStatusUpdated(workName, workConds, manifestConds, &noLaterThanTimestamp, &noLaterThanTimestamp)
			Eventually(workStatusUpdatedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to update work status")
		})

		It("should update the AppliedWork object status", func() {
			// Prepare the status information.
			appliedResourceMeta := []fleetv1beta1.AppliedResourceMeta{
				{
					WorkResourceIdentifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:  0,
						Group:    "",
						Version:  "v1",
						Kind:     "Namespace",
						Resource: "namespaces",
						Name:     nsName,
					},
					UID: regularNS.UID,
				},
				{
					WorkResourceIdentifier: fleetv1beta1.WorkResourceIdentifier{
						Ordinal:   1,
						Group:     "apps",
						Version:   "v1",
						Kind:      "Deployment",
						Resource:  "deployments",
						Name:      deployName,
						Namespace: nsName,
					},
					UID: regularDeploy.UID,
				},
			}

			appliedWorkStatusUpdatedActual := appliedWorkStatusUpdated(workName, appliedResourceMeta)
			Eventually(appliedWorkStatusUpdatedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to update appliedWork status")
		})

		AfterAll(func() {
			// Delete the Work object and related resources.
			cleanupWorkObject(workName)

			// Ensure that the AppliedWork object has been removed.
			appliedWorkRemovedActual := appliedWorkRemovedActual(workName)
			Eventually(appliedWorkRemovedActual, eventuallyDuration, eventuallyInternal).Should(Succeed(), "Failed to remove the AppliedWork object")

			// Ensure that the Deployment object has been left alone.
			regularDeployNotRemovedActual := regularDeployNotRemovedActual(nsName, deployName)
			Consistently(regularDeployNotRemovedActual, consistentlyDuration, consistentlyInternal).Should(Succeed(), "Failed to remove the deployment object")

			// The environment prepared by the envtest package does not support namespace
			// deletion; consequently this test suite would not attempt so verify its deletion.
		})
	})
})
