package membercluster

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"

	fleetnetworkingv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func TestHandleDelete(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		networkingEnabled bool
		validationMode    clusterv1beta1.DeleteValidationMode
		wantAllowed       bool
		wantMessageSubstr string
	}{
		"networking-disabled-allows-delete": {
			networkingEnabled: false,
			wantAllowed:       true,
			validationMode:    clusterv1beta1.DeleteValidationModeStrict,
		},
		"networking-enabled-denies-delete": {
			networkingEnabled: true,
			wantAllowed:       false,
			validationMode:    clusterv1beta1.DeleteValidationModeStrict,
			wantMessageSubstr: "Please delete serviceExport",
		},
		"delete-options-skip-bypasses-validation": {
			networkingEnabled: true,
			wantAllowed:       true,
			validationMode:    clusterv1beta1.DeleteValidationModeSkip,
		},
	}

	for name, tc := range testCases {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			mcName := fmt.Sprintf("member-%s", name)
			namespaceName := fmt.Sprintf(utils.NamespaceNameFormat, mcName)
			svcExport := newInternalServiceExport(mcName, namespaceName)

			validator := newMemberClusterValidatorForTest(t, tc.networkingEnabled, svcExport)
			mc := &clusterv1beta1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: mcName}}
			mc.Spec.DeleteOptions = &clusterv1beta1.DeleteOptions{ValidationMode: tc.validationMode}
			req := buildDeleteRequestFromObject(t, mc)

			resp := validator.Handle(context.Background(), req)
			if resp.Allowed != tc.wantAllowed {
				t.Fatalf("Handle() got response: %+v, want allowed %t", resp, tc.wantAllowed)
			}
			if tc.wantMessageSubstr != "" {
				if resp.Result == nil || !strings.Contains(resp.Result.Message, tc.wantMessageSubstr) {
					t.Fatalf("Handle()  got response result: %v,  want contain: %q", resp.Result, tc.wantMessageSubstr)
				}
			}
		})
	}
}

func newMemberClusterValidatorForTest(t *testing.T, networkingEnabled bool, objs ...client.Object) *memberClusterValidator {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := clusterv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add member cluster scheme: %v", err)
	}
	if err := fleetnetworkingv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add fleet networking scheme: %v", err)
	}
	scheme.AddKnownTypes(fleetnetworkingv1alpha1.GroupVersion,
		&fleetnetworkingv1alpha1.InternalServiceExport{},
		&fleetnetworkingv1alpha1.InternalServiceExportList{},
	)
	metav1.AddToGroupVersion(scheme, fleetnetworkingv1alpha1.GroupVersion)

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
	decoder := admission.NewDecoder(scheme)

	return &memberClusterValidator{
		client:                  fakeClient,
		decoder:                 decoder,
		networkingAgentsEnabled: networkingEnabled,
	}
}

func buildDeleteRequestFromObject(t *testing.T, mc *clusterv1beta1.MemberCluster) admission.Request {
	t.Helper()

	raw, err := json.Marshal(mc)
	if err != nil {
		t.Fatalf("failed to marshal member cluster: %v", err)
	}

	return admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Delete,
			Name:      mc.Name,
			OldObject: runtime.RawExtension{Raw: raw},
		},
	}
}

func newInternalServiceExport(clusterID, namespace string) *fleetnetworkingv1alpha1.InternalServiceExport {
	return &fleetnetworkingv1alpha1.InternalServiceExport{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sample-service",
			Namespace: namespace,
		},
		Spec: fleetnetworkingv1alpha1.InternalServiceExportSpec{
			ServiceReference: fleetnetworkingv1alpha1.ExportedObjectReference{
				ClusterID:       clusterID,
				Kind:            "Service",
				Namespace:       "work",
				Name:            "sample-service",
				ResourceVersion: "1",
				Generation:      1,
				UID:             types.UID("svc-uid"),
				NamespacedName:  "work/sample-service",
			},
		},
	}
}
