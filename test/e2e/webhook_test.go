/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package e2e

import (
	"errors"
	"fmt"
	"reflect"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

// Note that this container will run in parallel with other containers.
var _ = Describe("creating CRP and selecting resources by name", func() {
	crpName := fmt.Sprintf(crpNameTemplate, GinkgoParallelProcess())

	It("should deny create on CRP with invalid label selector", func() {
		selector := invalidWorkResourceSelector()
		// Create the CRP.
		crp := &placementv1beta1.ClusterResourcePlacement{
			ObjectMeta: metav1.ObjectMeta{
				Name: crpName,
				// Add a custom finalizer; this would allow us to better observe
				// the behavior of the controllers.
				Finalizers: []string{customDeletionBlockerFinalizer},
			},
			Spec: placementv1beta1.ClusterResourcePlacementSpec{
				ResourceSelectors: selector,
			},
		}
		By(fmt.Sprintf("expecting denial of CREATE placement %s", crpName))
		err := hubClient.Create(ctx, crp)
		fmt.Println(fmt.Sprintf("the labelSelector and name fields are mutually exclusive in selector %+v", selector))
		var statusErr *k8sErrors.StatusError
		Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create CRP call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
		Expect(string(statusErr.Status().Reason)).Should(Equal(fmt.Sprintf("the labelSelector and name fields are mutually exclusive in selector %+v", selector[0])))
	})
})
