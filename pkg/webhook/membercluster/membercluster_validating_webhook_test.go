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

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils"

	fleetnetworkingv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
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

func buildCreateRequestFromObject(t *testing.T, mc *clusterv1beta1.MemberCluster) admission.Request {
	t.Helper()

	raw, err := json.Marshal(mc)
	if err != nil {
		t.Fatalf("failed to marshal member cluster: %v", err)
	}
	return admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Operation: admissionv1.Create,
			Name:      mc.Name,
			Object:    runtime.RawExtension{Raw: raw},
		},
	}
}

func memberClusterWithAlias(name, alias string) *clusterv1beta1.MemberCluster {
	mc := &clusterv1beta1.MemberCluster{ObjectMeta: metav1.ObjectMeta{Name: name}}
	if alias != "" {
		mc.Labels = map[string]string{placementv1beta1.ClusterAliasLabel: alias}
	}
	return mc
}

// TestHandleClusterAliasCollision covers the alias uniqueness warning: a member cluster whose alias
// is already held by another is admitted with a warning, never denied, and the cases that must stay
// silent (no alias, a unique alias, and the alias's own holder) produce none.
func TestHandleClusterAliasCollision(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		incoming    *clusterv1beta1.MemberCluster
		wantWarning bool
	}{
		"no alias label is silent": {
			incoming:    memberClusterWithAlias("cluster-two", ""),
			wantWarning: false,
		},
		"a unique alias is silent": {
			incoming:    memberClusterWithAlias("cluster-two", "api-primary"),
			wantWarning: false,
		},
		"an alias already held by another cluster warns": {
			incoming:    memberClusterWithAlias("cluster-two", "web-primary"),
			wantWarning: true,
		},
		"the alias's own holder does not warn about itself": {
			incoming:    memberClusterWithAlias("cluster-one", "web-primary"),
			wantWarning: false,
		},
	}

	for name, tc := range testCases {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// A fresh object per subtest: the fake client stamps a resourceVersion onto the objects
			// it is built with, so a shared pointer would be written concurrently under -race.
			existing := memberClusterWithAlias("cluster-one", "web-primary")
			validator := newMemberClusterValidatorForTest(t, false, existing)
			resp := validator.Handle(context.Background(), buildCreateRequestFromObject(t, tc.incoming))

			if !resp.Allowed {
				t.Fatalf("Handle() = denied, want allowed regardless of alias collision: %+v", resp.Result)
			}
			if gotWarning := len(resp.Warnings) > 0; gotWarning != tc.wantWarning {
				t.Errorf("Handle() produced a warning = %v (%v), want %v", gotWarning, resp.Warnings, tc.wantWarning)
			}
		})
	}
}

// TestClusterAliasCollisionWarningTruncatesHolders covers the many-holders path: the message names
// at most three holders and summarizes the rest, with the surplus counted over every holder rather
// than over the names kept.
func TestClusterAliasCollisionWarningTruncatesHolders(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		holders int
		want    string
	}{
		{name: "one holder is named on its own", holders: 1, want: "holder-0"},
		{name: "holders up to the cap are all named", holders: 3, want: "holder-0, holder-1, holder-2"},
		{name: "one holder past the cap is summarized", holders: 4, want: "holder-0, holder-1, holder-2, and 1 more"},
		{name: "many holders are counted in full", holders: 6, want: "holder-0, holder-1, holder-2, and 3 more"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			scheme := runtime.NewScheme()
			if err := clusterv1beta1.AddToScheme(scheme); err != nil {
				t.Fatalf("failed to add member cluster scheme: %v", err)
			}
			seed := make([]client.Object, 0, tc.holders+2)
			for i := 0; i < tc.holders; i++ {
				seed = append(seed, memberClusterWithAlias(fmt.Sprintf("holder-%d", i), "web-primary"))
			}
			// Decoys: the count must come from the alias, not from the fleet size. These fail the
			// selector, so dropping it from the List would show up here as an inflated count.
			seed = append(seed, memberClusterWithAlias("other-alias", "db-primary"), memberClusterWithAlias("no-alias", ""))
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(seed...).Build()
			v := &memberClusterValidator{client: c, decoder: admission.NewDecoder(scheme)}

			got := v.clusterAliasCollisionWarning(context.Background(), memberClusterWithAlias("newcomer", "web-primary"))
			// The holder list is pinned up to the semicolon that ends it, so that a case naming
			// fewer holders than the cap also asserts that no surplus summary was appended.
			if want := fmt.Sprintf("used by %s;", tc.want); !strings.Contains(got, want) {
				t.Errorf("clusterAliasCollisionWarning() = %q, want it to name the holders as %q", got, want)
			}
		})
	}
}

// TestClusterAliasCollisionWarningEmptyAlias covers the guard on the alias value: a member cluster
// carrying the alias label with an explicit empty value is not an alias at all, and must not be
// matched against every other cluster whose alias is likewise empty.
func TestClusterAliasCollisionWarningEmptyAlias(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := clusterv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add member cluster scheme: %v", err)
	}
	emptyAliasCluster := func(name string) *clusterv1beta1.MemberCluster {
		return &clusterv1beta1.MemberCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:   name,
				Labels: map[string]string{placementv1beta1.ClusterAliasLabel: ""},
			},
		}
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(emptyAliasCluster("other")).Build()
	v := &memberClusterValidator{client: c, decoder: admission.NewDecoder(scheme)}

	if got := v.clusterAliasCollisionWarning(context.Background(), emptyAliasCluster("newcomer")); got != "" {
		t.Errorf("clusterAliasCollisionWarning() = %q, want no warning for an empty alias value", got)
	}
}

// TestClusterAliasCollisionWarningListError covers the fail-open path: a member cluster list that
// errors must admit the request without a warning rather than block it, since the check is advisory.
func TestClusterAliasCollisionWarningListError(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := clusterv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add member cluster scheme: %v", err)
	}
	// A real collision exists in the store, so a successful list WOULD warn; only the injected list
	// error can produce the empty result this asserts, which is what makes it a fail-open test
	// rather than a trivially-no-collisions one.
	failingClient := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(memberClusterWithAlias("cluster-one", "web-primary")).
		WithInterceptorFuncs(interceptor.Funcs{
			List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
				return fmt.Errorf("the member cluster list is unwell")
			},
		}).Build()
	v := &memberClusterValidator{client: failingClient, decoder: admission.NewDecoder(scheme)}

	if got := v.clusterAliasCollisionWarning(context.Background(), memberClusterWithAlias("cluster-two", "web-primary")); got != "" {
		t.Errorf("clusterAliasCollisionWarning() = %q, want empty on a list error (fail open)", got)
	}
}
