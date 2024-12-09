package clusterresourceoverride

import (
	"context"
	"fmt"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils"
	"k8s.io/klog/v2"
	"net/http"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var (
	// ValidationPath is the webhook service path which admission requests are routed to for validating ClusterResourceOverride resources.
	ValidationPath = fmt.Sprintf(utils.ValidationPathFmt, fleetv1beta1.GroupVersion.Group, fleetv1beta1.GroupVersion.Version, "clusterresourceoverride")
)

type clusterResourceOverrideValidator struct {
	client  client.Client
	decoder webhook.AdmissionDecoder
}

// Add registers the webhook for K8s bulit-in object types.
func Add(mgr manager.Manager) error {
	hookServer := mgr.GetWebhookServer()
	hookServer.Register(ValidationPath, &webhook.Admission{Handler: &clusterResourceOverrideValidator{mgr.GetClient(), admission.NewDecoder(mgr.GetScheme())}})
	return nil
}

// Handle v1alpha1ClusterResourceOverrideValidator checks to see if cluster resource override is valid
func (v *clusterResourceOverrideValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	var cro fleetv1beta1.ClusterResourceOverride
	klog.V(2).InfoS("Validating webhook handling cluster resource override", "operation", req.Operation)
	if err := v.decoder.Decode(req, &cro); err != nil {
		klog.ErrorS(err, "Failed to decode cluster resource override object for validating fields", "userName", req.UserInfo.Username, "groups", req.UserInfo.Groups)
		return admission.Errored(http.StatusBadRequest, err)
	}

	if cro.Spec.Placement == nil {
		err := fmt.Errorf("clusterResourceOverride %v has nil placement", cro.Name)
		klog.V(2).ErrorS(err, "ClusterResourceOverride has invalid fields, request is denied", "operation", req.Operation)
		return admission.Denied(err.Error())
	}
	return admission.Allowed("clusterResourceOverride has valid fields")
}
