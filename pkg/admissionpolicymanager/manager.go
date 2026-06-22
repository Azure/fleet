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

package admissionpolicymanager

import (
	"context"
	"reflect"
	"time"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"go.goms.io/fleet/pkg/utils/errors"
)

const (
	// The following labels are added to all policies created by the policy manager,
	// so that the agent can track the lifecycle of created policies across different runs
	// and act accordingly.
	VAPManagedByKubeFleetLabelKey                = "app.kubernetes.io/managed-by"
	VAPManagedByKubeFleetLabelValue              = "fleet-hub-agent"
	VAPPartOfKubeFleetLabelKey                   = "app.kubernetes.io/part-of"
	VAPPartOfKubeFleetLabelValue                 = "fleet"
	VAPComponentKubeFleetLabelKey                = "app.kubernetes.io/component"
	VAPComponentAdmissionPolicyManagerLabelValue = "admission-policy-manager"
)

var (
	// A list of all available policy generators.
	allGenerators = sets.Set[string]{}
)

func init() {
	// Add all available generators to the set.
	v := reflect.ValueOf(DefaultPolicyGeneratorConfigs).Elem()
	for i := range v.NumField() {
		field := v.Field(i)
		if field.IsNil() {
			continue
		}
		gen, ok := field.Interface().(ValidatingAdmissionPolicyGenerator)
		if !ok {
			continue
		}
		allGenerators.Insert(gen.Name())
	}
}

// AllGenerators returns a copy of all available policy generators.
func AllGenerators() sets.Set[string] {
	return allGenerators.Clone()
}

var (
	policyRWOpBackoff = wait.Backoff{
		Steps:    3,
		Duration: 1 * time.Second,
		Factor:   2.0,
		Jitter:   0.1,
	}
)

type PolicyWithBindings struct {
	Policy   *admissionregistrationv1.ValidatingAdmissionPolicy
	Bindings []*admissionregistrationv1.ValidatingAdmissionPolicyBinding
}

type ValidatingAdmissionPolicyGenerator interface {
	Name() string
	Validate() error
	PoliciesWithBindings() []PolicyWithBindings
}

type PolicyManager struct {
	Client client.Client

	enabledPolicyGenerators map[string]ValidatingAdmissionPolicyGenerator
}

func New(client client.Client, policyGeneratorConfigs *PolicyGeneratorConfigs) (*PolicyManager, error) {
	if policyGeneratorConfigs == nil {
		klog.V(2).Info("No admission policy generator configuration provided, falling back to the default configuration")
		policyGeneratorConfigs = DefaultPolicyGeneratorConfigs
	}
	// Prepare a set of generators based on the given configuration.
	enabledPolicyGenerators, err := preparePolicyGenerators(policyGeneratorConfigs)
	if err != nil {
		return nil, errors.Wraps(err, "failed to create policy manager")
	}

	return &PolicyManager{
		Client:                  client,
		enabledPolicyGenerators: enabledPolicyGenerators,
	}, nil
}

func preparePolicyGenerators(policyGeneratorConfigs *PolicyGeneratorConfigs) (map[string]ValidatingAdmissionPolicyGenerator, error) {
	enabledPolicyGenerators := make(map[string]ValidatingAdmissionPolicyGenerator)

	v := reflect.ValueOf(policyGeneratorConfigs).Elem()
	for i := range v.NumField() {
		field := v.Field(i)
		if field.IsNil() {
			continue
		}
		gen, ok := field.Interface().(ValidatingAdmissionPolicyGenerator)
		if !ok {
			continue
		}
		if gen != nil {
			enabledPolicyGenerators[gen.Name()] = gen
		}
	}

	return enabledPolicyGenerators, nil
}

func (m *PolicyManager) Start(ctx context.Context) error {
	// Generate all policies and policy bindings from the enabled generators, and apply them to the cluster.
	createdOrUpdatedPolicyNames, createdOrUpdatedPolicyBindingNames, err := m.createOrUpdatePoliciesAndBindingsForEnabledGenerators(ctx)
	if err != nil {
		return errors.Wraps(err, "failed to create or update validating admission policies and bindings for enabled generators")
	}

	// List all existing policies and policy bindings created by the manager, and delete those that are no longer needed.
	if err := m.garbageCollectUnusedPoliciesAndBindings(ctx, createdOrUpdatedPolicyNames, createdOrUpdatedPolicyBindingNames); err != nil {
		return errors.Wraps(err, "failed to garbage collect unused validating admission policies and bindings")
	}
	return nil
}

// createOrUpdatePoliciesAndBindingsForEnabledGenerators creates or updates validating admission
// policies and their bindings for all enabled generators, and returns the names of
// created or updated policies and policy bindings.
func (m *PolicyManager) createOrUpdatePoliciesAndBindingsForEnabledGenerators(ctx context.Context) (sets.Set[string], sets.Set[string], error) {
	createdOrUpdatedPolicyNames := sets.New[string]()
	createdOrUpdatedPolicyBindingNames := sets.New[string]()

	for _, gen := range m.enabledPolicyGenerators {
		// As a sanity check, do one more round of validation.
		//
		// Normally this check would never fail as the generators have been validated before
		// the manager initializes.
		if err := gen.Validate(); err != nil {
			return nil, nil, errors.Wraps(err, "policy generator is invalid", "generator", gen.Name())
		}

		policiesWithBindings := gen.PoliciesWithBindings()

		for _, pb := range policiesWithBindings {
			policy := pb.Policy
			policyBindings := pb.Bindings

			// Create the policy.
			addManagedByPartOfAndComponentLabels(policy)
			policyToCreateOrUpdate := &admissionregistrationv1.ValidatingAdmissionPolicy{
				ObjectMeta: policy.ObjectMeta,
			}

			err := retry.OnError(policyRWOpBackoff, buildRetryUnlessCtxErr(ctx), func() error {
				opRes, err := controllerutil.CreateOrUpdate(ctx, m.Client, policyToCreateOrUpdate, func() error {
					policyCopy := policy.DeepCopy()
					policyToCreateOrUpdate.Spec = policyCopy.Spec
					policyToCreateOrUpdate.Labels = policyCopy.Labels
					return nil
				})
				if err != nil {
					return errors.NewAPIServerError(err,
						"failed to create/update validating admission policy",
						false,
						"op", opRes, "policyName", policy.Name, "policyGenerator", gen.Name())
				}
				return nil
			})
			if err != nil {
				// No need to wrap this for another time. The inner error already contains sufficient context about the failure.
				return nil, nil, err
			}
			createdOrUpdatedPolicyNames.Insert(policy.Name)
			klog.V(2).InfoS("Successfully created or updated validating admission policy", "policyName", policy.Name, "policyGenerator", gen.Name())

			// Create the bindings.
			for idx := range policyBindings {
				policyBinding := policyBindings[idx]

				addManagedByPartOfAndComponentLabels(policyBinding)
				policyBindingToCreateOrUpdate := &admissionregistrationv1.ValidatingAdmissionPolicyBinding{
					ObjectMeta: policyBinding.ObjectMeta,
				}

				err := retry.OnError(policyRWOpBackoff, buildRetryUnlessCtxErr(ctx), func() error {
					opRes, err := controllerutil.CreateOrUpdate(ctx, m.Client, policyBindingToCreateOrUpdate, func() error {
						policyBindingCopy := policyBinding.DeepCopy()
						policyBindingToCreateOrUpdate.Spec = policyBindingCopy.Spec
						policyBindingToCreateOrUpdate.Labels = policyBindingCopy.Labels
						return nil
					})
					if err != nil {
						return errors.NewAPIServerError(err,
							"failed to create/update validating admission policy binding",
							false,
							"op", opRes, "policyBindingName", policyBinding.Name, "policyGenerator", gen.Name())
					}
					return nil
				})
				if err != nil {
					// No need to wrap this for another time. The inner error already contains sufficient context about the failure.
					return nil, nil, err
				}

				createdOrUpdatedPolicyBindingNames.Insert(policyBinding.Name)
				klog.V(2).InfoS("Successfully created or updated validating admission policy binding", "policyBindingName", policyBinding.Name, "policyGenerator", gen.Name())
			}
		}
	}

	return createdOrUpdatedPolicyNames, createdOrUpdatedPolicyBindingNames, nil
}

func (m *PolicyManager) garbageCollectUnusedPoliciesAndBindings(ctx context.Context, createdOrUpdatedPolicyNames sets.Set[string], createdOrUpdatedPolicyBindingNames sets.Set[string]) error {
	// List all existing policies and policy bindings created by the manager.
	existingPolicyList := &admissionregistrationv1.ValidatingAdmissionPolicyList{}
	existingPolicyBindingList := &admissionregistrationv1.ValidatingAdmissionPolicyBindingList{}

	err := retry.OnError(policyRWOpBackoff, buildRetryUnlessCtxErr(ctx), func() error {
		if err := m.Client.List(ctx, existingPolicyList, managedByAndPartOfKubeFleetLabelSelector); err != nil {
			return errors.NewAPIServerError(err, "failed to list all validating admission policies managed by KubeFleet", false)
		}
		return nil
	})
	if err != nil {
		// No need to wrap this for another time. The inner error already contains sufficient context about the failure.
		return err
	}

	err = retry.OnError(policyRWOpBackoff, buildRetryUnlessCtxErr(ctx), func() error {
		if err := m.Client.List(ctx, existingPolicyBindingList, managedByAndPartOfKubeFleetLabelSelector); err != nil {
			return errors.NewAPIServerError(err, "failed to list all validating admission policy bindings managed by KubeFleet", false)
		}
		return nil
	})
	if err != nil {
		// No need to wrap this for another time. The inner error already contains sufficient context about the failure.
		return err
	}

	// Delete policies that are created by the manager but no longer needed.
	for i := range existingPolicyList.Items {
		policy := &existingPolicyList.Items[i]
		if !createdOrUpdatedPolicyNames.Has(policy.Name) {
			err := retry.OnError(policyRWOpBackoff, buildRetryUnlessCtxErr(ctx), func() error {
				if err := m.Client.Delete(ctx, policy); err != nil && !apierrors.IsNotFound(err) {
					return errors.NewAPIServerError(err,
						"failed to delete validating admission policy",
						false,
						"policyName", policy.Name)
				}
				return nil
			})
			if err != nil {
				// No need to wrap this for another time. The inner error already contains sufficient context about the failure.
				return err
			}

			klog.V(2).InfoS("Successfully deleted validating admission policy", "policyName", policy.Name)
		}
	}

	// Delete policy bindings that are created by the manager but no longer needed.
	for i := range existingPolicyBindingList.Items {
		policyBinding := &existingPolicyBindingList.Items[i]
		if !createdOrUpdatedPolicyBindingNames.Has(policyBinding.Name) {
			err := retry.OnError(policyRWOpBackoff, buildRetryUnlessCtxErr(ctx), func() error {
				if err := m.Client.Delete(ctx, policyBinding); err != nil && !apierrors.IsNotFound(err) {
					return errors.NewAPIServerError(err,
						"failed to delete validating admission policy binding",
						false,
						"policyBindingName", policyBinding.Name)
				}
				return nil
			})

			if err != nil {
				// No need to wrap this for another time. The inner error already contains sufficient context about the failure.
				return err
			}

			klog.V(2).InfoS("Successfully deleted validating admission policy binding", "policyBindingName", policyBinding.Name)
		}
	}

	return nil
}
