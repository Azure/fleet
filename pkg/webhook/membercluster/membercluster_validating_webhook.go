package membercluster

import (
	"context"
	"fmt"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/utils/validator"

	fleetnetworkingv1alpha1 "go.goms.io/fleet-networking/api/v1alpha1"
)

var (
	// ValidationPath is the webhook service path which admission requests are routed to for validating ReplicaSet resources.
	ValidationPath = fmt.Sprintf(utils.ValidationPathFmt, clusterv1beta1.GroupVersion.Group, clusterv1beta1.GroupVersion.Version, "membercluster")
)

type memberClusterValidator struct {
	client  client.Client
	decoder webhook.AdmissionDecoder
}

// Add registers the webhook for K8s bulit-in object types.
func Add(mgr manager.Manager) error {
	hookServer := mgr.GetWebhookServer()
	hookServer.Register(ValidationPath, &webhook.Admission{Handler: &memberClusterValidator{client: mgr.GetClient(), decoder: admission.NewDecoder(mgr.GetScheme())}})
	return nil
}

// Handle memberClusterValidator checks to see if member cluster has valid fields.
func (v *memberClusterValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	mcObjectName := types.NamespacedName{Name: req.Name, Namespace: req.Namespace}
	klog.V(2).InfoS("Validating webhook handling member cluster", "operation", req.Operation, "memberCluster", mcObjectName)

	if req.Operation == admissionv1.Delete { // Will reject the requests whenever the serviceExport is not deleted
		klog.V(2).InfoS("Validating webhook member cluster DELETE", "memberCluster", mcObjectName)
		namespaceName := fmt.Sprintf(utils.NamespaceNameFormat, mcObjectName.Name)
		internalServiceExportList := &fleetnetworkingv1alpha1.InternalServiceExportList{}
		if err := v.client.List(ctx, internalServiceExportList, client.InNamespace(namespaceName)); err != nil {
			klog.ErrorS(err, "Failed to list internalServiceExportList when validating")
			return admission.Errored(http.StatusInternalServerError, fmt.Errorf("failed to list internalServiceExportList, please retry the request: %w", err))
		}
		for _, internalServiceExport := range internalServiceExportList.Items {
			if internalServiceExport.DeletionTimestamp.IsZero() {
				klog.Warning("ServiceExport exists in the member cluster, request is denied", "operation", req.Operation, "memberCluster", mcObjectName)
				return admission.Denied(fmt.Sprintf("Please delete serviceExport %s in the member cluster before leaving, request is denied", internalServiceExport.Spec.ServiceReference.NamespacedName))
			}
		}
		return admission.Allowed("Member cluster is ready to leave")
	}

	var mc clusterv1beta1.MemberCluster
	if err := v.decoder.Decode(req, &mc); err != nil {
		klog.ErrorS(err, "Failed to decode member cluster object for validating fields", "userName", req.UserInfo.Username, "groups", req.UserInfo.Groups)
		return admission.Errored(http.StatusBadRequest, err)
	}

	if err := validator.ValidateMemberCluster(mc); err != nil {
		klog.V(2).ErrorS(err, "Member cluster has invalid fields, request is denied", "operation", req.Operation, "memberCluster", mcObjectName)
		return admission.Denied(err.Error())
	}
	return admission.Allowed("Member cluster has valid fields")
}
