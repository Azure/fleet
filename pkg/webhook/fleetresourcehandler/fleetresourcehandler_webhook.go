package fleetresourcehandler

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
	fleetv1alpha1 "go.goms.io/fleet/apis/v1alpha1"
	"go.goms.io/fleet/pkg/utils"
	"go.goms.io/fleet/pkg/webhook/validation"
)

const (
	// ValidationPath is the webhook service path which admission requests are routed to for validating custom resource definition resources.
	ValidationPath             = "/validate-fleetresourcehandler"
	groupMatch                 = `^[^.]*\.(.*)`
	fleetMemberNamespacePrefix = "fleet-member"
	fleetNamespacePrefix       = "fleet"
	kubeNamespacePrefix        = "kube"
)

// Add registers the webhook for K8s built-in object types.
func Add(mgr manager.Manager, whiteListedUsers []string, isFleetV1Beta1API bool) error {
	hookServer := mgr.GetWebhookServer()
	handler := &fleetResourceValidator{
		client:            mgr.GetClient(),
		whiteListedUsers:  whiteListedUsers,
		isFleetV1Beta1API: isFleetV1Beta1API,
		decoder:           admission.NewDecoder(mgr.GetScheme()),
	}
	hookServer.Register(ValidationPath, &webhook.Admission{Handler: handler})
	return nil
}

type fleetResourceValidator struct {
	client            client.Client
	whiteListedUsers  []string
	isFleetV1Beta1API bool
	decoder           *admission.Decoder
}

// Handle receives the request then allows/denies the request to modify fleet resources.
func (v *fleetResourceValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	// special case for Kind:Namespace resources req.Name and req.Namespace has the same value the ObjectMeta.Name of Namespace.
	if req.Kind.Kind == "Namespace" {
		req.Namespace = ""
	}
	namespacedName := types.NamespacedName{Name: req.Name, Namespace: req.Namespace}
	var response admission.Response
	if req.Operation == admissionv1.Create || req.Operation == admissionv1.Update || req.Operation == admissionv1.Delete {
		switch {
		case req.Kind == utils.CRDMetaGVK:
			klog.V(2).InfoS("handling CRD resource", "name", req.Name, "operation", req.Operation, "subResource", req.SubResource)
			response = v.handleCRD(req)
		case req.Kind == utils.MCV1Alpha1MetaGVK:
			klog.V(2).InfoS("handling v1alpha1 member cluster resource", "name", req.Name, "operation", req.Operation, "subResource", req.SubResource)
			response = v.handleV1Alpha1MemberCluster(req)
		case req.Kind == utils.MCMetaGVK:
			klog.V(2).InfoS("handling member cluster resource", "name", req.Name, "operation", req.Operation, "subResource", req.SubResource)
			response = v.handleMemberCluster(req)
		case req.Kind == utils.NamespaceMetaGVK:
			klog.V(2).InfoS("handling namespace resource", "name", req.Name, "operation", req.Operation, "subResource", req.SubResource)
			response = v.handleNamespace(req)
		case req.Kind == utils.IMCV1Alpha1MetaGVK || req.Kind == utils.WorkV1Alpha1MetaGVK || req.Kind == utils.IMCMetaGVK || req.Kind == utils.WorkMetaGVK || req.Kind == utils.EndpointSliceExportMetaGVK || req.Kind == utils.EndpointSliceImportMetaGVK || req.Kind == utils.InternalServiceExportMetaGVK || req.Kind == utils.InternalServiceImportMetaGVK:
			klog.V(2).InfoS("handling fleet owned namespaced resource in fleet reserved namespaces", "GVK", req.RequestKind, "namespacedName", namespacedName, "operation", req.Operation, "subResource", req.SubResource)
			response = v.handleFleetReservedNamespacedResource(ctx, req)
		case req.Kind == utils.EventMetaGVK:
			klog.V(3).InfoS("handling event resource", "namespacedName", namespacedName, "operation", req.Operation, "subResource", req.SubResource)
			response = v.handleEvent(ctx, req)
		case req.Namespace != "":
			klog.V(2).InfoS("handling namespaced resource in fleet reserved namespaces", "GVK", req.RequestKind, "namespacedName", namespacedName, "operation", req.Operation, "subResource", req.SubResource)
			response = validation.ValidateUserForResource(req, v.whiteListedUsers)
		default:
			klog.V(3).InfoS("resource is not monitored by fleet resource validator webhook", "GVK", req.RequestKind, "namespacedName", namespacedName, "operation", req.Operation, "subResource", req.SubResource)
			response = admission.Allowed(fmt.Sprintf("user: %s in groups: %v is allowed to modify resource with GVK: %s", req.UserInfo.Username, req.UserInfo.Groups, req.Kind.String()))
		}
	}
	return response
}

// handleCRD allows/denies the request to modify CRD object after validation.
func (v *fleetResourceValidator) handleCRD(req admission.Request) admission.Response {
	var group string
	// This regex works because every CRD name in kubernetes follows this pattern <plural>.<group>.
	match := regexp.MustCompile(groupMatch).FindStringSubmatch(req.Name)
	if len(match) > 1 {
		group = match[1]
	}
	return validation.ValidateUserForFleetCRD(req, v.whiteListedUsers, group)
}

// handleV1Alpha1MemberCluster allows/denies the request to modify v1alpha1 member cluster object after validation.
func (v *fleetResourceValidator) handleV1Alpha1MemberCluster(req admission.Request) admission.Response {
	var currentMC fleetv1alpha1.MemberCluster
	if err := v.decodeRequestObject(req, &currentMC); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	if req.Operation == admissionv1.Update {
		var oldMC fleetv1alpha1.MemberCluster
		if err := v.decoder.DecodeRaw(req.OldObject, &oldMC); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
		return validation.ValidateV1Alpha1MemberClusterUpdate(currentMC, oldMC, req, v.whiteListedUsers)
	}
	return validation.ValidateUserForResource(req, v.whiteListedUsers)
}

// handleMemberCluster allows/denies the request to modify member cluster object after validation.
func (v *fleetResourceValidator) handleMemberCluster(req admission.Request) admission.Response {
	var currentMC clusterv1beta1.MemberCluster
	if err := v.decodeRequestObject(req, &currentMC); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	if req.Operation == admissionv1.Update {
		var oldMC clusterv1beta1.MemberCluster
		if err := v.decoder.DecodeRaw(req.OldObject, &oldMC); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
		return validation.ValidateMemberClusterUpdate(currentMC, oldMC, req, v.whiteListedUsers)
	}
	return validation.ValidateUserForResource(req, v.whiteListedUsers)
}

// handleFleetReservedNamespacedResource allows/denies the request to modify object after validation.
func (v *fleetResourceValidator) handleFleetReservedNamespacedResource(ctx context.Context, req admission.Request) admission.Response {
	var response admission.Response
	if strings.HasPrefix(req.Namespace, fleetMemberNamespacePrefix) {
		// check to see if valid users other than member agent is making the request.
		response = validation.ValidateUserForResource(req, v.whiteListedUsers)
		// check to see if member agent is making the request only on Update.
		if !response.Allowed {
			// if namespace name is just "fleet-member", mcName variable becomes empty and the request is allowed since that namespaces is not watched by member agents.
			mcName := parseMemberClusterNameFromNamespace(req.Namespace)
			return validation.ValidateMCIdentity(ctx, v.client, req, mcName, v.isFleetV1Beta1API)
		}
		return response
	} else if strings.HasPrefix(req.Namespace, fleetNamespacePrefix) || strings.HasPrefix(req.Namespace, kubeNamespacePrefix) {
		return validation.ValidateUserForResource(req, v.whiteListedUsers)
	}
	klog.V(3).InfoS("namespace name doesn't begin with fleet/kube prefix so we allow all operations on these namespaces",
		"user", req.UserInfo.Username, "groups", req.UserInfo.Groups, "operation", req.Operation, "kind", req.RequestKind.Kind, "subResource", req.SubResource, "namespacedName", types.NamespacedName{Name: req.Name, Namespace: req.Namespace})
	return admission.Allowed("namespace name doesn't begin with fleet/kube prefix so we allow all operations on these namespaces for the request object")
}

// handleEvent allows/denies request to modify event after validation.
func (v *fleetResourceValidator) handleEvent(_ context.Context, _ admission.Request) admission.Response {
	// currently allowing all events will handle events after v1alpha1 resources are removed.
	return admission.Allowed("all events are allowed")
}

// handlerNamespace allows/denies request to modify namespace after validation.
func (v *fleetResourceValidator) handleNamespace(req admission.Request) admission.Response {
	fleetMatchResult := strings.HasPrefix(req.Name, fleetNamespacePrefix)
	kubeMatchResult := strings.HasPrefix(req.Name, kubeNamespacePrefix)
	if fleetMatchResult || kubeMatchResult {
		return validation.ValidateUserForResource(req, v.whiteListedUsers)
	}
	// only handling reserved namespaces with prefix fleet/kube.
	return admission.Allowed("namespace name doesn't begin with fleet/kube prefix so we allow all operations on these namespaces")
}

// decodeRequestObject decodes the request object into the passed runtime object.
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

// parseMemberClusterNameFromNamespace returns member cluster name from fleet member cluster namespace.
// returns empty string if namespace is not a fleet member cluster namespace.
func parseMemberClusterNameFromNamespace(namespace string) string {
	var mcName string
	startIndex := len(utils.NamespaceNameFormat) - 2
	if len(namespace) > startIndex {
		mcName = namespace[startIndex:]
	}
	return mcName
}
