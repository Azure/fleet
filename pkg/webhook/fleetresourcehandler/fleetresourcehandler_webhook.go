package fleetresourcehandler

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	workv1alpha1 "sigs.k8s.io/work-api/pkg/apis/v1alpha1"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/webhook/validation"
)

const (
	// ValidationPath is the webhook service path which admission requests are routed to for validating custom resource definition resources.
	ValidationPath             = "/validate-v1-fleetresourcehandler"
	groupMatch                 = `^[^.]*\.(.*)`
	fleetMemberNamespacePrefix = "fleet-member"
)

var (
	crdGVK       = metav1.GroupVersionKind{Group: v1.SchemeGroupVersion.Group, Version: v1.SchemeGroupVersion.Version, Kind: "CustomResourceDefinition"}
	mcGVK        = metav1.GroupVersionKind{Group: fleetv1alpha1.GroupVersion.Group, Version: fleetv1alpha1.GroupVersion.Version, Kind: "MemberCluster"}
	imcGVK       = metav1.GroupVersionKind{Group: fleetv1alpha1.GroupVersion.Group, Version: fleetv1alpha1.GroupVersion.Version, Kind: "InternalMemberCluster"}
	namespaceGVK = metav1.GroupVersionKind{Group: corev1.SchemeGroupVersion.Group, Version: corev1.SchemeGroupVersion.Version, Kind: "Namespace"}
	workGVK      = metav1.GroupVersionKind{Group: workv1alpha1.GroupVersion.Group, Version: workv1alpha1.GroupVersion.Version, Kind: "Work"}
	eventGVK     = metav1.GroupVersionKind{Group: corev1.SchemeGroupVersion.Group, Version: corev1.SchemeGroupVersion.Version, Kind: "Event"}
)

// Add registers the webhook for K8s built-in object types.
func Add(mgr manager.Manager, whiteListedUsers []string) error {
	hookServer := mgr.GetWebhookServer()
	hookServer.Register(ValidationPath, &webhook.Admission{Handler: &fleetResourceValidator{client: mgr.GetClient(), whiteListedUsers: whiteListedUsers}})
	return nil
}

type fleetResourceValidator struct {
	client           client.Client
	whiteListedUsers []string
	decoder          *admission.Decoder
}

// Handle receives the request then allows/denies the request to modify fleet resources.
// TODO(Arvindthiru): Need to handle fleet v1beta1 resources before enabling webhook in RP cause events for v1beta1 IMC will be blocked.
func (v *fleetResourceValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	// special case for Kind:Namespace resources req.Name and req.Namespace has the same value the ObjectMeta.Name of Namespace.
	if req.Kind.Kind == "Namespace" {
		req.Namespace = ""
	}
	namespacedName := types.NamespacedName{Name: req.Name, Namespace: req.Namespace}
	var response admission.Response
	if req.Operation == admissionv1.Create || req.Operation == admissionv1.Update || req.Operation == admissionv1.Delete {
		switch {
		case req.Kind == crdGVK:
			klog.V(2).InfoS("handling CRD resource", "GVK", crdGVK, "namespacedName", namespacedName, "operation", req.Operation, "subResource", req.SubResource)
			response = v.handleCRD(req)
		case req.Kind == mcGVK:
			klog.V(2).InfoS("handling member cluster resource", "GVK", mcGVK, "namespacedName", namespacedName, "operation", req.Operation, "subResource", req.SubResource)
			response = v.handleMemberCluster(req)
		case req.Kind == namespaceGVK:
			klog.V(2).InfoS("handling namespace resource", "GVK", namespaceGVK, "namespacedName", namespacedName, "operation", req.Operation, "subResource", req.SubResource)
			response = v.handleNamespace(req)
		case req.Kind == imcGVK:
			klog.V(2).InfoS("handling internal member cluster resource", "GVK", imcGVK, "namespacedName", namespacedName, "operation", req.Operation, "subResource", req.SubResource)
			response = v.handleInternalMemberCluster(ctx, req)
		case req.Kind == workGVK:
			klog.V(2).InfoS("handling work resource", "GVK", namespaceGVK, "namespacedName", namespacedName, "operation", req.Operation)
			response = v.handleWork(ctx, req)
		case req.Kind == eventGVK:
			klog.V(2).InfoS("handling event resource", "GVK", eventGVK, "namespacedName", namespacedName, "operation", req.Operation, "subResource", req.SubResource)
			response = v.handleEvent(ctx, req)
		case req.Namespace != "":
			klog.V(2).InfoS(fmt.Sprintf("handling %s resource", req.Kind.Kind), "GVK", req.Kind, "namespacedName", namespacedName, "operation", req.Operation, "subResource", req.SubResource)
			response = validation.ValidateUserForResource(req.Kind.Kind, types.NamespacedName{Name: req.Name, Namespace: req.Namespace}, v.whiteListedUsers, req.UserInfo)
		default:
			klog.V(2).InfoS("resource is not monitored by fleet resource validator webhook", "GVK", req.Kind.String(), "namespacedName", namespacedName, "operation", req.Operation, "subResource", req.SubResource)
			response = admission.Allowed(fmt.Sprintf("user: %s in groups: %v is allowed to modify resource with GVK: %s", req.UserInfo.Username, req.UserInfo.Groups, req.Kind.String()))
		}
	}
	return response
}

// handleCRD allows/denies the request to modify CRD object after validation.
func (v *fleetResourceValidator) handleCRD(req admission.Request) admission.Response {
	var crd v1.CustomResourceDefinition
	if err := v.decodeRequestObject(req, &crd); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	// This regex works because every CRD name in kubernetes follows this pattern <plural>.<group>.
	group := regexp.MustCompile(groupMatch).FindStringSubmatch(crd.Name)[1]
	return validation.ValidateUserForFleetCRD(group, types.NamespacedName{Name: crd.Name}, v.whiteListedUsers, req.UserInfo)
}

// handleMemberCluster allows/denies the request to modify member cluster object after validation.
func (v *fleetResourceValidator) handleMemberCluster(req admission.Request) admission.Response {
	var currentMC fleetv1alpha1.MemberCluster
	if err := v.decodeRequestObject(req, &currentMC); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	if req.Operation == admissionv1.Update {
		var oldMC fleetv1alpha1.MemberCluster
		if err := v.decoder.DecodeRaw(req.OldObject, &oldMC); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
		return validation.ValidateMemberClusterUpdate(&currentMC, &oldMC, v.whiteListedUsers, req.UserInfo)
	}
	return validation.ValidateUserForResource(currentMC.Kind, types.NamespacedName{Name: currentMC.Name}, v.whiteListedUsers, req.UserInfo)
}

// handleInternalMemberCluster allows/denies the request to modify internal member cluster object after validation.
func (v *fleetResourceValidator) handleInternalMemberCluster(ctx context.Context, req admission.Request) admission.Response {
	if req.Operation == admissionv1.Update && req.SubResource == "status" {
		return validation.ValidateMCIdentity(ctx, v.client, req.UserInfo, types.NamespacedName{Name: req.Name, Namespace: req.Namespace}, req.RequestKind.Kind, req.SubResource, req.Name)
	}
	return validation.ValidateUserForResource(req.RequestKind.Kind, types.NamespacedName{Name: req.Name, Namespace: req.Namespace}, v.whiteListedUsers, req.UserInfo)
}

// handleWork allows/delete the request to modify work object after validation.
func (v *fleetResourceValidator) handleWork(ctx context.Context, req admission.Request) admission.Response {
	if req.Operation == admissionv1.Update && isFleetMemberNamespace(req.Namespace) && req.SubResource == "status" {
		mcName := parseMemberClusterNameFromNamespace(req.Namespace)
		return validation.ValidateMCIdentity(ctx, v.client, req.UserInfo, types.NamespacedName{Name: req.Name, Namespace: req.Namespace}, req.RequestKind.Kind, req.SubResource, mcName)
	}
	return validation.ValidateUserForResource(req.RequestKind.Kind, types.NamespacedName{Name: req.Name, Namespace: req.Namespace}, v.whiteListedUsers, req.UserInfo)
}

// handlerNamespace allows/denies request to modify namespace after validation.
func (v *fleetResourceValidator) handleNamespace(req admission.Request) admission.Response {
	var currentNS corev1.Namespace
	if err := v.decodeRequestObject(req, &currentNS); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	fleetMatchResult := strings.HasPrefix(currentNS.Name, "fleet")
	kubeMatchResult := strings.HasPrefix(currentNS.Name, "kube")
	if fleetMatchResult || kubeMatchResult {
		return validation.ValidateUserForResource(currentNS.Kind, types.NamespacedName{Name: currentNS.Name}, v.whiteListedUsers, req.UserInfo)
	}
	// only handling reserved namespaces with prefix fleet/kube.
	return admission.Allowed("namespace name doesn't begin with fleet/kube prefix so we allow all operations on these namespaces")
}

// handleEvent allows/denies request to modify event after validation.
func (v *fleetResourceValidator) handleEvent(ctx context.Context, req admission.Request) admission.Response {
	if isFleetMemberNamespace(req.Namespace) {
		mcName := parseMemberClusterNameFromNamespace(req.Namespace)
		return validation.ValidateMCIdentity(ctx, v.client, req.UserInfo, types.NamespacedName{Name: req.Name, Namespace: req.Namespace}, req.RequestKind.Kind, req.SubResource, mcName)
	}
	return validation.ValidateUserForResource(req.RequestKind.Kind, types.NamespacedName{Name: req.Name, Namespace: req.Namespace}, v.whiteListedUsers, req.UserInfo)
}

// decodeRequestObject decodes the request object into the passed runtime object.
func (v *fleetResourceValidator) decodeRequestObject(req admission.Request, obj runtime.Object) error {
	if req.Operation == admissionv1.Delete {
		// req.Object is not populated for delete: https://github.com/kubernetes-sigs/controller-runtime/issues/1762.
		if err := v.decoder.DecodeRaw(req.OldObject, obj); err != nil {
			klog.ErrorS(err, "failed to decode old request object for delete operation", "userName", req.UserInfo.Username, "groups", req.UserInfo.Groups)
			return err
		}
	} else {
		if err := v.decoder.Decode(req, obj); err != nil {
			klog.ErrorS(err, "failed to decode request object for create/update operation", "userName", req.UserInfo.Username, "groups", req.UserInfo.Groups)
			return err
		}
	}
	return nil
}

// InjectDecoder injects the decoder into fleetResourceValidator.
func (v *fleetResourceValidator) InjectDecoder(d *admission.Decoder) error {
	v.decoder = d
	return nil
}

// isFleetMemberNamespace returns true if namespace is a fleet member cluster namespace.
func isFleetMemberNamespace(namespace string) bool {
	return strings.HasPrefix(namespace, fleetMemberNamespacePrefix)
}

// parseMemberClusterNameFromNamespace returns member cluster name from fleet member cluster namespace.
// returns empty string if namespace is not a fleet member cluster namespace.
func parseMemberClusterNameFromNamespace(namespace string) string {
	// getting MC name from work namespace since work namespace name is of fleet-member-{member cluster name} format.
	var mcName string
	startIndex := len(utils.NamespaceNameFormat) - 2
	if len(namespace) > startIndex {
		mcName = namespace[startIndex:]
	}
	return mcName
}
