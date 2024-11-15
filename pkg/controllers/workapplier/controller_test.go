/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package workapplier

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
)

const (
	workName = "work-1"

	deployName      = "deploy-1"
	configMapName   = "configmap-1"
	nsName          = "ns-1"
	jobGenerateName = "job-"
	nsGenerateName  = "work-"
)

var (
	appliedWorkOwnerRef = &metav1.OwnerReference{
		APIVersion: "placement.kubernetes-fleet.io/v1beta1",
		Kind:       "AppliedWork",
		Name:       workName,
		UID:        "uid",
	}
)

var (
	ignoreFieldTypeMetaInNamespace = cmpopts.IgnoreFields(corev1.Namespace{}, "TypeMeta")
	ignoreFieldTypeMetaInJob       = cmpopts.IgnoreFields(batchv1.Job{}, "TypeMeta")
)

// TestDecodeManifest tests the decodeManifest method.
func TestDecodeManifest(_ *testing.T) {
	// Not yet implemented.
}

// TestRemoveLeftOverManifests tests the removeLeftOverManifests method.
func TestRemoveLeftOverManifests(_ *testing.T) {
	// Not yet implemented.
}

// TestRemoveOneLeftOverManifest tests the removeOneLeftOverManifest method.
func TestRemoveOneLeftOverManifest(t *testing.T) {
	ctx := context.Background()
	now := metav1.Now().Rfc3339Copy()
	leftOverManifest := fleetv1beta1.AppliedResourceMeta{
		WorkResourceIdentifier: *nsWRI,
	}

	testCases := []struct {
		name string
		// To simplify things, for this test Fleet uses a fixed concrete type.
		inMemberClusterObj     *corev1.Namespace
		wantInMemberClusterObj *corev1.Namespace
	}{
		{
			name: "not found",
		},
		{
			name: "already deleted",
			inMemberClusterObj: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:              nsName,
					DeletionTimestamp: &now,
				},
			},
			wantInMemberClusterObj: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:              nsName,
					DeletionTimestamp: &now,
				},
			},
		},
		{
			name: "not derived from manifest object",
			inMemberClusterObj: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: nsName,
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "v1",
							Kind:       "SuperNamespace",
							Name:       "super-ns",
							UID:        "super-ns-uid",
						},
					},
				},
			},
			wantInMemberClusterObj: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: nsName,
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "v1",
							Kind:       "SuperNamespace",
							Name:       "super-ns",
							UID:        "super-ns-uid",
						},
					},
				},
			},
		},
		{
			name: "multiple owners",
			inMemberClusterObj: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: nsName,
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "v1",
							Kind:       "SuperNamespace",
							Name:       "super-ns",
							UID:        "super-ns-uid",
						},
						*appliedWorkOwnerRef,
					},
				},
			},
			wantInMemberClusterObj: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: nsName,
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "v1",
							Kind:       "SuperNamespace",
							Name:       "super-ns",
							UID:        "super-ns-uid",
						},
					},
				},
			},
		},
		{
			name: "deletion",
			inMemberClusterObj: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: nsName,
					OwnerReferences: []metav1.OwnerReference{
						*appliedWorkOwnerRef,
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var fakeClient *fake.FakeDynamicClient
			if tc.inMemberClusterObj != nil {
				fakeClient = fake.NewSimpleDynamicClient(scheme.Scheme, tc.inMemberClusterObj)
			} else {
				fakeClient = fake.NewSimpleDynamicClient(scheme.Scheme)
			}

			r := &Reconciler{
				spokeDynamicClient: fakeClient,
			}
			if err := r.removeOneLeftOverManifest(ctx, leftOverManifest, appliedWorkOwnerRef); err != nil {
				t.Errorf("removeOneLeftOverManifest() = %v, want no error", err)
			}

			if tc.inMemberClusterObj != nil {
				var gotUnstructured *unstructured.Unstructured
				var err error
				// The method is expected to modify the object.
				gotUnstructured, err = fakeClient.
					Resource(nsGVR).
					Namespace(tc.inMemberClusterObj.GetNamespace()).
					Get(ctx, tc.inMemberClusterObj.GetName(), metav1.GetOptions{})
				switch {
				case errors.IsNotFound(err) && tc.wantInMemberClusterObj == nil:
					// The object is expected to be deleted.
					return
				case errors.IsNotFound(err):
					// An object is expected to be found.
					t.Errorf("Get(%v) = %v, want no error", klog.KObj(tc.inMemberClusterObj), err)
					return
				case err != nil:
					// An unexpected error occurred.
					t.Errorf("Get(%v) = %v, want no error", klog.KObj(tc.inMemberClusterObj), err)
					return
				}

				got := tc.wantInMemberClusterObj.DeepCopy()
				if err := runtime.DefaultUnstructuredConverter.FromUnstructured(gotUnstructured.Object, &got); err != nil {
					t.Errorf("FromUnstructured() = %v, want no error", err)
					return
				}

				if diff := cmp.Diff(got, tc.wantInMemberClusterObj, ignoreFieldTypeMetaInNamespace); diff != "" {
					t.Errorf("Get(%v) mismatches (-got +want):\n%s", klog.KObj(tc.inMemberClusterObj), diff)
				}
				return
			}
		})
	}
}

// TestRemoveOneLeftOverManifestWithGenerateName tests the removeOneLeftOverManifest method.
func TestRemoveOneLeftOverManifestWithGenerateName(t *testing.T) {
	ctx := context.Background()
	now := metav1.Now().Rfc3339Copy()
	leftOverManifest := fleetv1beta1.AppliedResourceMeta{
		WorkResourceIdentifier: *jobWithGenerateNameWRI,
	}

	testCases := []struct {
		name string
		// To simplify things, for this test Fleet uses a fixed concrete type.
		inMemberClusterObjs     []*batchv1.Job
		wantInMemberClusterObjs []*batchv1.Job
	}{
		{
			name: "not found",
		},
		{
			name: "no matching objects (no owner ref)",
			inMemberClusterObjs: []*batchv1.Job{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: nsName,
						Name:      fmt.Sprintf("%s%s", jobGenerateName, "1a2b3c"),
					},
				},
			},
			wantInMemberClusterObjs: []*batchv1.Job{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: nsName,
						Name:      fmt.Sprintf("%s%s", jobGenerateName, "1a2b3c"),
					},
				},
			},
		},
		{
			name: "no matching objects (naming pattern mismatches)",
			inMemberClusterObjs: []*batchv1.Job{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: nsName,
						Name:      "otherjob-1a2b3c",
						OwnerReferences: []metav1.OwnerReference{
							*appliedWorkOwnerRef,
						},
					},
				},
			},
			wantInMemberClusterObjs: []*batchv1.Job{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: nsName,
						Name:      "otherjob-1a2b3c",
						OwnerReferences: []metav1.OwnerReference{
							*appliedWorkOwnerRef,
						},
					},
				},
			},
		},
		{
			name: "single matched, already deleted",
			inMemberClusterObjs: []*batchv1.Job{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace:         nsName,
						Name:              fmt.Sprintf("%s%s", jobGenerateName, "1a2b3c"),
						DeletionTimestamp: &now,
						OwnerReferences: []metav1.OwnerReference{
							*appliedWorkOwnerRef,
						},
					},
				},
			},
			wantInMemberClusterObjs: []*batchv1.Job{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace:         nsName,
						Name:              fmt.Sprintf("%s%s", jobGenerateName, "1a2b3c"),
						DeletionTimestamp: &now,
						OwnerReferences: []metav1.OwnerReference{
							*appliedWorkOwnerRef,
						},
					},
				},
			},
		},
		{
			name: "single matched, multiple owners",
			inMemberClusterObjs: []*batchv1.Job{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: nsName,
						Name:      fmt.Sprintf("%s%s", jobGenerateName, "1a2b3c"),
						OwnerReferences: []metav1.OwnerReference{
							*appliedWorkOwnerRef,
							{
								APIVersion: "v1",
								Kind:       "SuperJob",
								Name:       "super-job",
								UID:        "super-job-uid",
							},
						},
					},
				},
			},
			wantInMemberClusterObjs: []*batchv1.Job{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: nsName,
						Name:      fmt.Sprintf("%s%s", jobGenerateName, "1a2b3c"),
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: "v1",
								Kind:       "SuperJob",
								Name:       "super-job",
								UID:        "super-job-uid",
							},
						},
					},
				},
			},
		},
		{
			name: "single matched, deletion",
			inMemberClusterObjs: []*batchv1.Job{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: nsName,
						Name:      fmt.Sprintf("%s%s", jobGenerateName, "1a2b3c"),
						OwnerReferences: []metav1.OwnerReference{
							*appliedWorkOwnerRef,
						},
					},
				},
			},
			wantInMemberClusterObjs: []*batchv1.Job{},
		},
		{
			// This normally should not occur.
			name: "multiple matched, mixed",
			inMemberClusterObjs: []*batchv1.Job{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace:         nsName,
						Name:              fmt.Sprintf("%s%s", jobGenerateName, "1a2b3c"),
						DeletionTimestamp: &now,
						OwnerReferences: []metav1.OwnerReference{
							*appliedWorkOwnerRef,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: nsName,
						Name:      fmt.Sprintf("%s%s", jobGenerateName, "4a5b6c"),
						OwnerReferences: []metav1.OwnerReference{
							*appliedWorkOwnerRef,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: nsName,
						Name:      fmt.Sprintf("%s%s", jobGenerateName, "7a8b9c"),
						OwnerReferences: []metav1.OwnerReference{
							*appliedWorkOwnerRef,
							{
								APIVersion: "v1",
								Kind:       "SuperJob",
								Name:       "super-job",
								UID:        "super-job-uid",
							},
						},
					},
				},
			},
			wantInMemberClusterObjs: []*batchv1.Job{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace:         nsName,
						Name:              fmt.Sprintf("%s%s", jobGenerateName, "1a2b3c"),
						DeletionTimestamp: &now,
						OwnerReferences: []metav1.OwnerReference{
							*appliedWorkOwnerRef,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: nsName,
						Name:      fmt.Sprintf("%s%s", jobGenerateName, "7a8b9c"),
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: "v1",
								Kind:       "SuperJob",
								Name:       "super-job",
								UID:        "super-job-uid",
							},
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var fakeClient *fake.FakeDynamicClient
			if len(tc.inMemberClusterObjs) > 0 {
				// Re-pack the slice to []runtime.Object.
				inMemberClusterObjs := make([]runtime.Object, len(tc.inMemberClusterObjs))
				for i, obj := range tc.inMemberClusterObjs {
					inMemberClusterObjs[i] = obj
				}
				fakeClient = fake.NewSimpleDynamicClient(scheme.Scheme, inMemberClusterObjs...)
			} else {
				fakeClient = fake.NewSimpleDynamicClient(scheme.Scheme)
			}

			r := &Reconciler{
				spokeDynamicClient: fakeClient,
			}
			if err := r.removeOneLeftOverManifestWithGenerateName(ctx, leftOverManifest, appliedWorkOwnerRef); err != nil {
				t.Errorf("removeOneLeftOverManifest() = %v, want no error", err)
			}

			if len(tc.inMemberClusterObjs) > 0 {
				gotJobs := make([]*batchv1.Job, 0, len(tc.wantInMemberClusterObjs))
				for idx := range tc.wantInMemberClusterObjs {
					// Use a copy of the object as placeholder.
					gotJob := tc.wantInMemberClusterObjs[idx].DeepCopy()
					gotUnstructured, err := fakeClient.
						Resource(jobGVR).
						Namespace(gotJob.GetNamespace()).
						Get(ctx, gotJob.GetName(), metav1.GetOptions{})
					switch {
					case errors.IsNotFound(err):
						continue
					case err != nil:
						t.Errorf("Get(%v) = %v, want no error", klog.KObj(gotJob), err)
						return
					}

					err = runtime.DefaultUnstructuredConverter.FromUnstructured(gotUnstructured.Object, gotJob)
					if err != nil {
						t.Errorf("FromUnstructured() = %v, want no error", err)
						return
					}

					gotJobs = append(gotJobs, gotJob)
				}

				if len(tc.wantInMemberClusterObjs) > 0 {
					// The method is expected to modify (some of) the objects.
					if diff := cmp.Diff(gotJobs, tc.wantInMemberClusterObjs, ignoreFieldTypeMetaInJob); diff != "" {
						t.Errorf("got jobs mismatches (-got +want):\n%s", diff)
					}
					return
				}

				// The method is expected to delete all of the objects.
				if len(gotJobs) > 0 {
					t.Errorf("got jobs = %v, want no jobs", gotJobs)
				}
				return
			}
		})
	}
}
