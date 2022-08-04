package validator

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/util/rand"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
)

func TestValidateClusterResourcePlacement(t *testing.T) {
	crpWithName := createClusterResourcePlacement(metav1.LabelSelector{
		MatchLabels: map[string]string{"test": "label"},
	}, metav1.LabelSelector{}, true)

	crpWithNameAndCst := createClusterResourcePlacement(metav1.LabelSelector{
		MatchLabels: map[string]string{"test": "label"},
	}, metav1.LabelSelector{
		MatchLabels: map[string]string{"cluster": "scoped"},
	}, false)

	crpWithNameAndInvalidCst := createClusterResourcePlacement(metav1.LabelSelector{
		MatchLabels: map[string]string{"test": "label"},
	}, metav1.LabelSelector{
		MatchLabels: map[string]string{"%cluster": "%scoped"},
	}, false)

	testCases := map[string]struct {
		tcrp          fleetv1alpha1.ClusterResourcePlacement
		expectedError error
	}{
		"Cluster Resource Placement has name and resource selector / validation fail": {
			crpWithName,
			fmt.Errorf("the labelSelector and name fields are mutually exclusive in selector"),
		},
		"Cluster Resource Placement has an invalid ClusterSelectorTerms / validation fail": {
			crpWithNameAndInvalidCst,
			fmt.Errorf("the labelSelector in cluster selector %+v is invalid", crpWithNameAndInvalidCst.Spec.Policy.Affinity.ClusterAffinity.ClusterSelectorTerms[0]),
		},
		"Cluster Resource Placement is valid / happy path": {
			crpWithNameAndCst,
			nil,
		},
	}

	for testName, testCase := range testCases {
		t.Run(testName, func(t *testing.T) {
			err := ValidateClusterResourcePlacement(&testCase.tcrp)
			if testCase.expectedError != nil {
				assert.Containsf(t, err.Error(), testCase.expectedError.Error(), "Unexpected error for Testcase %s", testName)
			} else {
				assert.Equal(t, testCase.expectedError, err)
			}
		})
	}
}

func createClusterResourcePlacement(ls metav1.LabelSelector, cst metav1.LabelSelector, name bool) fleetv1alpha1.ClusterResourcePlacement {

	clusterAffinity := fleetv1alpha1.ClusterAffinity{ClusterSelectorTerms: []fleetv1alpha1.ClusterSelectorTerm{{LabelSelector: cst}}}
	affinity := fleetv1alpha1.Affinity{ClusterAffinity: &clusterAffinity}
	policy := fleetv1alpha1.PlacementPolicy{
		ClusterNames: nil,
		Affinity:     &affinity,
	}

	crp := fleetv1alpha1.ClusterResourcePlacement{
		TypeMeta:   metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{},
		Spec: fleetv1alpha1.ClusterResourcePlacementSpec{
			ResourceSelectors: []fleetv1alpha1.ClusterResourceSelector{
				{
					LabelSelector: &ls,
				},
			},
			Policy: &policy,
		},
		Status: fleetv1alpha1.ClusterResourcePlacementStatus{},
	}

	if name {
		crp.Spec.ResourceSelectors[0].Name = rand.String(5)
	}
	return crp
}
