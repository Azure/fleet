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
	"fmt"
	"slices"
	"strings"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	placementv1alpha1 "github.com/kubefleet-dev/kubefleet/apis/kubefleet.dev/placement/v1alpha1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils"
	kferrors "github.com/kubefleet-dev/kubefleet/pkg/utils/errors"
	"github.com/kubefleet-dev/kubefleet/pkg/v1/managers/placementresourcesnapshot/uniquename"
	"github.com/kubefleet-dev/kubefleet/pkg/v1/utils/fieldindexers"
)

const (
	quorumReadNewerSnapshotErrMsg  = "a newer snapshot already exists (found via quorum reads)"
	quorumReadPrimaryExistsErrMsg  = "a primary placement resource snapshot already exists at the given index (found via quorum read)"
	orphansCleanedUpErrMsg         = "cleaned up orphaned secondary placement resource snapshots"
	primaryCreationFailedErrMsg    = "failed to create the primary placement resource snapshot"
	secondaryCreationFailedErrMsg  = "failed to create a secondary placement resource snapshot"
	fewerSubIndexedSnapshotsErrMsg = "there are fewer sub-indexed placement resource snapshots than the count label indicates"
	mismatchedContentsHashErrMsg   = "a sub-indexed placement resource snapshot has a different contents hash than the primary placement resource snapshot"
	staleOwnerSnapshotErrMsg       = "found a placement resource snapshot owned by a different placement policy of the same name"
	leftoverSnapshotsErrMsg        = "leftover resource snapshots are detected"
	unexpectedOwnerRefsErrMsg      = "that has unexpected owner references"

	// The expected limits on generated snapshot names and owned-by label values; they are spelled out here (rather
	// than reusing the constants of the package) so that the tests catch unintended changes.
	wantMaxSnapshotNameLen         = 251
	wantOwnedByLabelValLen         = 61
	wantOwnedByLabelValPrefixLen   = 48
	wantHashSuffixPattern          = `^[0-9a-f]{12}$`
	wantHashedPrimaryNamePattern   = `-resource-snapshot-0-[0-9a-f]{12}$`
	wantHashedSecondaryNamePattern = `-resource-snapshot-0-1-[0-9a-f]{12}$`
	// The snapshot names minus the hash suffix, should the placement policy name need no sanitization.
	wantUnsanitizedPrimaryNamePrefixFmt   = "%s-resource-snapshot-%d-"
	wantUnsanitizedSecondaryNamePrefixFmt = "%s-resource-snapshot-%d-%d-"
	concurrentSnapshotCallerCnt           = 8
	malformedPrimarySnapshotSuffix        = "-malformed"
	placementPolicyNameFillerLetter       = "a"
)

// decoySnapshotSpec describes a hand-crafted placement resource snapshot (a decoy).
type decoySnapshotSpec struct {
	name        string
	labels      map[string]string
	annotations map[string]string
}

var _ = Describe("corner cases (stale cache and inconsistent states)", func() {
	// The cache has yet to observe a primary placement resource snapshot at the next index; the scenario is
	// run for both placement policies and cluster placement policies.
	for _, clusterScoped := range []bool{false, true} {
		desc := "newer primary snapshot visible only to quorum reads"
		if clusterScoped {
			desc = "newer cluster primary snapshot visible only to quorum reads"
		}

		Context(desc, Ordered, func() {
			configMapName := fmt.Sprintf(configMapNameTemplate, utils.RandStr())

			var placementPolicy placementv1alpha1.PlacementPolicyAccessor
			if clusterScoped {
				selector := configMapNameSelector(configMapName)
				selector.Namespace = appNamespaceName
				placementPolicy = newClusterPlacementPolicy(selector)
			} else {
				placementPolicy = newPlacementPolicy(appNamespaceName, configMapNameSelector(configMapName))
			}
			snapshotNS := placementPolicy.GetNamespace()
			// The snapshot names depend on the UID of the placement policy; they are set once the policy is created.
			var primarySnapshotName0, primarySnapshotName1 string

			var hash string

			BeforeAll(func() {
				configMap := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: appNamespaceName},
					Data:       map[string]string{"key": "value"},
				}
				Expect(hubClient.Create(ctx, configMap)).To(Succeed(), "Failed to create the config map")
				Expect(hubClient.Create(ctx, placementPolicy)).To(Succeed(), "Failed to create the placement policy")
				primarySnapshotName0 = uniquename.PrimaryPlacementResourceSnapshotName(placementPolicy.GetName(), placementPolicy.GetUID(), 0)
				primarySnapshotName1 = uniquename.PrimaryPlacementResourceSnapshotName(placementPolicy.GetName(), placementPolicy.GetUID(), 1)
			})

			It("can create a placement resource snapshot", func() {
				gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
				verifyReturnedPrimarySnapshot(gotSnapshots, isUpToDate, err, placementPolicy, 0)
				hash = snapshotContentsHash(getPrimarySnapshotFromCache(placementPolicy, 0))
			})

			It("can create an unlabeled decoy at the primary snapshot name of the next index", func() {
				createDecoySnapshot(snapshotNS, primarySnapshotName1, nil, nil, nil)
			})

			It("should return a transient error when checking for staleness", func() {
				gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfStale(ctx, placementPolicy)
				verifySnapshotCallFailed(gotSnapshots, isUpToDate, err, kferrors.ErrCategoryTransient, quorumReadNewerSnapshotErrMsg)
			})

			It("should return a transient error even if only the existing snapshot is requested", func() {
				// This is expected; upon retry the manager finds the newer snapshot once the cache catches up.
				gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
				verifySnapshotCallFailed(gotSnapshots, isUpToDate, err, kferrors.ErrCategoryTransient, quorumReadNewerSnapshotErrMsg)
			})

			It("can simulate the cache catching up by turning the decoy into an indexed primary snapshot", func() {
				decoy := verifySnapshotExists(snapshotNS, primarySnapshotName1)
				decoy.SetLabels(decoyPrimaryLabels(placementPolicy, "1", "1"))
				// A snapshot that the manager creates is always owned by the placement policy.
				decoy.SetOwnerReferences(decoyOwnerRefs(placementPolicy))
				annotations := decoyOwnedByAnnotations(placementPolicy)
				// The selected resources have not changed; a real snapshot at the next index would carry the same hash.
				annotations[placementv1alpha1.PlacementResourceSnapshotContentsHashAnnotationKey] = hash
				decoy.SetAnnotations(annotations)
				Expect(hubClient.Update(ctx, decoy)).To(Succeed(), "Failed to update the decoy placement resource snapshot")
			})

			It("should return the newer snapshot on retry", func() {
				// Until the cache catches up, the manager keeps returning the transient error.
				Eventually(func() error {
					gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfStale(ctx, placementPolicy)
					if err != nil {
						return err
					}
					if diff := cmp.Diff(snapshotNamesOf(gotSnapshots), []string{primarySnapshotName1}); diff != "" || !isUpToDate {
						return fmt.Errorf("unexpected snapshots (up-to-date %t), names diff (-got +want):\n%s", isUpToDate, diff)
					}
					return nil
				}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to retrieve the newer placement resource snapshot")

				gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
				verifyReturnedSnapshots(gotSnapshots, isUpToDate, err, []string{primarySnapshotName1})
				verifyNoPrimarySnapshotAtIdx(placementPolicy, 2)
			})

			AfterAll(func() {
				deleteObjectsAndWait([]client.Object{
					placementResourceSnapshotObj(snapshotNS, primarySnapshotName0),
					placementResourceSnapshotObj(snapshotNS, primarySnapshotName1),
					placementPolicy,
					&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: appNamespaceName}},
				})
			})
		})
	}

	Context("primary snapshot name taken", Ordered, func() {
		configMapName := fmt.Sprintf(configMapNameTemplate, utils.RandStr())
		placementPolicy := newPlacementPolicy(appNamespaceName, configMapNameSelector(configMapName))
		// The snapshot name depends on the UID of the placement policy; it is set once the policy is created.
		var primarySnapshotName string

		var decoyUID types.UID

		BeforeAll(func() {
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: appNamespaceName},
				Data:       map[string]string{"key": "value"},
			}
			Expect(hubClient.Create(ctx, configMap)).To(Succeed(), "Failed to create the config map")
			Expect(hubClient.Create(ctx, placementPolicy)).To(Succeed(), "Failed to create the placement policy")
			primarySnapshotName = uniquename.PrimaryPlacementResourceSnapshotName(placementPolicy.Name, placementPolicy.UID, 0)

			// This mimics the situation where the manager has created the primary snapshot, yet the cache has
			// not observed it.
			decoyUID = createDecoySnapshot(appNamespaceName, primarySnapshotName, nil, nil, nil).GetUID()
		})

		It("should return an API server error when the primary snapshot name is taken", func() {
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
			verifySnapshotCallFailed(gotSnapshots, isUpToDate, err, kferrors.ErrCategoryAPIServer, primaryCreationFailedErrMsg)
			Expect(errors.IsAlreadyExists(err)).To(BeTrue(), "The error does not wrap an already exists error: %v", err)
		})

		It("should not modify the existing object", func() {
			decoy := verifySnapshotExists(appNamespaceName, primarySnapshotName)
			Expect(decoy.GetUID()).To(Equal(decoyUID), "The decoy placement resource snapshot has been re-created")
			Expect(decoy.GetLabels()).To(BeEmpty(), "The decoy placement resource snapshot has been modified")
		})

		It("can remove the decoy", func() {
			deleteObjectsAndWait([]client.Object{placementResourceSnapshotObj(appNamespaceName, primarySnapshotName)})
			waitForSnapshotGoneFromCache(appNamespaceName, primarySnapshotName)
		})

		It("can create the placement resource snapshot once the name is free", func() {
			_, wantHash, err := resourceSnapshotManager.retrieveAndHashSelectedResources(ctx, placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to retrieve and hash the selected resources")

			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
			verifyReturnedPrimarySnapshot(gotSnapshots, isUpToDate, err, placementPolicy, 0)
			verifySnapshotMetadata(getPrimarySnapshotFromCache(placementPolicy, 0), placementPolicy, 0, 0, 1, wantHash)
		})

		AfterAll(func() {
			deleteObjectsAndWait([]client.Object{
				placementResourceSnapshotObj(appNamespaceName, primarySnapshotName),
				placementPolicy,
				&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: appNamespaceName}},
			})
		})
	})

	Context("secondary snapshot name taken (partial provisioning)", Ordered, func() {
		groupLabels := map[string]string{placementGroupLabelKey: utils.RandStr()}
		configMapNamePrefix := fmt.Sprintf(configMapNameTemplate, utils.RandStr())
		var configMapNames []string

		// Seven resources, with at most three resources per snapshot: one primary and two secondary snapshots.
		placementPolicy := newPlacementPolicy(appNamespaceName, configMapLabelSelector(groupLabels))
		// The snapshot names depend on the UID of the placement policy; they are set once the policy is created.
		var wantSnapshotNames []string

		var wantHash string

		BeforeAll(func() {
			configMapNames = createLabeledConfigMaps(configMapNamePrefix, 7, groupLabels)
			Expect(hubClient.Create(ctx, placementPolicy)).To(Succeed(), "Failed to create the placement policy")
			wantSnapshotNames = []string{
				uniquename.PrimaryPlacementResourceSnapshotName(placementPolicy.Name, placementPolicy.UID, 0),
				uniquename.SecondaryPlacementResourceSnapshotName(placementPolicy.Name, placementPolicy.UID, 0, 1),
				uniquename.SecondaryPlacementResourceSnapshotName(placementPolicy.Name, placementPolicy.UID, 0, 2),
			}
			createDecoySnapshot(appNamespaceName, wantSnapshotNames[1], nil, nil, nil)
		})

		It("can compute the hash of selected resources", func() {
			_, hash, err := resourceSnapshotManager.retrieveAndHashSelectedResources(ctx, placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to retrieve and hash the selected resources")
			wantHash = hash
		})

		It("should return an API server error when a secondary snapshot name is taken", func() {
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
			verifySnapshotCallFailed(gotSnapshots, isUpToDate, err, kferrors.ErrCategoryAPIServer, secondaryCreationFailedErrMsg)
			Expect(errors.IsAlreadyExists(err)).To(BeTrue(), "The error does not wrap an already exists error: %v", err)
		})

		It("should have left a partially provisioned snapshot group behind", func() {
			// The secondary snapshots are created from the last sub-index down; the creation stops at the decoy.
			verifySnapshotExists(appNamespaceName, wantSnapshotNames[2])
			verifySnapshotNotFound(appNamespaceName, wantSnapshotNames[0])
			decoy := verifySnapshotExists(appNamespaceName, wantSnapshotNames[1])
			Expect(decoy.GetLabels()).To(BeEmpty(), "The decoy placement resource snapshot has been modified")
		})

		It("can remove the decoy", func() {
			deleteObjectsAndWait([]client.Object{placementResourceSnapshotObj(appNamespaceName, wantSnapshotNames[1])})
			waitForSnapshotGoneFromCache(appNamespaceName, wantSnapshotNames[1])
			// Wait until the cache has observed the orphaned secondary snapshot.
			getSnapshotFromCache(appNamespaceName, wantSnapshotNames[2])
		})

		It("should clean up the orphaned secondary snapshot and return a transient error", func() {
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
			verifySnapshotCallFailed(gotSnapshots, isUpToDate, err, kferrors.ErrCategoryTransient, orphansCleanedUpErrMsg)
			verifySnapshotNotFound(appNamespaceName, wantSnapshotNames[2])
		})

		It("can create all the snapshots once the orphan is gone", func() {
			waitForSnapshotGoneFromCache(appNamespaceName, wantSnapshotNames[2])

			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
			verifyReturnedSnapshots(gotSnapshots, isUpToDate, err, wantSnapshotNames)
			for subIdx, name := range wantSnapshotNames {
				verifySnapshotMetadata(getSnapshotFromCache(appNamespaceName, name), placementPolicy, 0, subIdx, len(wantSnapshotNames), wantHash)
			}
		})

		AfterAll(func() {
			objs := make([]client.Object, 0, len(wantSnapshotNames)+1+len(configMapNames))
			for _, name := range wantSnapshotNames {
				objs = append(objs, placementResourceSnapshotObj(appNamespaceName, name))
			}
			objs = append(objs, placementPolicy)
			objs = append(objs, configMapObjs(configMapNames)...)
			deleteObjectsAndWait(objs)
		})
	})

	Context("orphan clean-up deferred by a quorum read", Ordered, func() {
		configMapName := fmt.Sprintf(configMapNameTemplate, utils.RandStr())
		placementPolicy := newPlacementPolicy(appNamespaceName, configMapNameSelector(configMapName))
		// The snapshot names depend on the UID of the placement policy; they are set once the policy is created.
		var primarySnapshotName, secondarySnapshotName string

		BeforeAll(func() {
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: appNamespaceName},
				Data:       map[string]string{"key": "value"},
			}
			Expect(hubClient.Create(ctx, configMap)).To(Succeed(), "Failed to create the config map")
			Expect(hubClient.Create(ctx, placementPolicy)).To(Succeed(), "Failed to create the placement policy")
			primarySnapshotName = uniquename.PrimaryPlacementResourceSnapshotName(placementPolicy.Name, placementPolicy.UID, 0)
			secondarySnapshotName = uniquename.SecondaryPlacementResourceSnapshotName(placementPolicy.Name, placementPolicy.UID, 0, 1)

			// An indexed secondary snapshot (the orphan candidate), and an unindexed primary snapshot at the
			// same index; the latter mimics a primary snapshot that the cache has yet to observe.
			createDecoySnapshot(appNamespaceName, secondarySnapshotName,
				decoySecondaryLabels(placementPolicy, "0", "1"), decoyOwnedByAnnotations(placementPolicy), nil)
			createDecoySnapshot(appNamespaceName, primarySnapshotName, nil, nil, nil)
			getSnapshotFromCache(appNamespaceName, secondarySnapshotName)
		})

		It("should return a transient error without deleting the secondary snapshot", func() {
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
			verifySnapshotCallFailed(gotSnapshots, isUpToDate, err, kferrors.ErrCategoryTransient, quorumReadPrimaryExistsErrMsg)

			secondarySnapshot := verifySnapshotExists(appNamespaceName, secondarySnapshotName)
			Expect(secondarySnapshot.GetDeletionTimestamp()).To(BeNil(), "The secondary placement resource snapshot has been deleted")
		})

		It("can remove the primary decoy", func() {
			deleteObjectsAndWait([]client.Object{placementResourceSnapshotObj(appNamespaceName, primarySnapshotName)})
			waitForSnapshotGoneFromCache(appNamespaceName, primarySnapshotName)
		})

		It("should clean up the orphaned secondary snapshot now", func() {
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
			verifySnapshotCallFailed(gotSnapshots, isUpToDate, err, kferrors.ErrCategoryTransient, orphansCleanedUpErrMsg)
			verifySnapshotNotFound(appNamespaceName, secondarySnapshotName)
		})

		It("can create the placement resource snapshot", func() {
			waitForSnapshotGoneFromCache(appNamespaceName, secondarySnapshotName)

			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
			verifyReturnedSnapshots(gotSnapshots, isUpToDate, err, []string{primarySnapshotName})
		})

		AfterAll(func() {
			deleteObjectsAndWait([]client.Object{
				placementResourceSnapshotObj(appNamespaceName, primarySnapshotName),
				placementResourceSnapshotObj(appNamespaceName, secondarySnapshotName),
				placementPolicy,
				&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: appNamespaceName}},
			})
		})
	})

	Context("orphaned secondary snapshot pending deletion", Ordered, func() {
		configMapName := fmt.Sprintf(configMapNameTemplate, utils.RandStr())
		placementPolicy := newPlacementPolicy(appNamespaceName, configMapNameSelector(configMapName))
		// The snapshot names depend on the UID of the placement policy; they are set once the policy is created.
		var primarySnapshotName, secondarySnapshotName string

		BeforeAll(func() {
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: appNamespaceName},
				Data:       map[string]string{"key": "value"},
			}
			Expect(hubClient.Create(ctx, configMap)).To(Succeed(), "Failed to create the config map")
			Expect(hubClient.Create(ctx, placementPolicy)).To(Succeed(), "Failed to create the placement policy")
			primarySnapshotName = uniquename.PrimaryPlacementResourceSnapshotName(placementPolicy.Name, placementPolicy.UID, 0)
			secondarySnapshotName = uniquename.SecondaryPlacementResourceSnapshotName(placementPolicy.Name, placementPolicy.UID, 0, 1)

			// An orphaned secondary snapshot that cannot be deleted right away due to a finalizer.
			createDecoySnapshot(appNamespaceName, secondarySnapshotName,
				decoySecondaryLabels(placementPolicy, "0", "1"), decoyOwnedByAnnotations(placementPolicy), []string{testFinalizer})
			getSnapshotFromCache(appNamespaceName, secondarySnapshotName)
		})

		It("should delete the orphaned secondary snapshot and return a transient error", func() {
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
			verifySnapshotCallFailed(gotSnapshots, isUpToDate, err, kferrors.ErrCategoryTransient, orphansCleanedUpErrMsg)

			secondarySnapshot := verifySnapshotExists(appNamespaceName, secondarySnapshotName)
			Expect(secondarySnapshot.GetDeletionTimestamp()).ToNot(BeNil(), "The secondary placement resource snapshot has not been marked for deletion")
		})

		It("should keep returning a transient error while the orphan is being deleted", func() {
			Eventually(func() error {
				secondarySnapshot := placementResourceSnapshotObj(appNamespaceName, secondarySnapshotName)
				if err := hubClient.Get(ctx, client.ObjectKeyFromObject(secondarySnapshot), secondarySnapshot); err != nil {
					return err
				}
				if secondarySnapshot.GetDeletionTimestamp() == nil {
					return fmt.Errorf("the cache has not observed the deletion timestamp yet")
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to observe the deletion timestamp in the cache")

			Consistently(func() error {
				gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
				return checkSnapshotCallFailed(gotSnapshots, isUpToDate, err, kferrors.ErrCategoryTransient, orphansCleanedUpErrMsg)
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The manager did not keep returning the transient error")
			verifySnapshotNotFound(appNamespaceName, primarySnapshotName)
		})

		It("can remove the finalizer", func() {
			removeFinalizersAndDelete(placementResourceSnapshotObj(appNamespaceName, secondarySnapshotName))
			waitForSnapshotGoneFromCache(appNamespaceName, secondarySnapshotName)
		})

		It("can create the placement resource snapshot", func() {
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
			verifyReturnedSnapshots(gotSnapshots, isUpToDate, err, []string{primarySnapshotName})
		})

		AfterAll(func() {
			// The decoy is normally gone by now; remove the finalizer anyway in case a spec failed midway.
			removeFinalizersAndDelete(placementResourceSnapshotObj(appNamespaceName, secondarySnapshotName))
			deleteObjectsAndWait([]client.Object{
				placementResourceSnapshotObj(appNamespaceName, primarySnapshotName),
				placementPolicy,
				&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: appNamespaceName}},
			})
		})
	})

	Context("more sub-indexed snapshots than the count label indicates", Ordered, func() {
		groupLabels := map[string]string{placementGroupLabelKey: utils.RandStr()}
		configMapNamePrefix := fmt.Sprintf(configMapNameTemplate, utils.RandStr())
		var configMapNames []string

		// Four resources, with at most three resources per snapshot: one primary and one secondary snapshot.
		placementPolicy := newPlacementPolicy(appNamespaceName, configMapLabelSelector(groupLabels))

		// A placement policy that selects a single resource; its snapshot features a count label of 1.
		singleGroupPlacementPolicy := newPlacementPolicy(appNamespaceName, configMapNameSelector(configMapNamePrefix+"-0"))

		// The snapshot names depend on the UIDs of the placement policies; they are set once the policies are created.
		var wantSnapshotNames []string
		var extraSecondarySnapshotName string
		var singleGroupPrimarySnapshotName, singleGroupExtraSecondarySnapshotName string

		BeforeAll(func() {
			configMapNames = createLabeledConfigMaps(configMapNamePrefix, 4, groupLabels)
			Expect(hubClient.Create(ctx, placementPolicy)).To(Succeed(), "Failed to create the placement policy")
			Expect(hubClient.Create(ctx, singleGroupPlacementPolicy)).To(Succeed(), "Failed to create the placement policy")

			wantSnapshotNames = []string{
				uniquename.PrimaryPlacementResourceSnapshotName(placementPolicy.Name, placementPolicy.UID, 0),
				uniquename.SecondaryPlacementResourceSnapshotName(placementPolicy.Name, placementPolicy.UID, 0, 1),
			}
			extraSecondarySnapshotName = uniquename.SecondaryPlacementResourceSnapshotName(placementPolicy.Name, placementPolicy.UID, 0, 2)
			singleGroupPrimarySnapshotName = uniquename.PrimaryPlacementResourceSnapshotName(singleGroupPlacementPolicy.Name, singleGroupPlacementPolicy.UID, 0)
			singleGroupExtraSecondarySnapshotName = uniquename.SecondaryPlacementResourceSnapshotName(singleGroupPlacementPolicy.Name, singleGroupPlacementPolicy.UID, 0, 1)
		})

		It("can create a primary and a secondary snapshot", func() {
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
			verifyReturnedSnapshots(gotSnapshots, isUpToDate, err, wantSnapshotNames)
			for _, name := range wantSnapshotNames {
				getSnapshotFromCache(appNamespaceName, name)
			}
		})

		It("can create an extra secondary snapshot at the latest index", func() {
			createDecoySnapshot(appNamespaceName, extraSecondarySnapshotName,
				decoySecondaryLabels(placementPolicy, "0", "2"), decoyOwnedByAnnotations(placementPolicy), nil)
			getSnapshotFromCache(appNamespaceName, extraSecondarySnapshotName)
		})

		It("should return only the snapshots dictated by the count label", func() {
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
			verifyReturnedSnapshots(gotSnapshots, isUpToDate, err, wantSnapshotNames)

			gotSnapshots, isUpToDate, err = resourceSnapshotManager.SnapshotResourcesIfStale(ctx, placementPolicy)
			verifyReturnedSnapshots(gotSnapshots, isUpToDate, err, wantSnapshotNames)
			verifyNoPrimarySnapshotAtIdx(placementPolicy, 1)
		})

		It("can create a single-group snapshot and an extra secondary snapshot", func() {
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, singleGroupPlacementPolicy)
			verifyReturnedSnapshots(gotSnapshots, isUpToDate, err, []string{singleGroupPrimarySnapshotName})
			getSnapshotFromCache(appNamespaceName, singleGroupPrimarySnapshotName)

			createDecoySnapshot(appNamespaceName, singleGroupExtraSecondarySnapshotName,
				decoySecondaryLabels(singleGroupPlacementPolicy, "0", "1"), decoyOwnedByAnnotations(singleGroupPlacementPolicy), nil)
			getSnapshotFromCache(appNamespaceName, singleGroupExtraSecondarySnapshotName)
		})

		It("should ignore extra secondary snapshots when the count label is 1", func() {
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfStale(ctx, singleGroupPlacementPolicy)
			verifyReturnedSnapshots(gotSnapshots, isUpToDate, err, []string{singleGroupPrimarySnapshotName})
		})

		AfterAll(func() {
			deleteObjectsAndWait(slices.Concat(
				snapshotObjs(appNamespaceName, wantSnapshotNames[0], wantSnapshotNames[1], extraSecondarySnapshotName,
					singleGroupPrimarySnapshotName, singleGroupExtraSecondarySnapshotName),
				[]client.Object{placementPolicy, singleGroupPlacementPolicy},
				configMapObjs(configMapNames),
			))
		})
	})

	Context("fewer sub-indexed snapshots than the count label indicates", Ordered, func() {
		groupLabels := map[string]string{placementGroupLabelKey: utils.RandStr()}
		configMapNamePrefix := fmt.Sprintf(configMapNameTemplate, utils.RandStr())
		var configMapNames []string

		placementPolicy := newPlacementPolicy(appNamespaceName, configMapLabelSelector(groupLabels))
		// The snapshot names depend on the UID of the placement policy; they are set once the policy is created.
		var wantSnapshotNames []string

		var originalPrimarySnapshotUID types.UID

		BeforeAll(func() {
			configMapNames = createLabeledConfigMaps(configMapNamePrefix, 4, groupLabels)
			Expect(hubClient.Create(ctx, placementPolicy)).To(Succeed(), "Failed to create the placement policy")
			wantSnapshotNames = []string{
				uniquename.PrimaryPlacementResourceSnapshotName(placementPolicy.Name, placementPolicy.UID, 0),
				uniquename.SecondaryPlacementResourceSnapshotName(placementPolicy.Name, placementPolicy.UID, 0, 1),
			}
		})

		It("can create a primary and a secondary snapshot", func() {
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
			verifyReturnedSnapshots(gotSnapshots, isUpToDate, err, wantSnapshotNames)
			originalPrimarySnapshotUID = gotSnapshots[0].GetUID()
			for _, name := range wantSnapshotNames {
				getSnapshotFromCache(appNamespaceName, name)
			}
		})

		It("can delete the secondary snapshot", func() {
			// Note that this is for simulation purposes only; the manager never deletes a single secondary snapshot.
			deleteObjectsAndWait([]client.Object{placementResourceSnapshotObj(appNamespaceName, wantSnapshotNames[1])})
			waitForSnapshotGoneFromCache(appNamespaceName, wantSnapshotNames[1])
		})

		It("should return an unexpected error", func() {
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
			verifySnapshotCallFailed(gotSnapshots, isUpToDate, err, kferrors.ErrCategoryUnexpected, fewerSubIndexedSnapshotsErrMsg)

			gotSnapshots, isUpToDate, err = resourceSnapshotManager.SnapshotResourcesIfStale(ctx, placementPolicy)
			verifySnapshotCallFailed(gotSnapshots, isUpToDate, err, kferrors.ErrCategoryUnexpected, fewerSubIndexedSnapshotsErrMsg)
		})

		It("can update a selected config map", func() {
			updateConfigMapData(appNamespaceName, configMapNames[0], "changed")
		})

		It("should still return an unexpected error after a resource change alone", func() {
			// The latest snapshots are retrieved (and verified) before the selected resources are; a resource change
			// alone does not help the manager recover.
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfStale(ctx, placementPolicy)
			verifySnapshotCallFailed(gotSnapshots, isUpToDate, err, kferrors.ErrCategoryUnexpected, fewerSubIndexedSnapshotsErrMsg)
		})

		It("can manually clean up the broken primary snapshot", func() {
			deleteObjectsAndWait([]client.Object{placementResourceSnapshotObj(appNamespaceName, wantSnapshotNames[0])})
			waitForSnapshotGoneFromCache(appNamespaceName, wantSnapshotNames[0])
		})

		It("should create a new snapshot set after the manual clean-up", func() {
			_, wantHash, err := resourceSnapshotManager.retrieveAndHashSelectedResources(ctx, placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to retrieve and hash the selected resources")

			// No other primary snapshot exists; the snapshots are re-created at index 0.
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfStale(ctx, placementPolicy)
			verifyReturnedSnapshots(gotSnapshots, isUpToDate, err, wantSnapshotNames)
			Expect(gotSnapshots[0].GetUID()).ToNot(Equal(originalPrimarySnapshotUID), "The primary placement resource snapshot has not been re-created")
			for subIdx, name := range wantSnapshotNames {
				verifySnapshotMetadata(getSnapshotFromCache(appNamespaceName, name), placementPolicy, 0, subIdx, len(wantSnapshotNames), wantHash)
			}
		})

		AfterAll(func() {
			deleteObjectsAndWait(slices.Concat(
				snapshotObjs(appNamespaceName, wantSnapshotNames...),
				[]client.Object{placementPolicy},
				configMapObjs(configMapNames),
			))
		})
	})

	Context("malformed snapshot labels", Ordered, func() {
		configMapName := fmt.Sprintf(configMapNameTemplate, utils.RandStr())

		// The hash of the currently selected resources; every placement policy below selects the same config map.
		var currentHash string

		primaryDecoy := func(placementPolicy placementv1alpha1.PlacementPolicyAccessor, name, idx, cnt string, extraAnnotations map[string]string) decoySnapshotSpec {
			annotations := decoyOwnedByAnnotations(placementPolicy)
			for k, v := range extraAnnotations {
				annotations[k] = v
			}
			return decoySnapshotSpec{name: name, labels: decoyPrimaryLabels(placementPolicy, idx, cnt), annotations: annotations}
		}
		secondaryDecoy := func(placementPolicy placementv1alpha1.PlacementPolicyAccessor, name, idx, subIdx string, extraAnnotations map[string]string) decoySnapshotSpec {
			annotations := decoyOwnedByAnnotations(placementPolicy)
			for k, v := range extraAnnotations {
				annotations[k] = v
			}
			return decoySnapshotSpec{name: name, labels: decoySecondaryLabels(placementPolicy, idx, subIdx), annotations: annotations}
		}
		primaryName := func(placementPolicy placementv1alpha1.PlacementPolicyAccessor) string {
			return uniquename.PrimaryPlacementResourceSnapshotName(placementPolicy.GetName(), placementPolicy.GetUID(), 0)
		}
		secondaryName := func(placementPolicy placementv1alpha1.PlacementPolicyAccessor) string {
			return uniquename.SecondaryPlacementResourceSnapshotName(placementPolicy.GetName(), placementPolicy.GetUID(), 0, 1)
		}
		withHash := func(hash string) map[string]string {
			return map[string]string{placementv1alpha1.PlacementResourceSnapshotContentsHashAnnotationKey: hash}
		}

		type malformedSnapshotLabelsTestCase struct {
			desc            string
			placementPolicy *placementv1alpha1.PlacementPolicy
			// decoys returns the decoys to create; it is evaluated in BeforeAll, after the current hash is known.
			decoys            func(placementPolicy placementv1alpha1.PlacementPolicyAccessor) []decoySnapshotSpec
			useIfStale        bool
			wantErrMsgSubstrs []string
		}

		testCases := []malformedSnapshotLabelsTestCase{
			{
				desc:            "the count label is not an integer",
				placementPolicy: newPlacementPolicy(appNamespaceName, configMapNameSelector(configMapName)),
				decoys: func(pp placementv1alpha1.PlacementPolicyAccessor) []decoySnapshotSpec {
					return []decoySnapshotSpec{primaryDecoy(pp, primaryName(pp), "0", "abc", nil)}
				},
				wantErrMsgSubstrs: []string{"failed to convert sub-indexed placement resource snapshot count label to integer"},
			},
			{
				desc:            "the count label is less than 1",
				placementPolicy: newPlacementPolicy(appNamespaceName, configMapNameSelector(configMapName)),
				decoys: func(pp placementv1alpha1.PlacementPolicyAccessor) []decoySnapshotSpec {
					return []decoySnapshotSpec{primaryDecoy(pp, primaryName(pp), "0", "0", nil)}
				},
				wantErrMsgSubstrs: []string{"sub-indexed placement resource snapshot count label is less than 1"},
			},
			{
				desc:            "the index label of one of the primary snapshots is not an integer",
				placementPolicy: newPlacementPolicy(appNamespaceName, configMapNameSelector(configMapName)),
				decoys: func(pp placementv1alpha1.PlacementPolicyAccessor) []decoySnapshotSpec {
					return []decoySnapshotSpec{
						primaryDecoy(pp, primaryName(pp), "0", "1", nil),
						primaryDecoy(pp, pp.GetName()+malformedPrimarySnapshotSuffix, "abc", "1", nil),
					}
				},
				wantErrMsgSubstrs: []string{"failed to sort primary placement resource snapshots"},
			},
			{
				desc:            "the sub-index label of a secondary snapshot is not an integer",
				placementPolicy: newPlacementPolicy(appNamespaceName, configMapNameSelector(configMapName)),
				decoys: func(pp placementv1alpha1.PlacementPolicyAccessor) []decoySnapshotSpec {
					return []decoySnapshotSpec{
						primaryDecoy(pp, primaryName(pp), "0", "2", nil),
						secondaryDecoy(pp, secondaryName(pp), "0", "abc", nil),
					}
				},
				wantErrMsgSubstrs: []string{"failed to sort sub-indexed placement resource snapshots"},
			},
			{
				// The hash does not match; the manager attempts to create a new snapshot.
				desc:            "the index label of the latest primary snapshot is not an integer and the snapshot is stale",
				placementPolicy: newPlacementPolicy(appNamespaceName, configMapNameSelector(configMapName)),
				decoys: func(pp placementv1alpha1.PlacementPolicyAccessor) []decoySnapshotSpec {
					return []decoySnapshotSpec{primaryDecoy(pp, primaryName(pp), "abc", "1", nil)}
				},
				useIfStale:        true,
				wantErrMsgSubstrs: []string{"failed to convert last seen primary placement resource snapshot index label to integer"},
			},
			{
				// The hash matches; the manager attempts to verify (via a quorum read) that the snapshot is up-to-date.
				desc:            "the index label of the latest primary snapshot is not an integer and the hash matches",
				placementPolicy: newPlacementPolicy(appNamespaceName, configMapNameSelector(configMapName)),
				decoys: func(pp placementv1alpha1.PlacementPolicyAccessor) []decoySnapshotSpec {
					return []decoySnapshotSpec{primaryDecoy(pp, primaryName(pp), "abc", "1", withHash(currentHash))}
				},
				wantErrMsgSubstrs: []string{"failed to convert index label to integer"},
			},
			{
				desc:            "a secondary snapshot has a different contents hash than the primary snapshot",
				placementPolicy: newPlacementPolicy(appNamespaceName, configMapNameSelector(configMapName)),
				decoys: func(pp placementv1alpha1.PlacementPolicyAccessor) []decoySnapshotSpec {
					return []decoySnapshotSpec{
						primaryDecoy(pp, primaryName(pp), "0", "2", withHash("hash-a")),
						secondaryDecoy(pp, secondaryName(pp), "0", "1", withHash("hash-b")),
					}
				},
				wantErrMsgSubstrs: []string{mismatchedContentsHashErrMsg},
			},
		}
		// The index of the test case whose latest primary snapshot is stale and features a malformed index label.
		const staleMalformedIndexTestCaseIdx = 4

		var decoyNames [][]string

		BeforeAll(func() {
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: appNamespaceName},
				Data:       map[string]string{"key": "value"},
			}
			Expect(hubClient.Create(ctx, configMap)).To(Succeed(), "Failed to create the config map")

			_, hash, err := resourceSnapshotManager.retrieveAndHashSelectedResources(ctx, testCases[0].placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to retrieve and hash the selected resources")
			currentHash = hash

			decoyNames = make([][]string, len(testCases))
			for idx, tc := range testCases {
				Expect(hubClient.Create(ctx, tc.placementPolicy)).To(Succeed(), "Failed to create the placement policy (%s)", tc.desc)
				for _, decoy := range tc.decoys(tc.placementPolicy) {
					createOwnedDecoySnapshot(tc.placementPolicy, decoy.name, decoy.labels, decoy.annotations)
					getSnapshotFromCache(appNamespaceName, decoy.name)
					decoyNames[idx] = append(decoyNames[idx], decoy.name)
				}
			}
		})

		for _, tc := range testCases {
			It(fmt.Sprintf("should return an unexpected error when %s", tc.desc), func() {
				snapshotFn := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists
				if tc.useIfStale {
					snapshotFn = resourceSnapshotManager.SnapshotResourcesIfStale
				}
				gotSnapshots, isUpToDate, err := snapshotFn(ctx, tc.placementPolicy)
				verifySnapshotCallFailed(gotSnapshots, isUpToDate, err, kferrors.ErrCategoryUnexpected, tc.wantErrMsgSubstrs...)
			})
		}

		It("should return a malformed snapshot without error when only an existing snapshot is requested", func() {
			// The hash does not match, which short-circuits the up-to-date check before the index label is parsed.
			tc := testCases[staleMalformedIndexTestCaseIdx]
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, tc.placementPolicy)
			Expect(err).ToNot(HaveOccurred(), "Failed to snapshot resources")
			Expect(isUpToDate).To(BeFalse(), "The malformed placement resource snapshot is reported as up-to-date")
			if diff := cmp.Diff(snapshotNamesOf(gotSnapshots), []string{primaryName(tc.placementPolicy)}); diff != "" {
				Fail(fmt.Sprintf("Returned placement resource snapshot names diff (-got +want):\n%s", diff))
			}
		})

		AfterAll(func() {
			var objs []client.Object
			for idx, tc := range testCases {
				for _, name := range decoyNames[idx] {
					objs = append(objs, placementResourceSnapshotObj(appNamespaceName, name))
				}
				objs = append(objs, tc.placementPolicy)
			}
			objs = append(objs, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: appNamespaceName}})
			deleteObjectsAndWait(objs)
		})
	})

	Context("concurrent callers", Ordered, func() {
		groupLabels := map[string]string{placementGroupLabelKey: utils.RandStr()}
		configMapNamePrefix := fmt.Sprintf(configMapNameTemplate, utils.RandStr())
		var configMapNames []string

		// Four resources, with at most three resources per snapshot: one primary and one secondary snapshot.
		placementPolicy := newPlacementPolicy(appNamespaceName, configMapLabelSelector(groupLabels))
		// The snapshot names depend on the UID of the placement policy; they are set once the policy is created.
		var wantSnapshotNames0, wantSnapshotNames1 []string

		BeforeAll(func() {
			configMapNames = createLabeledConfigMaps(configMapNamePrefix, 4, groupLabels)
			Expect(hubClient.Create(ctx, placementPolicy)).To(Succeed(), "Failed to create the placement policy")
			wantSnapshotNames0 = []string{
				uniquename.PrimaryPlacementResourceSnapshotName(placementPolicy.Name, placementPolicy.UID, 0),
				uniquename.SecondaryPlacementResourceSnapshotName(placementPolicy.Name, placementPolicy.UID, 0, 1),
			}
			wantSnapshotNames1 = []string{
				uniquename.PrimaryPlacementResourceSnapshotName(placementPolicy.Name, placementPolicy.UID, 1),
				uniquename.SecondaryPlacementResourceSnapshotName(placementPolicy.Name, placementPolicy.UID, 1, 1),
			}
		})

		It("should create exactly one set of snapshots when called concurrently", func() {
			results := snapshotConcurrently(concurrentSnapshotCallerCnt, placementPolicy)
			verifyConcurrentSnapshotCalls(results, wantSnapshotNames0)
			verifyOwnedSnapshots(placementPolicy, wantSnapshotNames0)
		})

		It("should converge to the up-to-date snapshots", func() {
			for _, name := range wantSnapshotNames0 {
				getSnapshotFromCache(appNamespaceName, name)
			}
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfStale(ctx, placementPolicy)
			verifyReturnedSnapshots(gotSnapshots, isUpToDate, err, wantSnapshotNames0)
			verifyNoPrimarySnapshotAtIdx(placementPolicy, 1)
		})

		It("can update a selected config map", func() {
			updateConfigMapData(appNamespaceName, configMapNames[0], "changed")
		})

		It("should create exactly one new set of snapshots when called concurrently", func() {
			results := snapshotConcurrently(concurrentSnapshotCallerCnt, placementPolicy)
			verifyConcurrentSnapshotCalls(results, wantSnapshotNames1)
			verifyOwnedSnapshots(placementPolicy, append(slices.Clone(wantSnapshotNames0), wantSnapshotNames1...))
			verifyNoPrimarySnapshotAtIdx(placementPolicy, 2)
		})

		AfterAll(func() {
			deleteObjectsAndWait(slices.Concat(
				snapshotObjs(appNamespaceName, slices.Concat(wantSnapshotNames0, wantSnapshotNames1)...),
				[]client.Object{placementPolicy},
				configMapObjs(configMapNames),
			))
		})
	})
})

var _ = Describe("corner cases (long and dotted names)", func() {
	Context("name length boundaries", Ordered, func() {
		groupLabels := map[string]string{placementGroupLabelKey: utils.RandStr()}
		configMapNamePrefix := fmt.Sprintf(configMapNameTemplate, utils.RandStr())
		var configMapNames []string

		type nameLengthTestCase struct {
			desc                   string
			placementPolicy        placementv1alpha1.PlacementPolicyAccessor
			wantLabelTruncated     bool
			wantPrimarySanitized   bool
			wantSecondarySanitized bool
		}

		// Every placement policy selects the same four config maps, which yields one primary and one secondary
		// snapshot; this covers the naming of both kinds of snapshots.
		newNamedPlacementPolicy := func(nameLen int) placementv1alpha1.PlacementPolicyAccessor {
			placementPolicy := newPlacementPolicy(appNamespaceName, configMapLabelSelector(groupLabels))
			placementPolicy.Name = placementPolicyNameOfLen(nameLen)
			return placementPolicy
		}
		newNamedClusterPlacementPolicy := func(nameLen int) placementv1alpha1.PlacementPolicyAccessor {
			selector := configMapLabelSelector(groupLabels)
			selector.Namespace = appNamespaceName
			placementPolicy := newClusterPlacementPolicy(selector)
			placementPolicy.Name = placementPolicyNameOfLen(nameLen)
			return placementPolicy
		}

		testCases := []nameLengthTestCase{
			{
				desc:            "the longest name with an untruncated owned-by label (61 characters)",
				placementPolicy: newNamedPlacementPolicy(61),
			},
			{
				desc:               "the shortest name with a truncated owned-by label (62 characters)",
				placementPolicy:    newNamedPlacementPolicy(62),
				wantLabelTruncated: true,
			},
			{
				desc:               "the longest name with untruncated snapshot names (216 characters)",
				placementPolicy:    newNamedPlacementPolicy(216),
				wantLabelTruncated: true,
			},
			{
				desc:                   "the shortest name with a truncated secondary snapshot name (217 characters)",
				placementPolicy:        newNamedPlacementPolicy(217),
				wantLabelTruncated:     true,
				wantSecondarySanitized: true,
			},
			{
				desc:                   "the longest name with an untruncated primary snapshot name (218 characters)",
				placementPolicy:        newNamedPlacementPolicy(218),
				wantLabelTruncated:     true,
				wantSecondarySanitized: true,
			},
			{
				desc:                   "the shortest name with a truncated primary snapshot name (219 characters)",
				placementPolicy:        newNamedPlacementPolicy(219),
				wantLabelTruncated:     true,
				wantPrimarySanitized:   true,
				wantSecondarySanitized: true,
			},
			{
				desc:                   "the longest valid name (253 characters)",
				placementPolicy:        newNamedPlacementPolicy(253),
				wantLabelTruncated:     true,
				wantPrimarySanitized:   true,
				wantSecondarySanitized: true,
			},
			{
				desc:                   "the longest valid name of a cluster placement policy (253 characters)",
				placementPolicy:        newNamedClusterPlacementPolicy(253),
				wantLabelTruncated:     true,
				wantPrimarySanitized:   true,
				wantSecondarySanitized: true,
			},
		}

		BeforeAll(func() {
			configMapNames = createLabeledConfigMaps(configMapNamePrefix, 4, groupLabels)
			for _, tc := range testCases {
				Expect(hubClient.Create(ctx, tc.placementPolicy)).To(Succeed(), "Failed to create the placement policy (%s)", tc.desc)
			}
		})

		for _, tc := range testCases {
			It(fmt.Sprintf("should create and find snapshots for %s", tc.desc), func() {
				placementPolicy := tc.placementPolicy
				name := placementPolicy.GetName()
				snapshotNS := placementPolicy.GetNamespace()
				wantSnapshotNames := []string{
					uniquename.PrimaryPlacementResourceSnapshotName(name, placementPolicy.GetUID(), 0),
					uniquename.SecondaryPlacementResourceSnapshotName(name, placementPolicy.GetUID(), 0, 1),
				}

				gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
				verifyReturnedSnapshots(gotSnapshots, isUpToDate, err, wantSnapshotNames)

				verifySnapshotName(wantSnapshotNames[0], fmt.Sprintf(wantUnsanitizedPrimaryNamePrefixFmt, name, 0),
					wantHashedPrimaryNamePattern, tc.wantPrimarySanitized)
				verifySnapshotName(wantSnapshotNames[1], fmt.Sprintf(wantUnsanitizedSecondaryNamePrefixFmt, name, 0, 1),
					wantHashedSecondaryNamePattern, tc.wantSecondarySanitized)
				verifyOwnedByLabelVal(placementPolicy, tc.wantLabelTruncated)

				// The labels (possibly truncated) and the owned-by annotation (always the full name) on the snapshots.
				hash := snapshotContentsHash(gotSnapshots[0])
				for subIdx, snapshotName := range wantSnapshotNames {
					verifySnapshotMetadata(getSnapshotFromCache(snapshotNS, snapshotName), placementPolicy, 0, subIdx, len(wantSnapshotNames), hash)
				}

				// The field index lookups (keyed by the owned-by annotation) and the quorum reads (by name) should
				// find the existing snapshots.
				gotSnapshots, isUpToDate, err = resourceSnapshotManager.SnapshotResourcesIfStale(ctx, placementPolicy)
				verifyReturnedSnapshots(gotSnapshots, isUpToDate, err, wantSnapshotNames)
				verifyNoPrimarySnapshotAtIdx(placementPolicy, 1)
			})
		}

		AfterAll(func() {
			objs := make([]client.Object, 0, 3*len(testCases)+len(configMapNames))
			for _, tc := range testCases {
				name := tc.placementPolicy.GetName()
				uid := tc.placementPolicy.GetUID()
				snapshotNS := tc.placementPolicy.GetNamespace()
				objs = append(objs,
					placementResourceSnapshotObj(snapshotNS, uniquename.PrimaryPlacementResourceSnapshotName(name, uid, 0)),
					placementResourceSnapshotObj(snapshotNS, uniquename.SecondaryPlacementResourceSnapshotName(name, uid, 0, 1)),
					tc.placementPolicy,
				)
			}
			objs = append(objs, configMapObjs(configMapNames)...)
			deleteObjectsAndWait(objs)
		})
	})

	Context("long name lifecycle and garbage collection", Ordered, func() {
		groupLabels := map[string]string{placementGroupLabelKey: utils.RandStr()}
		configMapNamePrefix := fmt.Sprintf(configMapNameTemplate, utils.RandStr())
		var configMapNames []string

		placementPolicy := newPlacementPolicy(appNamespaceName, configMapLabelSelector(groupLabels))
		placementPolicy.Name = placementPolicyNameOfLen(240)
		// Keep only the latest snapshots so that garbage collection kicks in as soon as possible.
		placementPolicy.Spec.ResourceRevisionHistoryLimit = ptr.To(int32(1))

		placementBindingName := fmt.Sprintf(placementBindingNameTemplate, utils.RandStr())

		// The snapshot names depend on the UID of the placement policy; they are set once the policy is created.
		var wantSnapshotNames0, wantSnapshotNames1 []string

		// placementBindingCntInCache returns the number of placement bindings that use the primary snapshot at
		// index 0, as found via the cache (and its field index).
		placementBindingCntInCache := func() (int, error) {
			placementBindingList := &placementv1alpha1.PlacementBindingList{}
			err := hubClient.List(ctx, placementBindingList, client.InNamespace(appNamespaceName), client.MatchingFields{
				fieldindexers.PlacementBindingOwnedByAndInUseOfPrimaryResourceSnapshotCustomFieldName: fmt.Sprintf(
					fieldindexers.PlacementBindingOwnedByAndInUseOfPrimaryResourceSnapshotCustomFieldValFmt, placementPolicy.Name, wantSnapshotNames0[0]),
			})
			return len(placementBindingList.Items), err
		}

		BeforeAll(func() {
			configMapNames = createLabeledConfigMaps(configMapNamePrefix, 4, groupLabels)
			Expect(hubClient.Create(ctx, placementPolicy)).To(Succeed(), "Failed to create the placement policy")
			wantSnapshotNames0 = []string{
				uniquename.PrimaryPlacementResourceSnapshotName(placementPolicy.Name, placementPolicy.UID, 0),
				uniquename.SecondaryPlacementResourceSnapshotName(placementPolicy.Name, placementPolicy.UID, 0, 1),
			}
			wantSnapshotNames1 = []string{
				uniquename.PrimaryPlacementResourceSnapshotName(placementPolicy.Name, placementPolicy.UID, 1),
				uniquename.SecondaryPlacementResourceSnapshotName(placementPolicy.Name, placementPolicy.UID, 1, 1),
			}
		})

		It("can create a primary and a secondary snapshot with truncated names", func() {
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
			verifyReturnedSnapshots(gotSnapshots, isUpToDate, err, wantSnapshotNames0)
			verifySnapshotName(wantSnapshotNames0[0], fmt.Sprintf(wantUnsanitizedPrimaryNamePrefixFmt, placementPolicy.Name, 0),
				wantHashedPrimaryNamePattern, true)
			verifySnapshotName(wantSnapshotNames0[1], fmt.Sprintf(wantUnsanitizedSecondaryNamePrefixFmt, placementPolicy.Name, 0, 1),
				wantHashedSecondaryNamePattern, true)
			for _, name := range wantSnapshotNames0 {
				getSnapshotFromCache(appNamespaceName, name)
			}
		})

		It("should return the existing snapshots when they are not stale", func() {
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfStale(ctx, placementPolicy)
			verifyReturnedSnapshots(gotSnapshots, isUpToDate, err, wantSnapshotNames0)
			verifyNoPrimarySnapshotAtIdx(placementPolicy, 1)
		})

		It("can create a placement binding that uses the primary snapshot at index 0", func() {
			placementBinding := &placementv1alpha1.PlacementBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      placementBindingName,
					Namespace: appNamespaceName,
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: placementv1alpha1.GroupVersion.String(),
							Kind:       placementv1alpha1.PlacementPolicyKind,
							Name:       placementPolicy.Name,
							UID:        placementPolicy.UID,
						},
					},
				},
				Spec: placementv1alpha1.PlacementBindingSpec{
					PlacementPolicyName:  placementPolicy.Name,
					ClusterName:          "member-1",
					ResourceSnapshotName: wantSnapshotNames0[0],
				},
			}
			Expect(hubClient.Create(ctx, placementBinding)).To(Succeed(), "Failed to create the placement binding")

			// Wait until the cache (and its field index) has observed the placement binding; the garbage collection
			// process relies on it to tell if a snapshot is still in use.
			Eventually(placementBindingCntInCache, eventuallyDuration, eventuallyInterval).Should(Equal(1), "Failed to find the placement binding in the cache")
		})

		It("can update a selected config map", func() {
			updateConfigMapData(appNamespaceName, configMapNames[0], "changed")
		})

		It("should create new snapshots at index 1", func() {
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfStale(ctx, placementPolicy)
			verifyReturnedSnapshots(gotSnapshots, isUpToDate, err, wantSnapshotNames1)
			for _, name := range wantSnapshotNames1 {
				getSnapshotFromCache(appNamespaceName, name)
			}
		})

		It("should not garbage collect the snapshots at index 0 as they are in use", func() {
			// The snapshots at index 0 are now past the revision history limit; this call enqueues them for
			// garbage collection, which should skip them as a placement binding is using them.
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfStale(ctx, placementPolicy)
			verifyReturnedSnapshots(gotSnapshots, isUpToDate, err, wantSnapshotNames1)

			Consistently(func() error {
				for _, name := range wantSnapshotNames0 {
					if err := hubUncachedReader.Get(ctx, types.NamespacedName{Namespace: appNamespaceName, Name: name}, &placementv1alpha1.PlacementResourceSnapshot{}); err != nil {
						return err
					}
				}
				return nil
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "The snapshots at index 0 have been garbage collected unexpectedly")
		})

		It("can delete the placement binding", func() {
			deleteObjectsAndWait([]client.Object{
				&placementv1alpha1.PlacementBinding{ObjectMeta: metav1.ObjectMeta{Name: placementBindingName, Namespace: appNamespaceName}},
			})
			Eventually(placementBindingCntInCache, eventuallyDuration, eventuallyInterval).Should(Equal(0), "The placement binding is still found in the cache")
		})

		It("should garbage collect the primary and secondary snapshots at index 0", func() {
			// This call enqueues the snapshots at index 0 for garbage collection again.
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfStale(ctx, placementPolicy)
			verifyReturnedSnapshots(gotSnapshots, isUpToDate, err, wantSnapshotNames1)

			for _, name := range wantSnapshotNames0 {
				Eventually(func() bool {
					return errors.IsNotFound(hubUncachedReader.Get(ctx, types.NamespacedName{Namespace: appNamespaceName, Name: name}, &placementv1alpha1.PlacementResourceSnapshot{}))
				}, eventuallyDuration, eventuallyInterval).Should(BeTrue(), "Placement resource snapshot %s has not been garbage collected", name)
			}
			for _, name := range wantSnapshotNames1 {
				verifySnapshotExists(appNamespaceName, name)
			}
		})

		AfterAll(func() {
			deleteObjectsAndWait(slices.Concat(
				snapshotObjs(appNamespaceName, slices.Concat(wantSnapshotNames0, wantSnapshotNames1)...),
				[]client.Object{
					&placementv1alpha1.PlacementBinding{ObjectMeta: metav1.ObjectMeta{Name: placementBindingName, Namespace: appNamespaceName}},
					placementPolicy,
				},
				configMapObjs(configMapNames),
			))
		})
	})

	Context("dotted names", Ordered, func() {
		groupLabels := map[string]string{placementGroupLabelKey: utils.RandStr()}
		configMapNamePrefix := fmt.Sprintf(configMapNameTemplate, utils.RandStr())
		var configMapNames []string

		// The names share the same random suffix; `ab.c<r>` and `a.bc<r>` collide after dot removal.
		suffix := utils.RandStr()

		type dottedNameTestCase struct {
			placementPolicy *placementv1alpha1.PlacementPolicy
			wantSanitized   bool
			// The wanted prefix of the (sanitized) primary snapshot name.
			wantPrimaryNamePrefix string
		}

		newNamedPlacementPolicy := func(name string) *placementv1alpha1.PlacementPolicy {
			placementPolicy := newPlacementPolicy(appNamespaceName, configMapLabelSelector(groupLabels))
			placementPolicy.Name = name
			return placementPolicy
		}

		testCases := []dottedNameTestCase{
			{
				placementPolicy:       newNamedPlacementPolicy("policy." + suffix + ".v1"),
				wantSanitized:         true,
				wantPrimaryNamePrefix: "policy" + suffix + "v1-resource-snapshot-0-",
			},
			{
				placementPolicy:       newNamedPlacementPolicy("ab.c" + suffix),
				wantSanitized:         true,
				wantPrimaryNamePrefix: "abc" + suffix + "-resource-snapshot-0-",
			},
			{
				placementPolicy:       newNamedPlacementPolicy("a.bc" + suffix),
				wantSanitized:         true,
				wantPrimaryNamePrefix: "abc" + suffix + "-resource-snapshot-0-",
			},
			{
				placementPolicy: newNamedPlacementPolicy("abc" + suffix),
			},
		}

		wantSnapshotNamesOf := func(placementPolicy placementv1alpha1.PlacementPolicyAccessor) []string {
			return []string{
				uniquename.PrimaryPlacementResourceSnapshotName(placementPolicy.GetName(), placementPolicy.GetUID(), 0),
				uniquename.SecondaryPlacementResourceSnapshotName(placementPolicy.GetName(), placementPolicy.GetUID(), 0, 1),
			}
		}

		BeforeAll(func() {
			configMapNames = createLabeledConfigMaps(configMapNamePrefix, 4, groupLabels)
			for _, tc := range testCases {
				Expect(hubClient.Create(ctx, tc.placementPolicy)).To(Succeed(), "Failed to create placement policy %s", tc.placementPolicy.Name)
			}
		})

		for _, tc := range testCases {
			It(fmt.Sprintf("can create snapshots for a placement policy named %s", tc.placementPolicy.Name), func() {
				placementPolicy := tc.placementPolicy
				wantSnapshotNames := wantSnapshotNamesOf(placementPolicy)

				gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
				verifyReturnedSnapshots(gotSnapshots, isUpToDate, err, wantSnapshotNames)

				verifySnapshotName(wantSnapshotNames[0], fmt.Sprintf(wantUnsanitizedPrimaryNamePrefixFmt, placementPolicy.Name, 0),
					wantHashedPrimaryNamePattern, tc.wantSanitized)
				verifySnapshotName(wantSnapshotNames[1], fmt.Sprintf(wantUnsanitizedSecondaryNamePrefixFmt, placementPolicy.Name, 0, 1),
					wantHashedSecondaryNamePattern, tc.wantSanitized)
				if tc.wantSanitized {
					Expect(wantSnapshotNames[0]).To(HavePrefix(tc.wantPrimaryNamePrefix), "Unexpected prefix of the sanitized snapshot name")
				}

				// The name is short enough; both the owned-by label (dots are allowed in label values) and the
				// owned-by annotation should be the name itself.
				verifyOwnedByLabelVal(placementPolicy, false)
				hash := snapshotContentsHash(gotSnapshots[0])
				for subIdx, name := range wantSnapshotNames {
					verifySnapshotMetadata(getSnapshotFromCache(appNamespaceName, name), placementPolicy, 0, subIdx, len(wantSnapshotNames), hash)
				}
			})
		}

		It("should keep the snapshots of names that collide after dot removal apart", func() {
			allSnapshotNames := sets.New[string]()
			for _, tc := range testCases {
				allSnapshotNames.Insert(wantSnapshotNamesOf(tc.placementPolicy)...)
			}
			Expect(allSnapshotNames.Len()).To(Equal(2*len(testCases)), "Some placement resource snapshot names collide")

			for _, tc := range testCases {
				wantSnapshotNames := wantSnapshotNamesOf(tc.placementPolicy)
				verifyOwnedSnapshots(tc.placementPolicy, wantSnapshotNames)

				gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfStale(ctx, tc.placementPolicy)
				verifyReturnedSnapshots(gotSnapshots, isUpToDate, err, wantSnapshotNames)
			}
		})

		AfterAll(func() {
			var objs []client.Object
			for _, tc := range testCases {
				for _, name := range wantSnapshotNamesOf(tc.placementPolicy) {
					objs = append(objs, placementResourceSnapshotObj(appNamespaceName, name))
				}
				objs = append(objs, tc.placementPolicy)
			}
			objs = append(objs, configMapObjs(configMapNames)...)
			deleteObjectsAndWait(objs)
		})
	})

	Context("long names sharing a prefix", Ordered, func() {
		configMapName := fmt.Sprintf(configMapNameTemplate, utils.RandStr())

		// The names (240 characters) share the first 229 characters, which is more than what the owned-by label keeps.
		prefix := strings.Repeat("b", 229)
		placementPolicies := []*placementv1alpha1.PlacementPolicy{
			newPlacementPolicy(appNamespaceName, configMapNameSelector(configMapName)),
			newPlacementPolicy(appNamespaceName, configMapNameSelector(configMapName)),
		}
		for _, placementPolicy := range placementPolicies {
			placementPolicy.Name = prefix + "-" + utils.RandStr()
		}

		BeforeAll(func() {
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: appNamespaceName},
				Data:       map[string]string{"key": "value"},
			}
			Expect(hubClient.Create(ctx, configMap)).To(Succeed(), "Failed to create the config map")
			for _, placementPolicy := range placementPolicies {
				Expect(hubClient.Create(ctx, placementPolicy)).To(Succeed(), "Failed to create the placement policy")
			}
		})

		It("can create snapshots for both placement policies", func() {
			for _, placementPolicy := range placementPolicies {
				gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
				verifyReturnedPrimarySnapshot(gotSnapshots, isUpToDate, err, placementPolicy, 0)
				verifySnapshotName(gotSnapshots[0].GetName(), fmt.Sprintf(wantUnsanitizedPrimaryNamePrefixFmt, placementPolicy.Name, 0),
					wantHashedPrimaryNamePattern, true)
			}
		})

		It("should give each placement policy distinct owned-by labels and annotations", func() {
			labelVals := make([]string, 0, len(placementPolicies))
			for _, placementPolicy := range placementPolicies {
				verifyOwnedByLabelVal(placementPolicy, true)
				labelVals = append(labelVals, placementPolicyOwnerLabelVal(placementPolicy))

				snapshot := getPrimarySnapshotFromCache(placementPolicy, 0)
				verifySnapshotMetadata(snapshot, placementPolicy, 0, 0, 1, snapshotContentsHash(snapshot))
			}
			Expect(labelVals[0]).ToNot(Equal(labelVals[1]), "The placement policies share the same owned-by label value")
		})

		It("should keep the snapshots apart", func() {
			for _, placementPolicy := range placementPolicies {
				wantSnapshotNames := []string{uniquename.PrimaryPlacementResourceSnapshotName(placementPolicy.Name, placementPolicy.UID, 0)}
				verifyOwnedSnapshots(placementPolicy, wantSnapshotNames)

				gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfStale(ctx, placementPolicy)
				verifyReturnedSnapshots(gotSnapshots, isUpToDate, err, wantSnapshotNames)
			}
		})

		AfterAll(func() {
			objs := make([]client.Object, 0, 2*len(placementPolicies)+1)
			for _, placementPolicy := range placementPolicies {
				objs = append(objs, primarySnapshotObjs(placementPolicy, 1)...)
				objs = append(objs, placementPolicy)
			}
			objs = append(objs, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: appNamespaceName}})
			deleteObjectsAndWait(objs)
		})
	})

	Context("same names in different namespaces and scopes", Ordered, func() {
		name := fmt.Sprintf(placementPolicyNameTemplate, utils.RandStr())
		namespaceName := fmt.Sprintf(namespaceNameTemplate, utils.RandStr())
		// The config maps in the two namespaces share the same name.
		configMapName := fmt.Sprintf(configMapNameTemplate, utils.RandStr())

		appPlacementPolicy := newPlacementPolicy(appNamespaceName, configMapNameSelector(configMapName))
		appPlacementPolicy.Name = name
		nsPlacementPolicy := newPlacementPolicy(namespaceName, configMapNameSelector(configMapName))
		nsPlacementPolicy.Name = name
		clusterSelector := configMapNameSelector(configMapName)
		clusterSelector.Namespace = namespaceName
		clusterPlacementPolicy := newClusterPlacementPolicy(clusterSelector)
		clusterPlacementPolicy.Name = name

		placementPolicies := []placementv1alpha1.PlacementPolicyAccessor{appPlacementPolicy, nsPlacementPolicy, clusterPlacementPolicy}
		// The placement policies share the same name, but their snapshot names differ, as the names factor in
		// the UIDs of the placement policies.
		primarySnapshotNameOf := func(placementPolicy placementv1alpha1.PlacementPolicyAccessor, idx int) string {
			return uniquename.PrimaryPlacementResourceSnapshotName(placementPolicy.GetName(), placementPolicy.GetUID(), idx)
		}

		BeforeAll(func() {
			createNamespace(namespaceName)
			for _, ns := range []string{appNamespaceName, namespaceName} {
				configMap := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: ns},
					Data:       map[string]string{"key": "value"},
				}
				Expect(hubClient.Create(ctx, configMap)).To(Succeed(), "Failed to create the config map in namespace %s", ns)
			}
			for _, placementPolicy := range placementPolicies {
				Expect(hubClient.Create(ctx, placementPolicy)).To(Succeed(), "Failed to create placement policy %T", placementPolicy)
			}
		})

		It("can create snapshots for all three placement policies", func() {
			// The snapshots live in different namespaces (or at the cluster scope).
			for _, placementPolicy := range placementPolicies {
				gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
				verifyReturnedSnapshots(gotSnapshots, isUpToDate, err, []string{primarySnapshotNameOf(placementPolicy, 0)})
				getPrimarySnapshotFromCache(placementPolicy, 0)
			}
		})

		It("can update the config map in the app namespace", func() {
			updateConfigMapData(appNamespaceName, configMapName, "changed")
		})

		It("should create a new snapshot only for the placement policy that selects the updated config map", func() {
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfStale(ctx, appPlacementPolicy)
			verifyReturnedSnapshots(gotSnapshots, isUpToDate, err, []string{primarySnapshotNameOf(appPlacementPolicy, 1)})

			for _, placementPolicy := range []placementv1alpha1.PlacementPolicyAccessor{nsPlacementPolicy, clusterPlacementPolicy} {
				gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfStale(ctx, placementPolicy)
				verifyReturnedSnapshots(gotSnapshots, isUpToDate, err, []string{primarySnapshotNameOf(placementPolicy, 0)})
				verifyNoPrimarySnapshotAtIdx(placementPolicy, 1)
			}
		})

		AfterAll(func() {
			objs := primarySnapshotObjs(appPlacementPolicy, 2)
			objs = append(objs, primarySnapshotObjs(nsPlacementPolicy, 1)...)
			objs = append(objs, primarySnapshotObjs(clusterPlacementPolicy, 1)...)
			for _, placementPolicy := range placementPolicies {
				objs = append(objs, placementPolicy)
			}
			objs = append(objs,
				&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: appNamespaceName}},
				&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: namespaceName}},
			)
			deleteObjectsAndWait(objs)
		})
	})

	Context("owned-by label collision", Ordered, func() {
		groupALabels := map[string]string{placementGroupLabelKey: utils.RandStr()}
		groupBLabels := map[string]string{placementGroupLabelKey: utils.RandStr()}
		configMapNamePrefixA := fmt.Sprintf(configMapNameTemplate, utils.RandStr())
		configMapNamePrefixB := fmt.Sprintf(configMapNameTemplate, utils.RandStr())
		var configMapNames []string

		// Placement policy A has a long name, and placement policy B is named after the (truncated) owned-by label
		// value of placement policy A; the two share the same owned-by label value.
		placementPolicyA := newPlacementPolicy(appNamespaceName, configMapLabelSelector(groupALabels))
		placementPolicyA.Name = placementPolicyNameOfLen(240)
		placementPolicyB := newPlacementPolicy(appNamespaceName, configMapLabelSelector(groupBLabels))
		placementPolicyB.Name = placementPolicyOwnerLabelVal(placementPolicyA)

		// The snapshot names depend on the UIDs of the placement policies; they are set once the policies are created.
		var wantSnapshotNamesA0, wantSnapshotNamesA1, wantSnapshotNamesB0 []string

		BeforeAll(func() {
			configMapNames = append(createLabeledConfigMaps(configMapNamePrefixA, 4, groupALabels),
				createLabeledConfigMaps(configMapNamePrefixB, 1, groupBLabels)...)
			Expect(hubClient.Create(ctx, placementPolicyA)).To(Succeed(), "Failed to create placement policy A")
			Expect(hubClient.Create(ctx, placementPolicyB)).To(Succeed(), "Failed to create placement policy B")

			wantSnapshotNamesA0 = []string{
				uniquename.PrimaryPlacementResourceSnapshotName(placementPolicyA.Name, placementPolicyA.UID, 0),
				uniquename.SecondaryPlacementResourceSnapshotName(placementPolicyA.Name, placementPolicyA.UID, 0, 1),
			}
			wantSnapshotNamesA1 = []string{
				uniquename.PrimaryPlacementResourceSnapshotName(placementPolicyA.Name, placementPolicyA.UID, 1),
				uniquename.SecondaryPlacementResourceSnapshotName(placementPolicyA.Name, placementPolicyA.UID, 1, 1),
			}
			wantSnapshotNamesB0 = []string{uniquename.PrimaryPlacementResourceSnapshotName(placementPolicyB.Name, placementPolicyB.UID, 0)}
		})

		It("can create snapshots for the long-named placement policy", func() {
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicyA)
			verifyReturnedSnapshots(gotSnapshots, isUpToDate, err, wantSnapshotNamesA0)
			for _, name := range wantSnapshotNamesA0 {
				getSnapshotFromCache(appNamespaceName, name)
			}
		})

		It("should give both placement policies the same owned-by label but different annotations", func() {
			Expect(placementPolicyOwnerLabelVal(placementPolicyB)).To(Equal(placementPolicyOwnerLabelVal(placementPolicyA)),
				"The placement policies do not share the same owned-by label value")
			snapshot := getSnapshotFromCache(appNamespaceName, wantSnapshotNamesA0[0])
			Expect(snapshot.GetLabels()[placementv1alpha1.PlacementResourceSnapshotOwnedByLabelKey]).To(Equal(placementPolicyB.Name),
				"Unexpected owned-by label value")
			Expect(snapshot.GetAnnotations()[placementv1alpha1.PlacementResourceSnapshotOwnedByAnnotationKey]).To(Equal(placementPolicyA.Name),
				"Unexpected owned-by annotation value")
		})

		It("should not return the snapshots of the other placement policy", func() {
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicyB)
			verifyReturnedSnapshots(gotSnapshots, isUpToDate, err, wantSnapshotNamesB0)
			getSnapshotFromCache(appNamespaceName, wantSnapshotNamesB0[0])
		})

		It("should keep each placement policy's snapshot indices independent", func() {
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfStale(ctx, placementPolicyA)
			verifyReturnedSnapshots(gotSnapshots, isUpToDate, err, wantSnapshotNamesA0)
			gotSnapshots, isUpToDate, err = resourceSnapshotManager.SnapshotResourcesIfStale(ctx, placementPolicyB)
			verifyReturnedSnapshots(gotSnapshots, isUpToDate, err, wantSnapshotNamesB0)

			verifyOwnedSnapshots(placementPolicyA, wantSnapshotNamesA0)
			verifyOwnedSnapshots(placementPolicyB, wantSnapshotNamesB0)
		})

		It("can update a config map selected by the long-named placement policy", func() {
			updateConfigMapData(appNamespaceName, configMapNamePrefixA+"-0", "changed")
		})

		It("should create new snapshots only for that placement policy", func() {
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfStale(ctx, placementPolicyA)
			verifyReturnedSnapshots(gotSnapshots, isUpToDate, err, wantSnapshotNamesA1)
			for _, name := range wantSnapshotNamesA1 {
				getSnapshotFromCache(appNamespaceName, name)
			}

			gotSnapshots, isUpToDate, err = resourceSnapshotManager.SnapshotResourcesIfStale(ctx, placementPolicyB)
			verifyReturnedSnapshots(gotSnapshots, isUpToDate, err, wantSnapshotNamesB0)
			verifyNoPrimarySnapshotAtIdx(placementPolicyB, 1)
		})

		AfterAll(func() {
			deleteObjectsAndWait(slices.Concat(
				snapshotObjs(appNamespaceName, slices.Concat(wantSnapshotNamesA0, wantSnapshotNamesA1, wantSnapshotNamesB0)...),
				[]client.Object{placementPolicyA, placementPolicyB},
				configMapObjs(configMapNames),
			))
		})
	})
})

var _ = Describe("corner cases (leftover snapshots from previous placement policies)", func() {
	// The envtest environment does not run the Kubernetes GC: deleting a placement policy leaves its placement resource
	// snapshots behind, still owned by the deleted placement policy, which is exactly the state where the Kubernetes GC
	// has yet to collect them. Where needed, the test cases below stand in for the Kubernetes GC: they delete the
	// snapshots by hand, or remove their owner references to simulate a deletion with the orphan propagation policy.

	Context("placement policy re-created before the snapshots of the previous one are gone", Ordered, func() {
		configMapName := fmt.Sprintf(configMapNameTemplate, utils.RandStr())
		var placementPolicy placementv1alpha1.PlacementPolicyAccessor = newPlacementPolicy(appNamespaceName, configMapNameSelector(configMapName))

		var pausedMgr *Manager
		// The snapshot name depends on the UID of the (previous) placement policy; it is set once the policy is created.
		var previousSnapshotName string

		BeforeAll(func() {
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: appNamespaceName},
				Data:       map[string]string{"key": "value"},
			}
			Expect(hubClient.Create(ctx, configMap)).To(Succeed(), "Failed to create the config map")
			Expect(hubClient.Create(ctx, placementPolicy)).To(Succeed(), "Failed to create the placement policy")
			previousSnapshotName = uniquename.PrimaryPlacementResourceSnapshotName(placementPolicy.GetName(), placementPolicy.GetUID(), 0)
			pausedMgr = newPausedManager()
		})

		It("can create a snapshot", func() {
			snapshotAndVerify(pausedMgr, placementPolicy, []string{previousSnapshotName})
		})

		It("can delete and re-create the placement policy", func() {
			placementPolicy = recreatePlacementPolicy(placementPolicy)
			// The snapshot is still owned by the previous placement policy.
			verifySnapshotExists(appNamespaceName, previousSnapshotName)
		})

		It("should block snapshotting with a transient error", func() {
			gotSnapshots, isUpToDate, err := pausedMgr.SnapshotResourcesIfStale(ctx, placementPolicy)
			verifySnapshotCallFailed(gotSnapshots, isUpToDate, err, kferrors.ErrCategoryTransient, staleOwnerSnapshotErrMsg)
			gotSnapshots, isUpToDate, err = pausedMgr.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
			verifySnapshotCallFailed(gotSnapshots, isUpToDate, err, kferrors.ErrCategoryTransient, staleOwnerSnapshotErrMsg)

			// The snapshot of the previous placement policy is left to the Kubernetes GC; no GC request is enqueued.
			verifyGCRequests(drainGCRequests(pausedMgr), nil)
			verifyNoPrimarySnapshotAtIdx(placementPolicy, 0)
			Expect(checkOwnedSnapshots(placementPolicy, []string{previousSnapshotName})).To(Succeed())
		})

		It("should keep blocking snapshotting after a resource change", func() {
			updateConfigMapData(appNamespaceName, configMapName, "changed")
			gotSnapshots, isUpToDate, err := pausedMgr.SnapshotResourcesIfStale(ctx, placementPolicy)
			verifySnapshotCallFailed(gotSnapshots, isUpToDate, err, kferrors.ErrCategoryTransient, staleOwnerSnapshotErrMsg)
			verifyNoPrimarySnapshotAtIdx(placementPolicy, 0)
			verifyNoPrimarySnapshotAtIdx(placementPolicy, 1)
			verifyGCRequests(drainGCRequests(pausedMgr), nil)
		})

		It("can delete the snapshot of the previous placement policy (in place of the Kubernetes GC)", func() {
			deleteObjectsAndWait(snapshotObjs(appNamespaceName, previousSnapshotName))
			waitForSnapshotGoneFromCache(appNamespaceName, previousSnapshotName)
		})

		It("should create a snapshot owned by the re-created placement policy, under a new name", func() {
			snapshotName := uniquename.PrimaryPlacementResourceSnapshotName(placementPolicy.GetName(), placementPolicy.GetUID(), 0)
			Expect(snapshotName).ToNot(Equal(previousSnapshotName), "The snapshot name of the previous placement policy is reused")

			snapshotAndVerify(pausedMgr, placementPolicy, []string{snapshotName})
			if diff := cmp.Diff(getPrimarySnapshotFromCache(placementPolicy, 0).GetOwnerReferences(), wantPlacementPolicyOwnerRefs(placementPolicy)); diff != "" {
				Fail(fmt.Sprintf("Placement resource snapshot %s owner references diff (-got +want):\n%s", snapshotName, diff))
			}
			verifyGCRequests(drainGCRequests(pausedMgr), nil)
		})

		AfterAll(func() {
			if pausedMgr != nil {
				pausedMgr.gcwq.ShutDown()
			}
			objs := snapshotObjs(appNamespaceName,
				previousSnapshotName,
				uniquename.PrimaryPlacementResourceSnapshotName(placementPolicy.GetName(), placementPolicy.GetUID(), 0),
			)
			objs = append(objs, placementPolicy, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: appNamespaceName}})
			deleteObjectsAndWait(objs)
		})
	})

	// The scenario is run for both placement policies and cluster placement policies.
	for _, clusterScoped := range []bool{false, true} {
		desc := "placement policy re-created after the previous one is deleted with the orphan propagation policy"
		if clusterScoped {
			desc = "cluster placement policy re-created after the previous one is deleted with the orphan propagation policy"
		}

		Context(desc, Ordered, func() {
			groupLabels := map[string]string{placementGroupLabelKey: utils.RandStr()}
			configMapNamePrefix := fmt.Sprintf(configMapNameTemplate, utils.RandStr())
			placementBindingName := fmt.Sprintf(placementBindingNameTemplate, utils.RandStr())
			var configMapNames []string

			resourceSelector := configMapLabelSelector(groupLabels)
			var placementPolicy placementv1alpha1.PlacementPolicyAccessor
			if clusterScoped {
				resourceSelector.Namespace = appNamespaceName
				placementPolicy = newClusterPlacementPolicy(resourceSelector)
			} else {
				placementPolicy = newPlacementPolicy(appNamespaceName, resourceSelector)
			}
			snapshotNS := placementPolicy.GetNamespace()

			var pausedMgr *Manager
			// The snapshots of the previous placement policy; the names are set once the policy is created.
			var previousSnapshotNames []string
			var previousPrimarySnapshots []placementv1alpha1.PlacementResourceSnapshotAccessor

			BeforeAll(func() {
				// Four config maps make two groups, as each snapshot can hold at most three resources.
				configMapNames = createLabeledConfigMaps(configMapNamePrefix, 4, groupLabels)
				Expect(hubClient.Create(ctx, placementPolicy)).To(Succeed(), "Failed to create the placement policy")
				previousSnapshotNames = slices.Concat(snapshotSetNames(placementPolicy, 0, 2), snapshotSetNames(placementPolicy, 1, 2))
				pausedMgr = newPausedManager()
			})

			It("can create snapshots at index 0 and 1", func() {
				snapshotAndVerify(resourceSnapshotManager, placementPolicy, snapshotSetNames(placementPolicy, 0, 2))
				updateConfigMapData(appNamespaceName, configMapNames[0], "changed")
				snapshotAndVerify(resourceSnapshotManager, placementPolicy, snapshotSetNames(placementPolicy, 1, 2))
				previousPrimarySnapshots = []placementv1alpha1.PlacementResourceSnapshotAccessor{
					getPrimarySnapshotFromCache(placementPolicy, 0),
					getPrimarySnapshotFromCache(placementPolicy, 1),
				}
			})

			It("can create a placement binding without an owner reference that uses the snapshot at index 1", func() {
				// The placement binding mimics one orphaned along with the snapshots.
				createPlacementBinding(placementPolicy, placementBindingName, previousPrimarySnapshots[1].GetName(), nil)
				waitForPlacementBindingCntInCache(placementPolicy, previousPrimarySnapshots[1].GetName(), 1)
			})

			It("can orphan the snapshots and re-create the placement policy", func() {
				for _, name := range previousSnapshotNames {
					setSnapshotOwnerRefs(snapshotNS, name, nil)
				}
				placementPolicy = recreatePlacementPolicy(placementPolicy)
			})

			It("should block snapshotting and enqueue the orphaned primary snapshots for GC", func() {
				wantGCRequests := []snapshotGarbageCollectionRequest{
					gcRequestFor(placementPolicy, previousPrimarySnapshots[0]),
					gcRequestFor(placementPolicy, previousPrimarySnapshots[1]),
				}

				gotSnapshots, isUpToDate, err := pausedMgr.SnapshotResourcesIfStale(ctx, placementPolicy)
				verifySnapshotCallFailed(gotSnapshots, isUpToDate, err, kferrors.ErrCategoryTransient, leftoverSnapshotsErrMsg)
				verifyGCRequests(drainGCRequests(pausedMgr), wantGCRequests)

				gotSnapshots, isUpToDate, err = pausedMgr.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
				verifySnapshotCallFailed(gotSnapshots, isUpToDate, err, kferrors.ErrCategoryTransient, leftoverSnapshotsErrMsg)
				verifyGCRequests(drainGCRequests(pausedMgr), wantGCRequests)

				verifyNoPrimarySnapshotAtIdx(placementPolicy, 0)
			})

			It("should garbage collect all the orphaned snapshots, even though a placement binding uses one of them", func() {
				// Unlike the paused manager, the manager under test runs its GC worker.
				gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfStale(ctx, placementPolicy)
				verifySnapshotCallFailed(gotSnapshots, isUpToDate, err, kferrors.ErrCategoryTransient, leftoverSnapshotsErrMsg)
				Eventually(func() error {
					return checkOwnedSnapshots(placementPolicy, nil)
				}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to garbage collect the orphaned snapshots")
			})

			It("should create snapshots owned by the re-created placement policy, under new names", func() {
				wantNames := snapshotSetNames(placementPolicy, 0, 2)
				for _, name := range wantNames {
					Expect(previousSnapshotNames).ToNot(ContainElement(name), "A snapshot name of the previous placement policy is reused")
				}

				// The cache might take a moment to observe the removal of the orphaned snapshots.
				Eventually(func() error {
					gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfStale(ctx, placementPolicy)
					if err != nil {
						return err
					}
					if diff := cmp.Diff(snapshotNamesOf(gotSnapshots), wantNames); diff != "" || !isUpToDate {
						return fmt.Errorf("unexpected snapshots (up-to-date %t), names diff (-got +want):\n%s", isUpToDate, diff)
					}
					return nil
				}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to create snapshots for the re-created placement policy")
				Expect(checkOwnedSnapshots(placementPolicy, wantNames)).To(Succeed())
				if diff := cmp.Diff(getPrimarySnapshotFromCache(placementPolicy, 0).GetOwnerReferences(), wantPlacementPolicyOwnerRefs(placementPolicy)); diff != "" {
					Fail(fmt.Sprintf("Placement resource snapshot %s owner references diff (-got +want):\n%s", wantNames[0], diff))
				}
			})

			It("should leave the placement binding untouched", func() {
				placementBinding := placementBindingObj(snapshotNS, placementBindingName)
				Expect(hubUncachedReader.Get(ctx, client.ObjectKeyFromObject(placementBinding), placementBinding)).To(Succeed(),
					"Failed to get the placement binding")
				Expect(placementBinding.GetSpec().ResourceSnapshotName).To(Equal(previousPrimarySnapshots[1].GetName()),
					"The placement binding has been changed")
			})

			AfterAll(func() {
				if pausedMgr != nil {
					pausedMgr.gcwq.ShutDown()
				}
				objs := snapshotObjs(snapshotNS, slices.Concat(previousSnapshotNames, snapshotSetNames(placementPolicy, 0, 2))...)
				objs = append(objs, placementBindingObj(snapshotNS, placementBindingName), placementPolicy)
				objs = append(objs, configMapObjs(configMapNames)...)
				deleteObjectsAndWait(objs)
			})
		})
	}

	Context("orphaned primary snapshot pending deletion", Ordered, func() {
		configMapName := fmt.Sprintf(configMapNameTemplate, utils.RandStr())
		var placementPolicy placementv1alpha1.PlacementPolicyAccessor = newPlacementPolicy(appNamespaceName, configMapNameSelector(configMapName))

		// The snapshot name depends on the UID of the (previous) placement policy; it is set once the policy is created.
		var previousSnapshotName string

		BeforeAll(func() {
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: appNamespaceName},
				Data:       map[string]string{"key": "value"},
			}
			Expect(hubClient.Create(ctx, configMap)).To(Succeed(), "Failed to create the config map")
			Expect(hubClient.Create(ctx, placementPolicy)).To(Succeed(), "Failed to create the placement policy")
			previousSnapshotName = uniquename.PrimaryPlacementResourceSnapshotName(placementPolicy.GetName(), placementPolicy.GetUID(), 0)
		})

		It("can create a snapshot", func() {
			snapshotAndVerify(resourceSnapshotManager, placementPolicy, []string{previousSnapshotName})
		})

		It("can orphan the snapshot, block its deletion with a finalizer, and re-create the placement policy", func() {
			addSnapshotFinalizer(appNamespaceName, previousSnapshotName)
			setSnapshotOwnerRefs(appNamespaceName, previousSnapshotName, nil)
			placementPolicy = recreatePlacementPolicy(placementPolicy)
		})

		It("should keep blocking snapshotting while the orphaned snapshot is pending deletion", func() {
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfStale(ctx, placementPolicy)
			verifySnapshotCallFailed(gotSnapshots, isUpToDate, err, kferrors.ErrCategoryTransient, leftoverSnapshotsErrMsg)

			// The GC process deletes the orphaned snapshot, which the finalizer keeps around.
			Eventually(func() error {
				snapshot := placementResourceSnapshotObj(appNamespaceName, previousSnapshotName)
				if err := hubClient.Get(ctx, types.NamespacedName{Namespace: appNamespaceName, Name: previousSnapshotName}, snapshot); err != nil {
					return err
				}
				if snapshot.GetDeletionTimestamp().IsZero() {
					return fmt.Errorf("the orphaned snapshot has not been marked for deletion yet")
				}
				return nil
			}, eventuallyDuration, eventuallyInterval).Should(Succeed(), "Failed to mark the orphaned snapshot for deletion")

			Consistently(func() error {
				gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfStale(ctx, placementPolicy)
				return checkSnapshotCallFailed(gotSnapshots, isUpToDate, err, kferrors.ErrCategoryTransient, leftoverSnapshotsErrMsg)
			}, consistentlyDuration, consistentlyInterval).Should(Succeed(), "Snapshotting is not blocked while the orphaned snapshot is pending deletion")
			verifyNoPrimarySnapshotAtIdx(placementPolicy, 0)
		})

		It("should create a snapshot once the orphaned snapshot is gone", func() {
			removeFinalizersAndDelete(placementResourceSnapshotObj(appNamespaceName, previousSnapshotName))
			waitForSnapshotGoneFromCache(appNamespaceName, previousSnapshotName)

			snapshotAndVerify(resourceSnapshotManager, placementPolicy,
				[]string{uniquename.PrimaryPlacementResourceSnapshotName(placementPolicy.GetName(), placementPolicy.GetUID(), 0)})
		})

		AfterAll(func() {
			removeFinalizersAndDelete(placementResourceSnapshotObj(appNamespaceName, previousSnapshotName))
			objs := snapshotObjs(appNamespaceName, uniquename.PrimaryPlacementResourceSnapshotName(placementPolicy.GetName(), placementPolicy.GetUID(), 0))
			objs = append(objs, placementPolicy, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: appNamespaceName}})
			deleteObjectsAndWait(objs)
		})
	})

	Context("snapshot with unexpected owner references", Ordered, func() {
		configMapName := fmt.Sprintf(configMapNameTemplate, utils.RandStr())
		placementPolicy := newPlacementPolicy(appNamespaceName, configMapNameSelector(configMapName))

		var pausedMgr *Manager
		// The snapshot names depend on the UID of the placement policy; they are set once the policy is created.
		var snapshotName0, snapshotName1 string

		BeforeAll(func() {
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: appNamespaceName},
				Data:       map[string]string{"key": "value"},
			}
			Expect(hubClient.Create(ctx, configMap)).To(Succeed(), "Failed to create the config map")
			Expect(hubClient.Create(ctx, placementPolicy)).To(Succeed(), "Failed to create the placement policy")
			snapshotName0 = uniquename.PrimaryPlacementResourceSnapshotName(placementPolicy.Name, placementPolicy.UID, 0)
			snapshotName1 = uniquename.PrimaryPlacementResourceSnapshotName(placementPolicy.Name, placementPolicy.UID, 1)
			pausedMgr = newPausedManager()
		})

		It("can create a snapshot", func() {
			snapshotAndVerify(pausedMgr, placementPolicy, []string{snapshotName0})
		})

		It("can add a second owner reference to the snapshot", func() {
			configMap := &corev1.ConfigMap{}
			Expect(hubUncachedReader.Get(ctx, types.NamespacedName{Namespace: appNamespaceName, Name: configMapName}, configMap)).To(Succeed(),
				"Failed to get the config map")
			ownerRefs := append(wantPlacementPolicyOwnerRefs(placementPolicy), metav1.OwnerReference{
				APIVersion: "v1",
				Kind:       "ConfigMap",
				Name:       configMap.Name,
				UID:        configMap.UID,
			})
			setSnapshotOwnerRefs(appNamespaceName, snapshotName0, ownerRefs)
		})

		It("should block snapshotting with a user error that names the snapshot", func() {
			gotSnapshots, isUpToDate, err := pausedMgr.SnapshotResourcesIfStale(ctx, placementPolicy)
			verifySnapshotCallFailed(gotSnapshots, isUpToDate, err, kferrors.ErrCategoryUser, unexpectedOwnerRefsErrMsg, snapshotName0)
			gotSnapshots, isUpToDate, err = pausedMgr.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
			verifySnapshotCallFailed(gotSnapshots, isUpToDate, err, kferrors.ErrCategoryUser, unexpectedOwnerRefsErrMsg, snapshotName0)
			verifyGCRequests(drainGCRequests(pausedMgr), nil)
		})

		It("should keep blocking snapshotting after a resource change", func() {
			updateConfigMapData(appNamespaceName, configMapName, "changed")
			gotSnapshots, isUpToDate, err := pausedMgr.SnapshotResourcesIfStale(ctx, placementPolicy)
			verifySnapshotCallFailed(gotSnapshots, isUpToDate, err, kferrors.ErrCategoryUser, unexpectedOwnerRefsErrMsg, snapshotName0)
			verifyNoPrimarySnapshotAtIdx(placementPolicy, 1)
			verifyGCRequests(drainGCRequests(pausedMgr), nil)
		})

		It("can remove the extra owner reference", func() {
			setSnapshotOwnerRefs(appNamespaceName, snapshotName0, wantPlacementPolicyOwnerRefs(placementPolicy))
		})

		It("should create a snapshot for the resource change", func() {
			snapshotAndVerify(pausedMgr, placementPolicy, []string{snapshotName1})
			verifyGCRequests(drainGCRequests(pausedMgr), nil)
		})

		AfterAll(func() {
			if pausedMgr != nil {
				pausedMgr.gcwq.ShutDown()
			}
			objs := snapshotObjs(appNamespaceName, snapshotName0, snapshotName1)
			objs = append(objs, placementPolicy, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: appNamespaceName}})
			deleteObjectsAndWait(objs)
		})
	})

	Context("leftover secondary snapshots without a primary snapshot", Ordered, func() {
		configMapName := fmt.Sprintf(configMapNameTemplate, utils.RandStr())
		placementPolicy := newPlacementPolicy(appNamespaceName, configMapNameSelector(configMapName))

		// The leftover secondary snapshots mimic ones created by a previous placement policy of the same name in
		// interrupted snapshot creation attempts, and then orphaned; they are named after the previous placement policy.
		previousPlacementPolicyUID := types.UID(utils.RandStr())
		leftoverSnapshotName0 := uniquename.SecondaryPlacementResourceSnapshotName(placementPolicy.Name, previousPlacementPolicyUID, 0, 1)
		leftoverSnapshotName1 := uniquename.SecondaryPlacementResourceSnapshotName(placementPolicy.Name, previousPlacementPolicyUID, 1, 1)

		// The snapshot names depend on the UID of the placement policy; they are set once the policy is created.
		var snapshotName0, snapshotName1 string

		BeforeAll(func() {
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: appNamespaceName},
				Data:       map[string]string{"key": "value"},
			}
			Expect(hubClient.Create(ctx, configMap)).To(Succeed(), "Failed to create the config map")

			// The leftover secondary snapshots are indexed (they carry the owned-by annotation and the index labels),
			// but have no owners.
			createDecoySnapshot(appNamespaceName, leftoverSnapshotName0, decoySecondaryLabels(placementPolicy, "0", "1"), decoyOwnedByAnnotations(placementPolicy), nil)
			createDecoySnapshot(appNamespaceName, leftoverSnapshotName1, decoySecondaryLabels(placementPolicy, "1", "1"), decoyOwnedByAnnotations(placementPolicy), nil)
			getSnapshotFromCache(appNamespaceName, leftoverSnapshotName0)
			getSnapshotFromCache(appNamespaceName, leftoverSnapshotName1)

			Expect(hubClient.Create(ctx, placementPolicy)).To(Succeed(), "Failed to create the placement policy")
			snapshotName0 = uniquename.PrimaryPlacementResourceSnapshotName(placementPolicy.Name, placementPolicy.UID, 0)
			snapshotName1 = uniquename.PrimaryPlacementResourceSnapshotName(placementPolicy.Name, placementPolicy.UID, 1)
		})

		It("should not block snapshotting, but clean up the leftover secondary snapshot at index 0 first", func() {
			// Only primary snapshots are checked for leftovers; the leftover secondary snapshot at index 0 is cleaned
			// up as an orphan right before the manager creates snapshots at index 0.
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfStale(ctx, placementPolicy)
			verifySnapshotCallFailed(gotSnapshots, isUpToDate, err, kferrors.ErrCategoryTransient, orphansCleanedUpErrMsg)
			waitForSnapshotGoneFromCache(appNamespaceName, leftoverSnapshotName0)
			verifySnapshotExists(appNamespaceName, leftoverSnapshotName1)
		})

		It("should create a snapshot at index 0, leaving the leftover secondary snapshot at index 1 behind", func() {
			snapshotAndVerify(resourceSnapshotManager, placementPolicy, []string{snapshotName0})
			verifySnapshotExists(appNamespaceName, leftoverSnapshotName1)
		})

		It("should clean up the leftover secondary snapshot at index 1 before creating a snapshot there", func() {
			updateConfigMapData(appNamespaceName, configMapName, "changed")
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfStale(ctx, placementPolicy)
			verifySnapshotCallFailed(gotSnapshots, isUpToDate, err, kferrors.ErrCategoryTransient, orphansCleanedUpErrMsg)
			waitForSnapshotGoneFromCache(appNamespaceName, leftoverSnapshotName1)

			snapshotAndVerify(resourceSnapshotManager, placementPolicy, []string{snapshotName1})
		})

		AfterAll(func() {
			objs := snapshotObjs(appNamespaceName, leftoverSnapshotName0, leftoverSnapshotName1, snapshotName0, snapshotName1)
			objs = append(objs, placementPolicy, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: appNamespaceName}})
			deleteObjectsAndWait(objs)
		})
	})
})
