/*
Copyright 2025 The KubeFleet Authors.

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

package time

import (
	"testing"
	"time"
)

func TestMaxTime(t *testing.T) {
	t1 := time.Now()
	t2 := t1.Add(-1 * time.Hour)
	zero := time.Time{}
	tests := []struct {
		name string
		a, b time.Time
		want time.Time
	}{
		{
			name: "a after b",
			a:    t1,
			b:    t2,
			want: t1,
		},
		{
			name: "b after a",
			a:    t2,
			b:    t1,
			want: t1,
		},
		{
			name: "equal times",
			a:    t1,
			b:    t1,
			want: t1,
		},
		{
			name: "zero and non-zero",
			a:    zero,
			b:    t1,
			want: t1,
		},
		{
			name: "non-zero and zero",
			a:    t2,
			b:    zero,
			want: t2,
		},
		{
			name: "both zero",
			a:    zero,
			b:    zero,
			want: zero,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := MaxTime(tc.a, tc.b)
			if !got.Equal(tc.want) {
				t.Errorf("MaxTime(%v, %v) = %v; want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}
