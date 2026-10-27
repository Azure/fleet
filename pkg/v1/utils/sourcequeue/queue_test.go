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

package sourcequeue

import (
	"context"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"k8s.io/client-go/util/workqueue"
)

// TestQueue pins that requests added before Start are held and collapsed, and that requests added
// after it go straight to the controller's queue, collapsed by the queue itself.
func TestQueue(t *testing.T) {
	var q Queue[string]
	q.Add("a")
	q.Add("b")
	q.Add("a")

	wq := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[string]())
	defer wq.ShutDown()
	if err := q.Start(context.Background(), wq); err != nil {
		t.Fatalf("Start() = %v, want no error", err)
	}
	q.Add("c")
	q.Add("c")

	var got []string
	for wq.Len() > 0 {
		item, _ := wq.Get()
		got = append(got, item)
		wq.Done(item)
	}
	slices.Sort(got)
	if diff := cmp.Diff([]string{"a", "b", "c"}, got); diff != "" {
		t.Errorf("Queue handed the controller (-want +got):\n%s", diff)
	}
}
