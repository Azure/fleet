package v1alpha1

import (
	"context"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils/validator"
)

const (
	// ValidationPath is the webhook service path which admission requests are routed to for validating ReplicaSet resources.
	ValidationPath = "/validate-fleet.azure.com-v1beta1-clusterresourceplacement"
)

type clusterResourcePlacementValidator struct {
	Client  client.Client
	decoder *admission.Decoder
}

// Add registers the webhook for K8s bulit-in object types.
func Add(mgr manager.Manager, _ []string) error {
	hookServer := mgr.GetWebhookServer()
	hookServer.Register(ValidationPath, &webhook.Admission{Handler: &clusterResourcePlacementValidator{Client: mgr.GetClient()}})
	return nil
}

// Handle clusterResourcePlacementValidator handles create, update CRP requests.
func (v *clusterResourcePlacementValidator) Handle(_ context.Context, req admission.Request) admission.Response {
	var crp placementv1beta1.ClusterResourcePlacement
	if req.Operation == admissionv1.Create || req.Operation == admissionv1.Update {
		if err := v.decoder.Decode(req, &crp); err != nil {
			klog.ErrorS(err, "failed to decode v1beta1 CRP object for create/update operation", "userName", req.UserInfo.Username, "groups", req.UserInfo.Groups)
			return admission.Errored(http.StatusBadRequest, err)
		}
		if err := validator.ValidateClusterResourcePlacement(&crp); err != nil {
			klog.V(2).InfoS("v1beta1 cluster resource placement has invalid fields, request is denied", "operation", req.Operation, "namespacedName", types.NamespacedName{Name: crp.Name})
			return admission.Denied(err.Error())
		}
	}
	klog.V(2).InfoS("user is allowed to modify v1beta1 cluster resource placement", "operation", req.Operation, "user", req.UserInfo.Username, "group", req.UserInfo.Groups, "namespacedName", types.NamespacedName{Name: crp.Name})
	return admission.Allowed("any user is allowed to modify v1beta1 CRP")
}

// InjectDecoder injects the decoder.
func (v *clusterResourcePlacementValidator) InjectDecoder(d *admission.Decoder) error {
	v.decoder = d
	return nil
}
