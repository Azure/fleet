/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package managedresource

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	admv1 "k8s.io/api/admissionregistration/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestEnsureVAP(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := admv1.AddToScheme(scheme); err != nil {
		t.Fatalf("Failed to add admissionregistration scheme: %v", err)
	}

	vapHub := getValidatingAdmissionPolicy(true)
	vapMember := getValidatingAdmissionPolicy(false)
	binding := getValidatingAdmissionPolicyBinding()

	tests := []struct {
		name                 string
		isHub                bool
		existingObjs         []client.Object
		createOrUpdateErrors map[client.ObjectKey]error
		wantErr              bool
		wantErrMessage       string
		wantObjects          []client.Object
	}{
		{
			name:         "hub cluster - create new objects",
			isHub:        true,
			existingObjs: []client.Object{},
			wantErr:      false,
			wantObjects: []client.Object{
				vapHub.DeepCopy(),
				binding.DeepCopy(),
			},
		},
		{
			name:         "member cluster - create new objects",
			isHub:        false,
			existingObjs: []client.Object{},
			wantErr:      false,
			wantObjects: []client.Object{
				vapMember.DeepCopy(),
				binding.DeepCopy(),
			},
		},
		{
			name:  "hub cluster - update existing objects",
			isHub: true,
			existingObjs: func() []client.Object {
				existingVAP := vapHub.DeepCopy()
				existingBinding := binding.DeepCopy()

				existingVAP.Spec.Validations = nil
				existingBinding.Spec.ValidationActions = nil

				return []client.Object{existingVAP, existingBinding}
			}(),
			wantErr: false,
			wantObjects: []client.Object{
				vapHub.DeepCopy(),
				binding.DeepCopy(),
			},
		},
		{
			name:         "hub cluster - skip no match error",
			isHub:        true,
			existingObjs: []client.Object{},
			createOrUpdateErrors: map[client.ObjectKey]error{
				client.ObjectKeyFromObject(vapHub): &meta.NoKindMatchError{GroupKind: schema.GroupKind{Group: "admissionregistration.k8s.io", Kind: "ValidatingAdmissionPolicy"}},
			},
			wantErr: false,
		},
		{
			name:         "hub cluster - create error propagated",
			isHub:        true,
			existingObjs: []client.Object{},
			createOrUpdateErrors: map[client.ObjectKey]error{
				client.ObjectKeyFromObject(vapHub): apierrors.NewInternalError(errors.New("internal server error")),
			},
			wantErr:        true,
			wantErrMessage: "internal server error",
		},
		{
			name:         "member cluster - binding creation error",
			isHub:        false,
			existingObjs: []client.Object{},
			createOrUpdateErrors: map[client.ObjectKey]error{
				client.ObjectKeyFromObject(binding): apierrors.NewForbidden(schema.GroupResource{Group: "admissionregistration.k8s.io", Resource: "validatingadmissionpolicybindings"}, binding.Name, errors.New("forbidden")),
			},
			wantErr:        true,
			wantErrMessage: "forbidden",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt := tt
			t.Parallel()

			// Object store for tracking updates (since different names eliminate collisions)
			objectStore := make(map[client.ObjectKey]client.Object)
			for _, obj := range tt.existingObjs {
				key := client.ObjectKeyFromObject(obj)
				objectStore[key] = obj.DeepCopyObject().(client.Object)
			}

			interceptorFuncs := interceptor.Funcs{
				// This is needed for a test scenario that GET would retrieve the same object as what was created/updated.
				// TODO: refactor to simplify this. The store should not be needed as UPDATE should update the same object provided in fakeClient's WithObjects
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if storedObj, exists := objectStore[key]; exists {
						switch v := obj.(type) {
						case *admv1.ValidatingAdmissionPolicy:
							if stored, ok := storedObj.(*admv1.ValidatingAdmissionPolicy); ok {
								*v = *stored
							}
						case *admv1.ValidatingAdmissionPolicyBinding:
							if stored, ok := storedObj.(*admv1.ValidatingAdmissionPolicyBinding); ok {
								*v = *stored
							}
						}
						return nil
					}
					return c.Get(ctx, key, obj, opts...)
				},
				Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
					key := client.ObjectKeyFromObject(obj)
					if err, exists := tt.createOrUpdateErrors[key]; exists {
						return err
					}
					err := c.Create(ctx, obj, opts...)
					if err == nil {
						objectStore[key] = obj.DeepCopyObject().(client.Object)
					}
					return err
				},
				Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
					key := client.ObjectKeyFromObject(obj)
					if err, exists := tt.createOrUpdateErrors[key]; exists {
						return err
					}
					// Update our store with the new object state
					objectStore[key] = obj.DeepCopyObject().(client.Object)
					return nil
				},
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.existingObjs...).
				WithInterceptorFuncs(interceptorFuncs).
				Build()

			err := EnsureVAP(context.Background(), fakeClient, tt.isHub)

			if tt.wantErr {
				if err == nil {
					t.Error("EnsureVAP() = nil, want error")
					return
				}
				if tt.wantErrMessage != "" && !strings.Contains(err.Error(), tt.wantErrMessage) {
					t.Errorf("EnsureVAP() error = %v, want error containing %q", err, tt.wantErrMessage)
				}
				return
			}

			if err != nil {
				t.Errorf("EnsureVAP() = %v, want nil", err)
			}

			// Verify objects were created/updated correctly
			for _, wantObj := range tt.wantObjects {
				gotObj := wantObj.DeepCopyObject().(client.Object)
				err := fakeClient.Get(context.Background(), client.ObjectKeyFromObject(wantObj), gotObj)
				if err != nil {
					t.Errorf("Failed to get object %s: %v", wantObj.GetName(), err)
					continue
				}

				// Compare relevant fields (ignore managed fields, resource version, etc.)
				ignoreOpts := cmp.Options{
					cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion", "ManagedFields", "Generation"),
				}
				if diff := cmp.Diff(wantObj, gotObj, ignoreOpts); diff != "" {
					t.Errorf("Object %s mismatch (-want +got):\n%s", wantObj.GetName(), diff)
				}
			}
		})
	}
}

func TestEnsureNoVAP(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := admv1.AddToScheme(scheme); err != nil {
		t.Fatalf("Failed to add admissionregistration scheme: %v", err)
	}

	vapHub := getValidatingAdmissionPolicy(true)
	vapMember := getValidatingAdmissionPolicy(false)
	binding := getValidatingAdmissionPolicyBinding()

	tests := []struct {
		name           string
		isHub          bool
		existingObjs   []client.Object
		deleteErrors   map[client.ObjectKey]error
		wantErr        bool
		wantErrMessage string
	}{
		{
			name:         "hub cluster - no existing objects",
			isHub:        true,
			existingObjs: []client.Object{},
			wantErr:      false,
		},
		{
			name:  "hub cluster - existing objects deleted successfully",
			isHub: true,
			existingObjs: []client.Object{
				vapHub.DeepCopy(),
				binding.DeepCopy(),
			},
			wantErr: false,
		},
		{
			name:  "member cluster - existing objects deleted successfully",
			isHub: false,
			existingObjs: []client.Object{
				vapMember.DeepCopy(),
				binding.DeepCopy(),
			},
			wantErr: false,
		},
		{
			name:         "hub cluster - not found errors ignored",
			isHub:        true,
			existingObjs: []client.Object{},
			deleteErrors: map[client.ObjectKey]error{
				client.ObjectKeyFromObject(vapHub):  apierrors.NewNotFound(schema.GroupResource{Group: "admissionregistration.k8s.io", Resource: "validatingadmissionpolicies"}, vapHub.Name),
				client.ObjectKeyFromObject(binding): apierrors.NewNotFound(schema.GroupResource{Group: "admissionregistration.k8s.io", Resource: "validatingadmissionpolicybindings"}, binding.Name),
			},
			wantErr: false,
		},
		{
			name:         "hub cluster - no match error handled gracefully",
			isHub:        true,
			existingObjs: []client.Object{},
			deleteErrors: map[client.ObjectKey]error{
				client.ObjectKeyFromObject(vapHub): &meta.NoKindMatchError{GroupKind: schema.GroupKind{Group: "admissionregistration.k8s.io", Kind: "ValidatingAdmissionPolicy"}},
			},
			wantErr: false,
		},
		{
			name:         "hub cluster - delete error",
			isHub:        true,
			existingObjs: []client.Object{},
			deleteErrors: map[client.ObjectKey]error{
				client.ObjectKeyFromObject(vapHub): apierrors.NewInternalError(errors.New("internal server error")),
			},
			wantErr:        true,
			wantErrMessage: "internal server error",
		},
		{
			name:         "member cluster - delete error on binding propagated",
			isHub:        false,
			existingObjs: []client.Object{},
			deleteErrors: map[client.ObjectKey]error{
				client.ObjectKeyFromObject(binding): apierrors.NewForbidden(schema.GroupResource{Group: "admissionregistration.k8s.io", Resource: "validatingadmissionpolicybindings"}, binding.Name, errors.New("forbidden")),
			},
			wantErr:        true,
			wantErrMessage: "forbidden",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt := tt
			t.Parallel()

			interceptorFuncs := interceptor.Funcs{
				Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
					key := client.ObjectKeyFromObject(obj)
					if err, exists := tt.deleteErrors[key]; exists {
						return err
					}
					return c.Delete(ctx, obj, opts...)
				},
			}
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.existingObjs...).
				WithInterceptorFuncs(interceptorFuncs).
				Build()

			err := EnsureNoVAP(context.Background(), fakeClient, tt.isHub)

			if tt.wantErr {
				if err == nil {
					t.Error("EnsureNoVAP() = nil, want error")
					return
				}
				if tt.wantErrMessage != "" && !strings.Contains(err.Error(), tt.wantErrMessage) {
					t.Errorf("EnsureNoVAP() error = %v, want error containing %q", err, tt.wantErrMessage)
				}
				return
			}

			if err != nil {
				t.Errorf("EnsureNoVAP() = %v, want nil", err)
			}

			// Verify objects are deleted (or don't exist)
			expectedObjs := []client.Object{getValidatingAdmissionPolicy(tt.isHub), getValidatingAdmissionPolicyBinding()}
			for _, obj := range expectedObjs {
				err := fakeClient.Get(context.Background(), client.ObjectKeyFromObject(obj), obj)
				if !apierrors.IsNotFound(err) {
					t.Errorf("Expected object %s to be deleted, but Get() = %v", obj.GetName(), err)
				}
			}
		})
	}
}

func TestGetVAPWithMutator(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		isHub bool
	}{
		{
			name:  "hub cluster VAP",
			isHub: true,
		},
		{
			name:  "member cluster VAP",
			isHub: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vap, mutateFunc := getVAPWithMutator(tt.isHub)

			// Verify initial state
			if vap == nil {
				t.Fatal("getVAPWithMutator() returned nil VAP")
			}
			if mutateFunc == nil {
				t.Fatal("getVAPWithMutator() returned nil mutate function")
			}

			// Verify mutate function works
			originalVAP := vap.DeepCopy()
			expectedVAP := getValidatingAdmissionPolicy(tt.isHub)
			ignoreOpts := cmp.Options{
				cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion", "ManagedFields", "Generation"),
			}
			if diff := cmp.Diff(expectedVAP, vap, ignoreOpts); diff != "" {
				t.Errorf("VAP after mutation mismatch (-want +got):\n%s", diff)
			}

			vap.Spec = admv1.ValidatingAdmissionPolicySpec{} // Reset spec to empty to test idempotency
			if diff := cmp.Diff(originalVAP, vap); diff == "" {
				t.Error("VAP should be different after mutation")
			}

			// The mutation should restore the spec to the expected state
			err := mutateFunc()
			if err != nil {
				t.Errorf("second mutateFunc() = %v, want nil", err)
			}
			if diff := cmp.Diff(expectedVAP, vap, ignoreOpts); diff != "" {
				t.Errorf("VAP after second mutation mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestGetVAPBindingWithMutator(t *testing.T) {
	t.Parallel()

	vapb, mutateFunc := getVAPBindingWithMutator()

	// Verify initial state
	if vapb == nil {
		t.Fatal("getVAPBindingWithMutator() returned nil VAP binding")
	}
	if mutateFunc == nil {
		t.Fatal("getVAPBindingWithMutator() returned nil mutate function")
	}

	// Verify mutate function works
	originalVAPB := vapb.DeepCopy()
	err := mutateFunc()
	if err != nil {
		t.Errorf("mutateFunc() = %v, want nil", err)
	}

	expectedVAPB := getValidatingAdmissionPolicyBinding()
	ignoreOpts := cmp.Options{
		cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion", "ManagedFields", "Generation"),
	}
	if diff := cmp.Diff(expectedVAPB, vapb, ignoreOpts); diff != "" {
		t.Errorf("VAP binding mismatch (-want +got):\n%s", diff)
	}

	vapb.Spec = admv1.ValidatingAdmissionPolicyBindingSpec{} // Reset spec to empty to test mutation
	if diff := cmp.Diff(originalVAPB, vapb); diff == "" {
		t.Error("VAP binding should be different after mutation")
	}

	// mutation should restore the spec to the expected state
	err = mutateFunc()
	if err != nil {
		t.Errorf("second mutateFunc() = %v, want nil", err)
	}
	if diff := cmp.Diff(expectedVAPB, vapb, ignoreOpts); diff != "" {
		t.Errorf("VAP binding after second mutation mismatch (-want +got):\n%s", diff)
	}
}
