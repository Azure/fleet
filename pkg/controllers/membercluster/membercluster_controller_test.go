/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package membercluster

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Azure/fleet/pkg/utils"
)

const (
	namespaceCreationError = "namespace cannot be created"
	namespaceGetError      = "namespace cannot be retrieved"
)

func TestReconcilerCheckAndCreateNamespace(t *testing.T) {
	memberClusterName1 := "mc1"
	memberClusterName2 := "mc2"
	memberClusterName3 := "mc3"
	memberClusterName4 := "mc4"
	namespace1 := "fleet-mc1"
	namespace2 := "fleet-mc2"
	namespace3 := "fleet-mc3"
	namespace4 := "fleet-mc4"

	getMock := func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
		if key.Name == namespace2 || key.Name == namespace3 {
			return apierrors.NewNotFound(schema.GroupResource{Group: "", Resource: "Namespace"}, "namespace")
		} else if key.Name == namespace1 {
			o := obj.(*corev1.Namespace)
			*o = corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespace1,
				},
			}
		} else if key.Name == namespace4 {
			return fmt.Errorf(namespaceGetError)
		}
		return nil
	}

	createMock := func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
		o := obj.(*corev1.Namespace)
		if o.Name == namespace2 {
			return nil
		}
		return fmt.Errorf(namespaceCreationError)
	}

	expectedNamespace1 := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace1,
		},
	}
	expectedNamespace2 := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace2,
		},
	}
	tests := map[string]struct {
		r                 *Reconciler
		memberClusterName string
		wantedNamespace   *corev1.Namespace
		wantedError       error
	}{
		"namespace exists": {
			r: &Reconciler{
				Client: &test.MockClient{MockGet: getMock},
			},
			memberClusterName: memberClusterName1,
			wantedNamespace:   &expectedNamespace1,
			wantedError:       nil,
		},
		"namespace doesn't exist": {
			r: &Reconciler{
				Client: &test.MockClient{MockGet: getMock, MockCreate: createMock},
			},
			memberClusterName: memberClusterName2,
			wantedNamespace:   &expectedNamespace2,
			wantedError:       nil,
		},
		"namespace create error": {
			r: &Reconciler{
				Client: &test.MockClient{MockGet: getMock, MockCreate: createMock},
			},
			memberClusterName: memberClusterName3,
			wantedNamespace:   nil,
			wantedError:       errors.New(namespaceCreationError),
		},
		"namespace get error": {
			r: &Reconciler{
				Client: &test.MockClient{MockGet: getMock},
			},
			memberClusterName: memberClusterName4,
			wantedNamespace:   nil,
			wantedError:       errors.New(namespaceGetError),
		},
	}

	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			got, err := tt.r.checkAndCreateNamespace(context.Background(), tt.memberClusterName)
			assert.Equal(t, tt.wantedError, err, utils.TestCaseMsg, testName)
			assert.Equalf(t, tt.wantedNamespace, got, utils.TestCaseMsg, testName)
		})
	}
}
