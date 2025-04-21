package clusterresourceplacement

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	fleetv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/v1alpha1"
)

func TestIsClusterEligible(t *testing.T) {
	tests := map[string]struct {
		mc   *fleetv1alpha1.MemberCluster
		want bool
	}{
		"cluster is not eligible if the work agent has a false join condition": {
			mc: &fleetv1alpha1.MemberCluster{
				Status: fleetv1alpha1.MemberClusterStatus{
					AgentStatus: []fleetv1alpha1.AgentStatus{
						{
							Type: fleetv1alpha1.MemberAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(fleetv1alpha1.ConditionTypeMemberClusterJoined),
									Status: metav1.ConditionFalse,
								},
							},
						},
					},
				},
			},
			want: false,
		},
		"cluster is not eligible if the work agent has not reported status": {
			mc: &fleetv1alpha1.MemberCluster{
				Status: fleetv1alpha1.MemberClusterStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(fleetv1alpha1.ConditionTypeMemberClusterJoined),
							Status: metav1.ConditionUnknown,
						},
					},
					AgentStatus: []fleetv1alpha1.AgentStatus{
						{
							Type: fleetv1alpha1.MultiClusterServiceAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(fleetv1alpha1.ConditionTypeMemberClusterJoined),
									Status: metav1.ConditionTrue,
								},
							},
						},
					},
				},
			},
			want: false,
		},
		"cluster is not eligible if the work agent has no join condition": {
			mc: &fleetv1alpha1.MemberCluster{
				Status: fleetv1alpha1.MemberClusterStatus{
					AgentStatus: []fleetv1alpha1.AgentStatus{
						{
							Type: fleetv1alpha1.MemberAgent,
							Conditions: []metav1.Condition{
								{
									Type:   "other condition type",
									Status: metav1.ConditionTrue,
								},
							},
						},
					},
				},
			},
			want: false,
		},
		"cluster is eligible if the work agent has a true join condition": {
			mc: &fleetv1alpha1.MemberCluster{
				Status: fleetv1alpha1.MemberClusterStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(fleetv1alpha1.ConditionTypeMemberClusterJoined),
							Status: metav1.ConditionUnknown,
						},
					},
					AgentStatus: []fleetv1alpha1.AgentStatus{
						{
							Type: fleetv1alpha1.MemberAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(fleetv1alpha1.ConditionTypeMemberClusterJoined),
									Status: metav1.ConditionTrue,
								},
							},
						},
					},
				},
			},
			want: true,
		},
		"cluster is eligible if the work agent has an unknown join condition": {
			mc: &fleetv1alpha1.MemberCluster{
				Status: fleetv1alpha1.MemberClusterStatus{
					AgentStatus: []fleetv1alpha1.AgentStatus{
						{
							Type: fleetv1alpha1.MemberAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(fleetv1alpha1.ConditionTypeMemberClusterJoined),
									Status: metav1.ConditionUnknown,
								},
							},
						},
					},
				},
			},
			want: true,
		},
		"cluster is eligible even if the overall joined condition is false as long as work agent is joined": {
			mc: &fleetv1alpha1.MemberCluster{
				Status: fleetv1alpha1.MemberClusterStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(fleetv1alpha1.ConditionTypeMemberClusterJoined),
							Status: metav1.ConditionFalse,
						},
					},
					AgentStatus: []fleetv1alpha1.AgentStatus{
						{
							Type: fleetv1alpha1.MemberAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(fleetv1alpha1.ConditionTypeMemberClusterJoined),
									Status: metav1.ConditionTrue,
								},
							},
						},
						{
							Type: fleetv1alpha1.MultiClusterServiceAgent,
							Conditions: []metav1.Condition{
								{
									Type:   string(fleetv1alpha1.ConditionTypeMemberClusterJoined),
									Status: metav1.ConditionFalse,
								},
							},
						},
					},
				},
			},
			want: true,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equalf(t, tt.want, isClusterEligible(tt.mc), "isClusterEligible(%v)", tt.mc.Status.AgentStatus)
		})
	}
}
