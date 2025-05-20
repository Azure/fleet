/*
Copyright 2025 The KubeFleet Authors.

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

package e2e

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"go.goms.io/fleet/test/e2e/framework"
)

const (
	// Custom scope for testing
	testAKSScope       = "test-aks-scope-value"
	aksScopeEnvVarName = "AKS_SCOPE"
	
	// Test namespace
	testNamespace = "fleet-aks-scope-test"
	
	// Test container properties
	testImage         = "golang:1.19"
	testPodName       = "aks-scope-test-pod"
	testPodNameEmpty  = "aks-scope-test-pod-empty"
	testPodNameDirect = "aks-scope-test-pod-direct"
	
	// Test ConfigMap with Go program to verify scope
	testConfigMapName = "aks-scope-test-program"
	
	// Path to the Go program in the repository
	testProgramPath = "test/e2e/resources/aks_scope_test.go"
	
	// Direct scope value for testing
	directScopeValue = "direct-scope-value"
)

var _ = Describe("Azure MSI Auth Provider Configuration", Ordered, func() {
	// This test validates that the AKS scope can be configured via environment variable
	// and follows the expected hierarchy (direct parameter > env var > default)

	var testPods []*corev1.Pod
	var testConfigMap *corev1.ConfigMap

	BeforeAll(func() {
		// Create a namespace for our tests
		testNS := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: testNamespace,
			},
		}
		framework.CreateNamespace(hubCluster, testNS)
		
		// Read the test program file
		cmd := exec.Command("cat", filepath.Join("/home/runner/work/fleet/fleet", testProgramPath))
		programBytes, err := cmd.Output()
		Expect(err).Should(BeNil(), "Failed to read test program file")
		
		// Create ConfigMap with the test program
		testConfigMap = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testConfigMapName,
				Namespace: testNamespace,
			},
			Data: map[string]string{
				"test.go": string(programBytes),
			},
		}
		
		err = hubClient.Create(context.TODO(), testConfigMap)
		Expect(err).Should(BeNil(), "Failed to create ConfigMap with test program")
	})

	AfterAll(func() {
		// Clean up the test resources
		for _, pod := range testPods {
			hubClient.Delete(context.TODO(), pod)
		}
		
		hubClient.Delete(context.TODO(), &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testConfigMapName,
				Namespace: testNamespace,
			},
		})

		// Delete the test namespace and all its resources
		hubClient.Delete(context.TODO(), &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: testNamespace,
			},
		})
	})

	// Helper function to create a test pod
	createTestPod := func(name string, envVars []corev1.EnvVar, directScope string) *corev1.Pod {
		directScopeArg := ""
		if directScope != "" {
			directScopeArg = fmt.Sprintf(", %q", directScope)
		}
		
		testPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: testNamespace,
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "test-container",
						Image: testImage,
						Command: []string{
							"/bin/sh",
							"-c",
							// Compile and run the test program
							"cd /go/src && " +
								"echo \"${TEST_PROGRAM}\" > main.go && " +
								"go mod init aks-scope-test && " +
								"go mod edit -replace go.goms.io/fleet=/go/src/go.goms.io/fleet && " +
								"go get go.goms.io/fleet@v0.0.0 && " +
								fmt.Sprintf("go run main.go \"test-client-id\"%s", directScopeArg),
						},
						Env: append(envVars, corev1.EnvVar{
							Name: "TEST_PROGRAM",
							ValueFrom: &corev1.EnvVarSource{
								ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: testConfigMapName,
									},
									Key: "test.go",
								},
							},
						}),
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      "fleet-source",
								MountPath: "/go/src/go.goms.io/fleet",
							},
						},
					},
				},
				Volumes: []corev1.Volume{
					{
						Name: "fleet-source",
						VolumeSource: corev1.VolumeSource{
							HostPath: &corev1.HostPathVolumeSource{
								Path: "/home/runner/work/fleet/fleet",
							},
						},
					},
				},
				RestartPolicy: corev1.RestartPolicyNever,
			},
		}

		err := hubClient.Create(context.TODO(), testPod)
		Expect(err).Should(BeNil(), "Failed to create test pod")
		
		testPods = append(testPods, testPod)
		return testPod
	}
	
	// Helper function to wait for a pod to complete and get logs
	waitForPodAndGetLogs := func(name string) string {
		Eventually(func() corev1.PodPhase {
			var pod corev1.Pod
			err := hubClient.Get(context.TODO(), types.NamespacedName{
				Name:      name,
				Namespace: testNamespace,
			}, &pod)
			
			if err != nil {
				return corev1.PodUnknown
			}
			return pod.Status.Phase
		}, 60*time.Second, 1*time.Second).Should(Equal(corev1.PodSucceeded))

		logs, err := getPodLogs(hubClusterName, testNamespace, name)
		Expect(err).Should(BeNil(), "Failed to get pod logs")
		return logs
	}

	It("should use environment variable when no direct scope is provided", func() {
		By("Creating a test pod with AKS_SCOPE environment variable")
		
		envVars := []corev1.EnvVar{{
			Name:  aksScopeEnvVarName,
			Value: testAKSScope,
		}}
		
		createTestPod(testPodName, envVars, "")
		logs := waitForPodAndGetLogs(testPodName)
		
		Expect(logs).To(ContainSubstring(fmt.Sprintf("AKS_SCOPE environment variable: %s", testAKSScope)))
		Expect(logs).To(ContainSubstring(fmt.Sprintf("Configured scope in Azure MSI auth provider: %s", testAKSScope)))
		Expect(logs).To(ContainSubstring("Success: Provider is using the scope from environment variable"))
	})
	
	It("should use default scope when no environment variable or direct scope is provided", func() {
		By("Creating a test pod without AKS_SCOPE environment variable")
		
		createTestPod(testPodNameEmpty, []corev1.EnvVar{}, "")
		logs := waitForPodAndGetLogs(testPodNameEmpty)
		
		Expect(logs).To(ContainSubstring("AKS_SCOPE environment variable: "))
		Expect(logs).To(ContainSubstring("Success: Provider is using the default scope"))
	})
	
	It("should prioritize direct scope over environment variable", func() {
		By("Creating a test pod with both direct scope and environment variable")
		
		envVars := []corev1.EnvVar{{
			Name:  aksScopeEnvVarName,
			Value: testAKSScope,
		}}
		
		createTestPod(testPodNameDirect, envVars, directScopeValue)
		logs := waitForPodAndGetLogs(testPodNameDirect)
		
		Expect(logs).To(ContainSubstring(fmt.Sprintf("AKS_SCOPE environment variable: %s", testAKSScope)))
		Expect(logs).To(ContainSubstring(fmt.Sprintf("Configured scope in Azure MSI auth provider: %s", directScopeValue)))
		Expect(logs).NotTo(ContainSubstring("Success: Provider is using the scope from environment variable"))
	})
})

// getPodLogs retrieves the logs from a pod using kubectl
func getPodLogs(clusterName, namespace, podName string) (string, error) {
	cmd := exec.Command("kubectl", "--context", clusterName, "-n", namespace, "logs", podName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get logs: %v, output: %s", err, string(output))
	}
	
	return strings.TrimSpace(string(output)), nil
}