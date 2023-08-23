package fleetresourcehandler

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	admissionv1 "k8s.io/api/admission/v1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/webhook/validation"
)

const (
	// ValidationPath is the webhook service path which admission requests are routed to for validating custom resource definition resources.
	ValidationPath = "/validate-v1-fleetresourcehandler"
	groupMatch     = `^[^.]*\.(.*)`
)

var (
	crdGVK       = metav1.GroupVersionKind{Group: v1.SchemeGroupVersion.Group, Version: v1.SchemeGroupVersion.Version, Kind: "CustomResourceDefinition"}
	mcGVK        = metav1.GroupVersionKind{Group: fleetv1alpha1.GroupVersion.Group, Version: fleetv1alpha1.GroupVersion.Version, Kind: "MemberCluster"}
	namespaceGVK = metav1.GroupVersionKind{Group: corev1.SchemeGroupVersion.Group, Version: corev1.SchemeGroupVersion.Version, Kind: "Namespace"}

	appsV1GV        = schema.GroupVersion{Group: appsv1.SchemeGroupVersion.Group, Version: appsv1.SchemeGroupVersion.Version}
	rbacV1GV        = schema.GroupVersion{Group: rbacv1.SchemeGroupVersion.Group, Version: rbacv1.SchemeGroupVersion.Version}
	coreV1GV        = schema.GroupVersion{Group: corev1.SchemeGroupVersion.Group, Version: corev1.SchemeGroupVersion.Version}
	batchV1GV       = schema.GroupVersion{Group: batchv1.SchemeGroupVersion.Group, Version: batchv1.SchemeGroupVersion.Version}
	fleetV1Alpha1GV = schema.GroupVersion{Group: fleetv1alpha1.GroupVersion.Group, Version: fleetv1alpha1.GroupVersion.Version}
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
func (v *fleetResourceValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	// special case for Kind:Namespace resources req.Name and req.Namespace has the same value the ObjectMeta.Name of Namespace.
	if req.Kind.Kind == "Namespace" {
		req.Namespace = ""
	}
	namespacedName := types.NamespacedName{Name: req.Name, Namespace: req.Namespace}
	var response admission.Response
	if req.Operation == admissionv1.Create || req.Operation == admissionv1.Update || req.Operation == admissionv1.Delete {
		switch {
		case req.Namespace != "":
			response = v.handleNamespacedResources(ctx, req)
		case req.Kind == crdGVK:
			klog.V(2).InfoS("handling CRD resource", "GVK", crdGVK, "namespacedName", namespacedName, "operation", req.Operation)
			response = v.handleCRD(req)
		case req.Kind == mcGVK:
			klog.V(2).InfoS("handling member cluster resource", "GVK", mcGVK, "namespacedName", namespacedName, "operation", req.Operation)
			response = v.handleMemberCluster(req)
		case req.Kind == namespaceGVK:
			klog.V(2).InfoS("handling namespace resource", "GVK", namespaceGVK, "namespacedName", namespacedName, "operation", req.Operation)
			response = v.handleNamespace(req)
		default:
			klog.V(2).InfoS("resource is not monitored by fleet resource validator webhook", "GVK", req.Kind.String(), "namespacedName", namespacedName, "operation", req.Operation)
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
	var currentIMC fleetv1alpha1.InternalMemberCluster
	if err := v.decodeRequestObject(req, &currentIMC); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	if req.Operation == admissionv1.Update {
		var oldIMC fleetv1alpha1.InternalMemberCluster
		if err := v.decoder.DecodeRaw(req.OldObject, &oldIMC); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
		return validation.ValidateInternalMemberClusterUpdate(ctx, v.client, currentIMC, oldIMC, v.whiteListedUsers, req.UserInfo)
	}
	return validation.ValidateUserForResource(currentIMC.Kind, types.NamespacedName{Name: currentIMC.Name, Namespace: currentIMC.Namespace}, v.whiteListedUsers, req.UserInfo)
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

// handleResource allows/denies request to modify resource after validation.
func (v *fleetResourceValidator) handleResource(req admission.Request) admission.Response {
	return validation.ValidateUserForResource(req.Kind.Kind, types.NamespacedName{Name: req.Name, Namespace: req.Namespace}, v.whiteListedUsers, req.UserInfo)
}

// handleNamespaceResources allows/denies request to modify namespaced resources after validation.
func (v *fleetResourceValidator) handleNamespacedResources(ctx context.Context, req admission.Request) admission.Response {
	resourceMsgFormat := fmt.Sprintf("handling %s resource", req.Kind.Kind)
	reqGroupVersion := schema.GroupVersion{Group: req.Resource.Group, Version: req.Resource.Version}
	namespacedName := types.NamespacedName{Name: req.Name, Namespace: req.Namespace}
	var response admission.Response
	if reqGroupVersion == appsV1GV || reqGroupVersion == coreV1GV || reqGroupVersion == rbacV1GV || reqGroupVersion == batchV1GV {
		klog.V(2).InfoS(resourceMsgFormat, "GVK", req.Kind, "namespacedName", namespacedName, "operation", req.Operation)
		response = v.handleResource(req)
	} else if reqGroupVersion == fleetV1Alpha1GV {
		if req.Kind.Kind == "InternalMemberCluster" {
			klog.V(2).InfoS(resourceMsgFormat, "GVK", req.Kind, "namespacedName", namespacedName, "operation", req.Operation)
			response = v.handleInternalMemberCluster(ctx, req)
		}
	}
	return response
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
