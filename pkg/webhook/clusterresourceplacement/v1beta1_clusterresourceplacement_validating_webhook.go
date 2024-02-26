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
func Add(mgr manager.Manager) error {
	hookServer := mgr.GetWebhookServer()
	handler := &clusterResourcePlacementValidator{
		Client:  mgr.GetClient(),
		decoder: admission.NewDecoder(mgr.GetScheme()),
	}
	hookServer.Register(ValidationPath, &webhook.Admission{Handler: handler})
	return nil
}

// Handle clusterResourcePlacementValidator handles create, update CRP requests.
func (v *clusterResourcePlacementValidator) Handle(_ context.Context, req admission.Request) admission.Response {
	var crp placementv1beta1.ClusterResourcePlacement
	if req.Operation == admissionv1.Create || req.Operation == admissionv1.Update {
		klog.V(2).InfoS("handling CRP", "operation", req.Operation, "namespacedName", types.NamespacedName{Name: req.Name})
		if err := v.decoder.Decode(req, &crp); err != nil {
			klog.ErrorS(err, "failed to decode v1beta1 CRP object for create/update operation", "userName", req.UserInfo.Username, "groups", req.UserInfo.Groups)
			return admission.Errored(http.StatusBadRequest, err)
		}
		if err := validator.ValidateClusterResourcePlacement(&crp); err != nil {
			klog.V(2).InfoS("v1beta1 cluster resource placement has invalid fields, request is denied", "operation", req.Operation, "namespacedName", types.NamespacedName{Name: crp.Name})
			return admission.Denied(err.Error())
		}
		if req.Operation == admissionv1.Update {
			var oldCRP placementv1beta1.ClusterResourcePlacement
			if err := v.decoder.DecodeRaw(req.OldObject, &oldCRP); err != nil {
				return admission.Errored(http.StatusBadRequest, err)
			}
			// handle update case where placement type should be immutable.
			if validator.IsPlacementPolicyTypeUpdated(oldCRP.Spec.Policy, crp.Spec.Policy) {
				return admission.Denied("placement type is immutable")
			}
			// handle update case where existing tolerations were updated/deleted
			if validator.IsTolerationsUpdatedOrDeleted(getTolerations(oldCRP.Spec.Policy), getTolerations(crp.Spec.Policy)) {
				return admission.Denied("tolerations have been updated/deleted, only additions to tolerations are allowed")
			}
		}
	}
	klog.V(2).InfoS("user is allowed to modify v1beta1 cluster resource placement", "operation", req.Operation, "user", req.UserInfo.Username, "group", req.UserInfo.Groups, "namespacedName", types.NamespacedName{Name: crp.Name})
	return admission.Allowed("any user is allowed to modify v1beta1 CRP")
}

func getTolerations(policy *placementv1beta1.PlacementPolicy) []placementv1beta1.Toleration {
	if policy != nil {
		return policy.Tolerations
	}
	return nil
}
