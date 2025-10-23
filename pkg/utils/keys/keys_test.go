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

package keys

import (
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/clientcmd/api"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
)

var (
	secretObj = &api.Config{
		Kind:           "Test",
		APIVersion:     "v1/secret",
		CurrentContext: "federal-context",
	}
	crp = &placementv1beta1.ClusterResourcePlacement{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterResourcePlacement",
			APIVersion: "fleet.azure.com/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "foo",
			Name:      "bar",
		},
	}
	roleObj = &rbacv1.Role{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Role",
			APIVersion: "rbac.authorization.k8s.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "foo",
			Name:      "bar",
		},
	}
	clusterRoleObj = &rbacv1.ClusterRole{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Role",
			APIVersion: "rbac.authorization.k8s.io/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "bar",
		},
	}
)

func TestClusterWideKeyFunc(t *testing.T) {
	objMap, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(crp)
	tests := []struct {
		name         string
		object       interface{}
		expectErr    bool
		expectKeyStr string
	}{
		{
			name: "unstructured object",
			object: &unstructured.Unstructured{
				Object: objMap,
			},
			expectErr:    false,
			expectKeyStr: "fleet.azure.com/v1alpha1, kind=ClusterResourcePlacement, foo/bar",
		},
		{
			name:         "namespace scoped resource",
			object:       roleObj,
			expectErr:    false,
			expectKeyStr: "rbac.authorization.k8s.io/v1, kind=Role, foo/bar",
		},
		{
			name:         "cluster scoped resource",
			object:       clusterRoleObj,
			expectErr:    false,
			expectKeyStr: "rbac.authorization.k8s.io/v1beta1, kind=Role, bar",
		},
		{
			name:      "runtime object without meta",
			object:    secretObj,
			expectErr: true,
		},
		{
			name:      "non runtime object should be error",
			object:    "non-runtime-object",
			expectErr: true,
		},
		{
			name:      "nil object should be error",
			object:    nil,
			expectErr: true,
		},
	}

	for _, test := range tests {
		tc := test
		t.Run(tc.name, func(t *testing.T) {
			key, err := GetClusterWideKeyForObject(tc.object)
			if err != nil {
				if tc.expectErr == false {
					t.Fatalf("not expect error but error happed: %v", err)
				}

				return
			}

			if key.String() != tc.expectKeyStr {
				t.Fatalf("expect key string: %s, but got: %s", tc.expectKeyStr, key.String())
			}
		})
	}
}

func TestNamespaceKeyFunc(t *testing.T) {
	objMap, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(crp)
	tests := []struct {
		name         string
		object       interface{}
		expectErr    bool
		expectKeyStr string
	}{
		{
			name: "unstructured object",
			object: &unstructured.Unstructured{
				Object: objMap,
			},
			expectErr:    false,
			expectKeyStr: "foo/bar",
		},
		{
			name:         "namespace scoped resource",
			object:       roleObj,
			expectErr:    false,
			expectKeyStr: "foo/bar",
		},
		{
			name:         "cluster scoped resource",
			object:       clusterRoleObj,
			expectErr:    false,
			expectKeyStr: "bar",
		},
		{
			name:         "string works",
			object:       "string-key",
			expectErr:    false,
			expectKeyStr: "string-key",
		},
		{
			name:      "runtime object without meta",
			object:    secretObj,
			expectErr: true,
		},
		{
			name: "none runtime object should be error",
			object: ClusterWideKey{placementv1beta1.ResourceIdentifier{
				Name:      "foo",
				Namespace: "bar",
			}},
			expectErr: true,
		},
		{
			name:      "nil object should be error",
			object:    nil,
			expectErr: true,
		},
	}

	for _, test := range tests {
		tc := test
		t.Run(tc.name, func(t *testing.T) {
			key, err := GetNamespaceKeyForObject(tc.object)
			if err != nil {
				if tc.expectErr == false {
					t.Fatalf("not expect error but error happed: %v", err)
				}

				return
			}

			if key != tc.expectKeyStr {
				t.Fatalf("expect key string: %s, but got: %s", tc.expectKeyStr, key)
			}
		})
	}
}
