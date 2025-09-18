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

	vapHub := GetValidatingAdmissionPolicy(true)
	vapMember := GetValidatingAdmissionPolicy(false)
	binding := GetValidatingAdmissionPolicyBinding()

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
			existingObjs: []client.Object{
				vapHub.DeepCopy(),
				binding.DeepCopy(),
			},
			wantErr: false,
			wantObjects: []client.Object{
				vapHub.DeepCopy(),
				binding.DeepCopy(),
			},
		},
		{
			name:         "hub cluster - no match error handled gracefully",
			isHub:        true,
			existingObjs: []client.Object{},
			createOrUpdateErrors: map[client.ObjectKey]error{
				client.ObjectKeyFromObject(vapHub): &meta.NoKindMatchError{GroupKind: schema.GroupKind{Group: "admissionregistration.k8s.io", Kind: "ValidatingAdmissionPolicy"}},
			},
			wantErr: true, // Note: function returns error even for no match
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
			t.Parallel()

			interceptorFuncs := interceptor.Funcs{
				Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
					key := client.ObjectKeyFromObject(obj)
					if err, exists := tt.createOrUpdateErrors[key]; exists {
						return err
					}
					return c.Create(ctx, obj, opts...)
				},
				Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
					key := client.ObjectKeyFromObject(obj)
					if err, exists := tt.createOrUpdateErrors[key]; exists {
						return err
					}
					return c.Update(ctx, obj, opts...)
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
				ignoreOpts := cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion", "ManagedFields", "Generation")
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

	vapHub := GetValidatingAdmissionPolicy(true)
	vapMember := GetValidatingAdmissionPolicy(false)
	binding := GetValidatingAdmissionPolicyBinding()

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
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
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
			expectedObjs := []client.Object{GetValidatingAdmissionPolicy(tt.isHub), GetValidatingAdmissionPolicyBinding()}
			for _, obj := range expectedObjs {
				err := fakeClient.Get(context.Background(), client.ObjectKeyFromObject(obj), obj)
				if !apierrors.IsNotFound(err) {
					t.Errorf("Expected object %s to be deleted, but Get() = %v", obj.GetName(), err)
				}
			}
		})
	}
}
