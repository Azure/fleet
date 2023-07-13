package fleetresourcehandler

import (
	"context"
	"fmt"
	"net/http"
	"regexp"

	admissionv1 "k8s.io/api/admission/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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
	ValidationPath    = "/validate-v1-fleetresourcehandler"
	crdKind           = "CustomResourceDefinition"
	memberClusterKind = "MemberCluster"
	roleKind          = "Role"
	roleBindingKind   = "RoleBinding"
	groupMatch        = `^[^.]*\.(.*)`
)

// Add registers the webhook for K8s bulit-in object types.
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
func (v *fleetResourceValidator) Handle(_ context.Context, req admission.Request) admission.Response {
	namespacedName := types.NamespacedName{Name: req.Name, Namespace: req.Namespace}
	var response admission.Response
	if req.Operation == admissionv1.Create || req.Operation == admissionv1.Update || req.Operation == admissionv1.Delete {
		switch req.Kind {
		case createCRDGVK():
			klog.V(2).InfoS("handling CRD resource", "GVK", createCRDGVK(), "NamespacedName", namespacedName)
			response = v.handleCRD(req)
		case createMemberClusterGVK():
			klog.V(2).InfoS("handling Member cluster resource", "GVK", createMemberClusterGVK(), "NamespacedName", namespacedName)
			response = v.handleMemberCluster(req)
		case createRoleGVK():
			klog.V(2).InfoS("handling Role resource", "GVK", createRoleGVK(), "NamespacedName", namespacedName)
			response = v.handleRole(req)
		case createRoleBindingGVK():
			klog.V(2).InfoS("handling Role binding resource", "GVK", createRoleBindingGVK(), "NamespacedName", namespacedName)
			response = v.handleRoleBinding(req)
		default:
			klog.V(2).InfoS("resource is not monitored by fleet resource validator webhook", "GVK", req.Kind.String(), "NamespacedName", namespacedName)
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
	return validation.ValidateUserForFleetCRD(group, types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}, v.whiteListedUsers, req.UserInfo)
}

// handleMemberCluster allows/denies the request to modify member cluster object after validation.
func (v *fleetResourceValidator) handleMemberCluster(req admission.Request) admission.Response {
	var mc fleetv1alpha1.MemberCluster
	if err := v.decodeRequestObject(req, &mc); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	return validation.ValidateUserForFleetCR(mc.Kind, types.NamespacedName{Name: mc.Name, Namespace: mc.Namespace}, v.whiteListedUsers, req.UserInfo)
}

// handleRole allows/denies the request to modify role after validation.
func (v *fleetResourceValidator) handleRole(req admission.Request) admission.Response {
	var role rbacv1.Role
	if err := v.decodeRequestObject(req, &role); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	return validation.ValidateUserForResource(role.Kind, types.NamespacedName{Name: role.Name, Namespace: role.Namespace}, v.whiteListedUsers, req.UserInfo)
}

// handleRoleBinding allows/denies the request to modify role after validation.
func (v *fleetResourceValidator) handleRoleBinding(req admission.Request) admission.Response {
	var rb rbacv1.RoleBinding
	if err := v.decodeRequestObject(req, &rb); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	return validation.ValidateUserForResource(rb.Kind, types.NamespacedName{Name: rb.Name, Namespace: rb.Namespace}, v.whiteListedUsers, req.UserInfo)
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

// createCRDGVK returns GVK for CRD.
func createCRDGVK() metav1.GroupVersionKind {
	return metav1.GroupVersionKind{
		Group:   v1.SchemeGroupVersion.Group,
		Version: v1.SchemeGroupVersion.Version,
		Kind:    crdKind,
	}
}

// createMemberClusterGVK returns GVK for member cluster.
func createMemberClusterGVK() metav1.GroupVersionKind {
	return metav1.GroupVersionKind{
		Group:   fleetv1alpha1.GroupVersion.Group,
		Version: fleetv1alpha1.GroupVersion.Version,
		Kind:    memberClusterKind,
	}
}

// createRoleGVK return GVK for role.
func createRoleGVK() metav1.GroupVersionKind {
	return metav1.GroupVersionKind{
		Group:   rbacv1.SchemeGroupVersion.Group,
		Version: rbacv1.SchemeGroupVersion.Version,
		Kind:    roleKind,
	}
}

// createRoleBindingGVK returns GVK for role binding.
func createRoleBindingGVK() metav1.GroupVersionKind {
	return metav1.GroupVersionKind{
		Group:   rbacv1.SchemeGroupVersion.Group,
		Version: rbacv1.SchemeGroupVersion.Version,
		Kind:    roleBindingKind,
	}
}
