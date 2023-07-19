package clusteraffinity

import (
	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

type pluginState struct {
	// requiredAffinityTerms is a list of processed version of required cluster affinity terms.
	requiredAffinityTerms []AffinityTerm

	// preferredAffinityTerms is a list of processed version of preferred cluster affinity terms.
	preferredAffinityTerms *PreferredAffinityTerms
}

// preparePluginState initializes the state for the plugin to use in the scheduling cycle.
// TODO will call this func in the PreFilter stage.
func preparePluginState(policy *fleetv1beta1.ClusterSchedulingPolicySnapshot) (*pluginState, error) {
	if policy.Spec.Policy == nil ||
		policy.Spec.Policy.Affinity == nil ||
		policy.Spec.Policy.Affinity.ClusterAffinity == nil {
		return &pluginState{}, nil
	} // added for the defensive programming as the caller has already checked.

	clusterAffinity := policy.Spec.Policy.Affinity.ClusterAffinity
	var requiredTerms []AffinityTerm
	var err error
	if clusterAffinity.RequiredDuringSchedulingIgnoredDuringExecution != nil &&
		len(clusterAffinity.RequiredDuringSchedulingIgnoredDuringExecution.ClusterSelectorTerms) > 0 {
		requiredTerms, err = NewAffinityTerms(clusterAffinity.RequiredDuringSchedulingIgnoredDuringExecution.ClusterSelectorTerms)
		if err != nil {
			return nil, err
		}
	}
	var preferredTerms *PreferredAffinityTerms
	if len(clusterAffinity.PreferredDuringSchedulingIgnoredDuringExecution) > 0 {
		preferredTerms, err = NewPreferredAffinityTerms(clusterAffinity.PreferredDuringSchedulingIgnoredDuringExecution)
		if err != nil {
			return nil, err
		}
	}
	return &pluginState{requiredAffinityTerms: requiredTerms, preferredAffinityTerms: preferredTerms}, nil
}
