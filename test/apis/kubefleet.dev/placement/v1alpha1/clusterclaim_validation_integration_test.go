/*
Copyright 2026 The KubeFleet Authors.

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

package v1alpha1

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
)

var _ = Describe("Test ClusterClaim API validation", func() {
	It("should deny unsetting clusterSelectorTerms", func() {
		clusterClaim := &placementv1alpha1.ClusterClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name: "cluster-claim-selector-terms-immutability",
			},
			Spec: placementv1alpha1.ClusterClaimSpec{
				PlacementPolicyRef: &placementv1alpha1.ObjectReference{
					Name:       "test-placement-policy",
					APIVersion: placementv1alpha1.GroupVersion.Version,
					Kind:       "PlacementPolicy",
				},
				ClusterSelectorTerms: []placementv1alpha1.ClusterLabelAndPropertySelectorTerm{
					{
						MatchLabels: map[string]string{"region": "west"},
					},
				},
			},
		}
		Expect(hubClient.Create(ctx, clusterClaim)).Should(Succeed())
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(hubClient.Delete(ctx, clusterClaim))).Should(Succeed())
		})

		clusterClaim.Spec.ClusterSelectorTerms = nil
		Expect(hubClient.Update(ctx, clusterClaim)).Should(MatchError(ContainSubstring("the clusterSelectorTerms field cannot be added or removed after creation")))
	})
})
