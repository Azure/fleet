package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	k8sscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/work-api/pkg/apis/v1alpha1"
)

var SchemeGroupVersion = schema.GroupVersion{Group: "fleet.azure.com", Version: "v1alpha1"}

var Scheme = runtime.NewScheme()
var Codecs = serializer.NewCodecFactory(Scheme)
var ParameterCodec = runtime.NewParameterCodec(Scheme)

var (
	SchemeBuilder      runtime.SchemeBuilder
	localSchemeBuilder = &SchemeBuilder
)

func init() {
	localSchemeBuilder.Register(addKnownTypes, k8sscheme.AddToScheme)
}

func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(SchemeGroupVersion,
		&ClusterResourcePlacement{}, &ClusterResourcePlacementList{})
	scheme.AddKnownTypes(SchemeGroupVersion,
		&MemberCluster{}, &MemberClusterList{})
	scheme.AddKnownTypes(SchemeGroupVersion,
		&InternalMemberCluster{}, &InternalMemberClusterList{})
	scheme.AddKnownTypes(SchemeGroupVersion,
		&metav1.Status{})
	scheme.AddKnownTypes(schema.GroupVersion{
		Group:   "multicluster.x-k8s.io",
		Version: "v1alpha1",
	}, &v1alpha1.Work{}, &v1alpha1.WorkList{})

	metav1.AddToGroupVersion(scheme, SchemeGroupVersion)
	return nil
}

var AddToScheme = localSchemeBuilder.AddToScheme
