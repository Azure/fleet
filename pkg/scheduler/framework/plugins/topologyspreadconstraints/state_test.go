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

package topologyspreadconstraints

import "testing"

const (
	domainName1   = "test-domain"
	bindingCount1 = 1
	domainName2   = "alt-test-domain"
	bindingCount2 = 2
)

// TestBindingCounterByDomain tests if the bindingCounterByDomain struct correctly implements the
// bindingCounter interface.
func TestBindingCounterByDomain(t *testing.T) {
	testCases := []struct {
		name               string
		counter            bindingCounterByDomain
		initialized        bool
		wantCounts         map[domainName]count
		wantSmallest       count
		wantSecondSmallest count
		wantLargest        count
	}{
		{
			name:    "uninitialized",
			counter: bindingCounterByDomain{},
		},
		{
			name: "normal counter",
			counter: bindingCounterByDomain{
				counter: map[domainName]count{
					domainName1: bindingCount1,
					domainName2: bindingCount2,
				},
				smallest:       bindingCount1,
				secondSmallest: bindingCount2,
				largest:        bindingCount2,
			},
			initialized: true,
			wantCounts: map[domainName]count{
				domainName1: bindingCount1,
				domainName2: bindingCount2,
			},
			wantSmallest:       bindingCount1,
			wantSecondSmallest: bindingCount2,
			wantLargest:        bindingCount2,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.initialized == false {
				val, ok := tc.counter.Count(domainName1)
				if ok {
					t.Errorf("Count() = %v, true, want false", val)
				}

				val, ok = tc.counter.Smallest()
				if ok {
					t.Errorf("Smallest() = %v, true, want false", val)
				}

				val, ok = tc.counter.SecondSmallest()
				if ok {
					t.Errorf("SecondSmallest() = %v, true, want false", val)
				}

				val, ok = tc.counter.Largest()
				if ok {
					t.Errorf("Largest() = %v, true, want false", val)
				}

				return
			}

			for name, wantCount := range tc.wantCounts {
				count, ok := tc.counter.Count(name)
				if !ok || count != wantCount {
					t.Errorf("Count() = %v, %t, want %v, true", count, ok, wantCount)
				}
			}

			smallest, ok := tc.counter.Smallest()
			if !ok || smallest != tc.wantSmallest {
				t.Errorf("Smallest() = %v, %t, want %v, true", smallest, ok, tc.wantSmallest)
			}

			secondSmallest, ok := tc.counter.SecondSmallest()
			if !ok || secondSmallest != tc.wantSecondSmallest {
				t.Errorf("SecondSmallest() = %v, %t, want %v, true", secondSmallest, ok, tc.wantSecondSmallest)
			}

			largest, ok := tc.counter.Largest()
			if !ok || largest != tc.wantLargest {
				t.Errorf("Largest() = %v, %t, want %v, true", largest, ok, tc.wantLargest)
			}
		})
	}
}
