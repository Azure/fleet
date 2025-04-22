package workgenerator

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

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	appsv1 "k8s.io/api/apps/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/controllers/workapplier"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/condition"
	"go.goms.io/fleet/pkg/utils/controller"
	"go.goms.io/fleet/test/utils/informer"
)

var statusCmpOptions = []cmp.Option{
	// ignore the message as we may change the message in the future
	cmpopts.IgnoreFields(metav1.Condition{}, "Message"),
	cmp.Comparer(func(t1, t2 metav1.Time) bool {
		// we're within the margin (1s) if x + margin >= y
		return !t1.Time.Add(1 * time.Second).Before(t2.Time)
	}),
}

func TestGetWorkNamePrefixFromSnapshotName(t *testing.T) {
	tests := map[string]struct {
		resourceSnapshot *fleetv1beta1.ClusterResourceSnapshot
		wantErr          error
		wantedName       string
	}{
		"the work name is crp name + \"work\", if there is only one resource snapshot": {
			resourceSnapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: "placement-2",
					Labels: map[string]string{
						fleetv1beta1.CRPTrackingLabel: "placement",
					},
				},
			},
			wantErr:    nil,
			wantedName: "placement-work",
		},
		"should return error if the resource snapshot has negative subindex": {
			resourceSnapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: "placement-1-2",
					Labels: map[string]string{
						fleetv1beta1.CRPTrackingLabel: "placement",
					},
					Annotations: map[string]string{
						fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "-1",
					},
				},
			},
			wantErr:    controller.ErrUnexpectedBehavior,
			wantedName: "",
		},
		"the work name is the concatenation of the crp name and subindex start at 0": {
			resourceSnapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: "placement-1-2",
					Labels: map[string]string{
						fleetv1beta1.CRPTrackingLabel: "placement",
					},
					Annotations: map[string]string{
						fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "0",
					},
				},
			},
			wantErr:    nil,
			wantedName: "placement-0",
		},
		"the work name is the concatenation of the crp name and subindex": {
			resourceSnapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: "placement-1-2",
					Labels: map[string]string{
						fleetv1beta1.CRPTrackingLabel: "placement",
					},
					Annotations: map[string]string{
						fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "2",
					},
				},
			},
			wantErr:    nil,
			wantedName: "placement-2",
		},
		"test return error if the resource snapshot has invalid subindex": {
			resourceSnapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: "placement-1-2",
					Labels: map[string]string{
						fleetv1beta1.CRPTrackingLabel: "placement",
					},
					Annotations: map[string]string{
						fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "what?",
					},
				},
			},
			wantErr:    controller.ErrUnexpectedBehavior,
			wantedName: "",
		},
		"test return error if the resource snapshot does not have CRP track": {
			resourceSnapshot: &fleetv1beta1.ClusterResourceSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name: "placement-1-2",
					Annotations: map[string]string{
						fleetv1beta1.SubindexOfResourceSnapshotAnnotation: "what?",
					},
				},
			},
			wantErr:    controller.ErrUnexpectedBehavior,
			wantedName: "",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			workName, err := getWorkNamePrefixFromSnapshotName(tt.resourceSnapshot)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("failed getWorkNamePrefixFromSnapshotName test `%s` error = %v, wantErr %v", name, err, tt.wantErr)
				return
			}
			if workName != tt.wantedName {
				t.Errorf("getWorkNamePrefixFromSnapshotName test `%s` workName = `%v`, wantedName `%v`", name, workName, tt.wantedName)
			}
		})
	}
}

func TestExtractResFromConfigMap(t *testing.T) {
	tests := map[string]struct {
		uConfigMap *unstructured.Unstructured
		want       []fleetv1beta1.Manifest
		wantErr    bool
	}{
		"valid config map with no entries is fine": {
			uConfigMap: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
				},
			},
			want:    []fleetv1beta1.Manifest{},
			wantErr: false,
		},
		"config map with invalid JSON content should fail": {
			uConfigMap: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata": map[string]interface{}{
						"name":      "test-config",
						"namespace": "default",
					},
					"data": map[string]interface{}{
						"invalid": "{invalid-json}",
					},
				},
			},
			want:    nil,
			wantErr: true,
		},
		"config map with namespaced resource in different namespace should fail": {
			uConfigMap: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata": map[string]interface{}{
						"name":      "test-config",
						"namespace": "default",
					},
					"data": map[string]interface{}{
						"resource": `{"apiVersion": "v1", "kind": "Pod", "metadata": {"name": "test-pod", "namespace": "other-namespace"}}`,
					},
				},
			},
			want:    nil,
			wantErr: true,
		},
		"config map with valid and invalid entries should fail": {
			uConfigMap: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata": map[string]interface{}{
						"name":      "test-config",
						"namespace": "default",
					},
					"data": map[string]interface{}{
						"valid":   `{"apiVersion": "v1", "kind": "Pod", "metadata": {"name": "test-pod", "namespace": "default"}}`,
						"invalid": "{invalid-json}",
					},
				},
			},
			want:    nil,
			wantErr: true,
		},
		"config map with cluster and namespace scoped data in the correct namespace should pass": {
			uConfigMap: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata": map[string]interface{}{
						"name":      "test-config",
						"namespace": "default",
					},
					"data": map[string]interface{}{
						"resource":  `{"apiVersion": "v1", "kind": "Pod", "metadata": {"name": "test-pod", "namespace": "default"}}`,
						"resource2": `{"apiVersion": "v1", "kind": "ClusterRole", "metadata": {"name": "test-role"}}`,
					},
				},
			},
			want: []fleetv1beta1.Manifest{
				{RawExtension: runtime.RawExtension{Raw: []byte(`{"apiVersion": "v1", "kind": "Pod", "metadata": {"name": "test-pod", "namespace": "default"}}`)}},
				{RawExtension: runtime.RawExtension{Raw: []byte(`{"apiVersion": "v1", "kind": "ClusterRole", "metadata": {"name": "test-role"}}`)}},
			},
			wantErr: false,
		},
		"config map with cluster scoped and cross namespaced resources data in a different namespace should fail": {
			uConfigMap: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata": map[string]interface{}{
						"name":      "test-config",
						"namespace": "default",
					},
					"data": map[string]interface{}{
						"resource":  `{"apiVersion": "v1", "kind": "Pod", "metadata": {"name": "test-pod", "namespace": "not-default"}}`,
						"resource2": `{"apiVersion": "v1", "kind": "ClusterRole", "metadata": {"name": "test-role"}}`,
					},
				},
			},
			want:    nil,
			wantErr: true,
		},
		"config map with valid entries in different order should be sorted to order": {
			uConfigMap: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata": map[string]interface{}{
						"name":      "test-config",
						"namespace": "default",
					},
					"data": map[string]interface{}{
						"resource2": `{"apiVersion": "v1", "kind": "Pod", "metadata": {"name": "test-pod1", "namespace": "default"}}`,
						"resource1": `{"apiVersion": "v1", "kind": "Pod", "metadata": {"name": "test-pod2", "namespace": "default"}}`,
					},
				},
			},
			want: []fleetv1beta1.Manifest{
				{RawExtension: runtime.RawExtension{Raw: []byte(`{"apiVersion": "v1", "kind": "Pod", "metadata": {"name": "test-pod2", "namespace": "default"}}`)}},
				{RawExtension: runtime.RawExtension{Raw: []byte(`{"apiVersion": "v1", "kind": "Pod", "metadata": {"name": "test-pod1", "namespace": "default"}}`)}},
			},
			wantErr: false,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := extractResFromConfigMap(tt.uConfigMap)
			if (err != nil) != tt.wantErr {
				t.Fatalf("extractResFromConfigMap() error = %v, wantErr %v", err, tt.wantErr)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("extractResFromConfigMap() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestUpsertWork(t *testing.T) {
	workName := "work"
	namespace := "default"

	var cmpOptions = []cmp.Option{
		// ignore the message as we may change the message in the future
		cmpopts.IgnoreFields(fleetv1beta1.Work{}, "Status"),
		cmpopts.IgnoreFields(metav1.ObjectMeta{}, "CreationTimestamp"),
		cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion"),
		cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ManagedFields"),
		cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime"),
		cmpopts.IgnoreFields(fleetv1beta1.WorkloadTemplate{}, "Manifests"),
	}

	testDeployment := appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       utils.DeploymentKind,
			APIVersion: utils.DeploymentGVK.GroupVersion().String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "testDeployment",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas:        ptr.To(int32(2)),
			MinReadySeconds: 5,
		},
	}
	newWork := &fleetv1beta1.Work{
		ObjectMeta: metav1.ObjectMeta{
			Name:      workName,
			Namespace: namespace,
			Labels: map[string]string{
				fleetv1beta1.ParentResourceSnapshotIndexLabel: "1",
			},
			Annotations: map[string]string{
				fleetv1beta1.ParentResourceSnapshotNameAnnotation:                "snapshot-1",
				fleetv1beta1.ParentClusterResourceOverrideSnapshotHashAnnotation: "hash1",
				fleetv1beta1.ParentResourceOverrideSnapshotHashAnnotation:        "hash2",
			},
		},
		Spec: fleetv1beta1.WorkSpec{
			Workload: fleetv1beta1.WorkloadTemplate{
				Manifests: []fleetv1beta1.Manifest{{RawExtension: runtime.RawExtension{Object: &testDeployment}}},
			},
		},
	}

	resourceSnapshot := &fleetv1beta1.ClusterResourceSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name: "snapshot-1",
			Labels: map[string]string{
				fleetv1beta1.ResourceIndexLabel: "1",
			},
		},
	}

	tests := []struct {
		name          string
		existingWork  *fleetv1beta1.Work
		expectChanged bool
	}{
		{
			name:          "Create new work when existing work is nil",
			existingWork:  nil,
			expectChanged: true,
		},
		{
			name: "Update existing work with new annotations",
			existingWork: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name:      workName,
					Namespace: namespace,
					Labels: map[string]string{
						fleetv1beta1.ParentResourceSnapshotIndexLabel: "1",
					},
				},
				Spec: fleetv1beta1.WorkSpec{
					Workload: fleetv1beta1.WorkloadTemplate{
						Manifests: []fleetv1beta1.Manifest{{RawExtension: runtime.RawExtension{Raw: []byte("{}")}}},
					},
				},
			},
			expectChanged: true,
		},
		{
			name: "Update existing work even if it does not have the resource snapshot label",
			existingWork: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name:      workName,
					Namespace: namespace,
				},
				Spec: fleetv1beta1.WorkSpec{
					Workload: fleetv1beta1.WorkloadTemplate{
						Manifests: []fleetv1beta1.Manifest{{RawExtension: runtime.RawExtension{Raw: []byte("{}")}}},
					},
				},
			},
			expectChanged: true,
		},
		{
			name: "Update existing work if it misses annotations even if the resource snapshot label is correct",
			existingWork: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name:      workName,
					Namespace: namespace,
					Labels: map[string]string{
						fleetv1beta1.ParentResourceSnapshotIndexLabel: "1",
					},
				},
				Spec: fleetv1beta1.WorkSpec{
					Workload: fleetv1beta1.WorkloadTemplate{
						Manifests: []fleetv1beta1.Manifest{{RawExtension: runtime.RawExtension{Raw: []byte("{}")}}},
					},
				},
			},
			expectChanged: true,
		},
		{
			name: "Update existing work if it does not have correct override snapshot hash",
			existingWork: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name:      workName,
					Namespace: namespace,
					Labels: map[string]string{
						fleetv1beta1.ParentResourceSnapshotIndexLabel: "1",
					},
					Annotations: map[string]string{
						fleetv1beta1.ParentResourceSnapshotNameAnnotation:                "snapshot-1",
						fleetv1beta1.ParentClusterResourceOverrideSnapshotHashAnnotation: "wrong-hash"},
				},
				Spec: fleetv1beta1.WorkSpec{
					Workload: fleetv1beta1.WorkloadTemplate{
						Manifests: []fleetv1beta1.Manifest{{RawExtension: runtime.RawExtension{Raw: []byte("{}")}}},
					},
				},
			},
			expectChanged: true,
		},
		{
			name: "Do not update the existing work if it already points to the same resource and override snapshots",
			existingWork: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name:      workName,
					Namespace: namespace,
					Labels: map[string]string{
						fleetv1beta1.ParentResourceSnapshotIndexLabel: "1",
					},
					Annotations: map[string]string{
						fleetv1beta1.ParentResourceSnapshotNameAnnotation:                "snapshot-1",
						fleetv1beta1.ParentClusterResourceOverrideSnapshotHashAnnotation: "hash1",
						fleetv1beta1.ParentResourceOverrideSnapshotHashAnnotation:        "hash2",
					},
				},
				Spec: fleetv1beta1.WorkSpec{
					Workload: fleetv1beta1.WorkloadTemplate{
						Manifests: []fleetv1beta1.Manifest{{RawExtension: runtime.RawExtension{Raw: []byte("{}")}}},
					},
				},
			},
			expectChanged: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := serviceScheme(t)
			objects := []client.Object{resourceSnapshot}
			if tt.existingWork != nil {
				objects = append(objects, tt.existingWork)
			}
			fakeClient := fake.NewClientBuilder().
				WithStatusSubresource(objects...).
				WithScheme(scheme).
				WithObjects(objects...).
				Build()
			// Create reconciler with custom client
			reconciler := &Reconciler{
				Client:          fakeClient,
				recorder:        record.NewFakeRecorder(10),
				InformerManager: &informer.FakeManager{},
			}
			changed, _ := reconciler.upsertWork(ctx, newWork, tt.existingWork, resourceSnapshot)
			if changed != tt.expectChanged {
				t.Fatalf("expected changed: %v, got: %v", tt.expectChanged, changed)
			}
			upsertedWork := &fleetv1beta1.Work{}
			if fakeClient.Get(ctx, client.ObjectKeyFromObject(newWork), upsertedWork) != nil {
				t.Fatalf("failed to get upserted work")
			}
			if diff := cmp.Diff(newWork, upsertedWork, cmpOptions...); diff != "" {
				t.Errorf("upsertWork didn't update the work, mismatch (-want +got):\n%s", diff)
			}
			if tt.expectChanged {
				// check if the deployment is applied
				var u unstructured.Unstructured
				if err := u.UnmarshalJSON(upsertedWork.Spec.Workload.Manifests[0].Raw); err != nil {
					t.Fatalf("Failed to unmarshal the result: %v, want nil", err)
				}
				var deployment appsv1.Deployment
				if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &deployment); err != nil {
					t.Fatalf("Failed to convert the result to deployment: %v, want nil", err)
				}
				if diff := cmp.Diff(testDeployment, deployment); diff != "" {
					t.Errorf("The new Deployment mismatch (-want, +got):\n%s", diff)
				}
			}
		})
	}
}

func TestSetAllWorkAppliedCondition(t *testing.T) {
	tests := map[string]struct {
		works                            map[string]*fleetv1beta1.Work
		generation                       int64
		wantAppliedCond                  metav1.Condition
		wantWorkAppliedCondSummaryStatus workConditionSummarizedStatus
	}{
		"all works are applied successfully": {
			works: map[string]*fleetv1beta1.Work{
				"appliedWork1": {
					ObjectMeta: metav1.ObjectMeta{
						Name:       "work1",
						Generation: 123,
					},
					Status: fleetv1beta1.WorkStatus{
						Conditions: []metav1.Condition{
							{
								Type:               fleetv1beta1.WorkConditionTypeApplied,
								Status:             metav1.ConditionTrue,
								ObservedGeneration: 123,
							},
						},
					},
				},
				"appliedWork2": {
					ObjectMeta: metav1.ObjectMeta{
						Name:       "work2",
						Generation: 12,
					},
					Status: fleetv1beta1.WorkStatus{
						Conditions: []metav1.Condition{
							{
								Type:               fleetv1beta1.WorkConditionTypeApplied,
								Status:             metav1.ConditionTrue,
								ObservedGeneration: 12,
							},
						},
					},
				},
			},
			generation: 1,
			wantAppliedCond: metav1.Condition{
				Status:             metav1.ConditionTrue,
				Type:               string(fleetv1beta1.ResourceBindingApplied),
				Reason:             condition.AllWorkAppliedReason,
				ObservedGeneration: 1,
			},
			wantWorkAppliedCondSummaryStatus: workConditionSummarizedStatusTrue,
		},
		"one work has stale applied condition": {
			works: map[string]*fleetv1beta1.Work{
				"notAppliedWork1": {
					ObjectMeta: metav1.ObjectMeta{
						Name:       "work1",
						Generation: 123,
					},
					Status: fleetv1beta1.WorkStatus{
						Conditions: []metav1.Condition{
							{
								Type:               fleetv1beta1.WorkConditionTypeApplied,
								Status:             metav1.ConditionTrue,
								ObservedGeneration: 122, // not the latest generation
							},
						},
					},
				},
				"appliedWork2": {
					ObjectMeta: metav1.ObjectMeta{
						Name:       "work2",
						Generation: 12,
					},
					Status: fleetv1beta1.WorkStatus{
						Conditions: []metav1.Condition{
							{
								Type:               fleetv1beta1.WorkConditionTypeApplied,
								Status:             metav1.ConditionTrue,
								ObservedGeneration: 11,
							},
						},
					},
				},
			},
			generation: 1,
			wantAppliedCond: metav1.Condition{
				Status:             metav1.ConditionFalse,
				Type:               string(fleetv1beta1.ResourceBindingApplied),
				Reason:             condition.WorkNotAppliedReason,
				ObservedGeneration: 1,
			},
			wantWorkAppliedCondSummaryStatus: workConditionSummarizedStatusIncomplete,
		},
		"one work has apply op failure": {
			works: map[string]*fleetv1beta1.Work{
				"appliedWork1": {
					ObjectMeta: metav1.ObjectMeta{
						Name:       "work1",
						Generation: 123,
					},
					Status: fleetv1beta1.WorkStatus{
						Conditions: []metav1.Condition{
							{
								Type:               fleetv1beta1.WorkConditionTypeApplied,
								Status:             metav1.ConditionTrue,
								ObservedGeneration: 123,
							},
						},
					},
				},
				"notAppliedWork2": {
					ObjectMeta: metav1.ObjectMeta{
						Name:       "work2",
						Generation: 123,
					},
					Status: fleetv1beta1.WorkStatus{
						Conditions: []metav1.Condition{
							{
								Type:               fleetv1beta1.WorkConditionTypeApplied,
								Status:             metav1.ConditionFalse,
								ObservedGeneration: 123,
							},
						},
					},
				},
			},
			generation: 1,
			wantAppliedCond: metav1.Condition{
				Status:             metav1.ConditionFalse,
				Type:               string(fleetv1beta1.ResourceBindingApplied),
				Reason:             condition.WorkNotAppliedReason,
				ObservedGeneration: 1,
			},
			wantWorkAppliedCondSummaryStatus: workConditionSummarizedStatusFalse,
		},
		"one work has not been applied yet": {
			works: map[string]*fleetv1beta1.Work{
				"appliedWork1": {
					ObjectMeta: metav1.ObjectMeta{
						Name:       "work1",
						Generation: 123,
					},
					Status: fleetv1beta1.WorkStatus{
						Conditions: []metav1.Condition{
							{
								Type:               fleetv1beta1.WorkConditionTypeApplied,
								Status:             metav1.ConditionUnknown,
								ObservedGeneration: 123,
							},
						},
					},
				},
				"notAppliedWork2": {
					ObjectMeta: metav1.ObjectMeta{
						Name:       "work2",
						Generation: 123,
					},
					Status: fleetv1beta1.WorkStatus{
						Conditions: []metav1.Condition{},
					},
				},
			},
			generation: 1,
			wantAppliedCond: metav1.Condition{
				Status:             metav1.ConditionFalse,
				Type:               string(fleetv1beta1.ResourceBindingApplied),
				Reason:             condition.WorkNotAppliedReason,
				ObservedGeneration: 1,
			},
			wantWorkAppliedCondSummaryStatus: workConditionSummarizedStatusIncomplete,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			binding := &fleetv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test",
					Generation: tt.generation,
				},
			}
			workAppliedCondSummaryStatus := setAllWorkAppliedCondition(tt.works, binding)
			if workAppliedCondSummaryStatus != tt.wantWorkAppliedCondSummaryStatus {
				t.Errorf("setAllWorkAppliedCondition() = %v, want %v", workAppliedCondSummaryStatus, tt.wantWorkAppliedCondSummaryStatus)
			}

			appliedCond := meta.FindStatusCondition(binding.Status.Conditions, string(fleetv1beta1.ResourceBindingApplied))
			if diff := cmp.Diff(appliedCond, &tt.wantAppliedCond, cmpConditionOption); diff != "" {
				t.Errorf("buildAllWorkAppliedCondition test `%s` mismatch (-got +want):\n%s", name, diff)
			}
		})
	}
}

func TestSetAllWorkDiffReportedCondition(t *testing.T) {
	bindingTemplate := &fleetv1beta1.ClusterResourceBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "binding",
			Generation: 1,
		},
	}
	testCases := []struct {
		name                                  string
		works                                 map[string]*fleetv1beta1.Work
		wantDiffReportedCondition             *metav1.Condition
		wantWorkDiffReportedCondSummaryStatus workConditionSummarizedStatus
	}{
		{
			name: "all works have diff reported",
			works: map[string]*fleetv1beta1.Work{
				"work-1": {
					ObjectMeta: metav1.ObjectMeta{
						Name:       "work-1",
						Generation: 1,
					},
					Status: fleetv1beta1.WorkStatus{
						Conditions: []metav1.Condition{
							{
								Type:               fleetv1beta1.WorkConditionTypeDiffReported,
								Status:             metav1.ConditionTrue,
								ObservedGeneration: 1,
							},
						},
					},
				},
				"work-2": {
					ObjectMeta: metav1.ObjectMeta{
						Name:       "work-2",
						Generation: 1,
					},
					Status: fleetv1beta1.WorkStatus{
						Conditions: []metav1.Condition{
							{
								Type:               fleetv1beta1.WorkConditionTypeDiffReported,
								Status:             metav1.ConditionTrue,
								ObservedGeneration: 1,
							},
						},
					},
				},
			},
			wantDiffReportedCondition: &metav1.Condition{
				Status:             metav1.ConditionTrue,
				Type:               string(fleetv1beta1.ResourceBindingDiffReported),
				Reason:             condition.AllWorkDiffReportedReason,
				ObservedGeneration: 1,
			},
			wantWorkDiffReportedCondSummaryStatus: workConditionSummarizedStatusTrue,
		},
		{
			name: "one of two works have diff reported, the other has not reported yet",
			works: map[string]*fleetv1beta1.Work{
				"work-1": {
					ObjectMeta: metav1.ObjectMeta{
						Name:       "work-1",
						Generation: 1,
					},
					Status: fleetv1beta1.WorkStatus{
						Conditions: []metav1.Condition{
							{
								Type:               fleetv1beta1.WorkConditionTypeDiffReported,
								Status:             metav1.ConditionTrue,
								ObservedGeneration: 1,
							},
						},
					},
				},
				"work-2": {
					ObjectMeta: metav1.ObjectMeta{
						Name:       "work-2",
						Generation: 1,
					},
					Status: fleetv1beta1.WorkStatus{
						Conditions: []metav1.Condition{},
					},
				},
			},
			wantDiffReportedCondition: &metav1.Condition{
				Status:             metav1.ConditionFalse,
				Type:               string(fleetv1beta1.ResourceBindingDiffReported),
				Reason:             condition.WorkNotDiffReportedReason,
				ObservedGeneration: 1,
			},
			wantWorkDiffReportedCondSummaryStatus: workConditionSummarizedStatusIncomplete,
		},
		{
			name: "one of two works have diff reported, the other has failed to report diff",
			works: map[string]*fleetv1beta1.Work{
				"work-1": {
					ObjectMeta: metav1.ObjectMeta{
						Name:       "work-1",
						Generation: 1,
					},
					Status: fleetv1beta1.WorkStatus{
						Conditions: []metav1.Condition{
							{
								Type:               fleetv1beta1.WorkConditionTypeDiffReported,
								Status:             metav1.ConditionTrue,
								ObservedGeneration: 1,
							},
						},
					},
				},
				"work-2": {
					ObjectMeta: metav1.ObjectMeta{
						Name:       "work-2",
						Generation: 1,
					},
					Status: fleetv1beta1.WorkStatus{
						Conditions: []metav1.Condition{
							{
								Type:               fleetv1beta1.WorkConditionTypeDiffReported,
								Status:             metav1.ConditionFalse,
								ObservedGeneration: 1,
							},
						},
					},
				},
			},
			wantDiffReportedCondition: &metav1.Condition{
				Status:             metav1.ConditionFalse,
				Type:               string(fleetv1beta1.ResourceBindingDiffReported),
				Reason:             condition.WorkNotDiffReportedReason,
				ObservedGeneration: 1,
			},
			wantWorkDiffReportedCondSummaryStatus: workConditionSummarizedStatusFalse,
		},
		{
			name: "one of two works have diff reported, the other has stale diff information",
			works: map[string]*fleetv1beta1.Work{
				"work-1": {
					ObjectMeta: metav1.ObjectMeta{
						Name:       "work-1",
						Generation: 1,
					},
					Status: fleetv1beta1.WorkStatus{
						Conditions: []metav1.Condition{
							{
								Type:               fleetv1beta1.WorkConditionTypeDiffReported,
								Status:             metav1.ConditionTrue,
								ObservedGeneration: 1,
							},
						},
					},
				},
				"work-2": {
					ObjectMeta: metav1.ObjectMeta{
						Name:       "work-2",
						Generation: 1,
					},
					Status: fleetv1beta1.WorkStatus{
						Conditions: []metav1.Condition{
							{
								Type:               fleetv1beta1.WorkConditionTypeDiffReported,
								Status:             metav1.ConditionFalse,
								ObservedGeneration: 0,
							},
						},
					},
				},
			},
			wantDiffReportedCondition: &metav1.Condition{
				Status:             metav1.ConditionFalse,
				Type:               string(fleetv1beta1.ResourceBindingDiffReported),
				Reason:             condition.WorkNotDiffReportedReason,
				ObservedGeneration: 1,
			},
			wantWorkDiffReportedCondSummaryStatus: workConditionSummarizedStatusIncomplete,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			binding := bindingTemplate.DeepCopy()
			workDiffReportedCondSummaryStatus := setAllWorkDiffReportedCondition(tc.works, binding)
			if workDiffReportedCondSummaryStatus != tc.wantWorkDiffReportedCondSummaryStatus {
				t.Errorf("setAllWorkDiffReportedCondition() = %v, want %v", workDiffReportedCondSummaryStatus, tc.wantWorkDiffReportedCondSummaryStatus)
			}
			diffReportedCond := meta.FindStatusCondition(binding.Status.Conditions, string(fleetv1beta1.ResourceBindingDiffReported))
			if diff := cmp.Diff(diffReportedCond, tc.wantDiffReportedCondition, cmpConditionOption); diff != "" {
				t.Errorf("diff reported condition mismatches (-got +want):\n%s", diff)
			}
		})
	}
}

func TestSetAllWorkAvailableCondition(t *testing.T) {
	tests := map[string]struct {
		works                              map[string]*fleetv1beta1.Work
		binding                            *fleetv1beta1.ClusterResourceBinding
		wantAvailableCond                  *metav1.Condition
		wantWorkAvailableCondSummaryStatus workConditionSummarizedStatus
	}{
		"All works are available": {
			works: map[string]*fleetv1beta1.Work{
				"work1": {
					ObjectMeta: metav1.ObjectMeta{
						Name: "work1",
					},
					Status: fleetv1beta1.WorkStatus{
						Conditions: []metav1.Condition{
							{
								Type:   fleetv1beta1.WorkConditionTypeAvailable,
								Reason: "any",
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
				"work2": {
					ObjectMeta: metav1.ObjectMeta{
						Name: "work2",
					},
					Status: fleetv1beta1.WorkStatus{
						Conditions: []metav1.Condition{
							{
								Type:   fleetv1beta1.WorkConditionTypeAvailable,
								Reason: "any",
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			binding: &fleetv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Status: fleetv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(fleetv1beta1.ResourceBindingApplied),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
					},
				},
			},
			wantAvailableCond: &metav1.Condition{
				Status:             metav1.ConditionTrue,
				Type:               string(fleetv1beta1.ResourceBindingAvailable),
				Reason:             condition.AllWorkAvailableReason,
				ObservedGeneration: 1,
			},
			wantWorkAvailableCondSummaryStatus: workConditionSummarizedStatusTrue,
		},
		"All works are available but one of them is not trackable": {
			works: map[string]*fleetv1beta1.Work{
				"work1": {
					ObjectMeta: metav1.ObjectMeta{
						Name: "work1",
					},
					Status: fleetv1beta1.WorkStatus{
						Conditions: []metav1.Condition{
							{
								Type:   fleetv1beta1.WorkConditionTypeAvailable,
								Reason: workapplier.WorkNotAllManifestsTrackableReason,
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
				"work2": {
					ObjectMeta: metav1.ObjectMeta{
						Name: "work2",
					},
					Status: fleetv1beta1.WorkStatus{
						Conditions: []metav1.Condition{
							{
								Type:   fleetv1beta1.WorkConditionTypeAvailable,
								Reason: "any",
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			binding: &fleetv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Status: fleetv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(fleetv1beta1.ResourceBindingApplied),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
					},
				},
			},
			wantAvailableCond: &metav1.Condition{
				Status:             metav1.ConditionTrue,
				Type:               string(fleetv1beta1.ResourceBindingAvailable),
				Reason:             condition.WorkNotAvailabilityTrackableReason,
				ObservedGeneration: 1,
			},
			wantWorkAvailableCondSummaryStatus: workConditionSummarizedStatusTrue,
		},
		"All works are available but one of them is not trackable (new reason)": {
			works: map[string]*fleetv1beta1.Work{
				"work1": {
					ObjectMeta: metav1.ObjectMeta{
						Name: "work1",
					},
					Status: fleetv1beta1.WorkStatus{
						Conditions: []metav1.Condition{
							{
								Type:   fleetv1beta1.WorkConditionTypeAvailable,
								Reason: workapplier.WorkNotAllManifestsTrackableReasonNew,
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
				"work2": {
					ObjectMeta: metav1.ObjectMeta{
						Name: "work2",
					},
					Status: fleetv1beta1.WorkStatus{
						Conditions: []metav1.Condition{
							{
								Type:   fleetv1beta1.WorkConditionTypeAvailable,
								Reason: "any",
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			binding: &fleetv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Status: fleetv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(fleetv1beta1.ResourceBindingApplied),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
					},
				},
			},
			wantAvailableCond: &metav1.Condition{
				Status:             metav1.ConditionTrue,
				Type:               string(fleetv1beta1.ResourceBindingAvailable),
				Reason:             condition.WorkNotAvailabilityTrackableReason,
				ObservedGeneration: 1,
			},
			wantWorkAvailableCondSummaryStatus: workConditionSummarizedStatusTrue,
		},
		"Not all works are available": {
			works: map[string]*fleetv1beta1.Work{
				"work1": {
					Status: fleetv1beta1.WorkStatus{
						Conditions: []metav1.Condition{
							{
								Type:   fleetv1beta1.WorkConditionTypeAvailable,
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
				"work2": {
					Status: fleetv1beta1.WorkStatus{
						Conditions: []metav1.Condition{
							{
								Type:   fleetv1beta1.WorkConditionTypeAvailable,
								Status: metav1.ConditionFalse,
							},
						},
					},
				},
			},
			binding: &fleetv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Status: fleetv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(fleetv1beta1.ResourceBindingApplied),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
					},
				},
			},
			wantAvailableCond: &metav1.Condition{
				Status:             metav1.ConditionFalse,
				Type:               string(fleetv1beta1.ResourceBindingAvailable),
				Reason:             condition.WorkNotAvailableReason,
				Message:            "work object work2 is not available",
				ObservedGeneration: 1,
			},
			wantWorkAvailableCondSummaryStatus: workConditionSummarizedStatusFalse,
		},
		"Available condition of one work is unknown": {
			works: map[string]*fleetv1beta1.Work{
				"work1": {
					Status: fleetv1beta1.WorkStatus{
						Conditions: []metav1.Condition{
							{
								Type:   fleetv1beta1.WorkConditionTypeAvailable,
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
				"work2": {
					Status: fleetv1beta1.WorkStatus{
						Conditions: []metav1.Condition{
							{
								Type:   fleetv1beta1.WorkConditionTypeAvailable,
								Status: metav1.ConditionUnknown,
							},
						},
					},
				},
			},
			binding: &fleetv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Status: fleetv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(fleetv1beta1.ResourceBindingApplied),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
					},
				},
			},
			wantAvailableCond: &metav1.Condition{
				Status:             metav1.ConditionFalse,
				Type:               string(fleetv1beta1.ResourceBindingAvailable),
				Reason:             condition.WorkNotAvailableReason,
				Message:            "work object work2 is not available",
				ObservedGeneration: 1,
			},
			wantWorkAvailableCondSummaryStatus: workConditionSummarizedStatusIncomplete,
		},
		// This is a case that should not happen in practice.
		"one work has not completed availability check yet": {
			works: map[string]*fleetv1beta1.Work{
				"work1": {
					Status: fleetv1beta1.WorkStatus{
						Conditions: []metav1.Condition{
							{
								Type:   fleetv1beta1.WorkConditionTypeAvailable,
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
				"work2": {
					Status: fleetv1beta1.WorkStatus{
						Conditions: []metav1.Condition{},
					},
				},
			},
			binding: &fleetv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Status: fleetv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(fleetv1beta1.ResourceBindingApplied),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 1,
						},
					},
				},
			},
			wantAvailableCond: &metav1.Condition{
				Status:             metav1.ConditionFalse,
				Type:               string(fleetv1beta1.ResourceBindingAvailable),
				Reason:             condition.WorkNotAvailableReason,
				Message:            "work object work2 is not available",
				ObservedGeneration: 1,
			},
			wantWorkAvailableCondSummaryStatus: workConditionSummarizedStatusIncomplete,
		},
		"works are not applied successfully": {
			works: map[string]*fleetv1beta1.Work{
				"work1": {},
				"work2": {},
			},
			binding: &fleetv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Status: fleetv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(fleetv1beta1.ResourceBindingApplied),
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
						},
					},
				},
			},
			wantAvailableCond:                  nil,
			wantWorkAvailableCondSummaryStatus: workConditionSummarizedStatusFalse,
		},
		"works are not applied yet": {
			works: map[string]*fleetv1beta1.Work{
				"work1": {},
				"work2": {},
			},
			binding: &fleetv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 1,
				},
				Status: fleetv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{},
				},
			},
			wantAvailableCond:                  nil,
			wantWorkAvailableCondSummaryStatus: workConditionSummarizedStatusFalse,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			workAvailableCondSummaryStatus := setAllWorkAvailableCondition(tt.works, tt.binding)
			if workAvailableCondSummaryStatus != tt.wantWorkAvailableCondSummaryStatus {
				t.Errorf("buildAllWorkAvailableCondition() = %v, want %v", workAvailableCondSummaryStatus, tt.wantWorkAvailableCondSummaryStatus)
			}
			availableCond := meta.FindStatusCondition(tt.binding.Status.Conditions, string(fleetv1beta1.ResourceBindingAvailable))
			if diff := cmp.Diff(availableCond, tt.wantAvailableCond, cmpConditionOption); diff != "" {
				t.Errorf("buildAllWorkAvailableCondition test `%s` mismatch (-got +want):\n%s", name, diff)
			}
		})
	}
}

// TO-DO (chenyu1): refactor this unit test to cover all branches.
func TestSetBindingStatus(t *testing.T) {
	timeNow := time.Now()
	tests := map[string]struct {
		works                            map[string]*fleetv1beta1.Work
		applyStrategy                    *fleetv1beta1.ApplyStrategy
		maxFailedResourcePlacementLimit  *int
		wantFailedResourcePlacements     []fleetv1beta1.FailedResourcePlacement
		maxDriftedResourcePlacementLimit *int
		wantDriftedResourcePlacements    []fleetv1beta1.DriftedResourcePlacement
		maxDiffedResourcePlacementLimit  *int
		wantDiffedResourcePlacements     []fleetv1beta1.DiffedResourcePlacement
	}{
		"NoWorks": {
			works: map[string]*fleetv1beta1.Work{},
		},
		"both work are available": {
			works: map[string]*fleetv1beta1.Work{
				"work1": {
					Status: fleetv1beta1.WorkStatus{
						ManifestConditions: []fleetv1beta1.ManifestCondition{
							{
								Identifier: fleetv1beta1.WorkResourceIdentifier{
									Ordinal:   1,
									Group:     "",
									Version:   "v1",
									Kind:      "Service",
									Name:      "svc-name",
									Namespace: "svc-namespace",
								},
								Conditions: []metav1.Condition{
									{
										Type:   fleetv1beta1.WorkConditionTypeApplied,
										Status: metav1.ConditionTrue,
									},
									{
										Type:   fleetv1beta1.WorkConditionTypeAvailable,
										Status: metav1.ConditionTrue,
									},
								},
							},
						},
						Conditions: []metav1.Condition{
							{
								Type:   fleetv1beta1.WorkConditionTypeApplied,
								Status: metav1.ConditionTrue,
							},
							{
								Type:   fleetv1beta1.WorkConditionTypeAvailable,
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
				"work2": {
					Status: fleetv1beta1.WorkStatus{
						ManifestConditions: []fleetv1beta1.ManifestCondition{
							{
								Identifier: fleetv1beta1.WorkResourceIdentifier{
									Ordinal:   0,
									Group:     "",
									Version:   "v1",
									Kind:      "ConfigMap",
									Name:      "config-name",
									Namespace: "config-namespace",
								},
								Conditions: []metav1.Condition{
									{
										Type:   fleetv1beta1.WorkConditionTypeApplied,
										Status: metav1.ConditionTrue,
									},
									{
										Type:   fleetv1beta1.WorkConditionTypeAvailable,
										Status: metav1.ConditionTrue,
									},
								},
							},
						},
						Conditions: []metav1.Condition{
							{
								Type:   fleetv1beta1.WorkConditionTypeApplied,
								Status: metav1.ConditionTrue,
							},
							{
								Type:   fleetv1beta1.WorkConditionTypeAvailable,
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
		},
		"One work has one not available and one work has one not applied": {
			works: map[string]*fleetv1beta1.Work{
				"work1": {
					Status: fleetv1beta1.WorkStatus{
						ManifestConditions: []fleetv1beta1.ManifestCondition{
							{
								Identifier: fleetv1beta1.WorkResourceIdentifier{
									Ordinal:   0,
									Group:     "",
									Version:   "v1",
									Kind:      "ConfigMap",
									Name:      "config-name",
									Namespace: "config-namespace",
								},
								Conditions: []metav1.Condition{
									{
										Type:   fleetv1beta1.WorkConditionTypeApplied,
										Status: metav1.ConditionTrue,
									},
									{
										Type:   fleetv1beta1.WorkConditionTypeAvailable,
										Status: metav1.ConditionFalse,
									},
								},
							},
							{
								Identifier: fleetv1beta1.WorkResourceIdentifier{
									Ordinal:   1,
									Group:     "",
									Version:   "v1",
									Kind:      "Service",
									Name:      "svc-name",
									Namespace: "svc-namespace",
								},
								Conditions: []metav1.Condition{
									{
										Type:   fleetv1beta1.WorkConditionTypeApplied,
										Status: metav1.ConditionTrue,
									},
									{
										Type:   fleetv1beta1.WorkConditionTypeAvailable,
										Status: metav1.ConditionTrue,
									},
								},
							},
						},
						Conditions: []metav1.Condition{
							{
								Type:   fleetv1beta1.WorkConditionTypeApplied,
								Status: metav1.ConditionTrue,
							},
							{
								Type:   fleetv1beta1.WorkConditionTypeAvailable,
								Status: metav1.ConditionFalse,
							},
						},
					},
				},
				"work2": {
					Status: fleetv1beta1.WorkStatus{
						ManifestConditions: []fleetv1beta1.ManifestCondition{
							{
								Identifier: fleetv1beta1.WorkResourceIdentifier{
									Ordinal:   0,
									Group:     "",
									Version:   "v1",
									Kind:      "ConfigMap",
									Name:      "config-name-1",
									Namespace: "config-namespace",
								},
								Conditions: []metav1.Condition{
									{
										Type:   fleetv1beta1.WorkConditionTypeApplied,
										Status: metav1.ConditionTrue,
									},
									{
										Type:   fleetv1beta1.WorkConditionTypeAvailable,
										Status: metav1.ConditionTrue,
									},
								},
							},
							{
								Identifier: fleetv1beta1.WorkResourceIdentifier{
									Ordinal:   1,
									Group:     "",
									Version:   "v1",
									Kind:      "Service",
									Name:      "svc-name-1",
									Namespace: "svc-namespace",
								},
								Conditions: []metav1.Condition{
									{
										Type:   fleetv1beta1.WorkConditionTypeApplied,
										Status: metav1.ConditionFalse,
									},
								},
							},
						},
						Conditions: []metav1.Condition{
							{
								Type:   fleetv1beta1.WorkConditionTypeApplied,
								Status: metav1.ConditionFalse,
							},
							{
								Type:   fleetv1beta1.WorkConditionTypeAvailable,
								Status: metav1.ConditionFalse,
							},
						},
					},
				},
			},
			wantFailedResourcePlacements: []fleetv1beta1.FailedResourcePlacement{
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     "",
						Version:   "v1",
						Kind:      "ConfigMap",
						Name:      "config-name",
						Namespace: "config-namespace",
					},
					Condition: metav1.Condition{
						Type:   fleetv1beta1.WorkConditionTypeAvailable,
						Status: metav1.ConditionFalse,
					},
				},
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     "",
						Version:   "v1",
						Kind:      "Service",
						Name:      "svc-name-1",
						Namespace: "svc-namespace",
					},
					Condition: metav1.Condition{
						Type:   fleetv1beta1.WorkConditionTypeApplied,
						Status: metav1.ConditionFalse,
					},
				},
			},
		},
		"One work has one not available and one work has one not applied (exceed the maxFailedResourcePlacementLimit)": {
			works: map[string]*fleetv1beta1.Work{
				"work1": {
					Status: fleetv1beta1.WorkStatus{
						ManifestConditions: []fleetv1beta1.ManifestCondition{
							{
								Identifier: fleetv1beta1.WorkResourceIdentifier{
									Ordinal:   0,
									Group:     "",
									Version:   "v1",
									Kind:      "ConfigMap",
									Name:      "config-name",
									Namespace: "config-namespace",
								},
								Conditions: []metav1.Condition{
									{
										Type:   fleetv1beta1.WorkConditionTypeApplied,
										Status: metav1.ConditionTrue,
									},
									{
										Type:   fleetv1beta1.WorkConditionTypeAvailable,
										Status: metav1.ConditionFalse,
									},
								},
							},
							{
								Identifier: fleetv1beta1.WorkResourceIdentifier{
									Ordinal:   1,
									Group:     "",
									Version:   "v1",
									Kind:      "Service",
									Name:      "svc-name",
									Namespace: "svc-namespace",
								},
								Conditions: []metav1.Condition{
									{
										Type:   fleetv1beta1.WorkConditionTypeApplied,
										Status: metav1.ConditionTrue,
									},
									{
										Type:   fleetv1beta1.WorkConditionTypeAvailable,
										Status: metav1.ConditionTrue,
									},
								},
							},
						},
						Conditions: []metav1.Condition{
							{
								Type:   fleetv1beta1.WorkConditionTypeApplied,
								Status: metav1.ConditionTrue,
							},
							{
								Type:   fleetv1beta1.WorkConditionTypeAvailable,
								Status: metav1.ConditionFalse,
							},
						},
					},
				},
				"work2": {
					Status: fleetv1beta1.WorkStatus{
						ManifestConditions: []fleetv1beta1.ManifestCondition{
							{
								Identifier: fleetv1beta1.WorkResourceIdentifier{
									Ordinal:   0,
									Group:     "",
									Version:   "v1",
									Kind:      "ConfigMap",
									Name:      "config-name-1",
									Namespace: "config-namespace",
								},
								Conditions: []metav1.Condition{
									{
										Type:   fleetv1beta1.WorkConditionTypeApplied,
										Status: metav1.ConditionTrue,
									},
									{
										Type:   fleetv1beta1.WorkConditionTypeAvailable,
										Status: metav1.ConditionTrue,
									},
								},
							},
							{
								Identifier: fleetv1beta1.WorkResourceIdentifier{
									Ordinal:   1,
									Group:     "",
									Version:   "v1",
									Kind:      "Service",
									Name:      "svc-name-1",
									Namespace: "svc-namespace",
								},
								Conditions: []metav1.Condition{
									{
										Type:   fleetv1beta1.WorkConditionTypeApplied,
										Status: metav1.ConditionFalse,
									},
								},
							},
						},
						Conditions: []metav1.Condition{
							{
								Type:   fleetv1beta1.WorkConditionTypeApplied,
								Status: metav1.ConditionFalse,
							},
							{
								Type:   fleetv1beta1.WorkConditionTypeAvailable,
								Status: metav1.ConditionFalse,
							},
						},
					},
				},
			},
			maxFailedResourcePlacementLimit: ptr.To(1),
			wantFailedResourcePlacements: []fleetv1beta1.FailedResourcePlacement{
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     "",
						Version:   "v1",
						Kind:      "ConfigMap",
						Name:      "config-name",
						Namespace: "config-namespace",
					},
					Condition: metav1.Condition{
						Type:   fleetv1beta1.WorkConditionTypeAvailable,
						Status: metav1.ConditionFalse,
					},
				},
			},
		},
		"One work has one not available and one work all available": {
			works: map[string]*fleetv1beta1.Work{
				"work1": {
					Status: fleetv1beta1.WorkStatus{
						ManifestConditions: []fleetv1beta1.ManifestCondition{
							{
								Identifier: fleetv1beta1.WorkResourceIdentifier{
									Ordinal:   0,
									Group:     "",
									Version:   "v1",
									Kind:      "ConfigMap",
									Name:      "config-name",
									Namespace: "config-namespace",
								},
								Conditions: []metav1.Condition{
									{
										Type:   fleetv1beta1.WorkConditionTypeApplied,
										Status: metav1.ConditionTrue,
									},
									{
										Type:   fleetv1beta1.WorkConditionTypeAvailable,
										Status: metav1.ConditionTrue,
									},
								},
							},
							{
								Identifier: fleetv1beta1.WorkResourceIdentifier{
									Ordinal:   1,
									Group:     "",
									Version:   "v1",
									Kind:      "Service",
									Name:      "svc-name",
									Namespace: "svc-namespace",
								},
								Conditions: []metav1.Condition{
									{
										Type:   fleetv1beta1.WorkConditionTypeApplied,
										Status: metav1.ConditionTrue,
									},
									{
										Type:   fleetv1beta1.WorkConditionTypeAvailable,
										Status: metav1.ConditionTrue,
									},
								},
							},
						},
						Conditions: []metav1.Condition{
							{
								Type:   fleetv1beta1.WorkConditionTypeApplied,
								Status: metav1.ConditionTrue,
							},
							{
								Type:   fleetv1beta1.WorkConditionTypeAvailable,
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
				"all available work": {
					Status: fleetv1beta1.WorkStatus{
						ManifestConditions: []fleetv1beta1.ManifestCondition{
							{
								Identifier: fleetv1beta1.WorkResourceIdentifier{
									Ordinal:   0,
									Group:     "",
									Version:   "v1",
									Kind:      "ConfigMap",
									Name:      "config-name",
									Namespace: "config-namespace",
								},
								Conditions: []metav1.Condition{
									{
										Type:   fleetv1beta1.WorkConditionTypeApplied,
										Status: metav1.ConditionTrue,
									},
									{
										Type:   fleetv1beta1.WorkConditionTypeAvailable,
										Status: metav1.ConditionTrue,
									},
								},
							},
							{
								Identifier: fleetv1beta1.WorkResourceIdentifier{
									Ordinal:   1,
									Group:     "",
									Version:   "v1",
									Kind:      "Service",
									Name:      "svc-name",
									Namespace: "svc-namespace",
								},
								Conditions: []metav1.Condition{
									{
										Type:   fleetv1beta1.WorkConditionTypeApplied,
										Status: metav1.ConditionFalse,
									},
								},
							},
						},
						Conditions: []metav1.Condition{
							{
								Type:   fleetv1beta1.WorkConditionTypeApplied,
								Status: metav1.ConditionFalse,
							},
							{
								Type:   fleetv1beta1.WorkConditionTypeAvailable,
								Status: metav1.ConditionFalse,
							},
						},
					},
				},
			},
			wantFailedResourcePlacements: []fleetv1beta1.FailedResourcePlacement{
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     "",
						Version:   "v1",
						Kind:      "Service",
						Name:      "svc-name",
						Namespace: "svc-namespace",
					},
					Condition: metav1.Condition{
						Type:   fleetv1beta1.WorkConditionTypeApplied,
						Status: metav1.ConditionFalse,
					},
				},
			},
		},
		"exceed the maxDriftedResourcePlacementLimit": {
			works: map[string]*fleetv1beta1.Work{
				"work1": {
					Status: fleetv1beta1.WorkStatus{
						ManifestConditions: []fleetv1beta1.ManifestCondition{
							{
								Identifier: fleetv1beta1.WorkResourceIdentifier{
									Ordinal:   0,
									Group:     "",
									Version:   "v1",
									Kind:      "Service",
									Name:      "svc-name",
									Namespace: "svc-namespace",
								},
								Conditions: []metav1.Condition{
									{
										Type:   fleetv1beta1.WorkConditionTypeApplied,
										Status: metav1.ConditionFalse,
									},
								},
								DriftDetails: &fleetv1beta1.DriftDetails{
									ObservationTime:                   metav1.NewTime(timeNow),
									ObservedInMemberClusterGeneration: 2,
									FirstDriftedObservedTime:          metav1.NewTime(timeNow.Add(-time.Hour)),
									ObservedDrifts: []fleetv1beta1.PatchDetail{
										{
											Path:          "/spec/ports/0/port",
											ValueInHub:    "80",
											ValueInMember: "90",
										},
									},
								},
							},
						},
						Conditions: []metav1.Condition{
							{
								Type:   fleetv1beta1.WorkConditionTypeApplied,
								Status: metav1.ConditionFalse,
							},
						},
					},
				},
				"work2": {
					Status: fleetv1beta1.WorkStatus{
						ManifestConditions: []fleetv1beta1.ManifestCondition{
							{
								Identifier: fleetv1beta1.WorkResourceIdentifier{
									Ordinal:   0,
									Group:     "",
									Version:   "v1",
									Kind:      "ConfigMap",
									Name:      "config-name-1",
									Namespace: "config-namespace",
								},
								Conditions: []metav1.Condition{
									{
										Type:   fleetv1beta1.WorkConditionTypeApplied,
										Status: metav1.ConditionTrue,
									},
									{
										Type:   fleetv1beta1.WorkConditionTypeAvailable,
										Status: metav1.ConditionTrue,
									},
								},
								DriftDetails: &fleetv1beta1.DriftDetails{
									ObservationTime:                   metav1.NewTime(timeNow),
									ObservedInMemberClusterGeneration: 2,
									FirstDriftedObservedTime:          metav1.NewTime(timeNow.Add(-time.Second)),
									ObservedDrifts: []fleetv1beta1.PatchDetail{
										{
											Path:          "/metadata/labels/label1",
											ValueInHub:    "key1",
											ValueInMember: "key2",
										},
									},
								},
							},
							{
								Identifier: fleetv1beta1.WorkResourceIdentifier{
									Ordinal:   1,
									Group:     "",
									Version:   "v1",
									Kind:      "Service",
									Name:      "svc-name-1",
									Namespace: "svc-namespace",
								},
								Conditions: []metav1.Condition{
									{
										Type:   fleetv1beta1.WorkConditionTypeApplied,
										Status: metav1.ConditionTrue,
									},
									{
										Type:   fleetv1beta1.WorkConditionTypeAvailable,
										Status: metav1.ConditionTrue,
									},
								},
							},
						},
						Conditions: []metav1.Condition{
							{
								Type:   fleetv1beta1.WorkConditionTypeApplied,
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			wantFailedResourcePlacements: []fleetv1beta1.FailedResourcePlacement{
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Version:   "v1",
						Kind:      "Service",
						Name:      "svc-name",
						Namespace: "svc-namespace",
					},
					Condition: metav1.Condition{
						Type:   fleetv1beta1.WorkConditionTypeApplied,
						Status: metav1.ConditionFalse,
					},
				},
			},
			maxDriftedResourcePlacementLimit: ptr.To(1),
			wantDriftedResourcePlacements: []fleetv1beta1.DriftedResourcePlacement{
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     "",
						Version:   "v1",
						Kind:      "ConfigMap",
						Name:      "config-name-1",
						Namespace: "config-namespace",
					},
					ObservationTime:                 metav1.NewTime(timeNow),
					TargetClusterObservedGeneration: 2,
					FirstDriftedObservedTime:        metav1.NewTime(timeNow.Add(-time.Second)),
					ObservedDrifts: []fleetv1beta1.PatchDetail{
						{
							Path:          "/metadata/labels/label1",
							ValueInHub:    "key1",
							ValueInMember: "key2",
						},
					},
				},
			},
		},
		"exceed the maxDiffedResourcePlacementLimit": {
			works: map[string]*fleetv1beta1.Work{
				"work1": {
					Status: fleetv1beta1.WorkStatus{
						ManifestConditions: []fleetv1beta1.ManifestCondition{
							{
								Identifier: fleetv1beta1.WorkResourceIdentifier{
									Ordinal:   1,
									Group:     "",
									Version:   "v1",
									Kind:      "Service",
									Name:      "svc-name",
									Namespace: "svc-namespace",
								},
								Conditions: []metav1.Condition{
									{
										Type:   fleetv1beta1.WorkConditionTypeApplied,
										Status: metav1.ConditionFalse,
									},
								},
								DiffDetails: &fleetv1beta1.DiffDetails{
									ObservationTime:                   metav1.NewTime(timeNow),
									ObservedInMemberClusterGeneration: ptr.To(int64(2)),
									FirstDiffedObservedTime:           metav1.NewTime(timeNow.Add(-time.Hour)),
									ObservedDiffs: []fleetv1beta1.PatchDetail{
										{
											Path:          "/spec/ports/1/port",
											ValueInHub:    "80",
											ValueInMember: "90",
										},
									},
								},
							},
						},
						Conditions: []metav1.Condition{
							{
								Type:   fleetv1beta1.WorkConditionTypeApplied,
								Status: metav1.ConditionFalse,
							},
						},
					},
				},
				"work2": {
					Status: fleetv1beta1.WorkStatus{
						ManifestConditions: []fleetv1beta1.ManifestCondition{
							{
								Identifier: fleetv1beta1.WorkResourceIdentifier{
									Ordinal:   0,
									Group:     "",
									Version:   "v1",
									Kind:      "ConfigMap",
									Name:      "config-name-1",
									Namespace: "config-namespace",
								},
								Conditions: []metav1.Condition{
									{
										Type:   fleetv1beta1.WorkConditionTypeApplied,
										Status: metav1.ConditionFalse,
									},
								},
								DiffDetails: &fleetv1beta1.DiffDetails{
									ObservationTime:                   metav1.NewTime(timeNow),
									ObservedInMemberClusterGeneration: ptr.To(int64(2)),
									FirstDiffedObservedTime:           metav1.NewTime(timeNow.Add(-time.Second)),
									ObservedDiffs: []fleetv1beta1.PatchDetail{
										{
											Path:          "/metadata/labels/label1",
											ValueInHub:    "key1",
											ValueInMember: "key2",
										},
									},
								},
							},
							{
								Identifier: fleetv1beta1.WorkResourceIdentifier{
									Ordinal:   1,
									Group:     "",
									Version:   "v1",
									Kind:      "Service",
									Name:      "svc-name-1",
									Namespace: "svc-namespace",
								},
								Conditions: []metav1.Condition{
									{
										Type:   fleetv1beta1.WorkConditionTypeApplied,
										Status: metav1.ConditionTrue,
									},
									{
										Type:   fleetv1beta1.WorkConditionTypeAvailable,
										Status: metav1.ConditionTrue,
									},
								},
							},
						},
						Conditions: []metav1.Condition{
							{
								Type:   fleetv1beta1.WorkConditionTypeApplied,
								Status: metav1.ConditionFalse,
							},
						},
					},
				},
			},
			wantFailedResourcePlacements: []fleetv1beta1.FailedResourcePlacement{
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Version:   "v1",
						Kind:      "ConfigMap",
						Name:      "config-name-1",
						Namespace: "config-namespace",
					},
					Condition: metav1.Condition{
						Type:   fleetv1beta1.WorkConditionTypeApplied,
						Status: metav1.ConditionFalse,
					},
				},
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Version:   "v1",
						Kind:      "Service",
						Name:      "svc-name",
						Namespace: "svc-namespace",
					},
					Condition: metav1.Condition{
						Type:   fleetv1beta1.WorkConditionTypeApplied,
						Status: metav1.ConditionFalse,
					},
				},
			},
			maxDiffedResourcePlacementLimit: ptr.To(1),
			wantDiffedResourcePlacements: []fleetv1beta1.DiffedResourcePlacement{
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     "",
						Version:   "v1",
						Kind:      "ConfigMap",
						Name:      "config-name-1",
						Namespace: "config-namespace",
					},
					ObservationTime:                 metav1.NewTime(timeNow),
					TargetClusterObservedGeneration: ptr.To(int64(2)),
					FirstDiffedObservedTime:         metav1.NewTime(timeNow.Add(-time.Second)),
					ObservedDiffs: []fleetv1beta1.PatchDetail{
						{
							Path:          "/metadata/labels/label1",
							ValueInHub:    "key1",
							ValueInMember: "key2",
						},
					},
				},
			},
		},
		"One work has reported diff but one work has not reported diff yet": {
			works: map[string]*fleetv1beta1.Work{
				"work1": {
					Status: fleetv1beta1.WorkStatus{},
				},
				"work2": {
					Status: fleetv1beta1.WorkStatus{
						ManifestConditions: []fleetv1beta1.ManifestCondition{
							{
								Identifier: fleetv1beta1.WorkResourceIdentifier{
									Ordinal:   0,
									Group:     "",
									Version:   "v1",
									Kind:      "ConfigMap",
									Name:      "config-name-1",
									Namespace: "config-namespace",
								},
								Conditions: []metav1.Condition{
									{
										Type:   fleetv1beta1.WorkConditionTypeDiffReported,
										Status: metav1.ConditionTrue,
									},
								},
							},
							{
								Identifier: fleetv1beta1.WorkResourceIdentifier{
									Ordinal:   1,
									Group:     "",
									Version:   "v1",
									Kind:      "Service",
									Name:      "svc-name-1",
									Namespace: "svc-namespace",
								},
								Conditions: []metav1.Condition{
									{
										Type:   fleetv1beta1.WorkConditionTypeDiffReported,
										Status: metav1.ConditionTrue,
									},
								},
								DiffDetails: &fleetv1beta1.DiffDetails{
									ObservationTime:         metav1.Time{Time: timeNow},
									FirstDiffedObservedTime: metav1.Time{Time: timeNow},
									ObservedDiffs: []fleetv1beta1.PatchDetail{
										{
											Path:       "/",
											ValueInHub: "(the whole object)",
										},
									},
								},
							},
						},
						Conditions: []metav1.Condition{
							{
								Type:   fleetv1beta1.WorkConditionTypeDiffReported,
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			applyStrategy: &fleetv1beta1.ApplyStrategy{
				Type: fleetv1beta1.ApplyStrategyTypeReportDiff,
			},
		},
		"One work has reported diff but one work has failed to report diff": {
			works: map[string]*fleetv1beta1.Work{
				"work1": {
					Status: fleetv1beta1.WorkStatus{
						ManifestConditions: []fleetv1beta1.ManifestCondition{
							{
								Identifier: fleetv1beta1.WorkResourceIdentifier{
									Ordinal:   0,
									Group:     "",
									Version:   "v1",
									Kind:      "ConfigMap",
									Name:      "config-name",
									Namespace: "config-namespace",
								},
								Conditions: []metav1.Condition{
									{
										Type:   fleetv1beta1.WorkConditionTypeDiffReported,
										Status: metav1.ConditionFalse,
									},
								},
							},
							{
								Identifier: fleetv1beta1.WorkResourceIdentifier{
									Ordinal:   1,
									Group:     "",
									Version:   "v1",
									Kind:      "Service",
									Name:      "svc-name",
									Namespace: "svc-namespace",
								},
								Conditions: []metav1.Condition{
									{
										Type:   fleetv1beta1.WorkConditionTypeDiffReported,
										Status: metav1.ConditionFalse,
									},
								},
							},
						},
						Conditions: []metav1.Condition{
							{
								Type:   fleetv1beta1.WorkConditionTypeDiffReported,
								Status: metav1.ConditionFalse,
							},
						},
					},
				},
				"work2": {
					Status: fleetv1beta1.WorkStatus{
						ManifestConditions: []fleetv1beta1.ManifestCondition{
							{
								Identifier: fleetv1beta1.WorkResourceIdentifier{
									Ordinal:   0,
									Group:     "",
									Version:   "v1",
									Kind:      "ConfigMap",
									Name:      "config-name-1",
									Namespace: "config-namespace",
								},
								Conditions: []metav1.Condition{
									{
										Type:   fleetv1beta1.WorkConditionTypeDiffReported,
										Status: metav1.ConditionTrue,
									},
								},
							},
							{
								Identifier: fleetv1beta1.WorkResourceIdentifier{
									Ordinal:   1,
									Group:     "",
									Version:   "v1",
									Kind:      "Service",
									Name:      "svc-name-1",
									Namespace: "svc-namespace",
								},
								Conditions: []metav1.Condition{
									{
										Type:   fleetv1beta1.WorkConditionTypeDiffReported,
										Status: metav1.ConditionTrue,
									},
								},
								DiffDetails: &fleetv1beta1.DiffDetails{
									ObservationTime:         metav1.Time{Time: timeNow},
									FirstDiffedObservedTime: metav1.Time{Time: timeNow},
									ObservedDiffs: []fleetv1beta1.PatchDetail{
										{
											Path:       "/",
											ValueInHub: "(the whole object)",
										},
									},
								},
							},
						},
						Conditions: []metav1.Condition{
							{
								Type:   fleetv1beta1.WorkConditionTypeDiffReported,
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			applyStrategy: &fleetv1beta1.ApplyStrategy{
				Type: fleetv1beta1.ApplyStrategyTypeReportDiff,
			},
		},
		"Both works have reported diff": {
			works: map[string]*fleetv1beta1.Work{
				"work1": {
					Status: fleetv1beta1.WorkStatus{
						ManifestConditions: []fleetv1beta1.ManifestCondition{
							{
								Identifier: fleetv1beta1.WorkResourceIdentifier{
									Ordinal:   0,
									Group:     "",
									Version:   "v1",
									Kind:      "ConfigMap",
									Name:      "config-name",
									Namespace: "config-namespace",
								},
								Conditions: []metav1.Condition{
									{
										Type:   fleetv1beta1.WorkConditionTypeDiffReported,
										Status: metav1.ConditionTrue,
									},
								},
							},
							{
								Identifier: fleetv1beta1.WorkResourceIdentifier{
									Ordinal:   1,
									Group:     "",
									Version:   "v1",
									Kind:      "Service",
									Name:      "svc-name",
									Namespace: "svc-namespace",
								},
								Conditions: []metav1.Condition{
									{
										Type:   fleetv1beta1.WorkConditionTypeDiffReported,
										Status: metav1.ConditionTrue,
									},
								},
							},
						},
						Conditions: []metav1.Condition{
							{
								Type:   fleetv1beta1.WorkConditionTypeDiffReported,
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
				"work2": {
					Status: fleetv1beta1.WorkStatus{
						ManifestConditions: []fleetv1beta1.ManifestCondition{
							{
								Identifier: fleetv1beta1.WorkResourceIdentifier{
									Ordinal:   0,
									Group:     "",
									Version:   "v1",
									Kind:      "ConfigMap",
									Name:      "config-name-1",
									Namespace: "config-namespace",
								},
								Conditions: []metav1.Condition{
									{
										Type:   fleetv1beta1.WorkConditionTypeDiffReported,
										Status: metav1.ConditionTrue,
									},
								},
							},
							{
								Identifier: fleetv1beta1.WorkResourceIdentifier{
									Ordinal:   1,
									Group:     "",
									Version:   "v1",
									Kind:      "Service",
									Name:      "svc-name-1",
									Namespace: "svc-namespace",
								},
								Conditions: []metav1.Condition{
									{
										Type:   fleetv1beta1.WorkConditionTypeDiffReported,
										Status: metav1.ConditionTrue,
									},
								},
								DiffDetails: &fleetv1beta1.DiffDetails{
									ObservationTime:         metav1.Time{Time: timeNow},
									FirstDiffedObservedTime: metav1.Time{Time: timeNow},
									ObservedDiffs: []fleetv1beta1.PatchDetail{
										{
											Path:       "/",
											ValueInHub: "(the whole object)",
										},
									},
								},
							},
						},
						Conditions: []metav1.Condition{
							{
								Type:   fleetv1beta1.WorkConditionTypeDiffReported,
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			applyStrategy: &fleetv1beta1.ApplyStrategy{
				Type: fleetv1beta1.ApplyStrategyTypeReportDiff,
			},
			wantDiffedResourcePlacements: []fleetv1beta1.DiffedResourcePlacement{
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     "",
						Version:   "v1",
						Kind:      "Service",
						Name:      "svc-name-1",
						Namespace: "svc-namespace",
					},
					ObservationTime:         metav1.Time{Time: timeNow},
					FirstDiffedObservedTime: metav1.Time{Time: timeNow},
					ObservedDiffs: []fleetv1beta1.PatchDetail{
						{
							Path:       "/",
							ValueInHub: "(the whole object)",
						},
					},
				},
			},
		},
	}

	originalMaxFailedResourcePlacementLimit := maxFailedResourcePlacementLimit
	originalMaxDriftedResourcePlacementLimit := maxDriftedResourcePlacementLimit
	originalMaxDiffedResourcePlacementLimit := maxDiffedResourcePlacementLimit
	defer func() {
		maxFailedResourcePlacementLimit = originalMaxFailedResourcePlacementLimit
		maxDriftedResourcePlacementLimit = originalMaxDriftedResourcePlacementLimit
		maxDiffedResourcePlacementLimit = originalMaxDiffedResourcePlacementLimit
	}()
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if tt.maxFailedResourcePlacementLimit != nil {
				maxFailedResourcePlacementLimit = *tt.maxFailedResourcePlacementLimit
			} else {
				maxFailedResourcePlacementLimit = originalMaxFailedResourcePlacementLimit
			}

			if tt.maxDriftedResourcePlacementLimit != nil {
				maxDriftedResourcePlacementLimit = *tt.maxDriftedResourcePlacementLimit
			} else {
				maxDriftedResourcePlacementLimit = originalMaxDriftedResourcePlacementLimit
			}

			if tt.maxDiffedResourcePlacementLimit != nil {
				maxDiffedResourcePlacementLimit = *tt.maxDiffedResourcePlacementLimit
			} else {
				maxDiffedResourcePlacementLimit = originalMaxDiffedResourcePlacementLimit
			}

			binding := &fleetv1beta1.ClusterResourceBinding{
				Spec: fleetv1beta1.ResourceBindingSpec{
					ApplyStrategy: tt.applyStrategy,
				},
			}
			setBindingStatus(tt.works, binding)
			got := binding.Status.FailedPlacements
			// setBindingStatus is using map to populate the placements.
			// There is no default order in traversing the map.
			// When the result of Placements exceeds the limit, the result will be truncated and cannot be
			// guaranteed.
			if maxFailedResourcePlacementLimit == len(tt.wantFailedResourcePlacements) {
				opt := cmp.Comparer(func(x, y fleetv1beta1.FailedResourcePlacement) bool {
					return x.Condition.Status == y.Condition.Status // condition should be set as false
				})
				if diff := cmp.Diff(got, tt.wantFailedResourcePlacements, opt); diff != "" {
					t.Errorf("setBindingStatus got FailedPlacements mismatch (-got +want):\n%s", diff)
				}
				return
			}

			statusCmpOptions := []cmp.Option{
				cmpopts.SortSlices(func(i, j fleetv1beta1.FailedResourcePlacement) bool {
					if i.Group < j.Group {
						return true
					}
					if i.Kind < j.Kind {
						return true
					}
					return i.Name < j.Name
				}),
			}
			if diff := cmp.Diff(got, tt.wantFailedResourcePlacements, statusCmpOptions...); diff != "" {
				t.Errorf("setBindingStatus got FailedPlacements mismatch (-got +want):\n%s", diff)
			}

			gotDrifted := binding.Status.DriftedPlacements
			if maxDriftedResourcePlacementLimit == len(tt.wantDriftedResourcePlacements) {
				opt := []cmp.Option{
					cmpopts.SortSlices(utils.LessFuncDriftedResourcePlacements),
					cmp.Comparer(func(t1, t2 metav1.Time) bool {
						if t1.Time.IsZero() || t2.Time.IsZero() {
							return true // treat them as equal
						}
						if t1.Time.After(t2.Time) {
							t1, t2 = t2, t1 // ensure t1 is always before t2
						}
						// we're within the margin (10s) if x + margin >= y
						return !t1.Time.Add(10 * time.Second).Before(t2.Time)
					}),
				}
				if diff := cmp.Diff(gotDrifted, tt.wantDriftedResourcePlacements, opt...); diff != "" {
					t.Errorf("setBindingStatus got DriftedPlacements mismatch (-got +want):\n%s", diff)
				}
				return
			}

			resourceCmpOptions := []cmp.Option{
				cmpopts.SortSlices(utils.LessFuncDriftedResourcePlacements),
			}
			if diff := cmp.Diff(gotDrifted, tt.wantDriftedResourcePlacements, resourceCmpOptions...); diff != "" {
				t.Errorf("setBindingStatus got DriftedPlacements mismatch (-got +want):\n%s", diff)
			}

			gotDiffed := binding.Status.DiffedPlacements
			if maxDiffedResourcePlacementLimit == len(tt.wantDiffedResourcePlacements) {
				opt := []cmp.Option{
					cmpopts.SortSlices(utils.LessFuncDiffedResourcePlacements),
					cmp.Comparer(func(t1, t2 metav1.Time) bool {
						if t1.Time.IsZero() || t2.Time.IsZero() {
							return true // treat them as equal
						}
						if t1.Time.After(t2.Time) {
							t1, t2 = t2, t1 // ensure t1 is always before t2
						}
						// we're within the margin (10s) if x + margin >= y
						return !t1.Time.Add(10 * time.Second).Before(t2.Time)
					}),
				}
				if diff := cmp.Diff(gotDiffed, tt.wantDiffedResourcePlacements, opt...); diff != "" {
					t.Errorf("setBindingStatus got DiffedPlacements mismatch (-got +want):\n%s", diff)
				}
				return
			}

			resourceCmpOptions = []cmp.Option{
				cmpopts.SortSlices(utils.LessFuncDiffedResourcePlacements),
			}
			if diff := cmp.Diff(gotDiffed, tt.wantDiffedResourcePlacements, resourceCmpOptions...); diff != "" {
				t.Errorf("setBindingStatus got DiffedPlacements mismatch (-got +want):\n%s", diff)
			}
		})
	}
}

func TestExtractFailedResourcePlacementsFromWork(t *testing.T) {
	var statusCmpOptions = []cmp.Option{
		// ignore the message as we may change the message in the future
		cmpopts.IgnoreFields(metav1.Condition{}, "Message"),
		cmpopts.SortSlices(func(c1, c2 metav1.Condition) bool {
			return c1.Type < c2.Type
		}),
		cmpopts.SortSlices(func(s1, s2 string) bool {
			return s1 < s2
		}),
		cmpopts.SortSlices(func(n1, n2 fleetv1beta1.NamespacedName) bool {
			if n1.Namespace == n2.Namespace {
				return n1.Name < n2.Name
			}
			return n1.Namespace < n2.Namespace
		}),
		cmpopts.SortSlices(func(f1, f2 fleetv1beta1.FailedResourcePlacement) bool {
			return f1.ResourceIdentifier.Kind < f2.ResourceIdentifier.Kind
		}),
		cmp.Comparer(func(t1, t2 metav1.Time) bool {
			if t1.Time.IsZero() || t2.Time.IsZero() {
				return true // treat them as equal
			}
			if t1.Time.After(t2.Time) {
				t1, t2 = t2, t1 // ensure t1 is always before t2
			}
			// we're within the margin (10s) if x + margin >= y
			return !t1.Time.Add(10 * time.Second).Before(t2.Time)
		}),
	}
	workGeneration := int64(12)
	tests := []struct {
		name string
		work fleetv1beta1.Work
		want []fleetv1beta1.FailedResourcePlacement
	}{
		{
			name: "apply is true and available is false",
			work: fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Generation: workGeneration,
				},
				Status: fleetv1beta1.WorkStatus{
					ManifestConditions: []fleetv1beta1.ManifestCondition{
						{
							Identifier: fleetv1beta1.WorkResourceIdentifier{
								Ordinal:   0,
								Group:     "",
								Version:   "v1",
								Kind:      "ConfigMap",
								Name:      "config-name",
								Namespace: "config-namespace",
							},
							Conditions: []metav1.Condition{
								{
									Type:   fleetv1beta1.WorkConditionTypeApplied,
									Status: metav1.ConditionTrue,
								},
								{
									Type:   fleetv1beta1.WorkConditionTypeAvailable,
									Status: metav1.ConditionFalse,
								},
							},
						},
						{
							Identifier: fleetv1beta1.WorkResourceIdentifier{
								Ordinal:   1,
								Group:     "",
								Version:   "v1",
								Kind:      "Service",
								Name:      "svc-name",
								Namespace: "svc-namespace",
							},
							Conditions: []metav1.Condition{
								{
									Type:   fleetv1beta1.WorkConditionTypeApplied,
									Status: metav1.ConditionTrue,
								},
								{
									Type:   fleetv1beta1.WorkConditionTypeAvailable,
									Status: metav1.ConditionTrue,
								},
							},
						},
					},
					Conditions: []metav1.Condition{
						{
							Type:               fleetv1beta1.WorkConditionTypeApplied,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: workGeneration,
						},
						{
							Type:               fleetv1beta1.WorkConditionTypeAvailable,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: workGeneration,
						},
					},
				},
			},
			want: []fleetv1beta1.FailedResourcePlacement{
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     "",
						Version:   "v1",
						Kind:      "ConfigMap",
						Name:      "config-name",
						Namespace: "config-namespace",
					},
					Condition: metav1.Condition{
						Type:   fleetv1beta1.WorkConditionTypeAvailable,
						Status: metav1.ConditionFalse,
					},
				},
			},
		},
		{
			name: "apply is true and available is false for enveloped object",
			work: fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Generation: workGeneration,
					Labels: map[string]string{
						fleetv1beta1.EnvelopeNameLabel:      "test-env",
						fleetv1beta1.EnvelopeNamespaceLabel: "test-env-ns",
						fleetv1beta1.EnvelopeTypeLabel:      "pod",
					},
				},
				Status: fleetv1beta1.WorkStatus{
					ManifestConditions: []fleetv1beta1.ManifestCondition{
						{
							Identifier: fleetv1beta1.WorkResourceIdentifier{
								Ordinal:   0,
								Group:     "",
								Version:   "v1",
								Kind:      "ConfigMap",
								Name:      "config-name",
								Namespace: "config-namespace",
							},
							Conditions: []metav1.Condition{
								{
									Type:   fleetv1beta1.WorkConditionTypeApplied,
									Status: metav1.ConditionTrue,
								},
								{
									Type:   fleetv1beta1.WorkConditionTypeAvailable,
									Status: metav1.ConditionFalse,
								},
							},
						},
					},
					Conditions: []metav1.Condition{
						{
							Type:               fleetv1beta1.WorkConditionTypeApplied,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: workGeneration,
						},
						{
							Type:               fleetv1beta1.WorkConditionTypeAvailable,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: workGeneration,
						},
					},
				},
			},
			want: []fleetv1beta1.FailedResourcePlacement{
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     "",
						Version:   "v1",
						Kind:      "ConfigMap",
						Name:      "config-name",
						Namespace: "config-namespace",
						Envelope: &fleetv1beta1.EnvelopeIdentifier{
							Name:      "test-env",
							Namespace: "test-env-ns",
							Type:      "pod",
						},
					},
					Condition: metav1.Condition{
						Type:   fleetv1beta1.WorkConditionTypeAvailable,
						Status: metav1.ConditionFalse,
					},
				},
			},
		},
		{
			name: "both conditions are true",
			work: fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Generation: workGeneration,
				},
				Status: fleetv1beta1.WorkStatus{
					Conditions: []metav1.Condition{
						{
							Type:               fleetv1beta1.WorkConditionTypeApplied,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: workGeneration,
						},
						{
							Type:               fleetv1beta1.WorkConditionTypeAvailable,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: workGeneration,
						},
					},
				},
			},
			want: nil,
		},
		{
			name: "apply is true and available is unknown",
			work: fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Generation: workGeneration,
				},
				Status: fleetv1beta1.WorkStatus{
					Conditions: []metav1.Condition{
						{
							Type:               fleetv1beta1.WorkConditionTypeApplied,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: workGeneration,
						},
					},
				},
			},
			want: nil,
		},
		{
			name: "applied is false but not for the latest work",
			work: fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Generation: workGeneration,
				},
				Status: fleetv1beta1.WorkStatus{
					Conditions: []metav1.Condition{
						{
							Type:               fleetv1beta1.WorkConditionTypeApplied,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: workGeneration - 1,
						},
					},
				},
			},
			want: nil,
		},
		{
			name: "apply is false",
			work: fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Generation: workGeneration,
				},
				Status: fleetv1beta1.WorkStatus{
					ManifestConditions: []fleetv1beta1.ManifestCondition{
						{
							Identifier: fleetv1beta1.WorkResourceIdentifier{
								Ordinal:   0,
								Group:     "",
								Version:   "v1",
								Kind:      "ConfigMap",
								Name:      "config-name",
								Namespace: "config-namespace",
							},
							Conditions: []metav1.Condition{
								{
									Type:   fleetv1beta1.WorkConditionTypeApplied,
									Status: metav1.ConditionFalse,
								},
							},
						},
					},
					Conditions: []metav1.Condition{
						{
							Type:               fleetv1beta1.WorkConditionTypeApplied,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: workGeneration,
						},
					},
				},
			},
			want: []fleetv1beta1.FailedResourcePlacement{
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     "",
						Version:   "v1",
						Kind:      "ConfigMap",
						Name:      "config-name",
						Namespace: "config-namespace",
					},
					Condition: metav1.Condition{
						Type:   fleetv1beta1.WorkConditionTypeApplied,
						Status: metav1.ConditionFalse,
					},
				},
			},
		},
		{
			name: "apply is false for enveloped object",
			work: fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Generation: workGeneration,
					Labels: map[string]string{
						fleetv1beta1.EnvelopeNameLabel:      "test-env",
						fleetv1beta1.EnvelopeNamespaceLabel: "test-env-ns",
						fleetv1beta1.EnvelopeTypeLabel:      "pod",
					},
				},
				Status: fleetv1beta1.WorkStatus{
					ManifestConditions: []fleetv1beta1.ManifestCondition{
						{
							Identifier: fleetv1beta1.WorkResourceIdentifier{
								Ordinal:   0,
								Group:     "",
								Version:   "v1",
								Kind:      "ConfigMap",
								Name:      "config-name",
								Namespace: "config-namespace",
							},
							Conditions: []metav1.Condition{
								{
									Type:   fleetv1beta1.WorkConditionTypeApplied,
									Status: metav1.ConditionFalse,
								},
							},
						},
					},
					Conditions: []metav1.Condition{
						{
							Type:               fleetv1beta1.WorkConditionTypeApplied,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: workGeneration,
						},
					},
				},
			},
			want: []fleetv1beta1.FailedResourcePlacement{
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     "",
						Version:   "v1",
						Kind:      "ConfigMap",
						Name:      "config-name",
						Namespace: "config-namespace",
						Envelope: &fleetv1beta1.EnvelopeIdentifier{
							Name:      "test-env",
							Namespace: "test-env-ns",
							Type:      "pod",
						},
					},
					Condition: metav1.Condition{
						Type:   fleetv1beta1.WorkConditionTypeApplied,
						Status: metav1.ConditionFalse,
					},
				},
			},
		},
		{
			name: "apply condition is unknown",
			work: fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Generation: workGeneration,
				},
				Status: fleetv1beta1.WorkStatus{
					Conditions: []metav1.Condition{
						{
							Type:               fleetv1beta1.WorkConditionTypeApplied,
							Status:             metav1.ConditionUnknown,
							ObservedGeneration: workGeneration,
						},
					},
				},
			},
			want: nil,
		},
		{
			name: "multiple manifests in the failed work",
			work: fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Generation: workGeneration,
				},
				Status: fleetv1beta1.WorkStatus{
					ManifestConditions: []fleetv1beta1.ManifestCondition{
						{
							Identifier: fleetv1beta1.WorkResourceIdentifier{
								Ordinal:   0,
								Group:     "",
								Version:   "v1",
								Kind:      "ConfigMap",
								Name:      "config-name",
								Namespace: "config-namespace",
							},
							Conditions: []metav1.Condition{
								{
									Type:   fleetv1beta1.WorkConditionTypeApplied,
									Status: metav1.ConditionTrue,
								},
								{
									Type:   fleetv1beta1.WorkConditionTypeAvailable,
									Status: metav1.ConditionFalse,
								},
							},
						},
						{
							Identifier: fleetv1beta1.WorkResourceIdentifier{
								Ordinal:   1,
								Group:     "",
								Version:   "v1",
								Kind:      "Service",
								Name:      "svc-name",
								Namespace: "svc-namespace",
							},
							Conditions: []metav1.Condition{
								{
									Type:   fleetv1beta1.WorkConditionTypeApplied,
									Status: metav1.ConditionTrue,
								},
								{
									Type:   fleetv1beta1.WorkConditionTypeAvailable,
									Status: metav1.ConditionFalse,
								},
							},
						},
					},
					Conditions: []metav1.Condition{
						{
							Type:               fleetv1beta1.WorkConditionTypeApplied,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: workGeneration,
						},
						{
							Type:               fleetv1beta1.WorkConditionTypeAvailable,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: workGeneration,
						},
					},
				},
			},
			want: []fleetv1beta1.FailedResourcePlacement{
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     "",
						Version:   "v1",
						Kind:      "ConfigMap",
						Name:      "config-name",
						Namespace: "config-namespace",
					},
					Condition: metav1.Condition{
						Type:   fleetv1beta1.WorkConditionTypeAvailable,
						Status: metav1.ConditionFalse,
					},
				},
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     "",
						Version:   "v1",
						Kind:      "Service",
						Name:      "svc-name",
						Namespace: "svc-namespace",
					},
					Condition: metav1.Condition{
						Type:   fleetv1beta1.WorkConditionTypeAvailable,
						Status: metav1.ConditionFalse,
					},
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extractFailedResourcePlacementsFromWork(&tc.work)
			if diff := cmp.Diff(tc.want, got, statusCmpOptions...); diff != "" {
				t.Errorf("extractFailedResourcePlacementsFromWork() status mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestExtractDriftedResourcePlacementsFromWork(t *testing.T) {
	var options = []cmp.Option{
		cmpopts.SortSlices(func(s1, s2 string) bool {
			return s1 < s2
		}),
		cmpopts.SortSlices(func(n1, n2 fleetv1beta1.NamespacedName) bool {
			if n1.Namespace == n2.Namespace {
				return n1.Name < n2.Name
			}
			return n1.Namespace < n2.Namespace
		}),
		cmpopts.SortSlices(func(f1, f2 fleetv1beta1.DriftedResourcePlacement) bool {
			return f1.ResourceIdentifier.Kind < f2.ResourceIdentifier.Kind
		}),
		cmp.Comparer(func(t1, t2 metav1.Time) bool {
			if t1.Time.IsZero() || t2.Time.IsZero() {
				return true // treat them as equal
			}
			if t1.Time.After(t2.Time) {
				t1, t2 = t2, t1 // ensure t1 is always before t2
			}
			// we're within the margin (10s) if x + margin >= y
			return !t1.Time.Add(10 * time.Second).Before(t2.Time)
		}),
	}
	timeNow := time.Now()
	workGeneration := int64(12)
	tests := []struct {
		name string
		work fleetv1beta1.Work
		want []fleetv1beta1.DriftedResourcePlacement
	}{
		{
			name: "work with drifted details",
			work: fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Generation: workGeneration,
				},
				Status: fleetv1beta1.WorkStatus{
					ManifestConditions: []fleetv1beta1.ManifestCondition{
						{
							Identifier: fleetv1beta1.WorkResourceIdentifier{
								Ordinal:   0,
								Group:     "",
								Version:   "v1",
								Kind:      "ConfigMap",
								Name:      "config-name",
								Namespace: "config-namespace",
							},
							Conditions: []metav1.Condition{
								{
									Type:   fleetv1beta1.WorkConditionTypeApplied,
									Status: metav1.ConditionTrue,
								},
								{
									Type:   fleetv1beta1.WorkConditionTypeAvailable,
									Status: metav1.ConditionFalse,
								},
							},
						},
						{
							Identifier: fleetv1beta1.WorkResourceIdentifier{
								Ordinal:   1,
								Group:     "",
								Version:   "v1",
								Kind:      "Service",
								Name:      "svc-name",
								Namespace: "svc-namespace",
							},
							Conditions: []metav1.Condition{
								{
									Type:   fleetv1beta1.WorkConditionTypeApplied,
									Status: metav1.ConditionTrue,
								},
								{
									Type:   fleetv1beta1.WorkConditionTypeAvailable,
									Status: metav1.ConditionTrue,
								},
							},
							DriftDetails: &fleetv1beta1.DriftDetails{
								ObservationTime:                   metav1.NewTime(timeNow),
								ObservedInMemberClusterGeneration: 12,
								FirstDriftedObservedTime:          metav1.NewTime(timeNow.Add(-time.Hour)),
								ObservedDrifts: []fleetv1beta1.PatchDetail{
									{
										Path:          "/spec/ports/1/containerPort",
										ValueInHub:    "80",
										ValueInMember: "90",
									},
								},
							},
						},
					},
					Conditions: []metav1.Condition{
						{
							Type:               fleetv1beta1.WorkConditionTypeApplied,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: workGeneration,
						},
						{
							Type:               fleetv1beta1.WorkConditionTypeAvailable,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: workGeneration,
						},
					},
				},
			},
			want: []fleetv1beta1.DriftedResourcePlacement{
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     "",
						Version:   "v1",
						Kind:      "Service",
						Name:      "svc-name",
						Namespace: "svc-namespace",
					},
					ObservationTime:                 metav1.NewTime(timeNow),
					TargetClusterObservedGeneration: 12,
					FirstDriftedObservedTime:        metav1.NewTime(timeNow.Add(-time.Hour)),
					ObservedDrifts: []fleetv1beta1.PatchDetail{
						{
							Path:          "/spec/ports/1/containerPort",
							ValueInHub:    "80",
							ValueInMember: "90",
						},
					},
				},
			},
		},
		{
			name: "work with no drift details",
			work: fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Generation: workGeneration,
				},
				Status: fleetv1beta1.WorkStatus{
					ManifestConditions: []fleetv1beta1.ManifestCondition{
						{
							Identifier: fleetv1beta1.WorkResourceIdentifier{
								Ordinal:   1,
								Group:     "",
								Version:   "v1",
								Kind:      "Service",
								Name:      "svc-name",
								Namespace: "svc-namespace",
							},
							Conditions: []metav1.Condition{
								{
									Type:   fleetv1beta1.WorkConditionTypeApplied,
									Status: metav1.ConditionTrue,
								},
								{
									Type:   fleetv1beta1.WorkConditionTypeAvailable,
									Status: metav1.ConditionTrue,
								},
							},
						},
					},
					Conditions: []metav1.Condition{
						{
							Type:               fleetv1beta1.WorkConditionTypeApplied,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: workGeneration,
						},
						{
							Type:               fleetv1beta1.WorkConditionTypeAvailable,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: workGeneration,
						},
					},
				},
			},
			want: []fleetv1beta1.DriftedResourcePlacement{},
		},
		{
			name: "work with enveloped object drifted details",
			work: fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Generation: workGeneration,
					Labels: map[string]string{
						fleetv1beta1.EnvelopeNameLabel:      "test-env",
						fleetv1beta1.EnvelopeNamespaceLabel: "test-env-ns",
						fleetv1beta1.EnvelopeTypeLabel:      "pod",
					},
				},
				Status: fleetv1beta1.WorkStatus{
					ManifestConditions: []fleetv1beta1.ManifestCondition{
						{
							Identifier: fleetv1beta1.WorkResourceIdentifier{
								Ordinal:   0,
								Group:     "",
								Version:   "v1",
								Kind:      "ConfigMap",
								Name:      "config-name",
								Namespace: "config-namespace",
							},
							DriftDetails: &fleetv1beta1.DriftDetails{
								ObservationTime:                   metav1.NewTime(timeNow),
								ObservedInMemberClusterGeneration: 12,
								FirstDriftedObservedTime:          metav1.NewTime(timeNow.Add(-time.Hour)),
								ObservedDrifts: []fleetv1beta1.PatchDetail{
									{
										Path:          "/spec/containers/0/image",
										ValueInHub:    "nginx:1.19",
										ValueInMember: "nginx:1.20",
									},
								},
							},
							Conditions: []metav1.Condition{
								{
									Type:   fleetv1beta1.WorkConditionTypeApplied,
									Status: metav1.ConditionTrue,
								},
								{
									Type:   fleetv1beta1.WorkConditionTypeAvailable,
									Status: metav1.ConditionFalse,
								},
							},
						},
					},
					Conditions: []metav1.Condition{
						{
							Type:               fleetv1beta1.WorkConditionTypeApplied,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: workGeneration,
						},
						{
							Type:               fleetv1beta1.WorkConditionTypeAvailable,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: workGeneration,
						},
					},
				},
			},
			want: []fleetv1beta1.DriftedResourcePlacement{
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     "",
						Version:   "v1",
						Kind:      "ConfigMap",
						Name:      "config-name",
						Namespace: "config-namespace",
						Envelope: &fleetv1beta1.EnvelopeIdentifier{
							Name:      "test-env",
							Namespace: "test-env-ns",
							Type:      "pod",
						},
					},
					ObservationTime:                 metav1.NewTime(timeNow),
					TargetClusterObservedGeneration: 12,
					FirstDriftedObservedTime:        metav1.NewTime(timeNow.Add(-time.Hour)),
					ObservedDrifts: []fleetv1beta1.PatchDetail{
						{
							Path:          "/spec/containers/0/image",
							ValueInHub:    "nginx:1.19",
							ValueInMember: "nginx:1.20",
						},
					},
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extractDriftedResourcePlacementsFromWork(&tc.work)
			if diff := cmp.Diff(tc.want, got, options...); diff != "" {
				t.Errorf("extractDriftedResourcePlacementsFromWork() status mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestExtractDiffedResourcePlacementsFromWork(t *testing.T) {
	var options = []cmp.Option{
		cmpopts.SortSlices(func(s1, s2 string) bool {
			return s1 < s2
		}),
		cmpopts.SortSlices(func(n1, n2 fleetv1beta1.NamespacedName) bool {
			if n1.Namespace == n2.Namespace {
				return n1.Name < n2.Name
			}
			return n1.Namespace < n2.Namespace
		}),
		cmpopts.SortSlices(func(f1, f2 fleetv1beta1.DiffedResourcePlacement) bool {
			return f1.ResourceIdentifier.Kind < f2.ResourceIdentifier.Kind
		}),
		cmp.Comparer(func(t1, t2 metav1.Time) bool {
			if t1.Time.IsZero() || t2.Time.IsZero() {
				return true // treat them as equal
			}
			if t1.Time.After(t2.Time) {
				t1, t2 = t2, t1 // ensure t1 is always before t2
			}
			// we're within the margin (10s) if x + margin >= y
			return !t1.Time.Add(10 * time.Second).Before(t2.Time)
		}),
	}
	timeNow := time.Now()
	workGeneration := int64(12)
	tests := []struct {
		name string
		work fleetv1beta1.Work
		want []fleetv1beta1.DiffedResourcePlacement
	}{
		{
			name: "work with diffed details",
			work: fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Generation: workGeneration,
				},
				Status: fleetv1beta1.WorkStatus{
					ManifestConditions: []fleetv1beta1.ManifestCondition{
						{
							Identifier: fleetv1beta1.WorkResourceIdentifier{
								Ordinal:   0,
								Group:     "",
								Version:   "v1",
								Kind:      "ConfigMap",
								Name:      "config-name",
								Namespace: "config-namespace",
							},
							Conditions: []metav1.Condition{
								{
									Type:   fleetv1beta1.WorkConditionTypeApplied,
									Status: metav1.ConditionTrue,
								},
								{
									Type:   fleetv1beta1.WorkConditionTypeAvailable,
									Status: metav1.ConditionFalse,
								},
							},
						},
						{
							Identifier: fleetv1beta1.WorkResourceIdentifier{
								Ordinal:   1,
								Group:     "",
								Version:   "v1",
								Kind:      "Service",
								Name:      "svc-name",
								Namespace: "svc-namespace",
							},
							Conditions: []metav1.Condition{
								{
									Type:   fleetv1beta1.WorkConditionTypeApplied,
									Status: metav1.ConditionTrue,
								},
								{
									Type:   fleetv1beta1.WorkConditionTypeAvailable,
									Status: metav1.ConditionTrue,
								},
							},
							DiffDetails: &fleetv1beta1.DiffDetails{
								ObservationTime:                   metav1.NewTime(timeNow),
								ObservedInMemberClusterGeneration: ptr.To(int64(12)),
								FirstDiffedObservedTime:           metav1.NewTime(timeNow.Add(-time.Hour)),
								ObservedDiffs: []fleetv1beta1.PatchDetail{
									{
										Path:          "/spec/ports/1/containerPort",
										ValueInHub:    "80",
										ValueInMember: "90",
									},
								},
							},
						},
					},
					Conditions: []metav1.Condition{
						{
							Type:               fleetv1beta1.WorkConditionTypeApplied,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: workGeneration,
						},
						{
							Type:               fleetv1beta1.WorkConditionTypeAvailable,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: workGeneration,
						},
					},
				},
			},
			want: []fleetv1beta1.DiffedResourcePlacement{
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     "",
						Version:   "v1",
						Kind:      "Service",
						Name:      "svc-name",
						Namespace: "svc-namespace",
					},
					ObservationTime:                 metav1.NewTime(timeNow),
					TargetClusterObservedGeneration: ptr.To(int64(12)),
					FirstDiffedObservedTime:         metav1.NewTime(timeNow.Add(-time.Hour)),
					ObservedDiffs: []fleetv1beta1.PatchDetail{
						{
							Path:          "/spec/ports/1/containerPort",
							ValueInHub:    "80",
							ValueInMember: "90",
						},
					},
				},
			},
		},
		{
			name: "work with no diff details",
			work: fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Generation: workGeneration,
				},
				Status: fleetv1beta1.WorkStatus{
					ManifestConditions: []fleetv1beta1.ManifestCondition{
						{
							Identifier: fleetv1beta1.WorkResourceIdentifier{
								Ordinal:   1,
								Group:     "",
								Version:   "v1",
								Kind:      "Service",
								Name:      "svc-name",
								Namespace: "svc-namespace",
							},
							Conditions: []metav1.Condition{
								{
									Type:   fleetv1beta1.WorkConditionTypeApplied,
									Status: metav1.ConditionTrue,
								},
								{
									Type:   fleetv1beta1.WorkConditionTypeAvailable,
									Status: metav1.ConditionTrue,
								},
							},
						},
					},
					Conditions: []metav1.Condition{
						{
							Type:               fleetv1beta1.WorkConditionTypeApplied,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: workGeneration,
						},
						{
							Type:               fleetv1beta1.WorkConditionTypeAvailable,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: workGeneration,
						},
					},
				},
			},
			want: []fleetv1beta1.DiffedResourcePlacement{},
		},
		{
			name: "work with enveloped object diffed details",
			work: fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Generation: workGeneration,
					Labels: map[string]string{
						fleetv1beta1.EnvelopeNameLabel:      "test-env",
						fleetv1beta1.EnvelopeNamespaceLabel: "test-env-ns",
						fleetv1beta1.EnvelopeTypeLabel:      "pod",
					},
				},
				Status: fleetv1beta1.WorkStatus{
					ManifestConditions: []fleetv1beta1.ManifestCondition{
						{
							Identifier: fleetv1beta1.WorkResourceIdentifier{
								Ordinal:   0,
								Group:     "",
								Version:   "v1",
								Kind:      "ConfigMap",
								Name:      "config-name",
								Namespace: "config-namespace",
							},
							DiffDetails: &fleetv1beta1.DiffDetails{
								ObservationTime:                   metav1.NewTime(timeNow),
								ObservedInMemberClusterGeneration: ptr.To(int64(12)),
								FirstDiffedObservedTime:           metav1.NewTime(timeNow.Add(-time.Hour)),
								ObservedDiffs: []fleetv1beta1.PatchDetail{
									{
										Path:          "/spec/containers/0/image",
										ValueInHub:    "nginx:1.19",
										ValueInMember: "nginx:1.20",
									},
								},
							},
							Conditions: []metav1.Condition{
								{
									Type:   fleetv1beta1.WorkConditionTypeApplied,
									Status: metav1.ConditionTrue,
								},
								{
									Type:   fleetv1beta1.WorkConditionTypeAvailable,
									Status: metav1.ConditionFalse,
								},
							},
						},
					},
					Conditions: []metav1.Condition{
						{
							Type:               fleetv1beta1.WorkConditionTypeApplied,
							Status:             metav1.ConditionTrue,
							ObservedGeneration: workGeneration,
						},
						{
							Type:               fleetv1beta1.WorkConditionTypeAvailable,
							Status:             metav1.ConditionFalse,
							ObservedGeneration: workGeneration,
						},
					},
				},
			},
			want: []fleetv1beta1.DiffedResourcePlacement{
				{
					ResourceIdentifier: fleetv1beta1.ResourceIdentifier{
						Group:     "",
						Version:   "v1",
						Kind:      "ConfigMap",
						Name:      "config-name",
						Namespace: "config-namespace",
						Envelope: &fleetv1beta1.EnvelopeIdentifier{
							Name:      "test-env",
							Namespace: "test-env-ns",
							Type:      "pod",
						},
					},
					ObservationTime:                 metav1.NewTime(timeNow),
					TargetClusterObservedGeneration: ptr.To(int64(12)),
					FirstDiffedObservedTime:         metav1.NewTime(timeNow.Add(-time.Hour)),
					ObservedDiffs: []fleetv1beta1.PatchDetail{
						{
							Path:          "/spec/containers/0/image",
							ValueInHub:    "nginx:1.19",
							ValueInMember: "nginx:1.20",
						},
					},
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extractDiffedResourcePlacementsFromWork(&tc.work)
			if diff := cmp.Diff(tc.want, got, options...); diff != "" {
				t.Errorf("extractDiffedResourcePlacementsFromWork() status mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestUpdateBindingStatusWithRetry(t *testing.T) {
	lastTransitionTime := metav1.NewTime(time.Now())
	tests := []struct {
		name            string
		latestBinding   *fleetv1beta1.ClusterResourceBinding
		resourceBinding *fleetv1beta1.ClusterResourceBinding
		conflictCount   int
		expectError     bool
	}{
		// fakeClient checks to see ResourceVersion is set and the same in order to update.
		// (https://github.com/kubernetes-sigs/controller-runtime/blob/b901db121e1f53c47ec9f9683fad90a546688c3e/pkg/client/fake/client.go#L478)
		// If not set, fake client sets ResourceVersion to "999", so it leads them to not having the same resource version.
		// (https://github.com/kubernetes-sigs/controller-runtime/blob/b901db121e1f53c47ec9f9683fad90a546688c3e/pkg/client/fake/client.go#L289)

		{
			name: "update status successfully with no conflict",
			latestBinding: &fleetv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "test-binding-1",
					Generation:      4,
					ResourceVersion: "4",
				},
				Spec: fleetv1beta1.ResourceBindingSpec{
					State:                fleetv1beta1.BindingStateBound,
					TargetCluster:        "cluster-1",
					ResourceSnapshotName: "snapshot-1",
				},
				Status: fleetv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 4,
							Reason:             condition.RolloutStartedReason,
							LastTransitionTime: lastTransitionTime,
						},
					},
				},
			},
			resourceBinding: &fleetv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "test-binding-1",
					Generation:      4,
					ResourceVersion: "4",
				},
				Spec: fleetv1beta1.ResourceBindingSpec{
					State:                fleetv1beta1.BindingStateBound,
					TargetCluster:        "cluster-1",
					ResourceSnapshotName: "snapshot-1",
				},
				Status: fleetv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 4,
							Reason:             condition.RolloutStartedReason,
							LastTransitionTime: lastTransitionTime,
						},
						{
							Type:               string(fleetv1beta1.ResourceBindingOverridden),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 4,
							Reason:             condition.OverriddenSucceededReason,
						},
						{
							Type:               string(fleetv1beta1.ResourceBindingWorkSynchronized),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 4,
							Reason:             condition.AllWorkSyncedReason,
						},
						{
							Type:               string(fleetv1beta1.ResourceBindingApplied),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 4,
							Reason:             condition.AllWorkAppliedReason,
						},
						{
							Type:               string(fleetv1beta1.ResourceBindingAvailable),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 4,
							Reason:             condition.AllWorkAvailableReason,
						},
					},
				},
			},
			conflictCount: 0,
			expectError:   false,
		},
		{
			name: "update status after conflict",
			latestBinding: &fleetv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "test-binding-2",
					Generation:      3,
					ResourceVersion: "3",
				},
				Spec: fleetv1beta1.ResourceBindingSpec{
					State:                fleetv1beta1.BindingStateBound,
					TargetCluster:        "cluster-1",
					ResourceSnapshotName: "snapshot-1",
				},
				Status: fleetv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 2,
							Reason:             condition.RolloutNotStartedYetReason,
							LastTransitionTime: lastTransitionTime,
						},
					},
				},
			},
			resourceBinding: &fleetv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "test-binding-2",
					Generation:      3,
					ResourceVersion: "3",
				},
				Spec: fleetv1beta1.ResourceBindingSpec{
					State:                fleetv1beta1.BindingStateBound,
					TargetCluster:        "cluster-1",
					ResourceSnapshotName: "snapshot-1",
				},
				Status: fleetv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 2,
							Reason:             condition.RolloutNotStartedYetReason,
							LastTransitionTime: metav1.NewTime(lastTransitionTime.Add(-15 * time.Second)),
						},
						{
							Type:               string(fleetv1beta1.ResourceBindingOverridden),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 3,
							Reason:             condition.OverriddenSucceededReason,
						},
						{
							Type:               string(fleetv1beta1.ResourceBindingWorkSynchronized),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 3,
							Reason:             condition.AllWorkSyncedReason,
						},
						{
							Type:               string(fleetv1beta1.ResourceBindingApplied),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 3,
							Reason:             condition.AllWorkAppliedReason,
						},
						{
							Type:               string(fleetv1beta1.ResourceBindingAvailable),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 3,
							Reason:             condition.AllWorkAvailableReason,
						},
					},
				},
			},
			conflictCount: 1,
			expectError:   false,
		},
		{
			name: "does not update status because of conflict",
			latestBinding: &fleetv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "test-binding-3",
					Generation:      3,
					ResourceVersion: "3",
				},
				Spec: fleetv1beta1.ResourceBindingSpec{
					State:                fleetv1beta1.BindingStateBound,
					TargetCluster:        "cluster-1",
					ResourceSnapshotName: "snapshot-1",
				},
				Status: fleetv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{
						{
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 3,
							Reason:             condition.RolloutNotStartedYetReason,
							LastTransitionTime: lastTransitionTime,
						},
					},
				},
			},
			resourceBinding: &fleetv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "test-binding-3",
					Generation:      3,
					ResourceVersion: "3",
				},
				Spec: fleetv1beta1.ResourceBindingSpec{
					State:                fleetv1beta1.BindingStateBound,
					TargetCluster:        "cluster-1",
					ResourceSnapshotName: "snapshot-1",
				},
				Status: fleetv1beta1.ResourceBindingStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(fleetv1beta1.ResourceBindingRolloutStarted),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 2,
							Reason:             condition.RolloutStartedReason,
							LastTransitionTime: metav1.NewTime(lastTransitionTime.Add(-10 * time.Second)),
						},
						{
							Type:               string(fleetv1beta1.ResourceBindingOverridden),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 3,
							Reason:             condition.OverriddenSucceededReason,
						},
						{
							Type:               string(fleetv1beta1.ResourceBindingWorkSynchronized),
							Status:             metav1.ConditionTrue,
							ObservedGeneration: 3,
							Reason:             condition.AllWorkSyncedReason,
						},
						{
							Type:               string(fleetv1beta1.ResourceBindingApplied),
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 3,
							Reason:             condition.WorkApplyInProcess,
						},
					},
				},
			},
			conflictCount: 10,
			expectError:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			scheme := serviceScheme(t)
			objects := []client.Object{tt.latestBinding}
			fakeClient := fake.NewClientBuilder().
				WithStatusSubresource(objects...).
				WithScheme(scheme).
				WithObjects(objects...).
				Build()

			conflictClient := &conflictClient{
				Client:        fakeClient,
				conflictCount: tt.conflictCount,
			}
			// Create reconciler with custom client
			r := &Reconciler{
				Client:          conflictClient,
				recorder:        record.NewFakeRecorder(10),
				InformerManager: &informer.FakeManager{},
			}
			err := r.updateBindingStatusWithRetry(ctx, tt.resourceBinding)
			if (err != nil) != tt.expectError {
				t.Errorf("updateBindingStatusWithRetry() error = %v, wantErr %v", err, tt.expectError)
			}
			updatedBinding := &fleetv1beta1.ClusterResourceBinding{}
			if err := r.Client.Get(ctx, client.ObjectKeyFromObject(tt.resourceBinding), updatedBinding); err != nil {
				t.Errorf("updateBindingStatusWithRetry() error = %v, wantErr %v", err, nil)
			}
			if !tt.expectError {
				if len(updatedBinding.Status.Conditions) < 1 {
					t.Errorf("updateBindingStatusWithRetry() did not update binding")
				}
				latestRollout := tt.latestBinding.GetCondition(string(fleetv1beta1.ResourceBindingRolloutStarted))
				rollout := updatedBinding.GetCondition(string(fleetv1beta1.ResourceBindingRolloutStarted))
				// Check that the rolloutStarted condition is updated with the same values from tt.latestBinding
				if diff := cmp.Diff(latestRollout, rollout, statusCmpOptions...); diff != "" {
					t.Errorf("updateBindingStatusWithRetry() ResourceBindingRolloutStarted Condition got = %v, want %v", rollout, latestRollout)
				}
			}
		})
	}
}

func TestSyncApplyStrategy(t *testing.T) {
	bindingName := "test-binding-1"
	workName := "test-work-1"

	testCases := []struct {
		name              string
		resourceBinding   *fleetv1beta1.ClusterResourceBinding
		work              *fleetv1beta1.Work
		wantUpdated       bool
		wantApplyStrategy *fleetv1beta1.ApplyStrategy
	}{
		{
			name: "same apply strategy",
			resourceBinding: &fleetv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: bindingName,
				},
				Spec: fleetv1beta1.ResourceBindingSpec{},
			},
			work: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name: workName,
				},
				Spec: fleetv1beta1.WorkSpec{},
			},
		},
		{
			name: "apply strategy changed (work has default apply strategy)",
			resourceBinding: &fleetv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: bindingName,
				},
				Spec: fleetv1beta1.ResourceBindingSpec{
					ApplyStrategy: &fleetv1beta1.ApplyStrategy{
						ComparisonOption: fleetv1beta1.ComparisonOptionTypeFullComparison,
						Type:             fleetv1beta1.ApplyStrategyTypeServerSideApply,
						WhenToApply:      fleetv1beta1.WhenToApplyTypeIfNotDrifted,
						WhenToTakeOver:   fleetv1beta1.WhenToTakeOverTypeNever,
					},
				},
			},
			work: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name: workName,
				},
				Spec: fleetv1beta1.WorkSpec{},
			},
			wantUpdated: true,
			wantApplyStrategy: &fleetv1beta1.ApplyStrategy{
				ComparisonOption: fleetv1beta1.ComparisonOptionTypeFullComparison,
				Type:             fleetv1beta1.ApplyStrategyTypeServerSideApply,
				WhenToApply:      fleetv1beta1.WhenToApplyTypeIfNotDrifted,
				WhenToTakeOver:   fleetv1beta1.WhenToTakeOverTypeNever,
			},
		},
		{
			name: "apply strategy changed (work has custom apply strategy)",
			resourceBinding: &fleetv1beta1.ClusterResourceBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: bindingName,
				},
				Spec: fleetv1beta1.ResourceBindingSpec{
					ApplyStrategy: &fleetv1beta1.ApplyStrategy{
						ComparisonOption: fleetv1beta1.ComparisonOptionTypeFullComparison,
						Type:             fleetv1beta1.ApplyStrategyTypeServerSideApply,
						WhenToApply:      fleetv1beta1.WhenToApplyTypeIfNotDrifted,
						WhenToTakeOver:   fleetv1beta1.WhenToTakeOverTypeNever,
					},
				},
			},
			work: &fleetv1beta1.Work{
				ObjectMeta: metav1.ObjectMeta{
					Name: workName,
				},
				Spec: fleetv1beta1.WorkSpec{
					ApplyStrategy: &fleetv1beta1.ApplyStrategy{
						ComparisonOption: fleetv1beta1.ComparisonOptionTypePartialComparison,
						Type:             fleetv1beta1.ApplyStrategyTypeReportDiff,
						WhenToTakeOver:   fleetv1beta1.WhenToTakeOverTypeNever,
					},
				},
			},
			wantUpdated: true,
			wantApplyStrategy: &fleetv1beta1.ApplyStrategy{
				ComparisonOption: fleetv1beta1.ComparisonOptionTypeFullComparison,
				Type:             fleetv1beta1.ApplyStrategyTypeServerSideApply,
				WhenToApply:      fleetv1beta1.WhenToApplyTypeIfNotDrifted,
				WhenToTakeOver:   fleetv1beta1.WhenToTakeOverTypeNever,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			scheme := serviceScheme(t)
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tc.work).
				Build()

			r := &Reconciler{
				Client: fakeClient,
			}
			workUpdated, err := r.syncApplyStrategy(ctx, tc.resourceBinding, tc.work)
			if err != nil {
				t.Fatalf("syncApplyStrategy() = %v, want no error", err)
			}
			if workUpdated != tc.wantUpdated {
				t.Errorf("syncApplyStrategy() = %v, want %v", workUpdated, tc.wantUpdated)
			}

			updatedWork := &fleetv1beta1.Work{}
			if err := r.Client.Get(ctx, client.ObjectKeyFromObject(tc.work), updatedWork); err != nil {
				t.Fatalf("Get Work = %v, want no error", err)
			}
			if diff := cmp.Diff(updatedWork.Spec.ApplyStrategy, tc.wantApplyStrategy); diff != "" {
				t.Errorf("applyStrategy mismatches (-got, +want):\n%s", diff)
			}
		})
	}
}

type conflictClient struct {
	client.Client
	conflictCount int
}

func (c *conflictClient) Status() client.StatusWriter {
	return &conflictStatusWriter{
		StatusWriter:   c.Client.Status(),
		conflictClient: c,
	}
}

type conflictStatusWriter struct {
	client.StatusWriter
	conflictClient *conflictClient
}

func (s *conflictStatusWriter) Update(ctx context.Context, obj client.Object, _ ...client.SubResourceUpdateOption) error {
	if s.conflictClient.conflictCount > 0 {
		s.conflictClient.conflictCount--
		// Simulate a conflict error
		return k8serrors.NewConflict(schema.GroupResource{Resource: "ClusterResourceBinding"}, obj.GetName(), errors.New("the object has been modified; please apply your changes to the latest version and try again"))
	}
	return s.StatusWriter.Update(ctx, obj)
}
