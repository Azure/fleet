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
	groupMatch        = `^[^.]*\.(.*)`
	crdKind           = "CustomResourceDefinition"
	memberClusterKind = "MemberCluster"
	roleKind          = "Role"
	roleBindingKind   = "RoleBinding"
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

func (v *fleetResourceValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	var response admission.Response
	if req.Operation == admissionv1.Create || req.Operation == admissionv1.Update || req.Operation == admissionv1.Delete {
		switch req.Kind {
		case createCRDGVK():
			klog.V(2).InfoS("handling CRD resource", "GVK", createCRDGVK(), "Name/Namespace", fmt.Sprintf("%s/%s", req.Name, req.Namespace))
			response = v.handleCRD(req)
		case createMemberClusterGVK():
			klog.V(2).InfoS("handling Member cluster resource", "GVK", createMemberClusterGVK(), "Name/Namespace", fmt.Sprintf("%s/%s", req.Name, req.Namespace))
			response = v.handleMemberCluster(ctx, req)
		case createRoleGVK():
			klog.V(2).InfoS("handling Role resource", "GVK", createRoleGVK(), "Name/Namespace", fmt.Sprintf("%s/%s", req.Name, req.Namespace))
			response = v.handleRole(req)
		case createRoleBindingGVK():
			klog.V(2).InfoS("handling Role binding resource", "GVK", createRoleBindingGVK(), "Name/Namespace", fmt.Sprintf("%s/%s", req.Name, req.Namespace))
			response = v.handleRoleBinding(req)
		default:
			klog.V(2).InfoS("resource is not monitored by fleet resource validator webhook", "GVK", req.Kind.String(), "Name/Namespace", fmt.Sprintf("%s/%s", req.Name, req.Namespace))
			response = admission.Allowed(fmt.Sprintf("user: %s in groups: %v is allowed to modify resource with GVK: %s", req.UserInfo.Username, req.UserInfo.Groups, req.Kind.String()))
		}
	}
	return response
}

func (v *fleetResourceValidator) handleCRD(req admission.Request) admission.Response {
	var crd v1.CustomResourceDefinition
	if err := v.decodeRequestObject(req, &crd); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	// This regex works because every CRD name in kubernetes follows this pattern <plural>.<group>.
	group := regexp.MustCompile(groupMatch).FindStringSubmatch(crd.Name)[1]
	if validation.CheckCRDGroup(group) && !validation.IsMasterGroupUserOrWhiteListedUser(v.whiteListedUsers, req.UserInfo) {
		return admission.Denied(fmt.Sprintf("user: %s in groups: %v is not allowed to modify fleet CRD: %s", req.UserInfo.Username, req.UserInfo.Groups, crd.Name))
	}
	return admission.Allowed(fmt.Sprintf("user: %s in groups: %v is allowed to modify CRD: %s", req.UserInfo.Username, req.UserInfo.Groups, crd.Name))
}

func (v *fleetResourceValidator) handleMemberCluster(ctx context.Context, req admission.Request) admission.Response {
	var mc fleetv1alpha1.MemberCluster
	if err := v.decodeRequestObject(req, &mc); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	if !validation.ValidateUserForFleetCR(ctx, v.client, v.whiteListedUsers, req.UserInfo) {
		return admission.Denied(fmt.Sprintf("user: %s in groups: %v is not allowed to modify member cluster CR: %s", req.UserInfo.Username, req.UserInfo.Groups, mc.Name))
	}
	klog.V(2).InfoS("user in groups is allowed to modify member cluster CR", "user", req.UserInfo.Username, "groups", req.UserInfo.Groups)
	return admission.Allowed(fmt.Sprintf("user: %s in groups: %v is allowed to modify member cluster: %s", req.UserInfo.Username, req.UserInfo.Groups, mc.Name))
}

func (v *fleetResourceValidator) handleRole(req admission.Request) admission.Response {
	var role rbacv1.Role
	if err := v.decodeRequestObject(req, &role); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	return validation.ValidateUserForResource(v.whiteListedUsers, req.UserInfo, role.Kind, role.Name, role.Namespace)
}

func (v *fleetResourceValidator) handleRoleBinding(req admission.Request) admission.Response {
	var rb rbacv1.RoleBinding
	if err := v.decodeRequestObject(req, &rb); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	return validation.ValidateUserForResource(v.whiteListedUsers, req.UserInfo, rb.Kind, rb.Name, rb.Namespace)
}

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

func (v *fleetResourceValidator) InjectDecoder(d *admission.Decoder) error {
	v.decoder = d
	return nil
}

func createCRDGVK() metav1.GroupVersionKind {
	return metav1.GroupVersionKind{
		Group:   v1.SchemeGroupVersion.Group,
		Version: v1.SchemeGroupVersion.Version,
		Kind:    crdKind,
	}
}

func createMemberClusterGVK() metav1.GroupVersionKind {
	return metav1.GroupVersionKind{
		Group:   fleetv1alpha1.GroupVersion.Group,
		Version: fleetv1alpha1.GroupVersion.Version,
		Kind:    memberClusterKind,
	}
}

func createRoleGVK() metav1.GroupVersionKind {
	return metav1.GroupVersionKind{
		Group:   rbacv1.SchemeGroupVersion.Group,
		Version: rbacv1.SchemeGroupVersion.Version,
		Kind:    roleKind,
	}
}

func createRoleBindingGVK() metav1.GroupVersionKind {
	return metav1.GroupVersionKind{
		Group:   rbacv1.SchemeGroupVersion.Group,
		Version: rbacv1.SchemeGroupVersion.Version,
		Kind:    roleBindingKind,
	}
}
