package validator

import (
	"strings"
	"testing"

	clusterv1beta1 "go.goms.io/fleet/apis/cluster/v1beta1"
)

func TestValidateTaints(t *testing.T) {
	tests := map[string]struct {
		taints     []clusterv1beta1.Taint
		wantErr    bool
		wantErrMsg string
	}{
		"invalid taint, key is invalid": {
			taints: []clusterv1beta1.Taint{
				{
					Key:    "key@123:",
					Effect: "NoSchedule",
				},
			},
			wantErr:    true,
			wantErrMsg: "name part must consist of alphanumeric characters, '-', '_' or '.', and must start and end with an alphanumeric character",
		},
		"invalid taint, empty key": {
			taints: []clusterv1beta1.Taint{
				{
					Key:    "",
					Value:  "value1",
					Effect: "NoSchedule",
				},
			},
			wantErr:    true,
			wantErrMsg: "name part must be non-empty",
		},
		"invalid taint, value is invalid": {
			taints: []clusterv1beta1.Taint{
				{
					Key:    "key1",
					Value:  "val&123:98_",
					Effect: "NoSchedule",
				},
			},
			wantErr:    true,
			wantErrMsg: "a valid label must be an empty string or consist of alphanumeric characters, '-', '_' or '.', and must start and end with an alphanumeric character",
		},
		"invalid taint, non-unique taint": {
			taints: []clusterv1beta1.Taint{
				{
					Key:    "key1",
					Value:  "value1",
					Effect: "NoSchedule",
				},
				{
					Key:    "key2",
					Value:  "value2",
					Effect: "NoSchedule",
				},
				{
					Key:    "key1",
					Value:  "value1",
					Effect: "NoSchedule",
				},
			},
			wantErr:    true,
			wantErrMsg: "taints must be unique",
		},
		"valid taints": {
			taints: []clusterv1beta1.Taint{
				{
					Key:    "key1",
					Value:  "value1",
					Effect: "NoSchedule",
				},
				{
					Key:    "key2",
					Value:  "value2",
					Effect: "NoSchedule",
				},
				{
					Key:    "key3",
					Effect: "NoSchedule",
				},
			},
			wantErr: false,
		},
	}
	for testName, testCase := range tests {
		t.Run(testName, func(t *testing.T) {
			gotErr := validateTaints(testCase.taints)
			if (gotErr != nil) != testCase.wantErr {
				t.Errorf("validateTaints() error = %v, wantErr %v", gotErr, testCase.wantErr)
			}
			if testCase.wantErr && !strings.Contains(gotErr.Error(), testCase.wantErrMsg) {
				t.Errorf("validateTaints() got %v, should contain want %s", gotErr, testCase.wantErrMsg)
			}
		})
	}
}
