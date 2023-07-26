/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package membercluster

import (
	"log"
	"os"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/kubernetes/scheme"

	fleetv1beta1 "go.goms.io/fleet/apis/placement/v1beta1"
)

const (
	eventuallyDuration   = time.Second * 5
	eventuallyInterval   = time.Millisecond * 250
	consistentlyDuration = time.Second
	consistentlyInterval = time.Millisecond * 200
)

func TestMain(m *testing.M) {
	// Add custom APIs to the runtime scheme.
	if err := fleetv1beta1.AddToScheme(scheme.Scheme); err != nil {
		log.Fatalf("failed to add custom APIs to the runtime scheme: %v", err)
	}

	os.Exit(m.Run())
}

var _ = Describe("scheduler member cluster source controller", Serial, Ordered, func() {
	BeforeAll(func() {
		Eventually(func() int {
			return keyCollector.Len()
		}, eventuallyDuration, eventuallyInterval).Should(Equal(2))

		Consistently(func() int {
			return keyCollector.Len()
		}, consistentlyDuration, consistentlyInterval).Should(Equal(2))

		keyCollector.Reset()
	})

	Context("starting up", func() {
		It("should emit no keys when starting up", func() {
			Consistently(func() int {
				return keyCollector.Len()
			}, consistentlyDuration, consistentlyInterval).Should(Equal(0))
		})
	})
})
