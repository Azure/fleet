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

package webhook

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"

	admv1 "k8s.io/api/admissionregistration/v1"
	admv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	storagev1 "k8s.io/api/storage/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	workv1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/cmd/hubagent/options"
	"github.com/kubefleet-dev/kubefleet/pkg/webhook/clusterresourceoverride"
	"github.com/kubefleet-dev/kubefleet/pkg/webhook/clusterresourceplacement"
	"github.com/kubefleet-dev/kubefleet/pkg/webhook/clusterresourceplacementdisruptionbudget"
	"github.com/kubefleet-dev/kubefleet/pkg/webhook/clusterresourceplacementeviction"
	"github.com/kubefleet-dev/kubefleet/pkg/webhook/fleetresourcehandler"
	"github.com/kubefleet-dev/kubefleet/pkg/webhook/membercluster"
	"github.com/kubefleet-dev/kubefleet/pkg/webhook/pod"
	"github.com/kubefleet-dev/kubefleet/pkg/webhook/replicaset"
	"github.com/kubefleet-dev/kubefleet/pkg/webhook/resourceoverride"

	fleetnetworkingv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

const (
	fleetWebhookCertFileName      = "tls.crt"
	fleetWebhookKeyFileName       = "tls.key"
	fleetValidatingWebhookCfgName = "fleet-validating-webhook-configuration"
	fleetGuardRailWebhookCfgName  = "fleet-guard-rail-webhook-configuration"
	fleetMutatingWebhookCfgName   = "fleet-mutating-webhook-configuration"

	crdResourceName                      = "customresourcedefinitions"
	bindingResourceName                  = "bindings"
	configMapResourceName                = "configmaps"
	endPointResourceName                 = "endpoints"
	limitRangeResourceName               = "limitranges"
	persistentVolumeClaimsName           = "persistentvolumeclaims"
	podTemplateResourceName              = "podtemplates"
	replicationControllerResourceName    = "replicationcontrollers"
	resourceQuotaResourceName            = "resourcequotas"
	secretResourceName                   = "secrets"
	serviceAccountResourceName           = "serviceaccounts"
	servicesResourceName                 = "services"
	controllerRevisionResourceName       = "controllerrevisions"
	daemonSetResourceName                = "daemonsets"
	deploymentResourceName               = "deployments"
	statefulSetResourceName              = "statefulsets"
	localSubjectAccessReviewResourceName = "localsubjectaccessreviews"
	horizontalPodAutoScalerResourceName  = "horizontalpodautoscalers"
	cronJobResourceName                  = "cronjobs"
	jobResourceName                      = "jobs"
	workResourceName                     = "works"
	endPointSlicesResourceName           = "endpointslices"
	ingressResourceName                  = "ingresses"
	networkPolicyResourceName            = "networkpolicies"
	podDisruptionBudgetsResourceName     = "poddisruptionbudgets"
	roleResourceName                     = "roles"
	roleBindingResourceName              = "rolebindings"
	csiStorageCapacityResourceName       = "csistoragecapacities"
	memberClusterResourceName            = "memberclusters"
	internalMemberClusterResourceName    = "internalmemberclusters"
	endpointSliceExportResourceName      = "endpointsliceexports"
	endpointSliceImportResourceName      = "endpointsliceimports"
	internalServiceExportResourceName    = "internalserviceexports"
	internalServiceImportResourceName    = "internalserviceimports"
	namespaceResourceName                = "namespaces"
	replicaSetResourceName               = "replicasets"
	podResourceName                      = "pods"
	clusterResourceOverrideName          = "clusterresourceoverrides"
	resourceOverrideName                 = "resourceoverrides"
	evictionName                         = "clusterresourceplacementevictions"
	disruptionBudgetName                 = "clusterresourceplacementdisruptionbudgets"
)

var (
	admissionReviewVersions = []string{admv1.SchemeGroupVersion.Version, admv1beta1.SchemeGroupVersion.Version}

	ignoreFailurePolicy = admv1.Ignore
	failFailurePolicy   = admv1.Fail
	sideEffortsNone     = admv1.SideEffectClassNone
	namespacedScope     = admv1.NamespacedScope
	clusterScope        = admv1.ClusterScope
	shortWebhookTimeout = ptr.To(int32(1))
	longWebhookTimeout  = ptr.To(int32(5))
)

var AddToManagerFuncs []func(manager.Manager) error
var AddToManagerFleetResourceValidator func(manager.Manager, []string, bool) error

// AddToManager adds all Controllers to the Manager
func AddToManager(m manager.Manager, whiteListedUsers []string, denyModifyMemberClusterLabels bool) error {
	for _, f := range AddToManagerFuncs {
		if err := f(m); err != nil {
			return err
		}
	}
	return AddToManagerFleetResourceValidator(m, whiteListedUsers, denyModifyMemberClusterLabels)
}

type Config struct {
	mgr manager.Manager

	// webhook server info
	serviceNamespace string
	serviceName      string
	servicePort      int32
	serviceURL       string

	// caPEM is a PEM encoded CA bundle which will be used to validate the webhook's server certificate.
	caPEM []byte

	clientConnectionType *options.WebhookClientConnectionType

	enableGuardRail bool

	denyModifyMemberClusterLabels bool
}

func NewWebhookConfig(mgr manager.Manager, webhookServiceName string, port int32, clientConnectionType *options.WebhookClientConnectionType, certDir string, enableGuardRail bool, denyModifyMemberClusterLabels bool) (*Config, error) {
	// We assume the Pod namespace should be passed to env through downward API in the Pod spec.
	namespace := os.Getenv("POD_NAMESPACE")
	if namespace == "" {
		return nil, errors.New("fail to obtain Pod namespace from POD_NAMESPACE")
	}
	w := Config{
		mgr:                           mgr,
		servicePort:                   port,
		serviceNamespace:              namespace,
		serviceName:                   webhookServiceName,
		serviceURL:                    fmt.Sprintf("https://%s.%s.svc.cluster.local:%d", webhookServiceName, namespace, port),
		clientConnectionType:          clientConnectionType,
		enableGuardRail:               enableGuardRail,
		denyModifyMemberClusterLabels: denyModifyMemberClusterLabels,
	}
	caPEM, err := w.genCertificate(certDir)
	if err != nil {
		return nil, err
	}
	w.caPEM = caPEM
	return &w, err
}

func (w *Config) Start(ctx context.Context) error {
	klog.V(2).InfoS("setting up webhooks in apiserver from the leader")
	if err := w.createFleetWebhookConfiguration(ctx); err != nil {
		klog.ErrorS(err, "unable to setup webhook configurations in apiserver")
		return err
	}
	return nil
}

// createFleetWebhookConfiguration creates the ValidatingWebhookConfiguration object for the webhook.
func (w *Config) createFleetWebhookConfiguration(ctx context.Context) error {
	if err := w.createMutatingWebhookConfiguration(ctx, w.buildFleetMutatingWebhooks(), fleetMutatingWebhookCfgName); err != nil {
		return err
	}
	if err := w.createValidatingWebhookConfiguration(ctx, w.buildFleetValidatingWebhooks(), fleetValidatingWebhookCfgName); err != nil {
		return err
	}
	if w.enableGuardRail {
		if err := w.createValidatingWebhookConfiguration(ctx, w.buildFleetGuardRailValidatingWebhooks(), fleetGuardRailWebhookCfgName); err != nil {
			return err
		}
	}
	return nil
}

// createMutatingWebhookConfiguration creates the MutatingWebhookConfiguration object for the webhook.
func (w *Config) createMutatingWebhookConfiguration(ctx context.Context, webhooks []admv1.MutatingWebhook, configName string) error {
	mutatingWebhookConfig := admv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: configName,
			Labels: map[string]string{
				"admissions.enforcer/disabled": "true",
			},
		},
		Webhooks: webhooks,
	}

	if err := w.mgr.GetClient().Create(ctx, &mutatingWebhookConfig); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return err
		}
		klog.V(2).InfoS("mutating webhook configuration exists, need to overwrite", "name", configName)
		if err := w.mgr.GetClient().Delete(ctx, &mutatingWebhookConfig); err != nil {
			return err
		}
		if err = w.mgr.GetClient().Create(ctx, &mutatingWebhookConfig); err != nil {
			return err
		}
		klog.V(2).InfoS("successfully overwritten mutating webhook configuration", "name", configName)
		return nil
	}
	klog.V(2).InfoS("successfully created mutating webhook configuration", "name", configName)
	return nil
}

// buildFleetMutatingWebhooks returns a slice of fleet mutating webhook objects.
func (w *Config) buildFleetMutatingWebhooks() []admv1.MutatingWebhook {
	webHooks := []admv1.MutatingWebhook{
		{
			Name:                    "fleet.clusterresourceplacementv1beta1.mutating",
			ClientConfig:            w.createClientConfig(clusterresourceplacement.MutatingPath),
			FailurePolicy:           &ignoreFailurePolicy,
			SideEffects:             &sideEffortsNone,
			AdmissionReviewVersions: admissionReviewVersions,
			Rules: []admv1.RuleWithOperations{
				{
					Operations: []admv1.OperationType{
						admv1.Create,
						admv1.Update,
					},
					Rule: createRule([]string{placementv1beta1.GroupVersion.Group}, []string{placementv1beta1.GroupVersion.Version}, []string{placementv1beta1.ClusterResourcePlacementResource}, &clusterScope),
				},
			},
			TimeoutSeconds: longWebhookTimeout,
		},
	}
	return webHooks
}

func (w *Config) createValidatingWebhookConfiguration(ctx context.Context, webhooks []admv1.ValidatingWebhook, configName string) error {
	validatingWebhookConfig := admv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: configName,
			Labels: map[string]string{
				"admissions.enforcer/disabled": "true",
			},
		},
		Webhooks: webhooks,
	}

	// We need to ensure this webhook configuration is garbage collected if Fleet is uninstalled from the cluster.
	// Since the fleet-system namespace is a prerequisite for core Fleet components, we bind to this namespace.
	if err := bindWebhookConfigToFleetSystem(ctx, w.mgr.GetClient(), &validatingWebhookConfig); err != nil {
		return err
	}

	if err := w.mgr.GetClient().Create(ctx, &validatingWebhookConfig); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return err
		}
		klog.V(2).InfoS("validating webhook configuration exists, need to overwrite", "name", configName)
		// Here we simply use delete/create pattern to implement full overwrite
		if err := w.mgr.GetClient().Delete(ctx, &validatingWebhookConfig); err != nil {
			return err
		}
		if err = w.mgr.GetClient().Create(ctx, &validatingWebhookConfig); err != nil {
			return err
		}
		klog.V(2).InfoS("successfully overwritten validating webhook configuration", "name", configName)
		return nil
	}
	klog.V(2).InfoS("successfully created validating webhook configuration", "name", configName)
	return nil
}

// buildValidatingWebHooks returns a slice of fleet validating webhook objects.
func (w *Config) buildFleetValidatingWebhooks() []admv1.ValidatingWebhook {
	webHooks := []admv1.ValidatingWebhook{
		{
			Name:                    "fleet.pod.validating",
			ClientConfig:            w.createClientConfig(pod.ValidationPath),
			FailurePolicy:           &failFailurePolicy,
			SideEffects:             &sideEffortsNone,
			AdmissionReviewVersions: admissionReviewVersions,
			Rules: []admv1.RuleWithOperations{
				{
					Operations: []admv1.OperationType{
						admv1.Create,
					},
					Rule: createRule([]string{corev1.SchemeGroupVersion.Group}, []string{corev1.SchemeGroupVersion.Version}, []string{podResourceName}, &namespacedScope),
				},
			},
			TimeoutSeconds: longWebhookTimeout,
		},
		{
			Name:                    "fleet.clusterresourceplacementv1beta1.validating",
			ClientConfig:            w.createClientConfig(clusterresourceplacement.ValidationPath),
			FailurePolicy:           &failFailurePolicy,
			SideEffects:             &sideEffortsNone,
			AdmissionReviewVersions: admissionReviewVersions,
			Rules: []admv1.RuleWithOperations{
				{
					Operations: []admv1.OperationType{
						admv1.Create,
						admv1.Update,
					},
					Rule: createRule([]string{placementv1beta1.GroupVersion.Group}, []string{placementv1beta1.GroupVersion.Version}, []string{placementv1beta1.ClusterResourcePlacementResource}, &clusterScope),
				},
			},
			TimeoutSeconds: longWebhookTimeout,
		},
		{
			Name:                    "fleet.replicaset.validating",
			ClientConfig:            w.createClientConfig(replicaset.ValidationPath),
			FailurePolicy:           &failFailurePolicy,
			SideEffects:             &sideEffortsNone,
			AdmissionReviewVersions: admissionReviewVersions,
			Rules: []admv1.RuleWithOperations{
				{
					Operations: []admv1.OperationType{
						admv1.Create,
					},
					Rule: createRule([]string{appsv1.SchemeGroupVersion.Group}, []string{appsv1.SchemeGroupVersion.Version}, []string{replicaSetResourceName}, &namespacedScope),
				},
			},
			TimeoutSeconds: longWebhookTimeout,
		},
		{
			Name:                    "fleet.membercluster.validating",
			ClientConfig:            w.createClientConfig(membercluster.ValidationPath),
			FailurePolicy:           &failFailurePolicy,
			SideEffects:             &sideEffortsNone,
			AdmissionReviewVersions: admissionReviewVersions,
			Rules: []admv1.RuleWithOperations{
				{
					Operations: []admv1.OperationType{
						admv1.Create,
						admv1.Update,
						admv1.Delete,
					},
					Rule: createRule([]string{clusterv1beta1.GroupVersion.Group}, []string{clusterv1beta1.GroupVersion.Version}, []string{memberClusterResourceName}, &clusterScope),
				},
			},
			TimeoutSeconds: longWebhookTimeout,
		},
		{
			Name:                    "fleet.clusterresourceoverride.validating",
			ClientConfig:            w.createClientConfig(clusterresourceoverride.ValidationPath),
			FailurePolicy:           &failFailurePolicy,
			SideEffects:             &sideEffortsNone,
			AdmissionReviewVersions: admissionReviewVersions,
			Rules: []admv1.RuleWithOperations{
				{
					Operations: []admv1.OperationType{
						admv1.Create,
						admv1.Update,
					},
					Rule: createRule([]string{placementv1beta1.GroupVersion.Group}, []string{placementv1beta1.GroupVersion.Version}, []string{clusterResourceOverrideName}, &clusterScope),
				},
			},
			TimeoutSeconds: longWebhookTimeout,
		},
		{
			Name:                    "fleet.resourceoverride.validating",
			ClientConfig:            w.createClientConfig(resourceoverride.ValidationPath),
			FailurePolicy:           &failFailurePolicy,
			SideEffects:             &sideEffortsNone,
			AdmissionReviewVersions: admissionReviewVersions,
			Rules: []admv1.RuleWithOperations{
				{
					Operations: []admv1.OperationType{
						admv1.Create,
						admv1.Update,
					},
					Rule: createRule([]string{placementv1beta1.GroupVersion.Group}, []string{placementv1beta1.GroupVersion.Version}, []string{resourceOverrideName}, &namespacedScope),
				},
			},
			TimeoutSeconds: longWebhookTimeout,
		},
		{
			Name:                    "fleet.clusterresourceplacementeviction.validating",
			ClientConfig:            w.createClientConfig(clusterresourceplacementeviction.ValidationPath),
			FailurePolicy:           &failFailurePolicy,
			SideEffects:             &sideEffortsNone,
			AdmissionReviewVersions: admissionReviewVersions,
			Rules: []admv1.RuleWithOperations{
				{
					Operations: []admv1.OperationType{
						admv1.Create,
					},
					Rule: createRule([]string{placementv1beta1.GroupVersion.Group}, []string{placementv1beta1.GroupVersion.Version}, []string{evictionName}, &clusterScope),
				},
			},
			TimeoutSeconds: longWebhookTimeout,
		},
		{
			Name:                    "fleet.clusterresourceplacementdisruptionbudget.validating",
			ClientConfig:            w.createClientConfig(clusterresourceplacementdisruptionbudget.ValidationPath),
			FailurePolicy:           &failFailurePolicy,
			SideEffects:             &sideEffortsNone,
			AdmissionReviewVersions: admissionReviewVersions,
			Rules: []admv1.RuleWithOperations{
				{
					Operations: []admv1.OperationType{
						admv1.Create, admv1.Update,
					},
					Rule: createRule([]string{placementv1beta1.GroupVersion.Group}, []string{placementv1beta1.GroupVersion.Version}, []string{disruptionBudgetName}, &clusterScope),
				},
			},
			TimeoutSeconds: longWebhookTimeout,
		},
	}

	return webHooks
}

// buildFleetGuardRailValidatingWebhooks returns a slice of fleet guard rail validating webhook objects.
func (w *Config) buildFleetGuardRailValidatingWebhooks() []admv1.ValidatingWebhook {
	// MatchLabels/MatchExpressions values are ANDed to select resources.
	fleetMemberNamespaceSelector := &metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{
			{
				Key:      placementv1beta1.FleetResourceLabelKey,
				Operator: metav1.LabelSelectorOpIn,
				Values:   []string{"true"},
			},
		},
	}
	fleetSystemNamespaceSelector := &metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{
			{
				Key:      corev1.LabelMetadataName,
				Operator: metav1.LabelSelectorOpIn,
				Values:   []string{"fleet-system"},
			},
		},
	}
	kubeNamespaceSelector := &metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{
			{
				Key:      corev1.LabelMetadataName,
				Operator: metav1.LabelSelectorOpIn,
				Values:   []string{"kube-system", "kube-public", "kube-node-lease"},
			},
		},
	}
	cudOperations := []admv1.OperationType{
		admv1.Create,
		admv1.Update,
		admv1.Delete,
	}
	cuOperations := []admv1.OperationType{
		admv1.Create,
		admv1.Update,
	}
	// we don't monitor lease to prevent the deadlock issue, we also don't monitor events.
	namespacedResourcesRules := []admv1.RuleWithOperations{
		// we want to monitor delete operations on all fleet/kube pre-fixed namespaced resources.
		{
			Operations: []admv1.OperationType{admv1.Delete},
			Rule:       createRule([]string{"*"}, []string{"*"}, []string{"*/*"}, &namespacedScope),
		},
		// TODO(ArvindThiru): not handling pods, replicasets as part of the fleet guard rail since they have validating webhooks, need to remove validating webhooks before adding these resources to fleet guard rail.
		{
			Operations: cuOperations,
			Rule: createRule([]string{corev1.SchemeGroupVersion.Group}, []string{corev1.SchemeGroupVersion.Version}, []string{bindingResourceName, configMapResourceName, endPointResourceName,
				limitRangeResourceName, persistentVolumeClaimsName, persistentVolumeClaimsName + "/status", podTemplateResourceName,
				replicationControllerResourceName, replicationControllerResourceName + "/status", resourceQuotaResourceName, resourceQuotaResourceName + "/status", secretResourceName,
				serviceAccountResourceName, servicesResourceName, servicesResourceName + "/status"}, &namespacedScope),
		},
		{
			Operations: cuOperations,
			Rule: createRule([]string{appsv1.SchemeGroupVersion.Group}, []string{appsv1.SchemeGroupVersion.Version}, []string{controllerRevisionResourceName, daemonSetResourceName, daemonSetResourceName + "/status",
				deploymentResourceName, deploymentResourceName + "/status", statefulSetResourceName, statefulSetResourceName + "/status"}, &namespacedScope),
		},
		{
			Operations: cuOperations,
			Rule:       createRule([]string{authorizationv1.SchemeGroupVersion.Group}, []string{authorizationv1.SchemeGroupVersion.Version}, []string{localSubjectAccessReviewResourceName, localSubjectAccessReviewResourceName + "/status"}, &namespacedScope),
		},
		{
			Operations: cuOperations,
			Rule:       createRule([]string{autoscalingv1.SchemeGroupVersion.Group}, []string{autoscalingv1.SchemeGroupVersion.Version}, []string{horizontalPodAutoScalerResourceName, horizontalPodAutoScalerResourceName + "/status"}, &namespacedScope),
		},
		{
			Operations: cuOperations,
			Rule:       createRule([]string{batchv1.SchemeGroupVersion.Group}, []string{batchv1.SchemeGroupVersion.Version}, []string{cronJobResourceName, cronJobResourceName + "/status", jobResourceName, jobResourceName + "/status"}, &namespacedScope),
		},
		{
			Operations: cuOperations,
			Rule:       createRule([]string{discoveryv1.SchemeGroupVersion.Group}, []string{discoveryv1.SchemeGroupVersion.Version}, []string{endPointSlicesResourceName}, &namespacedScope),
		},
		{
			Operations: cuOperations,
			Rule:       createRule([]string{networkingv1.SchemeGroupVersion.Group}, []string{networkingv1.SchemeGroupVersion.Version}, []string{ingressResourceName, ingressResourceName + "/status", networkPolicyResourceName, networkPolicyResourceName + "/status"}, &namespacedScope),
		},
		{
			Operations: cuOperations,
			Rule:       createRule([]string{policyv1.SchemeGroupVersion.Group}, []string{policyv1.SchemeGroupVersion.Version}, []string{podDisruptionBudgetsResourceName, podDisruptionBudgetsResourceName + "/status"}, &namespacedScope),
		},
		{
			Operations: cuOperations,
			Rule:       createRule([]string{rbacv1.SchemeGroupVersion.Group}, []string{rbacv1.SchemeGroupVersion.Version}, []string{roleResourceName, roleBindingResourceName}, &namespacedScope),
		},
		// rules for fleet namespaced resources.
		{
			Operations: cuOperations,
			Rule:       createRule([]string{storagev1.SchemeGroupVersion.Group}, []string{storagev1.SchemeGroupVersion.Version}, []string{csiStorageCapacityResourceName}, &namespacedScope),
		},
		{
			Operations: cuOperations,
			Rule:       createRule([]string{clusterv1beta1.GroupVersion.Group}, []string{clusterv1beta1.GroupVersion.Version}, []string{internalMemberClusterResourceName, internalMemberClusterResourceName + "/status"}, &namespacedScope),
		},
		{
			Operations: cuOperations,
			Rule:       createRule([]string{placementv1beta1.GroupVersion.Group}, []string{placementv1beta1.GroupVersion.Version}, []string{workResourceName, workResourceName + "/status"}, &namespacedScope),
		},
		{
			Operations: cuOperations,
			Rule:       createRule([]string{workv1alpha1.GroupVersion.Group}, []string{workv1alpha1.GroupVersion.Version}, []string{workResourceName, workResourceName + "/status"}, &namespacedScope),
		},
		{
			Operations: cuOperations,
			Rule:       createRule([]string{fleetnetworkingv1alpha1.GroupVersion.Group}, []string{fleetnetworkingv1alpha1.GroupVersion.Version}, []string{endpointSliceExportResourceName, endpointSliceImportResourceName, internalServiceExportResourceName, internalServiceExportResourceName + "/status", internalServiceImportResourceName, internalServiceImportResourceName + "/status"}, &namespacedScope),
		},
	}
	guardRailWebhookConfigurations := []admv1.ValidatingWebhook{
		{
			Name:                    "fleet.customresourcedefinition.guardrail.validating",
			ClientConfig:            w.createClientConfig(fleetresourcehandler.ValidationPath),
			FailurePolicy:           &ignoreFailurePolicy,
			SideEffects:             &sideEffortsNone,
			AdmissionReviewVersions: admissionReviewVersions,
			Rules: []admv1.RuleWithOperations{
				{
					Operations: cudOperations,
					Rule:       createRule([]string{apiextensionsv1.SchemeGroupVersion.Group}, []string{apiextensionsv1.SchemeGroupVersion.Version}, []string{crdResourceName}, &clusterScope),
				},
			},
			TimeoutSeconds: shortWebhookTimeout,
		},
		{
			Name:                    "fleet.membercluster.guardrail.validating",
			ClientConfig:            w.createClientConfig(fleetresourcehandler.ValidationPath),
			FailurePolicy:           &ignoreFailurePolicy,
			SideEffects:             &sideEffortsNone,
			AdmissionReviewVersions: admissionReviewVersions,
			Rules: []admv1.RuleWithOperations{
				{
					Operations: cudOperations,
					Rule:       createRule([]string{clusterv1beta1.GroupVersion.Group}, []string{clusterv1beta1.GroupVersion.Version}, []string{memberClusterResourceName, memberClusterResourceName + "/status"}, &clusterScope),
				},
			},
			TimeoutSeconds: shortWebhookTimeout,
		},
		{
			Name:                    "fleet.fleetmembernamespacedresources.guardrail.validating",
			ClientConfig:            w.createClientConfig(fleetresourcehandler.ValidationPath),
			FailurePolicy:           &ignoreFailurePolicy,
			SideEffects:             &sideEffortsNone,
			AdmissionReviewVersions: admissionReviewVersions,
			NamespaceSelector:       fleetMemberNamespaceSelector,
			Rules:                   namespacedResourcesRules,
			TimeoutSeconds:          shortWebhookTimeout,
		},
		{
			Name:                    "fleet.fleetsystemnamespacedresources.guardrail.validating",
			ClientConfig:            w.createClientConfig(fleetresourcehandler.ValidationPath),
			FailurePolicy:           &ignoreFailurePolicy,
			SideEffects:             &sideEffortsNone,
			AdmissionReviewVersions: admissionReviewVersions,
			NamespaceSelector:       fleetSystemNamespaceSelector,
			Rules:                   namespacedResourcesRules,
			TimeoutSeconds:          shortWebhookTimeout,
		},
		{
			Name:                    "fleet.kubenamespacedresources.guardrail.validating",
			ClientConfig:            w.createClientConfig(fleetresourcehandler.ValidationPath),
			FailurePolicy:           &ignoreFailurePolicy,
			SideEffects:             &sideEffortsNone,
			AdmissionReviewVersions: admissionReviewVersions,
			NamespaceSelector:       kubeNamespaceSelector,
			Rules:                   namespacedResourcesRules,
			TimeoutSeconds:          shortWebhookTimeout,
		},
		{
			Name:                    "fleet.namespace.guardrail.validating",
			ClientConfig:            w.createClientConfig(fleetresourcehandler.ValidationPath),
			FailurePolicy:           &ignoreFailurePolicy,
			SideEffects:             &sideEffortsNone,
			AdmissionReviewVersions: admissionReviewVersions,
			Rules: []admv1.RuleWithOperations{
				{
					Operations: cudOperations,
					Rule:       createRule([]string{corev1.SchemeGroupVersion.Group}, []string{corev1.SchemeGroupVersion.Version}, []string{namespaceResourceName}, &clusterScope),
				},
			},
			TimeoutSeconds: shortWebhookTimeout,
		},
	}

	return guardRailWebhookConfigurations
}

// createClientConfig generates the client configuration with either service ref or URL for the argued interface.
func (w *Config) createClientConfig(validationPath string) admv1.WebhookClientConfig {
	serviceRef := admv1.ServiceReference{
		Namespace: w.serviceNamespace,
		Name:      w.serviceName,
		Port:      ptr.To(w.servicePort),
	}
	serviceEndpoint := w.serviceURL + validationPath
	serviceRef.Path = ptr.To(validationPath)
	config := admv1.WebhookClientConfig{
		CABundle: w.caPEM,
	}
	switch *w.clientConnectionType {
	case options.Service:
		config.Service = &serviceRef
	case options.URL:
		config.URL = ptr.To(serviceEndpoint)
	}
	return config
}

// genCertificate generates the serving cerficiate for the webhook server.
func (w *Config) genCertificate(certDir string) ([]byte, error) {
	caPEM, certPEM, keyPEM, err := w.genSelfSignedCert()
	if err != nil {
		klog.ErrorS(err, "fail to generate self-signed cert")
		return nil, err
	}

	// generate certificate files (i.e., tls.crt and tls.key)
	if err := genCertAndKeyFile(certPEM, keyPEM, certDir); err != nil {
		klog.ErrorS(err, "fail to generate certificate and key files")
		return nil, err
	}
	return caPEM, nil
}

// genSelfSignedCert generates the self signed Certificate/Key pair
func (w *Config) genSelfSignedCert() (caPEMByte, certPEMByte, keyPEMByte []byte, err error) {
	// CA config
	ca := &x509.Certificate{
		SerialNumber: big.NewInt(2022),
		Subject: pkix.Name{
			CommonName:         "fleet.azure.com",
			OrganizationalUnit: []string{"Azure Kubernetes Service"},
			Organization:       []string{"Microsoft"},
			Locality:           []string{"Redmond"},
			Province:           []string{"Washington"},
			Country:            []string{"United States of America"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(10, 0, 0), // Set expiration time to be 10 years for now
		IsCA:                  true,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}

	// CA private key
	caPrvKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return nil, nil, nil, err
	}

	// Self signed CA certificate
	caBytes, err := x509.CreateCertificate(rand.Reader, ca, ca, &caPrvKey.PublicKey, caPrvKey)
	if err != nil {
		return nil, nil, nil, err
	}

	// PEM encode CA cert
	caPEM := new(bytes.Buffer)
	if err := pem.Encode(caPEM, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: caBytes,
	}); err != nil {
		return nil, nil, nil, err
	}
	caPEMByte = caPEM.Bytes()

	dnsNames := []string{
		fmt.Sprintf("%s.%s.svc", w.serviceName, w.serviceNamespace),
		fmt.Sprintf("%s.%s.svc.cluster.local", w.serviceName, w.serviceNamespace),
	}
	// server cert config
	cert := &x509.Certificate{
		DNSNames:     dnsNames,
		SerialNumber: big.NewInt(2022),
		Subject: pkix.Name{
			CommonName:         fmt.Sprintf("%s.cert.server", w.serviceName),
			OrganizationalUnit: []string{"Azure Kubernetes Service"},
			Organization:       []string{"Microsoft"},
			Locality:           []string{"Redmond"},
			Province:           []string{"Washington"},
			Country:            []string{"United States of America"},
		},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().AddDate(10, 0, 0),
		SubjectKeyId: []byte{1, 2, 3, 4, 5},
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}

	// server private key
	certPrvKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return nil, nil, nil, err
	}

	// sign the server cert
	certBytes, err := x509.CreateCertificate(rand.Reader, cert, ca, &certPrvKey.PublicKey, caPrvKey)
	if err != nil {
		return nil, nil, nil, err
	}

	// PEM encode the  server cert and key
	certPEM := new(bytes.Buffer)
	if err := pem.Encode(certPEM, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certBytes,
	}); err != nil {
		return nil, nil, nil, err
	}
	certPEMByte = certPEM.Bytes()

	certPrvKeyPEM := new(bytes.Buffer)
	if err := pem.Encode(certPrvKeyPEM, &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(certPrvKey),
	}); err != nil {
		return nil, nil, nil, err
	}
	keyPEMByte = certPrvKeyPEM.Bytes()
	return caPEMByte, certPEMByte, keyPEMByte, nil
}

// genCertAndKeyFile creates the serving certificate/key files for the webhook server
func genCertAndKeyFile(certData, keyData []byte, certDir string) error {
	// always remove first
	if err := os.RemoveAll(certDir); err != nil {
		return fmt.Errorf("fail to remove certificates: %w", err)
	}
	if err := os.MkdirAll(certDir, 0755); err != nil {
		return fmt.Errorf("could not create directory %q to store certificates: %w", certDir, err)
	}
	certPath := filepath.Join(certDir, fleetWebhookCertFileName)
	f, err := os.OpenFile(filepath.Clean(certPath), os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("could not open %q: %w", certPath, err)
	}
	defer f.Close()
	certBlock, _ := pem.Decode(certData)
	if certBlock == nil {
		return fmt.Errorf("invalid certificate data")
	}
	if err := pem.Encode(f, certBlock); err != nil {
		return err
	}

	keyPath := filepath.Join(certDir, fleetWebhookKeyFileName)
	kf, err := os.OpenFile(filepath.Clean(keyPath), os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("could not open %q: %w", keyPath, err)
	}

	keyBlock, _ := pem.Decode(keyData)
	if keyBlock == nil {
		return fmt.Errorf("invalid key data")
	}
	if err := pem.Encode(kf, keyBlock); err != nil {
		return err
	}
	klog.V(2).InfoS("successfully generate certificate and key files")
	return nil
}

// bindWebhookConfigToFleetSystem sets the OwnerReference of the argued ValidatingWebhookConfiguration to the cluster scoped fleet-system namespace.
func bindWebhookConfigToFleetSystem(ctx context.Context, k8Client client.Client, validatingWebhookConfig *admv1.ValidatingWebhookConfiguration) error {
	var fleetNs corev1.Namespace
	if err := k8Client.Get(ctx, client.ObjectKey{Name: "fleet-system"}, &fleetNs); err != nil {
		return err
	}

	ownerRef := metav1.OwnerReference{
		APIVersion:         fleetNs.GroupVersionKind().GroupVersion().String(),
		Kind:               fleetNs.Kind,
		Name:               fleetNs.GetName(),
		UID:                fleetNs.GetUID(),
		BlockOwnerDeletion: ptr.To(false),
	}

	validatingWebhookConfig.OwnerReferences = []metav1.OwnerReference{ownerRef}
	return nil
}

// createRule returns a admission rule using the arguments passed.
func createRule(apiGroups, apiVersions, resources []string, scopeType *admv1.ScopeType) admv1.Rule {
	return admv1.Rule{
		APIGroups:   apiGroups,
		APIVersions: apiVersions,
		Resources:   resources,
		Scope:       scopeType,
	}
}
