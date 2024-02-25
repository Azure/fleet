package clusterresourceplacement

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

	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils/validator"
)

const (
	// V1Alpha1CRPValidationPath is the webhook service path which admission requests are routed to for validating ReplicaSet resources.
	V1Alpha1CRPValidationPath = "/validate-fleet.azure.com-v1alpha1-clusterresourceplacement"
)

type v1alpha1ClusterResourcePlacementValidator struct {
	Client  client.Client
	decoder *admission.Decoder
}

// AddV1Alpha1 registers the webhook for K8s bulit-in object types.
func AddV1Alpha1(mgr manager.Manager) error {
	hookServer := mgr.GetWebhookServer()
	hander := &v1alpha1ClusterResourcePlacementValidator{
		Client:  mgr.GetClient(),
		decoder: admission.NewDecoder(mgr.GetScheme()),
	}
	hookServer.Register(V1Alpha1CRPValidationPath, &webhook.Admission{Handler: hander})
	return nil
}

// Handle clusterResourcePlacementValidator handles create, update CRP requests.
func (v *v1alpha1ClusterResourcePlacementValidator) Handle(_ context.Context, req admission.Request) admission.Response {
	var crp fleetv1alpha1.ClusterResourcePlacement
	if req.Operation == admissionv1.Create || req.Operation == admissionv1.Update {
		klog.V(2).InfoS("handling CRP", "operation", req.Operation, "namespacedName", types.NamespacedName{Name: req.Name})
		if err := v.decoder.Decode(req, &crp); err != nil {
			klog.ErrorS(err, "failed to decode v1alpha1 CRP object for create/update operation", "userName", req.UserInfo.Username, "groups", req.UserInfo.Groups)
			return admission.Errored(http.StatusBadRequest, err)
		}
		if err := validator.ValidateClusterResourcePlacementAlpha(&crp); err != nil {
			klog.V(2).InfoS("v1alpha1 cluster resource placement has invalid fields, request is denied", "operation", req.Operation, "namespacedName", types.NamespacedName{Name: crp.Name})
			return admission.Denied(err.Error())
		}
	}
	klog.V(2).InfoS("user is allowed to modify v1alpha1 cluster resource placement", "operation", req.Operation, "user", req.UserInfo.Username, "group", req.UserInfo.Groups, "namespacedName", types.NamespacedName{Name: crp.Name})
	return admission.Allowed("any user is allowed to modify v1alpha1 CRP")
}
