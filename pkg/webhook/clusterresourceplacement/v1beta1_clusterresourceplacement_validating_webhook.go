package clusterresourceplacement

import (
	"context"
	"fmt"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/validator"
)

const (
	allowUpdateOldInvalidCRPFmt   = "allow update on old invalid v1beta1 CRP with DeletionTimestamp set"
	denyUpdateOldInvalidCRPFmt    = "deny update on old invalid v1beta1 CRP with DeletionTimestamp not set %s"
	denyCreateUpdateInvalidCRPFmt = "deny create/update v1beta1 CRP has invalid fields %s"
)

var (
	// ValidationPath is the webhook service path which admission requests are routed to for validating v1beta1 CRP resources.
	ValidationPath = fmt.Sprintf(utils.ValidationPathFmt, placementv1beta1.GroupVersion.Group, placementv1beta1.GroupVersion.Version, "clusterresourceplacement")
)

type clusterResourcePlacementValidator struct {
	decoder webhook.AdmissionDecoder
}

// Add registers the webhook for K8s bulit-in object types.
func Add(mgr manager.Manager) error {
	hookServer := mgr.GetWebhookServer()
	hookServer.Register(ValidationPath, &webhook.Admission{Handler: &clusterResourcePlacementValidator{admission.NewDecoder(mgr.GetScheme())}})
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
		if req.Operation == admissionv1.Update {
			var oldCRP placementv1beta1.ClusterResourcePlacement
			if err := v.decoder.DecodeRaw(req.OldObject, &oldCRP); err != nil {
				return admission.Errored(http.StatusBadRequest, err)
			}
			// this is a special case where we allow updates to old v1beta1 CRP with invalid fields so that we can
			// update the CRP to remove finalizer then delete CRP.
			if err := validator.ValidateClusterResourcePlacement(&oldCRP); err != nil {
				if crp.DeletionTimestamp != nil {
					return admission.Allowed(allowUpdateOldInvalidCRPFmt)
				}
				return admission.Denied(fmt.Sprintf(denyUpdateOldInvalidCRPFmt, err))
			}
			// handle update case where placement type should be immutable.
			if validator.IsPlacementPolicyTypeUpdated(oldCRP.Spec.Policy, crp.Spec.Policy) {
				return admission.Denied("placement type is immutable")
			}
			// handle update case where existing tolerations were updated/deleted
			if validator.IsTolerationsUpdatedOrDeleted(oldCRP.Tolerations(), crp.Tolerations()) {
				return admission.Denied("tolerations have been updated/deleted, only additions to tolerations are allowed")
			}
		}
		if err := validator.ValidateClusterResourcePlacement(&crp); err != nil {
			klog.V(2).InfoS("v1beta1 cluster resource placement has invalid fields, request is denied", "operation", req.Operation, "namespacedName", types.NamespacedName{Name: crp.Name})
			return admission.Denied(fmt.Sprintf(denyCreateUpdateInvalidCRPFmt, err))
		}
	}
	klog.V(2).InfoS("user is allowed to modify v1beta1 cluster resource placement", "operation", req.Operation, "user", req.UserInfo.Username, "group", req.UserInfo.Groups, "namespacedName", types.NamespacedName{Name: crp.Name})
	return admission.Allowed("any user is allowed to modify v1beta1 CRP")
}
