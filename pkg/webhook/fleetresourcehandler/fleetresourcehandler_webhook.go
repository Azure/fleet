package fleetresourcehandler

import (
	"context"
	"fmt"
	"net/http"
	"regexp"

	admissionv1 "k8s.io/api/admission/v1"
	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/webhook/validation"
)

const (
	// ValidationPath is the webhook service path which admission requests are routed to for validating custom resource definition resources.
	ValidationPath    = "/validate-v1-fleetresourcehandler"
	groupMatch        = `^[^.]*\.(.*)`
	crdKind           = "CustomResourceDefinition"
	memberClusterKind = "MemberCluster"
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
			klog.V(2).InfoS("handling CRD resource", "GVK", createCRDGVK())
			response = v.handleCRD(req)
		case createV1beta1MemberClusterGVK():
			response = v.handleMemberCluster(ctx, req)
		default:
			klog.V(2).InfoS("resource is not monitored by fleet resource validator webhook", "GVK", req.Kind.String())
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
	if validation.CheckCRDGroup(group) && !validation.ValidateUserForCRD(v.whiteListedUsers, req.UserInfo) {
		return admission.Denied(fmt.Sprintf("failed to validate user: %s in groups: %v to modify fleet CRD: %s", req.UserInfo.Username, req.UserInfo.Groups, crd.Name))
	}
	return admission.Allowed(fmt.Sprintf("user: %s in groups: %v is allowed to modify CRD: %s", req.UserInfo.Username, req.UserInfo.Groups, crd.Name))
}

func (v *fleetResourceValidator) handleMemberCluster(ctx context.Context, req admission.Request) admission.Response {
	var mc fleetv1beta1.MemberCluster
	if err := v.decodeRequestObject(req, &mc); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	if !validation.ValidateUserForFleetCR(ctx, v.client, v.whiteListedUsers, req.UserInfo) {
		return admission.Denied(fmt.Sprintf("failed to validate user: %s in groups: %v to modify member cluster CR: %s", req.UserInfo.Username, req.UserInfo.Groups, mc.Name))
	}
	klog.V(2).InfoS("user in groups is allowed to modify member cluster CR", "user", req.UserInfo.Username, "groups", req.UserInfo.Groups)
	return admission.Allowed(fmt.Sprintf("user: %s in groups: %v is allowed to modify member cluster: %s", req.UserInfo.Username, req.UserInfo.Groups, mc.Name))
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

func createV1beta1MemberClusterGVK() metav1.GroupVersionKind {
	return metav1.GroupVersionKind{
		Group:   fleetv1beta1.GroupVersion.Group,
		Version: fleetv1beta1.GroupVersion.Version,
		Kind:    memberClusterKind,
	}
}
