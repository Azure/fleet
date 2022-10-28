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
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"

	"go.goms.io/fleet/pkg/utils"
)

const (
	kubeSystemNs  = "kube-system"
	fleetSystemNs = "fleet-system"
)

var (
	whitelistedNamespaces = []corev1.Namespace{
		{ObjectMeta: metav1.ObjectMeta{Name: kubeSystemNs}},
		{ObjectMeta: metav1.ObjectMeta{Name: fleetSystemNs}},
	}
)

var _ = Describe("Fleet's Hub cluster webhook tests", func() {
	Context("ReplicaSet validation webhook", func() {
		It("should deny create operation on ReplicaSets", func() {
			rs := &v1.ReplicaSet{
				TypeMeta: metav1.TypeMeta{
					Kind:       "ReplicaSet",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      utils.RandStr(),
					Namespace: utils.RandStr(),
				},
				Spec: v1.ReplicaSetSpec{
					Replicas:        pointer.Int32(2),
					MinReadySeconds: 30,
					Selector: &metav1.LabelSelector{
						MatchLabels: nil,
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      utils.RandStr(),
								Operator: metav1.LabelSelectorOpIn,
								Values:   []string{utils.RandStr()},
							},
						},
					},
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{},
					},
				},
			}

			By("attempting to create a ReplicaSet")
			err := HubCluster.KubeClient.Create(ctx, rs)
			var statusErr *k8sErrors.StatusError
			Expect(errors.As(err, &statusErr)).To(BeTrue(), fmt.Sprintf("Create ReplicaSet call produced error %s. Error type wanted is %s.", reflect.TypeOf(err), reflect.TypeOf(&k8sErrors.StatusError{})))
			Expect(statusErr.ErrStatus.Message).Should(MatchRegexp(`admission webhook "fleet.replicaset.validating" denied the request`))
		})
	})
})
