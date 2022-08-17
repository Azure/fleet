/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package e2e

import (
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"go.goms.io/fleet/apis/v1alpha1"
)

func NewMemberCluster(name string, heartbeat int32, state v1alpha1.ClusterState) *v1alpha1.MemberCluster {
	identity := rbacv1.Subject{
		Name:      name,
		Kind:      "ServiceAccount",
		Namespace: "fleet-system",
	}
	return &v1alpha1.MemberCluster{
		ObjectMeta: v1.ObjectMeta{
			Name: name,
		},
		Spec: v1alpha1.MemberClusterSpec{
			Identity:               identity,
			State:                  state,
			HeartbeatPeriodSeconds: heartbeat,
		},
	}
}

func NewInternalMemberCluster(name, namespace string) *v1alpha1.InternalMemberCluster {
	return &v1alpha1.InternalMemberCluster{
		ObjectMeta: v1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
}

func NewServiceAccount(name, namespace string) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		ObjectMeta: v1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
}

func NewNamespace(name string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: v1.ObjectMeta{
			Name: name,
		},
	}
}
