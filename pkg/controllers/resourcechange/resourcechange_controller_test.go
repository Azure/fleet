package resourcechange

import (
	"github.com/stretchr/testify/assert"
	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/mocks"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/cache"
	"testing"
)

func TestFindMatchedPolicies(t *testing.T) {
	namespaceScopedResources := prepareNameSpaceScopedResources()
	fakeInformerManager := &mocks.InformerManager{}
	fakeLister := &FakeGenericLister{}

	fakeInformerManager.On("Lister", utils.ClusterResourcePlacementGVR).Return(fakeLister)
	fakeInformerManager.On("GetNameSpaceScopedResources").Return(namespaceScopedResources)

	reconciler := &Reconciler{
		InformerManager: fakeInformerManager,
	}

	// policy1 covers namespace ns1 and ns2, policy2 covers namespace ns2
	// request objects coming from ns1 should only match policy1
	testnamespace := "ns1"

	requestObj1 := unstructuredFor(schema.GroupVersionKind{
		Group:   "apps",
		Version: "v1",
		Kind:    "Deployment",
	}, "d1", testnamespace)

	requestObj2 := unstructuredFor(schema.GroupVersionKind{
		Group:   "apps",
		Version: "v1",
		Kind:    "Deployment",
	}, "d2", testnamespace)

	rs, _ := reconciler.findMatchedPolicies(requestObj1)
	assert.Equal(t, 1, len(rs))
	for _, policy := range rs {
		assert.Equal(t, "policy1", policy.Name)
	}

	rs, _ = reconciler.findMatchedPolicies(requestObj2)
	assert.Equal(t, 1, len(rs))
	for _, policy := range rs {
		assert.Equal(t, "policy1", policy.Name)
	}

	//now set namespace to ns2, should match both polices
	testnamespace = "ns2"

	requestObj3 := unstructuredFor(schema.GroupVersionKind{
		Group:   "apps",
		Version: "v1",
		Kind:    "Deployment",
	}, "d1", testnamespace)

	rs, _ = reconciler.findMatchedPolicies(requestObj3)
	assert.Equal(t, 2, len(rs))
	assert.Equal(t, "policy1", rs[0].Name)
	assert.Equal(t, "policy2", rs[1].Name)

	// now set namespace to ns3, there should be no policy match
	testnamespace = "ns3"
	requestObj4 := unstructuredFor(schema.GroupVersionKind{
		Group:   "apps",
		Version: "v1",
		Kind:    "Deployment",
	}, "d1", testnamespace)
	rs, _ = reconciler.findMatchedPolicies(requestObj4)
	assert.Equal(t, 0, len(rs))

}

func TestMatchSelectorGVK(t *testing.T) {
	testCases := []struct {
		name        string
		targetGVK   schema.GroupVersionKind
		crpSelector fleetv1alpha1.ClusterResourceSelector
		expected    bool
	}{
		{
			name: "target gvk matches with crp's selector",
			targetGVK: schema.GroupVersionKind{
				Group:   "g",
				Version: "v",
				Kind:    "k",
			},
			crpSelector: fleetv1alpha1.ClusterResourceSelector{
				Group:         "g",
				Version:       "v",
				Kind:          "k",
				Name:          "testname",
				LabelSelector: nil,
			},
			expected: true,
		},
		{
			name: "target gvk not matches with crp's selector",
			targetGVK: schema.GroupVersionKind{
				Group:   "g",
				Version: "v1",
				Kind:    "k",
			},
			crpSelector: fleetv1alpha1.ClusterResourceSelector{
				Group:         "g",
				Version:       "v2",
				Kind:          "k",
				Name:          "testname",
				LabelSelector: nil,
			},
			expected: false,
		},
	}

	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			r := matchSelectorGVK(test.targetGVK, test.crpSelector)
			assert.Equal(t, r, test.expected)
		})
	}
}

func TestMatchSelectorLabelSelector(t *testing.T) {
	testCases := []struct {
		name         string
		targetLabels map[string]string
		crpSelector  fleetv1alpha1.ClusterResourceSelector
		expected     bool
	}{
		{
			name: "crp selector is nil, not match",
			targetLabels: map[string]string{
				"k1": "v1",
			},
			crpSelector: fleetv1alpha1.ClusterResourceSelector{
				LabelSelector: nil,
			},
			expected: false,
		},
		{
			name: "label selector exact the same, return true",
			targetLabels: map[string]string{
				"k1": "v1",
			},
			crpSelector: fleetv1alpha1.ClusterResourceSelector{
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"k1": "v1"},
				},
			},
			expected: true,
		},
		{
			name: "label selector not match, return false",
			targetLabels: map[string]string{
				"k2": "v1",
			},
			crpSelector: fleetv1alpha1.ClusterResourceSelector{
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"k1": "v1"},
				},
			},
			expected: false,
		},
		{
			name: "target labels is only a subset of crp, return false",
			targetLabels: map[string]string{
				"k1": "v1",
			},
			crpSelector: fleetv1alpha1.ClusterResourceSelector{
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"k1":  "v1",
						"abc": "xyz",
					},
				},
			},
			expected: false,
		},
		{
			name: "target labels includes crp, return true",
			targetLabels: map[string]string{
				"k1":  "v1",
				"abc": "xyz",
			},
			crpSelector: fleetv1alpha1.ClusterResourceSelector{
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"k1": "v1",
					},
				},
			},
			expected: true,
		},
	}

	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			r, err := matchSelectorLabelSelector(test.targetLabels, test.crpSelector)
			assert.Nil(t, err)
			assert.Equal(t, test.expected, r)
		})
	}
}

func prepareCRPList() []*fleetv1alpha1.ClusterResourcePlacement {
	list := make([]*fleetv1alpha1.ClusterResourcePlacement, 0)
	crp1 := &fleetv1alpha1.ClusterResourcePlacement{

		ObjectMeta: metav1.ObjectMeta{
			Name: "policy1",
		},
		Spec: fleetv1alpha1.ClusterResourcePlacementSpec{
			ResourceSelectors: []fleetv1alpha1.ClusterResourceSelector{
				{
					Group:   "",
					Version: "v1",
					Kind:    "Namespace",
					Name:    "ns1",
				},
				{
					Group:   "",
					Version: "v1",
					Kind:    "Namespace",
					Name:    "ns2",
				},
			},
			Policy: fleetv1alpha1.PlacementPolicy{
				ClusterNames: []string{
					"member-a", "member-b",
				},
			},
		},
	}
	crp2 := &fleetv1alpha1.ClusterResourcePlacement{
		ObjectMeta: metav1.ObjectMeta{
			Name: "policy2",
		},
		Spec: fleetv1alpha1.ClusterResourcePlacementSpec{
			ResourceSelectors: []fleetv1alpha1.ClusterResourceSelector{
				{
					Group:   "",
					Version: "v1",
					Kind:    "Namespace",
					Name:    "ns2",
				},
			},
			Policy: fleetv1alpha1.PlacementPolicy{
				ClusterNames: []string{
					"member-a", "member-c",
				},
			},
		},
	}
	list = append(list, crp1)
	list = append(list, crp2)
	return list
}

func prepareNameSpaceScopedResources() []schema.GroupVersionResource {
	deploymentGVK := schema.GroupVersionKind{
		Group:   "apps",
		Version: "v1",
		Kind:    "Deployment",
	}
	return []schema.GroupVersionResource{
		{
			Group:    deploymentGVK.Group,
			Version:  deploymentGVK.Version,
			Resource: PluralName(deploymentGVK.Kind),
		},
	}
}

type FakeGenericLister struct{}

func (fl *FakeGenericLister) List(selector labels.Selector) (ret []runtime.Object, err error) {
	crpList := prepareCRPList()
	ret = make([]runtime.Object, 0)
	for _, crp := range crpList {
		ret = append(ret, crp)
	}
	return ret, nil
}

func (fl *FakeGenericLister) Get(name string) (runtime.Object, error) {
	return nil, nil
}

func (fl *FakeGenericLister) ByNamespace(namespace string) cache.GenericNamespaceLister {
	return nil
}

func unstructuredFor(gvk schema.GroupVersionKind, name string, namespace string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(gvk)
	u.SetName(name)
	u.SetNamespace(namespace)
	return u
}
