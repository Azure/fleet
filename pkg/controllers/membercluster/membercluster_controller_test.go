package membercluster

import (
	"context"
	"fmt"
	"testing"

	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	//ctrl "sigs.k8s.io/controller-runtime"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fleetv1alpha1 "github.com/Azure/fleet/apis/v1alpha1"
)

func TestReconcilerCheckAndCreateNamespace(t *testing.T) {
	memberClusterName1 := "mc1"
	memberClusterName2 := "mc2"
	memberClusterName3 := "mc3"
	namespace1 := "mc1-namespace"
	namespace2 := "mc2-namespace"
	namespace3 := "mc3-namespace"

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
			return nil
		}
		return nil
	}

	createMock := func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
		o := obj.(*corev1.Namespace)
		if o.Name == namespace2 {
			return nil
		}
		return fmt.Errorf("namespace cannot be created")
	}

	expectedNamespace := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "mc1-namespace",
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
			wantedNamespace:   &expectedNamespace,
			wantedError:       nil,
		},
		"namespace doesn't exist": {
			r: &Reconciler{
				Client: &test.MockClient{MockGet: getMock, MockCreate: createMock},
			},
			memberClusterName: memberClusterName2,
			wantedNamespace:   nil,
			wantedError:       nil,
		},
		"namespace creation error": {
			r: &Reconciler{
				Client: &test.MockClient{MockGet: getMock, MockCreate: createMock},
			},
			memberClusterName: memberClusterName3,
			wantedNamespace:   nil,
			wantedError:       nil,
		},
	}

	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			got, _ := tt.r.checkAndCreateNamespace(context.Background(), tt.memberClusterName)
			assert.Equalf(t, tt.wantedNamespace, got, fleetv1alpha1.TestCaseMsg, testName)
		})
	}
}
