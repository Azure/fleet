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

package admissionpolicymanager

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kubefleet-dev/kubefleet/pkg/utils"
)

const (
	kubeSystemNSName = "kube-system"
)

func newPodSpec() corev1.PodSpec {
	return corev1.PodSpec{
		Containers: []corev1.Container{
			{
				Name:  "nginx",
				Image: "nginx",
			},
		},
	}
}

func newPod(namespace, name string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: newPodSpec(),
	}
}

func newReplicaSet(namespace, name string) *appsv1.ReplicaSet {
	podLabels := map[string]string{"app": name}
	return &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: appsv1.ReplicaSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: podLabels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: podLabels,
				},
				Spec: newPodSpec(),
			},
		},
	}
}

func newDeployment(namespace, name string) *appsv1.Deployment {
	podLabels := map[string]string{"app": name}
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: podLabels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: podLabels,
				},
				Spec: newPodSpec(),
			},
		},
	}
}

// All the requests in this container are sent as the cluster admin (system:masters), which also verifies that the
// admission policy does not exempt any caller.
var _ = Describe("deny pods and replica sets outside reserved namespaces", Ordered, func() {
	suffix := utils.RandStr()
	workNSName := fmt.Sprintf("work-%s", suffix)
	fleetNSName := fmt.Sprintf("fleet-%s", suffix)
	// A namespace whose name starts with a reserved namespace prefix minus the trailing hyphen.
	fleetPrefixedNSName := fmt.Sprintf("fleetwork-%s", suffix)

	// The objects that the test cases create in the kube-system namespace, which other test containers share;
	// they are cleaned up after all the test cases have run.
	var kubeSystemObjs []client.Object

	BeforeAll(func() {
		createNamespace(workNSName)
		createNamespace(fleetNSName)
		createNamespace(fleetPrefixedNSName)

		By("waiting for the admission policy to take effect")
		waitUntilPolicyIsEnforced(func() error {
			return hubUncachedClient.Create(ctx, newPod(workNSName, fmt.Sprintf("probe-%s", utils.RandStr())))
		}, podsAndReplicaSetsVAPPolicyName, wantPodsAndReplicaSetsDenialMsg)
	})

	It("should deny creating pods in a non-reserved namespace", func() {
		err := hubUncachedClient.Create(ctx, newPod(workNSName, fmt.Sprintf("pod-%s", utils.RandStr())))
		Expect(wantDeniedByPolicy(err, podsAndReplicaSetsVAPPolicyName, wantPodsAndReplicaSetsDenialMsg)).To(Succeed())
	})

	It("should deny creating replica sets in a non-reserved namespace", func() {
		err := hubUncachedClient.Create(ctx, newReplicaSet(workNSName, fmt.Sprintf("rs-%s", utils.RandStr())))
		Expect(wantDeniedByPolicy(err, podsAndReplicaSetsVAPPolicyName, wantPodsAndReplicaSetsDenialMsg)).To(Succeed())
	})

	It("should deny creating pods in a namespace whose name starts with a reserved prefix minus the hyphen", func() {
		err := hubUncachedClient.Create(ctx, newPod(fleetPrefixedNSName, fmt.Sprintf("pod-%s", utils.RandStr())))
		Expect(wantDeniedByPolicy(err, podsAndReplicaSetsVAPPolicyName, wantPodsAndReplicaSetsDenialMsg)).To(Succeed())
	})

	It("should allow creating pods and replica sets in a fleet- prefixed namespace", func() {
		Expect(hubUncachedClient.Create(ctx, newPod(fleetNSName, fmt.Sprintf("pod-%s", utils.RandStr())))).To(Succeed(), "Failed to create a pod")
		Expect(hubUncachedClient.Create(ctx, newReplicaSet(fleetNSName, fmt.Sprintf("rs-%s", utils.RandStr())))).To(Succeed(), "Failed to create a replica set")
	})

	It("should allow creating pods and replica sets in a kube- prefixed namespace", func() {
		pod := newPod(kubeSystemNSName, fmt.Sprintf("pod-%s", utils.RandStr()))
		Expect(hubUncachedClient.Create(ctx, pod)).To(Succeed(), "Failed to create a pod")
		kubeSystemObjs = append(kubeSystemObjs, pod)

		rs := newReplicaSet(kubeSystemNSName, fmt.Sprintf("rs-%s", utils.RandStr()))
		Expect(hubUncachedClient.Create(ctx, rs)).To(Succeed(), "Failed to create a replica set")
		kubeSystemObjs = append(kubeSystemObjs, rs)
	})

	It("should allow creating deployments in a non-reserved namespace", func() {
		Expect(hubUncachedClient.Create(ctx, newDeployment(workNSName, fmt.Sprintf("deploy-%s", utils.RandStr())))).To(Succeed(), "Failed to create a deployment")
	})

	AfterAll(func() {
		for _, obj := range kubeSystemObjs {
			Expect(client.IgnoreNotFound(hubUncachedClient.Delete(ctx, obj))).To(Succeed(), "Failed to delete object %s", client.ObjectKeyFromObject(obj))
		}
	})
})
