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

package resource

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/restmapper"
)

// Common verbs for API resources
var (
	// VerbsAll includes all standard Kubernetes verbs
	VerbsAll = []string{"list", "watch", "get", "create", "update", "patch", "delete"}
	// VerbsReadOnly includes verbs for read-only access
	VerbsReadOnly = []string{"list", "watch", "get"}
	// VerbsNoWatch includes verbs without watch capability
	VerbsNoWatch = []string{"get", "create", "update", "patch", "delete"}
)

// APIGroupV1 returns a standard core v1 API group for testing
func APIGroupV1() metav1.APIGroup {
	return metav1.APIGroup{
		Name: "",
		Versions: []metav1.GroupVersionForDiscovery{
			{GroupVersion: "v1", Version: "v1"},
		},
		PreferredVersion: metav1.GroupVersionForDiscovery{
			GroupVersion: "v1",
			Version:      "v1",
		},
	}
}

// APIGroupAppsV1 returns a standard apps/v1 API group for testing
func APIGroupAppsV1() metav1.APIGroup {
	return metav1.APIGroup{
		Name: "apps",
		Versions: []metav1.GroupVersionForDiscovery{
			{GroupVersion: "apps/v1", Version: "v1"},
		},
		PreferredVersion: metav1.GroupVersionForDiscovery{
			GroupVersion: "apps/v1",
			Version:      "v1",
		},
	}
}

// APIGroupResourcesV1 returns APIGroupResources for core v1 with the provided resources
func APIGroupResourcesV1(resources ...metav1.APIResource) *restmapper.APIGroupResources {
	return &restmapper.APIGroupResources{
		Group: APIGroupV1(),
		VersionedResources: map[string][]metav1.APIResource{
			"v1": resources,
		},
	}
}

// APIGroupResourcesAppsV1 returns APIGroupResources for apps/v1 with the provided resources
func APIGroupResourcesAppsV1(resources ...metav1.APIResource) *restmapper.APIGroupResources {
	return &restmapper.APIGroupResources{
		Group: APIGroupAppsV1(),
		VersionedResources: map[string][]metav1.APIResource{
			"v1": resources,
		},
	}
}

// APIResourceConfigMap returns a standard ConfigMap APIResource for testing
func APIResourceConfigMap() metav1.APIResource {
	return metav1.APIResource{
		Name:       "configmaps",
		Kind:       "ConfigMap",
		Namespaced: true,
		Verbs:      VerbsReadOnly,
	}
}

// APIResourceSecret returns a standard Secret APIResource for testing
func APIResourceSecret() metav1.APIResource {
	return metav1.APIResource{
		Name:       "secrets",
		Kind:       "Secret",
		Namespaced: true,
		Verbs:      VerbsReadOnly,
	}
}

// APIResourcePod returns a standard Pod APIResource for testing
func APIResourcePod() metav1.APIResource {
	return metav1.APIResource{
		Name:       "pods",
		Kind:       "Pod",
		Namespaced: true,
		Verbs:      VerbsReadOnly,
	}
}

// APIResourceService returns a standard Service APIResource for testing
func APIResourceService() metav1.APIResource {
	return metav1.APIResource{
		Name:       "services",
		Kind:       "Service",
		Namespaced: true,
		Verbs:      VerbsReadOnly,
	}
}

// APIResourceNamespace returns a standard Namespace APIResource for testing
func APIResourceNamespace() metav1.APIResource {
	return metav1.APIResource{
		Name:       "namespaces",
		Kind:       "Namespace",
		Namespaced: false,
		Verbs:      VerbsReadOnly,
	}
}

// APIResourceNode returns a standard Node APIResource for testing
func APIResourceNode() metav1.APIResource {
	return metav1.APIResource{
		Name:       "nodes",
		Kind:       "Node",
		Namespaced: false,
		Verbs:      VerbsReadOnly,
	}
}

// APIResourceDeployment returns a standard Deployment APIResource for testing
func APIResourceDeployment() metav1.APIResource {
	return metav1.APIResource{
		Name:       "deployments",
		Kind:       "Deployment",
		Namespaced: true,
		Verbs:      VerbsReadOnly,
	}
}

// APIResourceStatefulSet returns a standard StatefulSet APIResource for testing
func APIResourceStatefulSet() metav1.APIResource {
	return metav1.APIResource{
		Name:       "statefulsets",
		Kind:       "StatefulSet",
		Namespaced: true,
		Verbs:      VerbsReadOnly,
	}
}

// APIResourceDaemonSet returns a standard DaemonSet APIResource for testing
func APIResourceDaemonSet() metav1.APIResource {
	return metav1.APIResource{
		Name:       "daemonsets",
		Kind:       "DaemonSet",
		Namespaced: true,
		Verbs:      VerbsReadOnly,
	}
}

// APIResourceClusterRole returns a standard ClusterRole APIResource for testing
func APIResourceClusterRole() metav1.APIResource {
	return metav1.APIResource{
		Name:       "clusterroles",
		Kind:       "ClusterRole",
		Namespaced: false,
		Verbs:      VerbsReadOnly,
	}
}

// APIResourceListV1 returns a standard v1 APIResourceList for testing with common core resources
func APIResourceListV1() *metav1.APIResourceList {
	return &metav1.APIResourceList{
		GroupVersion: "v1",
		APIResources: []metav1.APIResource{
			APIResourceConfigMap(),
			APIResourceSecret(),
			APIResourcePod(),
			APIResourceService(),
			APIResourceNamespace(),
			APIResourceNode(),
		},
	}
}

// APIResourceListAppsV1 returns a standard apps/v1 APIResourceList for testing
func APIResourceListAppsV1() *metav1.APIResourceList {
	return &metav1.APIResourceList{
		GroupVersion: "apps/v1",
		APIResources: []metav1.APIResource{
			APIResourceDeployment(),
			APIResourceStatefulSet(),
			APIResourceDaemonSet(),
		},
	}
}

// APIResourceWithVerbs creates a custom APIResource with specified verbs for testing
func APIResourceWithVerbs(name, kind string, namespaced bool, verbs []string) metav1.APIResource {
	return metav1.APIResource{
		Name:       name,
		Kind:       kind,
		Namespaced: namespaced,
		Verbs:      verbs,
	}
}

// GVK helpers - GroupVersionKind for common resources

// GVKConfigMap returns the GroupVersionKind for ConfigMap
func GVKConfigMap() schema.GroupVersionKind {
	return schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
}

// GVKSecret returns the GroupVersionKind for Secret
func GVKSecret() schema.GroupVersionKind {
	return schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Secret"}
}

// GVKPod returns the GroupVersionKind for Pod
func GVKPod() schema.GroupVersionKind {
	return schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"}
}

// GVKService returns the GroupVersionKind for Service
func GVKService() schema.GroupVersionKind {
	return schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Service"}
}

// GVKNamespace returns the GroupVersionKind for Namespace
func GVKNamespace() schema.GroupVersionKind {
	return schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Namespace"}
}

// GVKNode returns the GroupVersionKind for Node
func GVKNode() schema.GroupVersionKind {
	return schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Node"}
}

// GVKDeployment returns the GroupVersionKind for Deployment
func GVKDeployment() schema.GroupVersionKind {
	return schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}
}

// GVKStatefulSet returns the GroupVersionKind for StatefulSet
func GVKStatefulSet() schema.GroupVersionKind {
	return schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "StatefulSet"}
}

// GVKDaemonSet returns the GroupVersionKind for DaemonSet
func GVKDaemonSet() schema.GroupVersionKind {
	return schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "DaemonSet"}
}

// GVKClusterRole returns the GroupVersionKind for ClusterRole
func GVKClusterRole() schema.GroupVersionKind {
	return schema.GroupVersionKind{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRole"}
}

// GVR helpers - GroupVersionResource for common resources

// GVRConfigMap returns the GroupVersionResource for configmaps
func GVRConfigMap() schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}
}

// GVRSecret returns the GroupVersionResource for secrets
func GVRSecret() schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: "", Version: "v1", Resource: "secrets"}
}

// GVRPod returns the GroupVersionResource for pods
func GVRPod() schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
}

// GVRService returns the GroupVersionResource for services
func GVRService() schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: "", Version: "v1", Resource: "services"}
}

// GVRNamespace returns the GroupVersionResource for namespaces
func GVRNamespace() schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: "", Version: "v1", Resource: "namespaces"}
}

// GVRNode returns the GroupVersionResource for nodes
func GVRNode() schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: "", Version: "v1", Resource: "nodes"}
}

// GVRDeployment returns the GroupVersionResource for deployments
func GVRDeployment() schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
}

// GVRStatefulSet returns the GroupVersionResource for statefulsets
func GVRStatefulSet() schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"}
}

// GVRDaemonSet returns the GroupVersionResource for daemonsets
func GVRDaemonSet() schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "daemonsets"}
}

// GVRClusterRole returns the GroupVersionResource for clusterroles
func GVRClusterRole() schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterroles"}
}
