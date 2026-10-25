/*
Copyright 2026 The KubeFleet Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package placementresourcesnapshot

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	deploymentutil "k8s.io/kubectl/pkg/util/deployment"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
	kferrors "github.com/kubefleet-dev/kubefleet/pkg/utils/errors"
	"github.com/kubefleet-dev/kubefleet/pkg/v1/managers/placementresourcesnapshot/uniquename"
)

const (
	namespaceNameTemplate      = "ns-%s"
	fleetNamespaceNameTemplate = "fleet-%s"
	serviceAccountNameTemplate = "serviceaccount-%s"
	secretNameTemplate         = "secret-%s"
	hpaNameTemplate            = "hpa-%s"
	clusterRoleNameTemplate    = "clusterrole-%s"

	placementGroupLabelKey = "placement-group"
	customAnnotationKey    = "custom-annotation"

	// The default namespace and the Kubernetes API service within it are created by the API server itself,
	// including the one that the envtest package runs.
	defaultNamespaceName     = "default"
	kubernetesAPIServiceName = "kubernetes"

	testFinalizer        = "test.kubefleet.dev/block-deletion"
	anotherTestFinalizer = "test.kubefleet.dev/another"

	// The objects below are created automatically by the Kubernetes control plane in a real cluster; the
	// environment prepared by the envtest package does not run the controllers that create them, so the
	// test cases create them by hand.
	kubeRootCACertConfigMapName = "kube-root-ca.crt"
	defaultServiceAccountName   = "default"

	ineligibleResourceErrMsg          = "selected resource is not eligible for placement"
	ineligibleNamespaceErrMsg         = "selected namespace is not eligible for placement"
	ineligibleSelectorNamespaceErrMsg = "resource selector specifies a namespace that is ineligible for placement"
	ineligibleGVKErrMsg               = "resource selector specifies a GVK that is ineligible for placement"
	invalidLabelSelectorErrMsg        = "invalid label selector"
	noMatchForGVKErrMsg               = "no match found for GVK"
	resourceNotFoundErrMsg            = "failed to get selected resource directly from the API server"
)

var _ = Describe("resource selection (placement policies)", func() {
	Context("invalid resource selectors", Ordered, func() {
		configMapName := fmt.Sprintf(configMapNameTemplate, utils.RandStr())
		// The names of resources that do not exist; the errors are expected to occur before any retrieval.
		resourceName := utils.RandStr()

		selfSelectingPlacementPolicy := newPlacementPolicy(appNamespaceName)
		selfSelectingPlacementPolicy.Spec.ResourceSelectors = []placementv1alpha1.ResourceSelector{
			{
				APIGroup:   placementv1alpha1.GroupVersion.Group,
				APIVersion: placementv1alpha1.GroupVersion.Version,
				Kind:       placementv1alpha1.PlacementPolicyKind,
				Name:       selfSelectingPlacementPolicy.Name,
			},
		}

		testCases := []snapshotErrTestCase{
			{
				desc: "the resource selector specifies a namespace other than that of the placement policy",
				placementPolicy: newPlacementPolicy(appNamespaceName, placementv1alpha1.ResourceSelector{
					APIVersion: "v1",
					Kind:       "ConfigMap",
					Namespace:  fmt.Sprintf(namespaceNameTemplate, utils.RandStr()),
					Name:       configMapName,
				}),
				wantErrCategory:   kferrors.ErrCategoryUser,
				wantErrMsgSubstrs: []string{"resource selector specifies an invalid namespace"},
			},
			{
				desc: "the placement policy is in a reserved Kubernetes namespace",
				placementPolicy: newPlacementPolicy("kube-public", placementv1alpha1.ResourceSelector{
					APIVersion: "v1",
					Kind:       "ConfigMap",
					Name:       configMapName,
				}),
				wantErrCategory:   kferrors.ErrCategoryUser,
				wantErrMsgSubstrs: []string{"placement policy is in a namespace that is ineligible for placement"},
			},
			{
				desc: "the resource selector selects a cluster-scoped resource",
				placementPolicy: newPlacementPolicy(appNamespaceName, placementv1alpha1.ResourceSelector{
					APIGroup:   rbacv1.GroupName,
					APIVersion: "v1",
					Kind:       "ClusterRole",
					Name:       resourceName,
				}),
				wantErrCategory:   kferrors.ErrCategoryUser,
				wantErrMsgSubstrs: []string{"cluster-scoped resource cannot be selected by a placement policy"},
			},
			{
				desc: "the resource selector selects pods",
				placementPolicy: newPlacementPolicy(appNamespaceName, placementv1alpha1.ResourceSelector{
					APIVersion: "v1",
					Kind:       "Pod",
					Name:       resourceName,
				}),
				wantErrCategory:   kferrors.ErrCategoryUser,
				wantErrMsgSubstrs: []string{ineligibleGVKErrMsg},
			},
			{
				desc: "the resource selector selects leases",
				placementPolicy: newPlacementPolicy(appNamespaceName, placementv1alpha1.ResourceSelector{
					APIGroup:   "coordination.k8s.io",
					APIVersion: "v1",
					Kind:       "Lease",
					Name:       resourceName,
				}),
				wantErrCategory:   kferrors.ErrCategoryUser,
				wantErrMsgSubstrs: []string{ineligibleGVKErrMsg},
			},
			{
				desc:              "the resource selector selects the placement policy itself",
				placementPolicy:   selfSelectingPlacementPolicy,
				wantErrCategory:   kferrors.ErrCategoryUser,
				wantErrMsgSubstrs: []string{ineligibleGVKErrMsg},
			},
			{
				desc: "a valid resource selector is followed by one that selects an ineligible resource type",
				placementPolicy: newPlacementPolicy(appNamespaceName,
					placementv1alpha1.ResourceSelector{
						APIVersion: "v1",
						Kind:       "ConfigMap",
						Name:       configMapName,
					},
					placementv1alpha1.ResourceSelector{
						APIVersion: "v1",
						Kind:       "Pod",
						Name:       resourceName,
					},
				),
				wantErrCategory:         kferrors.ErrCategoryUser,
				wantErrMsgSubstrs:       []string{ineligibleGVKErrMsg},
				wantResourceSelectorIdx: ptr.To(1),
			},
			{
				// The CRD only enforces that the two fields are mutually exclusive.
				desc: "the resource selector specifies neither a name nor a label selector",
				placementPolicy: newPlacementPolicy(appNamespaceName, placementv1alpha1.ResourceSelector{
					APIVersion: "v1",
					Kind:       "ConfigMap",
				}),
				wantErrCategory:   kferrors.ErrCategoryUser,
				wantErrMsgSubstrs: []string{"neither name nor label selector is specified"},
			},
			{
				desc: "the label selector uses the In operator without any value",
				placementPolicy: newPlacementPolicy(appNamespaceName, placementv1alpha1.ResourceSelector{
					APIVersion: "v1",
					Kind:       "ConfigMap",
					LabelSelector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{Key: placementGroupLabelKey, Operator: metav1.LabelSelectorOpIn},
						},
					},
				}),
				wantErrCategory:   kferrors.ErrCategoryUser,
				wantErrMsgSubstrs: []string{"invalid label selector", "values set can't be empty"},
			},
			{
				desc: "the label selector uses an unknown operator",
				placementPolicy: newPlacementPolicy(appNamespaceName, placementv1alpha1.ResourceSelector{
					APIVersion: "v1",
					Kind:       "ConfigMap",
					LabelSelector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{Key: placementGroupLabelKey, Operator: "Foo", Values: []string{"bar"}},
						},
					},
				}),
				wantErrCategory:   kferrors.ErrCategoryUser,
				wantErrMsgSubstrs: []string{"invalid label selector", "is not a valid label selector operator"},
			},
			{
				desc: "the resource selector specifies an unknown kind",
				placementPolicy: newPlacementPolicy(appNamespaceName, placementv1alpha1.ResourceSelector{
					APIVersion: "v1",
					Kind:       "ConfigMapp",
					Name:       resourceName,
				}),
				wantErrCategory:   kferrors.ErrCategoryUser,
				wantErrMsgSubstrs: []string{noMatchForGVKErrMsg, `no matches for kind "ConfigMapp" in version "v1"`},
			},
			{
				desc: "the resource selector specifies an unknown API version",
				placementPolicy: newPlacementPolicy(appNamespaceName, placementv1alpha1.ResourceSelector{
					APIVersion: "v9",
					Kind:       "ConfigMap",
					Name:       resourceName,
				}),
				wantErrCategory:   kferrors.ErrCategoryUser,
				wantErrMsgSubstrs: []string{noMatchForGVKErrMsg, `no matches for kind "ConfigMap" in version "v9"`},
			},
			{
				desc: "the resource selector specifies an unknown API group",
				placementPolicy: newPlacementPolicy(appNamespaceName, placementv1alpha1.ResourceSelector{
					APIGroup:   "example.com",
					APIVersion: "v1",
					Kind:       "Widget",
					Name:       resourceName,
				}),
				wantErrCategory:   kferrors.ErrCategoryUser,
				wantErrMsgSubstrs: []string{noMatchForGVKErrMsg, `no matches for kind "Widget" in version "example.com/v1"`},
			},
			{
				desc: "the resource selector specifies an unknown kind with a label selector",
				placementPolicy: newPlacementPolicy(appNamespaceName, placementv1alpha1.ResourceSelector{
					APIVersion:    "v1",
					Kind:          "ConfigMapp",
					LabelSelector: &metav1.LabelSelector{},
				}),
				wantErrCategory:   kferrors.ErrCategoryUser,
				wantErrMsgSubstrs: []string{"failed to retrieve selected resources (label selector-based selector)", noMatchForGVKErrMsg},
			},
		}

		BeforeAll(func() {
			// Create a config map that a valid resource selector can select.
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMapName,
					Namespace: appNamespaceName,
				},
				Data: map[string]string{"key": "value"},
			}
			Expect(hubClient.Create(ctx, configMap)).To(Succeed(), "Failed to create the config map")

			// Create the placement policies; this also verifies that the API server accepts them.
			for _, tc := range testCases {
				Expect(hubClient.Create(ctx, tc.placementPolicy)).To(Succeed(), "Failed to create the placement policy (%s)", tc.desc)
			}
		})

		for _, tc := range testCases {
			It(fmt.Sprintf("should return an error when %s", tc.desc), func() {
				verifySnapshotErrTestCase(tc)
			})
		}

		It("should return an unexpected error when no resource selectors are present", func() {
			// The CRD requires at least one resource selector; use an in-memory placement policy instead.
			placementPolicy := newPlacementPolicy(appNamespaceName)
			err := snapshotResourcesAndExpectErr(placementPolicy)
			verifyErrCategoryAndMsg(err, kferrors.ErrCategoryUnexpected, "no resource selectors are present")
		})

		It("should return an unexpected error when the placement policy accessor is nil", func() {
			// Note that a literal nil is passed here; a typed nil pointer would yield a non-nil interface value.
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfStale(ctx, nil)
			Expect(gotSnapshots).To(BeEmpty(), "Placement resource snapshots are returned unexpectedly")
			Expect(isUpToDate).To(BeFalse(), "The up-to-date flag is set unexpectedly")
			verifyErrCategoryAndMsg(err, kferrors.ErrCategoryUnexpected, "placement policy accessor is nil")
		})

		AfterAll(func() {
			objs := make([]client.Object, 0, len(testCases)+1)
			objs = append(objs, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: appNamespaceName}})
			for _, tc := range testCases {
				objs = append(objs, tc.placementPolicy)
			}
			deleteObjectsAndWait(objs)
		})
	})

	Context("named resource not found", Ordered, func() {
		configMapName := fmt.Sprintf(configMapNameTemplate, utils.RandStr())

		placementPolicy := newPlacementPolicy(appNamespaceName, placementv1alpha1.ResourceSelector{
			APIVersion: "v1",
			Kind:       "ConfigMap",
			Name:       configMapName,
		})

		BeforeAll(func() {
			// Create a placement policy that selects a config map which does not exist yet.
			Expect(hubClient.Create(ctx, placementPolicy)).To(Succeed(), "Failed to create the placement policy")
		})

		It("should return an API server error when the selected resource does not exist", func() {
			err := snapshotResourcesAndExpectErr(placementPolicy)
			verifyErrCategoryAndMsg(err, kferrors.ErrCategoryAPIServer, resourceNotFoundErrMsg)
			Expect(errors.IsNotFound(err)).To(BeTrue(), "The error does not wrap a not found error: %v", err)
		})

		It("can create the selected config map", func() {
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMapName,
					Namespace: appNamespaceName,
				},
				Data: map[string]string{"key": "value"},
			}
			Expect(hubClient.Create(ctx, configMap)).To(Succeed(), "Failed to create the config map")
		})

		It("can create a placement resource snapshot once the selected resource exists", func() {
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
			verifyReturnedPrimarySnapshot(gotSnapshots, isUpToDate, err, placementPolicy, 0)

			snapshot := getPrimarySnapshotFromCache(placementPolicy, 0)
			wantIdentifiers := []placementv1alpha1.ObjectReference{
				{
					Namespace:  appNamespaceName,
					Name:       configMapName,
					APIVersion: "v1",
					Kind:       "ConfigMap",
				},
			}
			verifySnapshottedResourceIdentifiers(snapshot.GetSpec().Resources, wantIdentifiers)
		})

		AfterAll(func() {
			objs := primarySnapshotObjs(placementPolicy, 1)
			objs = append(objs,
				placementPolicy,
				&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: appNamespaceName}},
			)
			deleteObjectsAndWait(objs)
		})
	})

	Context("ineligible resources (name-based selection)", Ordered, func() {
		// Some of the ineligible resources feature fixed names; use a fresh namespace.
		namespaceName := fmt.Sprintf(namespaceNameTemplate, utils.RandStr())
		nameSuffix := utils.RandStr()
		ownerConfigMapName := fmt.Sprintf(configMapNameTemplate, nameSuffix) + "-owner"
		ownedConfigMapName := fmt.Sprintf(configMapNameTemplate, nameSuffix) + "-owned"
		deletingConfigMapName := fmt.Sprintf(configMapNameTemplate, nameSuffix) + "-deleting"
		tokenSecretName := fmt.Sprintf(secretNameTemplate, nameSuffix) + "-token"
		opaqueSecretName := fmt.Sprintf(secretNameTemplate, nameSuffix) + "-opaque"

		testCases := []snapshotErrTestCase{
			{
				desc: "the built-in CA certificate config map is selected by name",
				placementPolicy: newPlacementPolicy(namespaceName, placementv1alpha1.ResourceSelector{
					APIVersion: "v1",
					Kind:       "ConfigMap",
					Name:       kubeRootCACertConfigMapName,
				}),
				wantErrCategory: kferrors.ErrCategoryUser,
				wantErrMsgSubstrs: []string{ineligibleResourceErrMsg,
					"the built-in CA certificate config map is created automatically in every namespace and cannot be placed"},
			},
			{
				desc: "the default service account is selected by name",
				placementPolicy: newPlacementPolicy(namespaceName, placementv1alpha1.ResourceSelector{
					APIVersion: "v1",
					Kind:       "ServiceAccount",
					Name:       defaultServiceAccountName,
				}),
				wantErrCategory: kferrors.ErrCategoryUser,
				wantErrMsgSubstrs: []string{ineligibleResourceErrMsg,
					"the default service account is created automatically in every namespace and cannot be placed"},
			},
			{
				desc: "a service account token secret is selected by name",
				placementPolicy: newPlacementPolicy(namespaceName, placementv1alpha1.ResourceSelector{
					APIVersion: "v1",
					Kind:       "Secret",
					Name:       tokenSecretName,
				}),
				wantErrCategory: kferrors.ErrCategoryUser,
				wantErrMsgSubstrs: []string{ineligibleResourceErrMsg,
					"service account token secrets are created automatically along with their service accounts and cannot be placed"},
			},
			{
				desc: "a resource owned by a controller is selected by name",
				placementPolicy: newPlacementPolicy(namespaceName, placementv1alpha1.ResourceSelector{
					APIVersion: "v1",
					Kind:       "ConfigMap",
					Name:       ownedConfigMapName,
				}),
				wantErrCategory:   kferrors.ErrCategoryUser,
				wantErrMsgSubstrs: []string{ineligibleResourceErrMsg, "the resource is under the management of a controller"},
			},
			{
				desc: "a resource being deleted is selected by name",
				placementPolicy: newPlacementPolicy(namespaceName, placementv1alpha1.ResourceSelector{
					APIVersion: "v1",
					Kind:       "ConfigMap",
					Name:       deletingConfigMapName,
				}),
				wantErrCategory:   kferrors.ErrCategoryUser,
				wantErrMsgSubstrs: []string{ineligibleResourceErrMsg, "the resource is being deleted and cannot be placed"},
			},
		}

		// The control placement policy selects eligible resources only.
		controlPlacementPolicy := newPlacementPolicy(namespaceName,
			placementv1alpha1.ResourceSelector{
				APIVersion: "v1",
				Kind:       "ConfigMap",
				Name:       ownerConfigMapName,
			},
			placementv1alpha1.ResourceSelector{
				APIVersion: "v1",
				Kind:       "Secret",
				Name:       opaqueSecretName,
			},
		)

		BeforeAll(func() {
			createNamespace(namespaceName)

			objs := []client.Object{
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: kubeRootCACertConfigMapName, Namespace: namespaceName},
					Data:       map[string]string{"ca.crt": "dummy"},
				},
				&corev1.ServiceAccount{
					ObjectMeta: metav1.ObjectMeta{Name: defaultServiceAccountName, Namespace: namespaceName},
				},
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      tokenSecretName,
						Namespace: namespaceName,
						// The API server requires this annotation for service account token secrets.
						Annotations: map[string]string{corev1.ServiceAccountNameKey: defaultServiceAccountName},
					},
					Type: corev1.SecretTypeServiceAccountToken,
				},
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: opaqueSecretName, Namespace: namespaceName},
					Type:       corev1.SecretTypeOpaque,
					Data:       map[string][]byte{"key": []byte("value")},
				},
			}
			for _, obj := range objs {
				Expect(hubClient.Create(ctx, obj)).To(Succeed(), "Failed to create object %T %s", obj, obj.GetName())
			}

			ownerConfigMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: ownerConfigMapName, Namespace: namespaceName},
				Data:       map[string]string{"key": "value"},
			}
			Expect(hubClient.Create(ctx, ownerConfigMap)).To(Succeed(), "Failed to create the owner config map")
			ownedConfigMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ownedConfigMapName,
					Namespace: namespaceName,
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "v1",
							Kind:       "ConfigMap",
							Name:       ownerConfigMapName,
							UID:        ownerConfigMap.UID,
							Controller: ptr.To(true),
						},
					},
				},
				Data: map[string]string{"key": "value"},
			}
			Expect(hubClient.Create(ctx, ownedConfigMap)).To(Succeed(), "Failed to create the owned config map")

			createDeletingConfigMap(deletingConfigMapName, namespaceName, nil)

			for _, tc := range testCases {
				Expect(hubClient.Create(ctx, tc.placementPolicy)).To(Succeed(), "Failed to create the placement policy (%s)", tc.desc)
			}
			Expect(hubClient.Create(ctx, controlPlacementPolicy)).To(Succeed(), "Failed to create the control placement policy")
		})

		for _, tc := range testCases {
			It(fmt.Sprintf("should return a user error when %s", tc.desc), func() {
				verifySnapshotErrTestCase(tc)
			})
		}

		It("can snapshot eligible resources selected by name", func() {
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, controlPlacementPolicy)
			verifyReturnedPrimarySnapshot(gotSnapshots, isUpToDate, err, controlPlacementPolicy, 0)

			// The resources are sorted by their unique IDs, i.e., the config map comes first.
			snapshot := getPrimarySnapshotFromCache(controlPlacementPolicy, 0)
			wantIdentifiers := []placementv1alpha1.ObjectReference{
				{
					Namespace:  namespaceName,
					Name:       ownerConfigMapName,
					APIVersion: "v1",
					Kind:       "ConfigMap",
				},
				{
					Namespace:  namespaceName,
					Name:       opaqueSecretName,
					APIVersion: "v1",
					Kind:       "Secret",
				},
			}
			resources := snapshot.GetSpec().Resources
			verifySnapshottedResourceIdentifiers(resources, wantIdentifiers)

			gotSecret := &corev1.Secret{}
			Expect(json.Unmarshal(resources[1].Manifest.Raw, gotSecret)).To(Succeed(), "Failed to unmarshal the secret manifest")
			wantSecret := &corev1.Secret{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      opaqueSecretName,
					Namespace: namespaceName,
				},
				Type: corev1.SecretTypeOpaque,
				Data: map[string][]byte{"key": []byte("value")},
			}
			if diff := cmp.Diff(gotSecret, wantSecret); diff != "" {
				Fail(fmt.Sprintf("Snapshotted secret diff (-got +want):\n%s", diff))
			}
		})

		AfterAll(func() {
			removeFinalizersAndDelete(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: deletingConfigMapName, Namespace: namespaceName}})

			objs := primarySnapshotObjs(controlPlacementPolicy, 1)
			objs = append(objs, controlPlacementPolicy)
			for _, tc := range testCases {
				objs = append(objs, tc.placementPolicy)
			}
			objs = append(objs,
				&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: kubeRootCACertConfigMapName, Namespace: namespaceName}},
				&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: defaultServiceAccountName, Namespace: namespaceName}},
				&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: tokenSecretName, Namespace: namespaceName}},
				&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: opaqueSecretName, Namespace: namespaceName}},
				&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: ownedConfigMapName, Namespace: namespaceName}},
				&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: ownerConfigMapName, Namespace: namespaceName}},
			)
			deleteObjectsAndWait(objs)
		})
	})

	Context("ineligible resources (label-based selection)", Ordered, func() {
		// Some of the ineligible resources feature fixed names; use a fresh namespace.
		namespaceName := fmt.Sprintf(namespaceNameTemplate, utils.RandStr())
		groupLabels := map[string]string{placementGroupLabelKey: utils.RandStr()}

		nameSuffix := utils.RandStr()
		configMapName := fmt.Sprintf(configMapNameTemplate, nameSuffix)
		ownerConfigMapName := configMapName + "-owner"
		ownedConfigMapName := configMapName + "-owned"
		deletingConfigMapName := configMapName + "-deleting"
		serviceAccountName := fmt.Sprintf(serviceAccountNameTemplate, nameSuffix)
		secretName := fmt.Sprintf(secretNameTemplate, nameSuffix)
		tokenSecretName := secretName + "-token"

		placementPolicy := newPlacementPolicy(namespaceName,
			placementv1alpha1.ResourceSelector{
				APIVersion:    "v1",
				Kind:          "ConfigMap",
				LabelSelector: &metav1.LabelSelector{MatchLabels: groupLabels},
			},
			placementv1alpha1.ResourceSelector{
				APIVersion:    "v1",
				Kind:          "ServiceAccount",
				LabelSelector: &metav1.LabelSelector{MatchLabels: groupLabels},
			},
			placementv1alpha1.ResourceSelector{
				APIVersion:    "v1",
				Kind:          "Secret",
				LabelSelector: &metav1.LabelSelector{MatchLabels: groupLabels},
			},
		)

		// Only the eligible resources are selected; they are sorted by their unique IDs.
		wantIdentifiers := []placementv1alpha1.ObjectReference{
			{
				Namespace:  namespaceName,
				Name:       configMapName,
				APIVersion: "v1",
				Kind:       "ConfigMap",
			},
			{
				Namespace:  namespaceName,
				Name:       secretName,
				APIVersion: "v1",
				Kind:       "Secret",
			},
			{
				Namespace:  namespaceName,
				Name:       serviceAccountName,
				APIVersion: "v1",
				Kind:       "ServiceAccount",
			},
		}

		var wantHash string

		BeforeAll(func() {
			createNamespace(namespaceName)

			// The owner config map is not labeled and thus not selected.
			ownerConfigMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: ownerConfigMapName, Namespace: namespaceName},
				Data:       map[string]string{"key": "value"},
			}
			Expect(hubClient.Create(ctx, ownerConfigMap)).To(Succeed(), "Failed to create the owner config map")

			objs := []client.Object{
				// Eligible resources.
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: namespaceName, Labels: groupLabels},
					Data:       map[string]string{"key": "value"},
				},
				&corev1.ServiceAccount{
					ObjectMeta: metav1.ObjectMeta{Name: serviceAccountName, Namespace: namespaceName, Labels: groupLabels},
				},
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: namespaceName, Labels: groupLabels},
					Type:       corev1.SecretTypeOpaque,
					Data:       map[string][]byte{"key": []byte("value")},
				},
				// Ineligible resources.
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: kubeRootCACertConfigMapName, Namespace: namespaceName, Labels: groupLabels},
					Data:       map[string]string{"ca.crt": "dummy"},
				},
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      ownedConfigMapName,
						Namespace: namespaceName,
						Labels:    groupLabels,
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: "v1",
								Kind:       "ConfigMap",
								Name:       ownerConfigMapName,
								UID:        ownerConfigMap.UID,
								Controller: ptr.To(true),
							},
						},
					},
					Data: map[string]string{"key": "value"},
				},
				&corev1.ServiceAccount{
					ObjectMeta: metav1.ObjectMeta{Name: defaultServiceAccountName, Namespace: namespaceName, Labels: groupLabels},
				},
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:        tokenSecretName,
						Namespace:   namespaceName,
						Labels:      groupLabels,
						Annotations: map[string]string{corev1.ServiceAccountNameKey: defaultServiceAccountName},
					},
					Type: corev1.SecretTypeServiceAccountToken,
				},
			}
			for _, obj := range objs {
				Expect(hubClient.Create(ctx, obj)).To(Succeed(), "Failed to create object %T %s", obj, obj.GetName())
			}
			createDeletingConfigMap(deletingConfigMapName, namespaceName, groupLabels)

			Expect(hubClient.Create(ctx, placementPolicy)).To(Succeed(), "Failed to create the placement policy")
		})

		It("can compute the hash of selected resources", func() {
			resources, hash, err := resourceSnapshotManager.retrieveAndHashSelectedResources(ctx, placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to retrieve and hash the selected resources")
			verifySnapshottedResourceIdentifiers(resources, wantIdentifiers)
			wantHash = hash
		})

		It("can create a placement resource snapshot with eligible resources only", func() {
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
			verifyReturnedPrimarySnapshot(gotSnapshots, isUpToDate, err, placementPolicy, 0)

			snapshot := getPrimarySnapshotFromCache(placementPolicy, 0)
			verifySnapshotMetadata(snapshot, placementPolicy, 0, 0, 1, wantHash)
			verifySnapshottedResourceIdentifiers(snapshot.GetSpec().Resources, wantIdentifiers)
		})

		It("can make a selected config map ineligible by adding a controller owner reference", func() {
			ownerConfigMap := &corev1.ConfigMap{}
			Expect(hubUncachedReader.Get(ctx, types.NamespacedName{Namespace: namespaceName, Name: ownerConfigMapName}, ownerConfigMap)).To(Succeed(),
				"Failed to get the owner config map")

			configMap := &corev1.ConfigMap{}
			Expect(hubUncachedReader.Get(ctx, types.NamespacedName{Namespace: namespaceName, Name: configMapName}, configMap)).To(Succeed(),
				"Failed to get the config map")
			configMap.OwnerReferences = []metav1.OwnerReference{
				{
					APIVersion: "v1",
					Kind:       "ConfigMap",
					Name:       ownerConfigMapName,
					UID:        ownerConfigMap.UID,
					Controller: ptr.To(true),
				},
			}
			Expect(hubClient.Update(ctx, configMap)).To(Succeed(), "Failed to update the config map")
		})

		It("should create a new snapshot that excludes the ineligible config map", func() {
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfStale(ctx, placementPolicy)
			verifyReturnedPrimarySnapshot(gotSnapshots, isUpToDate, err, placementPolicy, 1)

			snapshot := getPrimarySnapshotFromCache(placementPolicy, 1)
			verifySnapshottedResourceIdentifiers(snapshot.GetSpec().Resources, wantIdentifiers[1:])
			gotHash := snapshot.GetAnnotations()[placementv1alpha1.PlacementResourceSnapshotContentsHashAnnotationKey]
			Expect(gotHash).ToNot(Equal(wantHash), "The hash of the selected resources has not changed")
		})

		It("can make the config map eligible again by removing the owner reference", func() {
			configMap := &corev1.ConfigMap{}
			Expect(hubUncachedReader.Get(ctx, types.NamespacedName{Namespace: namespaceName, Name: configMapName}, configMap)).To(Succeed(),
				"Failed to get the config map")
			configMap.OwnerReferences = nil
			Expect(hubClient.Update(ctx, configMap)).To(Succeed(), "Failed to update the config map")
		})

		It("should create another snapshot that includes the config map again", func() {
			// The selected resources are the same as those in the snapshot at index 0; however, as the manager
			// compares the selected resources with the latest snapshot (at index 1), a new snapshot is created.
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfStale(ctx, placementPolicy)
			verifyReturnedPrimarySnapshot(gotSnapshots, isUpToDate, err, placementPolicy, 2)

			snapshot := getPrimarySnapshotFromCache(placementPolicy, 2)
			verifySnapshotMetadata(snapshot, placementPolicy, 2, 0, 1, wantHash)
			verifySnapshottedResourceIdentifiers(snapshot.GetSpec().Resources, wantIdentifiers)
		})

		AfterAll(func() {
			removeFinalizersAndDelete(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: deletingConfigMapName, Namespace: namespaceName}})

			objs := primarySnapshotObjs(placementPolicy, 3)
			objs = append(objs,
				placementPolicy,
				&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: namespaceName}},
				&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: ownerConfigMapName, Namespace: namespaceName}},
				&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: ownedConfigMapName, Namespace: namespaceName}},
				&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: kubeRootCACertConfigMapName, Namespace: namespaceName}},
				&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: serviceAccountName, Namespace: namespaceName}},
				&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: defaultServiceAccountName, Namespace: namespaceName}},
				&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: namespaceName}},
				&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: tokenSecretName, Namespace: namespaceName}},
			)
			deleteObjectsAndWait(objs)
		})
	})

	Context("resources with non-controller owner references", Ordered, func() {
		// Owner references that do not point to a controller do not make a resource ineligible for placement; they
		// are stripped from the snapshotted manifests, as the owners do not exist on the member clusters.
		groupLabels := map[string]string{placementGroupLabelKey: utils.RandStr()}
		configMapNamePrefix := fmt.Sprintf(configMapNameTemplate, utils.RandStr())
		ownerConfigMapName := configMapNamePrefix + "-owner"
		namedConfigMapName := configMapNamePrefix + "-named"
		labeledConfigMapName := configMapNamePrefix + "-labeled"

		placementPolicy := newPlacementPolicy(appNamespaceName,
			configMapNameSelector(namedConfigMapName),
			configMapLabelSelector(groupLabels),
		)

		BeforeAll(func() {
			// The owner config map is not labeled and thus not selected.
			ownerConfigMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: ownerConfigMapName, Namespace: appNamespaceName},
				Data:       map[string]string{"key": "value"},
			}
			Expect(hubClient.Create(ctx, ownerConfigMap)).To(Succeed(), "Failed to create the owner config map")

			ownerRefs := func(controller *bool) []metav1.OwnerReference {
				return []metav1.OwnerReference{
					{
						APIVersion: "v1",
						Kind:       "ConfigMap",
						Name:       ownerConfigMapName,
						UID:        ownerConfigMap.UID,
						Controller: controller,
					},
				}
			}
			configMaps := []*corev1.ConfigMap{
				{
					// A config map, selected by name, whose owner reference leaves the controller field unset.
					ObjectMeta: metav1.ObjectMeta{
						Name:            namedConfigMapName,
						Namespace:       appNamespaceName,
						OwnerReferences: ownerRefs(nil),
					},
					Data: map[string]string{"key": "value-named"},
				},
				{
					// A config map, selected by labels, whose owner reference explicitly states that the owner
					// is not a controller.
					ObjectMeta: metav1.ObjectMeta{
						Name:            labeledConfigMapName,
						Namespace:       appNamespaceName,
						Labels:          groupLabels,
						OwnerReferences: ownerRefs(ptr.To(false)),
					},
					Data: map[string]string{"key": "value-labeled"},
				},
			}
			for _, configMap := range configMaps {
				Expect(hubClient.Create(ctx, configMap)).To(Succeed(), "Failed to create config map %s", configMap.Name)
			}
			Expect(hubClient.Create(ctx, placementPolicy)).To(Succeed(), "Failed to create the placement policy")
		})

		It("can create a placement resource snapshot with the owned config maps", func() {
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
			verifyReturnedPrimarySnapshot(gotSnapshots, isUpToDate, err, placementPolicy, 0)
		})

		It("should strip the owner references", func() {
			snapshot := getPrimarySnapshotFromCache(placementPolicy, 0)
			resources := snapshot.GetSpec().Resources

			// The resources are sorted by their unique IDs, i.e., the labeled config map comes first.
			//
			// Compare the manifests as generic maps; this verifies that the owner references are absent.
			wantManifests := []map[string]interface{}{
				{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata": map[string]interface{}{
						"name":      labeledConfigMapName,
						"namespace": appNamespaceName,
						"labels":    map[string]interface{}{placementGroupLabelKey: groupLabels[placementGroupLabelKey]},
					},
					"data": map[string]interface{}{"key": "value-labeled"},
				},
				{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata": map[string]interface{}{
						"name":      namedConfigMapName,
						"namespace": appNamespaceName,
					},
					"data": map[string]interface{}{"key": "value-named"},
				},
			}
			Expect(resources).To(HaveLen(len(wantManifests)), "Unexpected number of snapshotted resources")
			for idx := range resources {
				if diff := cmp.Diff(manifestAsMap(resources[idx].Manifest.Raw), wantManifests[idx]); diff != "" {
					Fail(fmt.Sprintf("Snapshotted config map manifest (#%d) diff (-got +want):\n%s", idx, diff))
				}
			}
		})

		AfterAll(func() {
			objs := primarySnapshotObjs(placementPolicy, 1)
			objs = append(objs,
				placementPolicy,
				&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: namedConfigMapName, Namespace: appNamespaceName}},
				&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: labeledConfigMapName, Namespace: appNamespaceName}},
				&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: ownerConfigMapName, Namespace: appNamespaceName}},
			)
			deleteObjectsAndWait(objs)
		})
	})

	Context("no resources selected", Ordered, func() {
		groupLabels := map[string]string{placementGroupLabelKey: utils.RandStr()}
		configMapName := fmt.Sprintf(configMapNameTemplate, utils.RandStr())

		placementPolicy := newPlacementPolicy(appNamespaceName, placementv1alpha1.ResourceSelector{
			APIVersion:    "v1",
			Kind:          "ConfigMap",
			LabelSelector: &metav1.LabelSelector{MatchLabels: groupLabels},
		})

		var emptyHash string

		BeforeAll(func() {
			// Create a placement policy whose label selector matches no resources (yet).
			Expect(hubClient.Create(ctx, placementPolicy)).To(Succeed(), "Failed to create the placement policy")
		})

		It("can create an empty placement resource snapshot when the label selector matches no resources", func() {
			resources, hash, err := resourceSnapshotManager.retrieveAndHashSelectedResources(ctx, placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to retrieve and hash the selected resources")
			Expect(resources).To(BeEmpty(), "Resources are selected unexpectedly")
			emptyHash = hash

			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
			verifyReturnedPrimarySnapshot(gotSnapshots, isUpToDate, err, placementPolicy, 0)

			// Wait until the cache has observed the new snapshot.
			snapshot := getPrimarySnapshotFromCache(placementPolicy, 0)
			verifySnapshotMetadata(snapshot, placementPolicy, 0, 0, 1, emptyHash)
			Expect(snapshot.GetSpec().Resources).To(BeEmpty(), "The placement resource snapshot is not empty")
		})

		It("should return the existing empty snapshot as it is up-to-date", func() {
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfStale(ctx, placementPolicy)
			verifyReturnedPrimarySnapshot(gotSnapshots, isUpToDate, err, placementPolicy, 0)
			verifyNoPrimarySnapshotAtIdx(placementPolicy, 1)
		})

		It("can create a config map that matches the label selector", func() {
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMapName,
					Namespace: appNamespaceName,
					Labels:    groupLabels,
				},
				Data: map[string]string{"key": "value"},
			}
			Expect(hubClient.Create(ctx, configMap)).To(Succeed(), "Failed to create the config map")
		})

		It("should create a new placement resource snapshot once a resource matches", func() {
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfStale(ctx, placementPolicy)
			verifyReturnedPrimarySnapshot(gotSnapshots, isUpToDate, err, placementPolicy, 1)

			// Wait until the cache has observed the new snapshot.
			snapshot := getPrimarySnapshotFromCache(placementPolicy, 1)
			wantIdentifiers := []placementv1alpha1.ObjectReference{
				{
					Namespace:  appNamespaceName,
					Name:       configMapName,
					APIVersion: "v1",
					Kind:       "ConfigMap",
				},
			}
			verifySnapshottedResourceIdentifiers(snapshot.GetSpec().Resources, wantIdentifiers)
		})

		It("can delete the config map", func() {
			deleteObjectsAndWait([]client.Object{
				&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: appNamespaceName}},
			})
		})

		It("should create a new empty placement resource snapshot when no resources are selected any more", func() {
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfStale(ctx, placementPolicy)
			verifyReturnedPrimarySnapshot(gotSnapshots, isUpToDate, err, placementPolicy, 2)

			snapshot := getPrimarySnapshotFromCache(placementPolicy, 2)
			verifySnapshotMetadata(snapshot, placementPolicy, 2, 0, 1, emptyHash)
			Expect(snapshot.GetSpec().Resources).To(BeEmpty(), "The placement resource snapshot is not empty")
		})

		AfterAll(func() {
			objs := primarySnapshotObjs(placementPolicy, 3)
			objs = append(objs,
				placementPolicy,
				&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: appNamespaceName}},
			)
			deleteObjectsAndWait(objs)
		})
	})

	Context("duplicate selections (name-based and label-based)", Ordered, func() {
		groupLabels := map[string]string{placementGroupLabelKey: utils.RandStr()}
		configMapNamePrefix := fmt.Sprintf(configMapNameTemplate, utils.RandStr())
		configMapNames := []string{configMapNamePrefix + "-0", configMapNamePrefix + "-1"}

		placementPolicy := newPlacementPolicy(appNamespaceName,
			// A placement policy can specify its own namespace in a resource selector.
			placementv1alpha1.ResourceSelector{
				APIVersion: "v1",
				Kind:       "ConfigMap",
				Namespace:  appNamespaceName,
				Name:       configMapNames[0],
			},
			// The label selector selects both config maps, including the one selected above by name.
			placementv1alpha1.ResourceSelector{
				APIVersion:    "v1",
				Kind:          "ConfigMap",
				LabelSelector: &metav1.LabelSelector{MatchLabels: groupLabels},
			},
		)

		wantIdentifiers := []placementv1alpha1.ObjectReference{
			{
				Namespace:  appNamespaceName,
				Name:       configMapNames[0],
				APIVersion: "v1",
				Kind:       "ConfigMap",
			},
			{
				Namespace:  appNamespaceName,
				Name:       configMapNames[1],
				APIVersion: "v1",
				Kind:       "ConfigMap",
			},
		}

		var wantHash string

		BeforeAll(func() {
			for idx, name := range configMapNames {
				configMap := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      name,
						Namespace: appNamespaceName,
						Labels:    groupLabels,
					},
					Data: map[string]string{"key": fmt.Sprintf("value-%d", idx)},
				}
				Expect(hubClient.Create(ctx, configMap)).To(Succeed(), "Failed to create the config map")
			}
			Expect(hubClient.Create(ctx, placementPolicy)).To(Succeed(), "Failed to create the placement policy")
		})

		It("can compute the hash of selected resources", func() {
			resources, hash, err := resourceSnapshotManager.retrieveAndHashSelectedResources(ctx, placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to retrieve and hash the selected resources")
			verifySnapshottedResourceIdentifiers(resources, wantIdentifiers)
			wantHash = hash
		})

		It("can create a placement resource snapshot that includes each selected resource only once", func() {
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
			verifyReturnedPrimarySnapshot(gotSnapshots, isUpToDate, err, placementPolicy, 0)

			snapshot := getPrimarySnapshotFromCache(placementPolicy, 0)
			verifySnapshotMetadata(snapshot, placementPolicy, 0, 0, 1, wantHash)
			verifySnapshottedResourceIdentifiers(snapshot.GetSpec().Resources, wantIdentifiers)
		})

		AfterAll(func() {
			objs := primarySnapshotObjs(placementPolicy, 1)
			objs = append(objs, placementPolicy)
			for _, name := range configMapNames {
				objs = append(objs, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: appNamespaceName}})
			}
			deleteObjectsAndWait(objs)
		})
	})

	Context("same resource selected via different API versions", Ordered, func() {
		hpaName := fmt.Sprintf(hpaNameTemplate, utils.RandStr())

		hpaSelector := func(version string) placementv1alpha1.ResourceSelector {
			return placementv1alpha1.ResourceSelector{
				APIGroup:   autoscalingv1.GroupName,
				APIVersion: version,
				Kind:       "HorizontalPodAutoscaler",
				Name:       hpaName,
			}
		}
		hpaIdentifier := func(version string) placementv1alpha1.ObjectReference {
			return placementv1alpha1.ObjectReference{
				Namespace:  appNamespaceName,
				Name:       hpaName,
				APIGroup:   autoscalingv1.GroupName,
				APIVersion: version,
				Kind:       "HorizontalPodAutoscaler",
			}
		}

		// The same HPA is selected twice; the resource selector that appears first wins.
		placementPolicy := newPlacementPolicy(appNamespaceName, hpaSelector("v1"), hpaSelector("v2"))

		var firstHash string

		BeforeAll(func() {
			hpa := &autoscalingv1.HorizontalPodAutoscaler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      hpaName,
					Namespace: appNamespaceName,
				},
				Spec: autoscalingv1.HorizontalPodAutoscalerSpec{
					// The scale target does not need to exist.
					ScaleTargetRef: autoscalingv1.CrossVersionObjectReference{
						APIVersion: "apps/v1",
						Kind:       "Deployment",
						Name:       fmt.Sprintf(deploymentNameTemplate, utils.RandStr()),
					},
					MinReplicas:                    ptr.To(int32(1)),
					MaxReplicas:                    3,
					TargetCPUUtilizationPercentage: ptr.To(int32(50)),
				},
			}
			Expect(hubClient.Create(ctx, hpa)).To(Succeed(), "Failed to create the HPA")
			Expect(hubClient.Create(ctx, placementPolicy)).To(Succeed(), "Failed to create the placement policy")
		})

		It("can create a placement resource snapshot", func() {
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
			verifyReturnedPrimarySnapshot(gotSnapshots, isUpToDate, err, placementPolicy, 0)

			snapshot := getPrimarySnapshotFromCache(placementPolicy, 0)
			resources := snapshot.GetSpec().Resources
			verifySnapshottedResourceIdentifiers(resources, []placementv1alpha1.ObjectReference{hpaIdentifier("v1")})
			firstHash = snapshot.GetAnnotations()[placementv1alpha1.PlacementResourceSnapshotContentsHashAnnotationKey]

			// The manifest should be that of the v1 API.
			liveHPA := &autoscalingv1.HorizontalPodAutoscaler{}
			Expect(hubUncachedReader.Get(ctx, types.NamespacedName{Namespace: appNamespaceName, Name: hpaName}, liveHPA)).To(Succeed(),
				"Failed to get the HPA")
			gotHPA := &autoscalingv1.HorizontalPodAutoscaler{}
			Expect(json.Unmarshal(resources[0].Manifest.Raw, gotHPA)).To(Succeed(), "Failed to unmarshal the HPA manifest")
			wantHPA := &autoscalingv1.HorizontalPodAutoscaler{
				TypeMeta: metav1.TypeMeta{APIVersion: "autoscaling/v1", Kind: "HorizontalPodAutoscaler"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      hpaName,
					Namespace: appNamespaceName,
				},
				Spec: liveHPA.Spec,
			}
			if diff := cmp.Diff(gotHPA, wantHPA, cmpopts.EquateEmpty()); diff != "" {
				Fail(fmt.Sprintf("Snapshotted HPA diff (-got +want):\n%s", diff))
			}
		})

		It("can swap the order of the resource selectors", func() {
			updatedPlacementPolicy := &placementv1alpha1.PlacementPolicy{}
			Expect(hubUncachedReader.Get(ctx, client.ObjectKeyFromObject(placementPolicy), updatedPlacementPolicy)).To(Succeed(),
				"Failed to get the placement policy")
			slices.Reverse(updatedPlacementPolicy.Spec.ResourceSelectors)
			Expect(hubClient.Update(ctx, updatedPlacementPolicy)).To(Succeed(), "Failed to update the placement policy")
			placementPolicy = updatedPlacementPolicy
		})

		It("should create a new snapshot that carries the resource as selected by the first resource selector", func() {
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfStale(ctx, placementPolicy)
			verifyReturnedPrimarySnapshot(gotSnapshots, isUpToDate, err, placementPolicy, 1)

			snapshot := getPrimarySnapshotFromCache(placementPolicy, 1)
			resources := snapshot.GetSpec().Resources
			verifySnapshottedResourceIdentifiers(resources, []placementv1alpha1.ObjectReference{hpaIdentifier("v2")})
			gotHash := snapshot.GetAnnotations()[placementv1alpha1.PlacementResourceSnapshotContentsHashAnnotationKey]
			Expect(gotHash).ToNot(Equal(firstHash), "The hash of the selected resources has not changed")

			// The manifest should be that of the v2 API.
			gotHPA := &autoscalingv2.HorizontalPodAutoscaler{}
			Expect(json.Unmarshal(resources[0].Manifest.Raw, gotHPA)).To(Succeed(), "Failed to unmarshal the HPA manifest")
			Expect(gotHPA.APIVersion).To(Equal("autoscaling/v2"), "Unexpected API version of the snapshotted HPA")
			wantMetrics := []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: corev1.ResourceCPU,
						Target: autoscalingv2.MetricTarget{
							Type:               autoscalingv2.UtilizationMetricType,
							AverageUtilization: ptr.To(int32(50)),
						},
					},
				},
			}
			if diff := cmp.Diff(gotHPA.Spec.Metrics, wantMetrics); diff != "" {
				Fail(fmt.Sprintf("Snapshotted HPA metrics diff (-got +want):\n%s", diff))
			}
		})

		AfterAll(func() {
			objs := primarySnapshotObjs(placementPolicy, 2)
			objs = append(objs,
				placementPolicy,
				&autoscalingv1.HorizontalPodAutoscaler{ObjectMeta: metav1.ObjectMeta{Name: hpaName, Namespace: appNamespaceName}},
			)
			deleteObjectsAndWait(objs)
		})
	})

	Context("reordered resource selectors", Ordered, func() {
		configMapName := fmt.Sprintf(configMapNameTemplate, utils.RandStr())
		deploymentName := fmt.Sprintf(deploymentNameTemplate, utils.RandStr())

		placementPolicy := newPlacementPolicy(appNamespaceName,
			placementv1alpha1.ResourceSelector{
				APIVersion: "v1",
				Kind:       "ConfigMap",
				Name:       configMapName,
			},
			placementv1alpha1.ResourceSelector{
				APIGroup:   appsv1.GroupName,
				APIVersion: "v1",
				Kind:       "Deployment",
				Name:       deploymentName,
			},
		)

		BeforeAll(func() {
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMapName,
					Namespace: appNamespaceName,
				},
				Data: map[string]string{"key": "value"},
			}
			Expect(hubClient.Create(ctx, configMap)).To(Succeed(), "Failed to create the config map")
			deployment := newDeployment(deploymentName, appNamespaceName)
			Expect(hubClient.Create(ctx, deployment)).To(Succeed(), "Failed to create the Deployment")
			waitForDeploymentsInCache(deployment)
			Expect(hubClient.Create(ctx, placementPolicy)).To(Succeed(), "Failed to create the placement policy")
		})

		It("can create a placement resource snapshot", func() {
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
			verifyReturnedPrimarySnapshot(gotSnapshots, isUpToDate, err, placementPolicy, 0)

			// Wait until the cache has observed the new snapshot.
			_ = getPrimarySnapshotFromCache(placementPolicy, 0)
		})

		It("can reverse the order of the resource selectors", func() {
			updatedPlacementPolicy := &placementv1alpha1.PlacementPolicy{}
			Expect(hubUncachedReader.Get(ctx, client.ObjectKeyFromObject(placementPolicy), updatedPlacementPolicy)).To(Succeed(),
				"Failed to get the placement policy")
			slices.Reverse(updatedPlacementPolicy.Spec.ResourceSelectors)
			Expect(hubClient.Update(ctx, updatedPlacementPolicy)).To(Succeed(), "Failed to update the placement policy")
			placementPolicy = updatedPlacementPolicy
		})

		It("should return the existing snapshot as the selected resources have not changed", func() {
			wantSnapshot := getPrimarySnapshotFromCache(placementPolicy, 0)

			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfStale(ctx, placementPolicy)
			verifyReturnedPrimarySnapshot(gotSnapshots, isUpToDate, err, placementPolicy, 0)
			if diff := cmp.Diff(gotSnapshots[0], wantSnapshot, cmpopts.IgnoreFields(metav1.TypeMeta{}, "APIVersion", "Kind")); diff != "" {
				Fail(fmt.Sprintf("Returned placement resource snapshot diff (-got +want):\n%s", diff))
			}

			verifyNoPrimarySnapshotAtIdx(placementPolicy, 1)
		})

		AfterAll(func() {
			objs := primarySnapshotObjs(placementPolicy, 2)
			objs = append(objs,
				placementPolicy,
				&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: deploymentName, Namespace: appNamespaceName}},
				&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: appNamespaceName}},
			)
			deleteObjectsAndWait(objs)
		})
	})

	Context("manifest sanitization (config maps)", Ordered, func() {
		configMapNamePrefix := fmt.Sprintf(configMapNameTemplate, utils.RandStr())
		// The names are in ascending order, which is also the order in which the selected resources are
		// snapshotted.
		configMapNames := []string{configMapNamePrefix + "-0", configMapNamePrefix + "-1", configMapNamePrefix + "-2"}

		lastAppliedConfig := `{"apiVersion":"v1","kind":"ConfigMap"}`

		placementPolicy := newPlacementPolicy(appNamespaceName)
		for _, name := range configMapNames {
			placementPolicy.Spec.ResourceSelectors = append(placementPolicy.Spec.ResourceSelectors, placementv1alpha1.ResourceSelector{
				APIVersion: "v1",
				Kind:       "ConfigMap",
				Name:       name,
			})
		}

		BeforeAll(func() {
			configMaps := []*corev1.ConfigMap{
				{
					// A config map with the last applied configuration annotation and a custom annotation.
					ObjectMeta: metav1.ObjectMeta{
						Name:      configMapNames[0],
						Namespace: appNamespaceName,
						Annotations: map[string]string{
							corev1.LastAppliedConfigAnnotation: lastAppliedConfig,
							customAnnotationKey:                "keep",
						},
					},
					Data: map[string]string{"key": "value-0"},
				},
				{
					// A config map with the last applied configuration annotation only.
					ObjectMeta: metav1.ObjectMeta{
						Name:      configMapNames[1],
						Namespace: appNamespaceName,
						Annotations: map[string]string{
							corev1.LastAppliedConfigAnnotation: lastAppliedConfig,
						},
					},
					Data: map[string]string{"key": "value-1"},
				},
				{
					// A config map with a finalizer.
					ObjectMeta: metav1.ObjectMeta{
						Name:       configMapNames[2],
						Namespace:  appNamespaceName,
						Finalizers: []string{testFinalizer},
					},
					Data: map[string]string{"key": "value-2"},
				},
			}
			for _, configMap := range configMaps {
				Expect(hubClient.Create(ctx, configMap)).To(Succeed(), "Failed to create the config map")
			}
			Expect(hubClient.Create(ctx, placementPolicy)).To(Succeed(), "Failed to create the placement policy")
		})

		It("can create a placement resource snapshot", func() {
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
			verifyReturnedPrimarySnapshot(gotSnapshots, isUpToDate, err, placementPolicy, 0)
		})

		It("should strip the last applied configuration annotation, the finalizers, and the read-only fields", func() {
			snapshot := getPrimarySnapshotFromCache(placementPolicy, 0)
			resources := snapshot.GetSpec().Resources
			Expect(resources).To(HaveLen(len(configMapNames)), "Unexpected number of snapshotted resources")

			// Compare the manifests as generic maps; this verifies that the stripped fields are absent.
			wantManifests := []map[string]interface{}{
				{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata": map[string]interface{}{
						"name":        configMapNames[0],
						"namespace":   appNamespaceName,
						"annotations": map[string]interface{}{customAnnotationKey: "keep"},
					},
					"data": map[string]interface{}{"key": "value-0"},
				},
				{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					// The annotations field should be removed altogether, as no annotation is left.
					"metadata": map[string]interface{}{
						"name":      configMapNames[1],
						"namespace": appNamespaceName,
					},
					"data": map[string]interface{}{"key": "value-1"},
				},
				{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					// The finalizers are stripped, even though the live object still carries one.
					"metadata": map[string]interface{}{
						"name":      configMapNames[2],
						"namespace": appNamespaceName,
					},
					"data": map[string]interface{}{"key": "value-2"},
				},
			}
			for idx := range resources {
				if diff := cmp.Diff(manifestAsMap(resources[idx].Manifest.Raw), wantManifests[idx]); diff != "" {
					Fail(fmt.Sprintf("Snapshotted config map manifest (#%d) diff (-got +want):\n%s", idx, diff))
				}
			}
		})

		It("can update stripped fields only", func() {
			configMap := &corev1.ConfigMap{}
			Expect(hubUncachedReader.Get(ctx, types.NamespacedName{Namespace: appNamespaceName, Name: configMapNames[0]}, configMap)).To(Succeed(),
				"Failed to get the config map")
			configMap.Annotations[corev1.LastAppliedConfigAnnotation] = `{"apiVersion":"v1","kind":"ConfigMap","data":{"key":"value-0"}}`
			Expect(hubClient.Update(ctx, configMap)).To(Succeed(), "Failed to update the config map")

			configMap = &corev1.ConfigMap{}
			Expect(hubUncachedReader.Get(ctx, types.NamespacedName{Namespace: appNamespaceName, Name: configMapNames[2]}, configMap)).To(Succeed(),
				"Failed to get the config map")
			configMap.Finalizers = append(configMap.Finalizers, anotherTestFinalizer)
			Expect(hubClient.Update(ctx, configMap)).To(Succeed(), "Failed to update the config map")
		})

		It("should return the existing snapshot as stripped fields are not snapshotted", func() {
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfStale(ctx, placementPolicy)
			verifyReturnedPrimarySnapshot(gotSnapshots, isUpToDate, err, placementPolicy, 0)
			verifyNoPrimarySnapshotAtIdx(placementPolicy, 1)
		})

		It("can update a retained annotation", func() {
			configMap := &corev1.ConfigMap{}
			Expect(hubUncachedReader.Get(ctx, types.NamespacedName{Namespace: appNamespaceName, Name: configMapNames[0]}, configMap)).To(Succeed(),
				"Failed to get the config map")
			configMap.Annotations[customAnnotationKey] = "changed"
			Expect(hubClient.Update(ctx, configMap)).To(Succeed(), "Failed to update the config map")
		})

		It("should create a new snapshot when a retained annotation changes", func() {
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfStale(ctx, placementPolicy)
			verifyReturnedPrimarySnapshot(gotSnapshots, isUpToDate, err, placementPolicy, 1)

			snapshot := getPrimarySnapshotFromCache(placementPolicy, 1)
			resources := snapshot.GetSpec().Resources
			Expect(resources).To(HaveLen(len(configMapNames)), "Unexpected number of snapshotted resources")
			gotMetadata := manifestAsMap(resources[0].Manifest.Raw)["metadata"]
			wantMetadata := map[string]interface{}{
				"name":        configMapNames[0],
				"namespace":   appNamespaceName,
				"annotations": map[string]interface{}{customAnnotationKey: "changed"},
			}
			if diff := cmp.Diff(gotMetadata, wantMetadata); diff != "" {
				Fail(fmt.Sprintf("Snapshotted config map metadata diff (-got +want):\n%s", diff))
			}
		})

		AfterAll(func() {
			removeFinalizersAndDelete(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: configMapNames[2], Namespace: appNamespaceName}})

			objs := primarySnapshotObjs(placementPolicy, 2)
			objs = append(objs,
				placementPolicy,
				&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: configMapNames[0], Namespace: appNamespaceName}},
				&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: configMapNames[1], Namespace: appNamespaceName}},
			)
			deleteObjectsAndWait(objs)
		})
	})

	Context("manifest sanitization (deployments)", Ordered, func() {
		deploymentName := fmt.Sprintf(deploymentNameTemplate, utils.RandStr())

		placementPolicy := newPlacementPolicy(appNamespaceName, placementv1alpha1.ResourceSelector{
			APIGroup:   appsv1.GroupName,
			APIVersion: "v1",
			Kind:       "Deployment",
			Name:       deploymentName,
		})

		BeforeAll(func() {
			deployment := newDeployment(deploymentName, appNamespaceName)
			deployment.Annotations = map[string]string{
				deploymentutil.RevisionAnnotation: "1",
				customAnnotationKey:               "keep",
			}
			Expect(hubClient.Create(ctx, deployment)).To(Succeed(), "Failed to create the Deployment")

			// The envtest environment does not run the Deployment controller; set the status by hand.
			deployment.Status = appsv1.DeploymentStatus{
				ObservedGeneration: 1,
				Replicas:           2,
				ReadyReplicas:      2,
			}
			Expect(hubClient.Status().Update(ctx, deployment)).To(Succeed(), "Failed to update the Deployment status")
			waitForDeploymentsInCache(deployment)

			Expect(hubClient.Create(ctx, placementPolicy)).To(Succeed(), "Failed to create the placement policy")
		})

		It("can create a placement resource snapshot", func() {
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
			verifyReturnedPrimarySnapshot(gotSnapshots, isUpToDate, err, placementPolicy, 0)
		})

		It("should strip the revision annotation, the status, and the read-only fields", func() {
			snapshot := getPrimarySnapshotFromCache(placementPolicy, 0)
			resources := snapshot.GetSpec().Resources
			Expect(resources).To(HaveLen(1), "Unexpected number of snapshotted resources")

			// Only the top-level metadata is compared as a generic map; the pod template (part of the spec)
			// carries its own metadata, which is not subject to sanitization.
			gotManifest := manifestAsMap(resources[0].Manifest.Raw)
			Expect(gotManifest).ToNot(HaveKey("status"), "The status of the Deployment has not been stripped")
			wantMetadata := map[string]interface{}{
				"name":        deploymentName,
				"namespace":   appNamespaceName,
				"labels":      map[string]interface{}{"app": "demo"},
				"annotations": map[string]interface{}{customAnnotationKey: "keep"},
			}
			if diff := cmp.Diff(gotManifest["metadata"], wantMetadata); diff != "" {
				Fail(fmt.Sprintf("Snapshotted Deployment metadata diff (-got +want):\n%s", diff))
			}

			// The spec should match that of the live object (which carries the defaulted fields).
			liveDeployment := &appsv1.Deployment{}
			Expect(hubUncachedReader.Get(ctx, types.NamespacedName{Namespace: appNamespaceName, Name: deploymentName}, liveDeployment)).To(Succeed(),
				"Failed to get the Deployment")
			gotDeployment := &appsv1.Deployment{}
			Expect(json.Unmarshal(resources[0].Manifest.Raw, gotDeployment)).To(Succeed(), "Failed to unmarshal the Deployment manifest")
			if diff := cmp.Diff(gotDeployment.Spec, liveDeployment.Spec, cmpopts.EquateEmpty()); diff != "" {
				Fail(fmt.Sprintf("Snapshotted Deployment spec diff (-got +want):\n%s", diff))
			}
		})

		It("can update the status and the revision annotation", func() {
			deployment := &appsv1.Deployment{}
			Expect(hubUncachedReader.Get(ctx, types.NamespacedName{Namespace: appNamespaceName, Name: deploymentName}, deployment)).To(Succeed(),
				"Failed to get the Deployment")
			deployment.Status.ReadyReplicas = 1
			deployment.Status.UpdatedReplicas = 1
			Expect(hubClient.Status().Update(ctx, deployment)).To(Succeed(), "Failed to update the Deployment status")

			deployment = &appsv1.Deployment{}
			Expect(hubUncachedReader.Get(ctx, types.NamespacedName{Namespace: appNamespaceName, Name: deploymentName}, deployment)).To(Succeed(),
				"Failed to get the Deployment")
			deployment.Annotations[deploymentutil.RevisionAnnotation] = "2"
			Expect(hubClient.Update(ctx, deployment)).To(Succeed(), "Failed to update the Deployment")
			waitForDeploymentsInCache(deployment)
		})

		It("should return the existing snapshot as only stripped fields have changed", func() {
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfStale(ctx, placementPolicy)
			verifyReturnedPrimarySnapshot(gotSnapshots, isUpToDate, err, placementPolicy, 0)
			verifyNoPrimarySnapshotAtIdx(placementPolicy, 1)
		})

		AfterAll(func() {
			objs := primarySnapshotObjs(placementPolicy, 2)
			objs = append(objs,
				placementPolicy,
				&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: deploymentName, Namespace: appNamespaceName}},
			)
			deleteObjectsAndWait(objs)
		})
	})

	Context("resources in the default namespace", Ordered, func() {
		// The default namespace itself cannot be placed, but the resources within it can.
		groupLabels := map[string]string{placementGroupLabelKey: utils.RandStr()}
		configMapName := fmt.Sprintf(configMapNameTemplate, utils.RandStr())
		labeledConfigMapName := configMapName + "-labeled"

		placementPolicy := newPlacementPolicy(defaultNamespaceName,
			configMapNameSelector(configMapName),
			configMapLabelSelector(groupLabels),
		)
		kubernetesAPIServicePlacementPolicy := newPlacementPolicy(defaultNamespaceName, placementv1alpha1.ResourceSelector{
			APIVersion: "v1",
			Kind:       "Service",
			Name:       kubernetesAPIServiceName,
		})

		// The resources are sorted by their unique IDs; the labeled config map has a longer name with the same prefix.
		wantIdentifiers := []placementv1alpha1.ObjectReference{
			{
				Namespace:  defaultNamespaceName,
				Name:       configMapName,
				APIVersion: "v1",
				Kind:       "ConfigMap",
			},
			{
				Namespace:  defaultNamespaceName,
				Name:       labeledConfigMapName,
				APIVersion: "v1",
				Kind:       "ConfigMap",
			},
		}

		BeforeAll(func() {
			// Make sure that the Kubernetes API service has been created by the API server.
			Eventually(func() error {
				return hubUncachedReader.Get(ctx, types.NamespacedName{Namespace: defaultNamespaceName, Name: kubernetesAPIServiceName}, &corev1.Service{})
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to find the Kubernetes API service")

			objs := []client.Object{
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: defaultNamespaceName},
					Data:       map[string]string{"key": "value"},
				},
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: labeledConfigMapName, Namespace: defaultNamespaceName, Labels: groupLabels},
					Data:       map[string]string{"key": "value"},
				},
				placementPolicy,
				kubernetesAPIServicePlacementPolicy,
			}
			for _, obj := range objs {
				Expect(hubClient.Create(ctx, obj)).To(Succeed(), "Failed to create object %T %s", obj, obj.GetName())
			}
		})

		It("can create a placement resource snapshot with resources in the default namespace", func() {
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
			verifyReturnedPrimarySnapshot(gotSnapshots, isUpToDate, err, placementPolicy, 0)

			snapshot := getPrimarySnapshotFromCache(placementPolicy, 0)
			verifySnapshottedResourceIdentifiers(snapshot.GetSpec().Resources, wantIdentifiers)
		})

		It("should return a user error when the Kubernetes API service is selected by name", func() {
			err := snapshotResourcesAndExpectErr(kubernetesAPIServicePlacementPolicy)
			verifyErrCategoryAndMsg(err, kferrors.ErrCategoryUser, ineligibleResourceErrMsg,
				"the Kubernetes API service is created automatically in the default namespace and cannot be placed")
		})

		AfterAll(func() {
			objs := primarySnapshotObjs(placementPolicy, 1)
			objs = append(objs,
				placementPolicy,
				kubernetesAPIServicePlacementPolicy,
				&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: defaultNamespaceName}},
				&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: labeledConfigMapName, Namespace: defaultNamespaceName}},
			)
			deleteObjectsAndWait(objs)
		})
	})
})

var _ = Describe("resource selection (cluster placement policies)", func() {
	Context("invalid resource selectors", Ordered, func() {
		configMapName := fmt.Sprintf(configMapNameTemplate, utils.RandStr())
		// The names of resources that do not exist; the errors are expected to occur before any retrieval.
		resourceName := utils.RandStr()

		testCases := []snapshotErrTestCase{
			{
				desc: "the resource selector specifies a reserved Kubernetes namespace",
				placementPolicy: newClusterPlacementPolicy(placementv1alpha1.ResourceSelector{
					APIVersion: "v1",
					Kind:       "ConfigMap",
					Namespace:  "kube-system",
					Name:       resourceName,
				}),
				wantErrCategory:   kferrors.ErrCategoryUser,
				wantErrMsgSubstrs: []string{"resource selector specifies a namespace that is ineligible for placement"},
			},
			{
				desc: "the resource selector specifies a reserved KubeFleet namespace",
				placementPolicy: newClusterPlacementPolicy(placementv1alpha1.ResourceSelector{
					APIVersion: "v1",
					Kind:       "ConfigMap",
					// The namespace does not need to exist.
					Namespace: fmt.Sprintf(fleetNamespaceNameTemplate, utils.RandStr()),
					Name:      resourceName,
				}),
				wantErrCategory:   kferrors.ErrCategoryUser,
				wantErrMsgSubstrs: []string{"resource selector specifies a namespace that is ineligible for placement"},
			},
			{
				desc: "the resource selector selects a namespace-scoped resource without specifying a namespace",
				placementPolicy: newClusterPlacementPolicy(placementv1alpha1.ResourceSelector{
					APIVersion: "v1",
					Kind:       "ConfigMap",
					Name:       resourceName,
				}),
				wantErrCategory:   kferrors.ErrCategoryUser,
				wantErrMsgSubstrs: []string{"a namespace must be specified for namespace-scoped resources"},
			},
			{
				desc: "the resource selector selects a cluster-scoped resource with a namespace specified",
				placementPolicy: newClusterPlacementPolicy(placementv1alpha1.ResourceSelector{
					APIGroup:   rbacv1.GroupName,
					APIVersion: "v1",
					Kind:       "ClusterRole",
					Namespace:  appNamespaceName,
					Name:       resourceName,
				}),
				wantErrCategory:   kferrors.ErrCategoryUser,
				wantErrMsgSubstrs: []string{"namespace must not be specified for cluster-scoped resources"},
			},
			{
				desc: "the resource selector selects pods",
				placementPolicy: newClusterPlacementPolicy(placementv1alpha1.ResourceSelector{
					APIVersion: "v1",
					Kind:       "Pod",
					Namespace:  appNamespaceName,
					Name:       resourceName,
				}),
				wantErrCategory:   kferrors.ErrCategoryUser,
				wantErrMsgSubstrs: []string{ineligibleGVKErrMsg},
			},
			{
				desc: "the resource selector specifies neither a name nor a label selector",
				placementPolicy: newClusterPlacementPolicy(placementv1alpha1.ResourceSelector{
					APIVersion: "v1",
					Kind:       "ConfigMap",
					Namespace:  appNamespaceName,
				}),
				wantErrCategory:   kferrors.ErrCategoryUser,
				wantErrMsgSubstrs: []string{"neither name nor label selector is specified"},
			},
			{
				desc: "the label selector uses an unknown operator",
				placementPolicy: newClusterPlacementPolicy(placementv1alpha1.ResourceSelector{
					APIVersion: "v1",
					Kind:       "ConfigMap",
					Namespace:  appNamespaceName,
					LabelSelector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{Key: placementGroupLabelKey, Operator: "Foo", Values: []string{"bar"}},
						},
					},
				}),
				wantErrCategory:   kferrors.ErrCategoryUser,
				wantErrMsgSubstrs: []string{"invalid label selector", "is not a valid label selector operator"},
			},
			{
				desc: "the resource selector specifies an unknown kind",
				placementPolicy: newClusterPlacementPolicy(placementv1alpha1.ResourceSelector{
					APIVersion: "v1",
					Kind:       "ConfigMapp",
					Namespace:  appNamespaceName,
					Name:       resourceName,
				}),
				wantErrCategory:   kferrors.ErrCategoryUser,
				wantErrMsgSubstrs: []string{noMatchForGVKErrMsg, `no matches for kind "ConfigMapp" in version "v1"`},
			},
			{
				desc: "the selected resource does not exist",
				placementPolicy: newClusterPlacementPolicy(placementv1alpha1.ResourceSelector{
					APIVersion: "v1",
					Kind:       "ConfigMap",
					Namespace:  appNamespaceName,
					Name:       configMapName,
				}),
				wantErrCategory:   kferrors.ErrCategoryAPIServer,
				wantErrMsgSubstrs: []string{resourceNotFoundErrMsg},
			},
		}

		BeforeAll(func() {
			// Create the cluster placement policies; this also verifies that the API server accepts them.
			for _, tc := range testCases {
				Expect(hubClient.Create(ctx, tc.placementPolicy)).To(Succeed(), "Failed to create the cluster placement policy (%s)", tc.desc)
			}
		})

		for _, tc := range testCases {
			It(fmt.Sprintf("should return an error when %s", tc.desc), func() {
				verifySnapshotErrTestCase(tc)
			})
		}

		It("should return an unexpected error when no resource selectors are present", func() {
			// The CRD requires at least one resource selector; use an in-memory cluster placement policy instead.
			placementPolicy := newClusterPlacementPolicy()
			err := snapshotResourcesAndExpectErr(placementPolicy)
			verifyErrCategoryAndMsg(err, kferrors.ErrCategoryUnexpected, "no resource selectors are present")
		})

		AfterAll(func() {
			objs := make([]client.Object, 0, len(testCases))
			for _, tc := range testCases {
				objs = append(objs, tc.placementPolicy)
			}
			deleteObjectsAndWait(objs)
		})
	})

	Context("namespaces (name-based selection)", Ordered, func() {
		namespaceName := fmt.Sprintf(namespaceNameTemplate, utils.RandStr())
		fleetNamespaceName := fmt.Sprintf(fleetNamespaceNameTemplate, utils.RandStr())

		namespaceSelector := func(name string) placementv1alpha1.ResourceSelector {
			return placementv1alpha1.ResourceSelector{
				APIVersion: "v1",
				Kind:       "Namespace",
				Name:       name,
			}
		}

		testCases := []snapshotErrTestCase{
			{
				desc:              "a reserved Kubernetes namespace is selected by name",
				placementPolicy:   newClusterPlacementPolicy(namespaceSelector("kube-system")),
				wantErrCategory:   kferrors.ErrCategoryUser,
				wantErrMsgSubstrs: []string{ineligibleNamespaceErrMsg},
			},
			{
				desc:            "the default namespace is selected by name",
				placementPolicy: newClusterPlacementPolicy(namespaceSelector("default")),
				wantErrCategory: kferrors.ErrCategoryUser,
				wantErrMsgSubstrs: []string{ineligibleResourceErrMsg,
					"the default namespace itself cannot be placed"},
			},
			{
				desc:              "a reserved KubeFleet namespace is selected by name",
				placementPolicy:   newClusterPlacementPolicy(namespaceSelector(fleetNamespaceName)),
				wantErrCategory:   kferrors.ErrCategoryUser,
				wantErrMsgSubstrs: []string{ineligibleNamespaceErrMsg},
			},
		}

		placementPolicy := newClusterPlacementPolicy(namespaceSelector(namespaceName))

		BeforeAll(func() {
			createNamespace(namespaceName)
			createNamespace(fleetNamespaceName)

			for _, tc := range testCases {
				Expect(hubClient.Create(ctx, tc.placementPolicy)).To(Succeed(), "Failed to create the cluster placement policy (%s)", tc.desc)
			}
			Expect(hubClient.Create(ctx, placementPolicy)).To(Succeed(), "Failed to create the cluster placement policy")
		})

		for _, tc := range testCases {
			It(fmt.Sprintf("should return a user error when %s", tc.desc), func() {
				verifySnapshotErrTestCase(tc)
			})
		}

		It("can create a placement resource snapshot for an eligible namespace", func() {
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
			verifyReturnedPrimarySnapshot(gotSnapshots, isUpToDate, err, placementPolicy, 0)

			snapshot := getPrimarySnapshotFromCache(placementPolicy, 0)
			if diff := cmp.Diff(snapshot.GetOwnerReferences(), wantPlacementPolicyOwnerRefs(placementPolicy)); diff != "" {
				Fail(fmt.Sprintf("Placement resource snapshot owner references diff (-got +want):\n%s", diff))
			}
			wantIdentifiers := []placementv1alpha1.ObjectReference{
				{
					Name:       namespaceName,
					APIVersion: "v1",
					Kind:       "Namespace",
				},
			}
			verifySnapshottedResourceIdentifiers(snapshot.GetSpec().Resources, wantIdentifiers)
		})

		It("should strip the status of the namespace", func() {
			snapshot := getPrimarySnapshotFromCache(placementPolicy, 0)
			resources := snapshot.GetSpec().Resources
			Expect(resources).To(HaveLen(1), "Unexpected number of snapshotted resources")
			Expect(manifestAsMap(resources[0].Manifest.Raw)).ToNot(HaveKey("status"), "The status of the namespace has not been stripped")

			// Note that the finalizers in the spec of the namespace are kept; only those in the metadata are stripped.
			gotNamespace := &corev1.Namespace{}
			Expect(json.Unmarshal(resources[0].Manifest.Raw, gotNamespace)).To(Succeed(), "Failed to unmarshal the namespace manifest")
			wantNamespace := &corev1.Namespace{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Namespace"},
				ObjectMeta: metav1.ObjectMeta{
					Name:   namespaceName,
					Labels: map[string]string{corev1.LabelMetadataName: namespaceName},
				},
				Spec: corev1.NamespaceSpec{
					Finalizers: []corev1.FinalizerName{corev1.FinalizerKubernetes},
				},
			}
			if diff := cmp.Diff(gotNamespace, wantNamespace); diff != "" {
				Fail(fmt.Sprintf("Snapshotted namespace diff (-got +want):\n%s", diff))
			}
		})

		AfterAll(func() {
			objs := primarySnapshotObjs(placementPolicy, 1)
			objs = append(objs, placementPolicy)
			for _, tc := range testCases {
				objs = append(objs, tc.placementPolicy)
			}
			deleteObjectsAndWait(objs)
		})
	})

	Context("namespaces (label-based selection)", Ordered, func() {
		namespaceName := fmt.Sprintf(namespaceNameTemplate, utils.RandStr())
		fleetNamespaceName := fmt.Sprintf(fleetNamespaceNameTemplate, utils.RandStr())

		// Namespaces are labeled with their names automatically.
		namespaceSelectorByNames := func(names ...string) placementv1alpha1.ResourceSelector {
			return placementv1alpha1.ResourceSelector{
				APIVersion: "v1",
				Kind:       "Namespace",
				LabelSelector: &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      corev1.LabelMetadataName,
							Operator: metav1.LabelSelectorOpIn,
							Values:   names,
						},
					},
				},
			}
		}

		placementPolicy := newClusterPlacementPolicy(namespaceSelectorByNames(
			"default", "kube-system", "kube-public", "kube-node-lease", fleetNamespaceName, namespaceName))
		// This cluster placement policy selects ineligible namespaces only.
		allIneligiblePlacementPolicy := newClusterPlacementPolicy(namespaceSelectorByNames(
			"default", "kube-system", fleetNamespaceName))

		wantIdentifiers := []placementv1alpha1.ObjectReference{
			{
				Name:       namespaceName,
				APIVersion: "v1",
				Kind:       "Namespace",
			},
		}

		BeforeAll(func() {
			createNamespace(namespaceName)
			createNamespace(fleetNamespaceName)

			Expect(hubClient.Create(ctx, placementPolicy)).To(Succeed(), "Failed to create the cluster placement policy")
			Expect(hubClient.Create(ctx, allIneligiblePlacementPolicy)).To(Succeed(), "Failed to create the cluster placement policy")
		})

		It("can compute the hash of selected resources", func() {
			// The ineligible namespaces are skipped silently.
			resources, _, err := resourceSnapshotManager.retrieveAndHashSelectedResources(ctx, placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to retrieve and hash the selected resources")
			verifySnapshottedResourceIdentifiers(resources, wantIdentifiers)
		})

		It("can create a placement resource snapshot with the eligible namespace only", func() {
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
			verifyReturnedPrimarySnapshot(gotSnapshots, isUpToDate, err, placementPolicy, 0)

			snapshot := getPrimarySnapshotFromCache(placementPolicy, 0)
			verifySnapshottedResourceIdentifiers(snapshot.GetSpec().Resources, wantIdentifiers)
		})

		It("can create an empty placement resource snapshot when every matched namespace is ineligible", func() {
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, allIneligiblePlacementPolicy)
			verifyReturnedPrimarySnapshot(gotSnapshots, isUpToDate, err, allIneligiblePlacementPolicy, 0)

			snapshot := getPrimarySnapshotFromCache(allIneligiblePlacementPolicy, 0)
			Expect(snapshot.GetSpec().Resources).To(BeEmpty(), "The placement resource snapshot is not empty")
		})

		AfterAll(func() {
			objs := primarySnapshotObjs(placementPolicy, 1)
			objs = append(objs, primarySnapshotObjs(allIneligiblePlacementPolicy, 1)...)
			objs = append(objs, placementPolicy, allIneligiblePlacementPolicy)
			deleteObjectsAndWait(objs)
		})
	})

	Context("cluster-scoped and namespaced resources", Ordered, func() {
		namespaceName := fmt.Sprintf(namespaceNameTemplate, utils.RandStr())
		// The config maps in the two namespaces share the same name.
		configMapName := fmt.Sprintf(configMapNameTemplate, utils.RandStr())
		deploymentName := fmt.Sprintf(deploymentNameTemplate, utils.RandStr())
		clusterRoleName := fmt.Sprintf(clusterRoleNameTemplate, utils.RandStr())

		// The resource selectors are deliberately listed in the reverse order of the snapshotted resources.
		placementPolicy := newClusterPlacementPolicy(
			placementv1alpha1.ResourceSelector{
				APIGroup:   rbacv1.GroupName,
				APIVersion: "v1",
				Kind:       "ClusterRole",
				Name:       clusterRoleName,
			},
			placementv1alpha1.ResourceSelector{
				APIGroup:   appsv1.GroupName,
				APIVersion: "v1",
				Kind:       "Deployment",
				Namespace:  appNamespaceName,
				Name:       deploymentName,
			},
			placementv1alpha1.ResourceSelector{
				APIVersion: "v1",
				Kind:       "Namespace",
				Name:       namespaceName,
			},
			placementv1alpha1.ResourceSelector{
				APIVersion: "v1",
				Kind:       "ConfigMap",
				Namespace:  namespaceName,
				Name:       configMapName,
			},
			placementv1alpha1.ResourceSelector{
				APIVersion: "v1",
				Kind:       "ConfigMap",
				Namespace:  appNamespaceName,
				Name:       configMapName,
			},
		)

		// The resources are sorted by their unique IDs, i.e., `[API-GROUP]/[KIND]/[NAMESPACE]/[NAME]`; the namespace
		// `app` sorts before `ns-...`.
		wantIdentifiers := []placementv1alpha1.ObjectReference{
			{
				Namespace:  appNamespaceName,
				Name:       configMapName,
				APIVersion: "v1",
				Kind:       "ConfigMap",
			},
			{
				Namespace:  namespaceName,
				Name:       configMapName,
				APIVersion: "v1",
				Kind:       "ConfigMap",
			},
			{
				Name:       namespaceName,
				APIVersion: "v1",
				Kind:       "Namespace",
			},
			{
				Namespace:  appNamespaceName,
				Name:       deploymentName,
				APIGroup:   appsv1.GroupName,
				APIVersion: "v1",
				Kind:       "Deployment",
			},
			{
				Name:       clusterRoleName,
				APIGroup:   rbacv1.GroupName,
				APIVersion: "v1",
				Kind:       "ClusterRole",
			},
		}

		var wantHash string

		BeforeAll(func() {
			createNamespace(namespaceName)

			deployment := newDeployment(deploymentName, appNamespaceName)
			objs := []client.Object{
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: appNamespaceName},
					Data:       map[string]string{"key": "value"},
				},
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: namespaceName},
					Data:       map[string]string{"key": "value"},
				},
				deployment,
				&rbacv1.ClusterRole{
					ObjectMeta: metav1.ObjectMeta{Name: clusterRoleName},
				},
			}
			for _, obj := range objs {
				Expect(hubClient.Create(ctx, obj)).To(Succeed(), "Failed to create object %T %s", obj, obj.GetName())
			}
			waitForDeploymentsInCache(deployment)
			Expect(hubClient.Create(ctx, placementPolicy)).To(Succeed(), "Failed to create the cluster placement policy")
		})

		It("can compute the hash of selected resources", func() {
			resources, hash, err := resourceSnapshotManager.retrieveAndHashSelectedResources(ctx, placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to retrieve and hash the selected resources")
			verifySnapshottedResourceIdentifiers(resources, wantIdentifiers)
			wantHash = hash
		})

		It("can create a primary and a secondary placement resource snapshot", func() {
			// Five resources, with at most three resources per snapshot: one primary and one secondary snapshot.
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to snapshot resources")
			Expect(isUpToDate).To(BeTrue(), "The placement resource snapshots are not up-to-date")

			wantNames := []string{
				uniquename.PrimaryPlacementResourceSnapshotName(placementPolicy.Name, placementPolicy.UID, 0),
				uniquename.SecondaryPlacementResourceSnapshotName(placementPolicy.Name, placementPolicy.UID, 0, 1),
			}
			gotNames := make([]string, 0, len(gotSnapshots))
			for _, snapshot := range gotSnapshots {
				gotNames = append(gotNames, snapshot.GetName())
			}
			if diff := cmp.Diff(gotNames, wantNames); diff != "" {
				Fail(fmt.Sprintf("Returned placement resource snapshot names diff (-got +want):\n%s", diff))
			}
		})

		It("should persist the snapshots with resources in the sorted order", func() {
			primarySnapshot := getSnapshotFromCache("", uniquename.PrimaryPlacementResourceSnapshotName(placementPolicy.Name, placementPolicy.UID, 0))
			verifySnapshotMetadata(primarySnapshot, placementPolicy, 0, 0, 2, wantHash)
			verifySnapshottedResourceIdentifiers(primarySnapshot.GetSpec().Resources, wantIdentifiers[:3])

			secondarySnapshot := getSnapshotFromCache("", uniquename.SecondaryPlacementResourceSnapshotName(placementPolicy.Name, placementPolicy.UID, 0, 1))
			verifySnapshotMetadata(secondarySnapshot, placementPolicy, 0, 1, 2, wantHash)
			secondaryResources := secondarySnapshot.GetSpec().Resources
			verifySnapshottedResourceIdentifiers(secondaryResources, wantIdentifiers[3:])

			// Verify the manifest of the cluster-scoped cluster role.
			gotClusterRole := &rbacv1.ClusterRole{}
			Expect(json.Unmarshal(secondaryResources[1].Manifest.Raw, gotClusterRole)).To(Succeed(), "Failed to unmarshal the cluster role manifest")
			wantClusterRole := &rbacv1.ClusterRole{
				TypeMeta: metav1.TypeMeta{APIVersion: rbacv1.SchemeGroupVersion.String(), Kind: "ClusterRole"},
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterRoleName,
				},
			}
			if diff := cmp.Diff(gotClusterRole, wantClusterRole); diff != "" {
				Fail(fmt.Sprintf("Snapshotted cluster role diff (-got +want):\n%s", diff))
			}
		})

		AfterAll(func() {
			objs := []client.Object{
				placementResourceSnapshotObj("", uniquename.PrimaryPlacementResourceSnapshotName(placementPolicy.Name, placementPolicy.UID, 0)),
				placementResourceSnapshotObj("", uniquename.SecondaryPlacementResourceSnapshotName(placementPolicy.Name, placementPolicy.UID, 0, 1)),
				placementPolicy,
				&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: appNamespaceName}},
				&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: namespaceName}},
				&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: deploymentName, Namespace: appNamespaceName}},
				&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: clusterRoleName}},
			}
			deleteObjectsAndWait(objs)
		})
	})

	Context("resources in the default namespace", Ordered, func() {
		// The default namespace itself cannot be placed, but the resources within it can.
		groupLabels := map[string]string{placementGroupLabelKey: utils.RandStr()}
		configMapName := fmt.Sprintf(configMapNameTemplate, utils.RandStr())
		labeledConfigMapName := configMapName + "-labeled"

		placementPolicy := newClusterPlacementPolicy(
			placementv1alpha1.ResourceSelector{
				APIVersion: "v1",
				Kind:       "ConfigMap",
				Namespace:  defaultNamespaceName,
				Name:       configMapName,
			},
			placementv1alpha1.ResourceSelector{
				APIVersion:    "v1",
				Kind:          "ConfigMap",
				Namespace:     defaultNamespaceName,
				LabelSelector: &metav1.LabelSelector{MatchLabels: groupLabels},
			},
		)

		// The resources are sorted by their unique IDs; the labeled config map has a longer name with the same prefix.
		wantIdentifiers := []placementv1alpha1.ObjectReference{
			{
				Namespace:  defaultNamespaceName,
				Name:       configMapName,
				APIVersion: "v1",
				Kind:       "ConfigMap",
			},
			{
				Namespace:  defaultNamespaceName,
				Name:       labeledConfigMapName,
				APIVersion: "v1",
				Kind:       "ConfigMap",
			},
		}

		BeforeAll(func() {
			objs := []client.Object{
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: defaultNamespaceName},
					Data:       map[string]string{"key": "value"},
				},
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: labeledConfigMapName, Namespace: defaultNamespaceName, Labels: groupLabels},
					Data:       map[string]string{"key": "value"},
				},
				placementPolicy,
			}
			for _, obj := range objs {
				Expect(hubClient.Create(ctx, obj)).To(Succeed(), "Failed to create object %T %s", obj, obj.GetName())
			}
		})

		It("can create a placement resource snapshot with resources in the default namespace", func() {
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
			verifyReturnedPrimarySnapshot(gotSnapshots, isUpToDate, err, placementPolicy, 0)

			snapshot := getPrimarySnapshotFromCache(placementPolicy, 0)
			verifySnapshottedResourceIdentifiers(snapshot.GetSpec().Resources, wantIdentifiers)
		})

		AfterAll(func() {
			objs := primarySnapshotObjs(placementPolicy, 1)
			objs = append(objs,
				placementPolicy,
				&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: defaultNamespaceName}},
				&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: labeledConfigMapName, Namespace: defaultNamespaceName}},
			)
			deleteObjectsAndWait(objs)
		})
	})
})
