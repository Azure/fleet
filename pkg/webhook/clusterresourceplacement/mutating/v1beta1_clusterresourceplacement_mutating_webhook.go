package mutating

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	placementv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/mutator"
)

var (
	// MutationPath is the webhook service path which admission requests are routed to for mutating v1beta1 CRP resources.
	MutationPath = fmt.Sprintf(utils.MutationPathFmt, placementv1beta1.GroupVersion.Group, placementv1beta1.GroupVersion.Version, "clusterresourceplacement")
)

type clusterResourcePlacementMutator struct {
	decoder webhook.AdmissionDecoder
}

// Add registers the webhook for K8s built-in object types.
func Add(mgr manager.Manager) error {
	hookServer := mgr.GetWebhookServer()
	hookServer.Register(MutationPath, &webhook.Admission{Handler: &clusterResourcePlacementMutator{admission.NewDecoder(mgr.GetScheme())}})
	return nil
}

// Handle clusterResourcePlacementMutator handles create, update CRP requests.
func (v *clusterResourcePlacementMutator) Handle(_ context.Context, req admission.Request) admission.Response {
	var crp placementv1beta1.ClusterResourcePlacement
	if req.Operation == admissionv1.Create || req.Operation == admissionv1.Update {
		klog.V(2).InfoS("handling CRP", "operation", req.Operation, "namespacedName", types.NamespacedName{Name: req.Name})
		if err := v.decoder.Decode(req, &crp); err != nil {
			klog.ErrorS(err, "failed to decode v1beta1 CRP object for create/update operation", "userName", req.UserInfo.Username, "groups", req.UserInfo.Groups)
			return admission.Errored(http.StatusBadRequest, err)
		}
		var copyCRP runtime.Object = crp.DeepCopy()
		if err := mutator.MutateClusterResourcePlacement(&crp); err != nil {
			klog.V(2).InfoS("failed to mutate cluster resource placement", "operation", req.Operation, "namespacedName", types.NamespacedName{Name: crp.Name})
			return admission.Errored(http.StatusInternalServerError, err)
		}
		if reflect.DeepEqual(copyCRP, &crp) {
			return admission.Allowed("")
		}
		marshalledCRP, err := json.Marshal(crp)
		if err != nil {
			return admission.Errored(http.StatusInternalServerError, err)
		}
		resp := admission.PatchResponseFromRaw(req.AdmissionRequest.Object.Raw, marshalledCRP)
		if len(resp.Patches) > 0 {
			klog.V(2).Infof("Admit CRP %s/%s patches", crp.Namespace, crp.Name)
		}
		return resp
	}
	return admission.Allowed("")
}
