package membercluster

import (
	"context"
	"net/http"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	"go.goms.io/fleet/pkg/utils/validator"
)

const (
	// ValidationPath is the webhook service path which admission requests are routed to for validating ReplicaSet resources.
	ValidationPath = "/validate-fleet.azure.com-membercluster"
)

type memberClusterValidator struct {
	decoder *admission.Decoder
}

// Add registers the webhook for K8s bulit-in object types.
func Add(mgr manager.Manager) error {
	hookServer := mgr.GetWebhookServer()
	hookServer.Register(ValidationPath, &webhook.Admission{Handler: &memberClusterValidator{admission.NewDecoder(mgr.GetScheme())}})
	return nil
}

// Handle memberClusterValidator checks to see if member cluster has valid fields.
func (v *memberClusterValidator) Handle(_ context.Context, req admission.Request) admission.Response {
	var mc clusterv1beta1.MemberCluster
	klog.V(2).InfoS("validating webhook handling member cluster", "operation", req.Operation, "namespacedName", types.NamespacedName{Name: req.Name})
	if err := v.decoder.Decode(req, &mc); err != nil {
		klog.ErrorS(err, "failed to decode member cluster object for validating fields", "userName", req.UserInfo.Username, "groups", req.UserInfo.Groups)
		return admission.Errored(http.StatusBadRequest, err)
	}
	if err := validator.ValidateMC(mc); err != nil {
		klog.V(2).ErrorS(err, "member cluster has invalid fields, request is denied", "operation", req.Operation, "namespacedName", types.NamespacedName{Name: mc.Name})
		return admission.Denied(err.Error())
	}
	return admission.Allowed("member cluster has valid fields")
}
