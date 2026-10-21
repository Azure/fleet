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

	// The expected limits on generated snapshot names and owned-by label values; they are spelled out here (rather
	// than reusing the constants of the package) so that the tests catch unintended changes.
	wantMaxSnapshotNameLen          = 251
	wantOwnedByLabelValLen          = 61
	wantOwnedByLabelValPrefixLen    = 48
	wantHashSuffixPattern           = `^[0-9a-f]{12}$`
	wantHashedPrimaryNamePattern    = `-resource-snapshot-0-[0-9a-f]{12}$`
	wantHashedSecondaryNamePattern  = `-resource-snapshot-0-1-[0-9a-f]{12}$`
	concurrentSnapshotCallerCnt     = 8
	malformedPrimarySnapshotSuffix  = "-malformed"
	placementPolicyNameFillerLetter = "a"
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
			primarySnapshotName0 := uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicy.GetName(), 0)
			primarySnapshotName1 := uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicy.GetName(), 1)

			var hash string

			BeforeAll(func() {
				configMap := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: appNamespaceName},
					Data:       map[string]string{"key": "value"},
				}
				Expect(hubClient.Create(ctx, configMap)).To(Succeed(), "Failed to create the config map")
				Expect(hubClient.Create(ctx, placementPolicy)).To(Succeed(), "Failed to create the placement policy")
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
		primarySnapshotName := uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicy.Name, 0)

		var decoyUID types.UID

		BeforeAll(func() {
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: appNamespaceName},
				Data:       map[string]string{"key": "value"},
			}
			Expect(hubClient.Create(ctx, configMap)).To(Succeed(), "Failed to create the config map")
			Expect(hubClient.Create(ctx, placementPolicy)).To(Succeed(), "Failed to create the placement policy")

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
		wantSnapshotNames := []string{
			uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicy.Name, 0),
			uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicy.Name, 0, 1),
			uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicy.Name, 0, 2),
		}

		var wantHash string

		BeforeAll(func() {
			configMapNames = createLabeledConfigMaps(configMapNamePrefix, 7, groupLabels)
			Expect(hubClient.Create(ctx, placementPolicy)).To(Succeed(), "Failed to create the placement policy")
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
		primarySnapshotName := uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicy.Name, 0)
		secondarySnapshotName := uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicy.Name, 0, 1)

		BeforeAll(func() {
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: appNamespaceName},
				Data:       map[string]string{"key": "value"},
			}
			Expect(hubClient.Create(ctx, configMap)).To(Succeed(), "Failed to create the config map")
			Expect(hubClient.Create(ctx, placementPolicy)).To(Succeed(), "Failed to create the placement policy")

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
		primarySnapshotName := uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicy.Name, 0)
		secondarySnapshotName := uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicy.Name, 0, 1)

		BeforeAll(func() {
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: appNamespaceName},
				Data:       map[string]string{"key": "value"},
			}
			Expect(hubClient.Create(ctx, configMap)).To(Succeed(), "Failed to create the config map")
			Expect(hubClient.Create(ctx, placementPolicy)).To(Succeed(), "Failed to create the placement policy")

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
		wantSnapshotNames := []string{
			uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicy.Name, 0),
			uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicy.Name, 0, 1),
		}
		extraSecondarySnapshotName := uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicy.Name, 0, 2)

		// A placement policy that selects a single resource; its snapshot features a count label of 1.
		singleGroupPlacementPolicy := newPlacementPolicy(appNamespaceName, configMapNameSelector(configMapNamePrefix+"-0"))
		singleGroupPrimarySnapshotName := uniqueNameForPrimaryPlacementResourceSnapshot(singleGroupPlacementPolicy.Name, 0)
		singleGroupExtraSecondarySnapshotName := uniqueNameForSecondaryPlacementResourceSnapshot(singleGroupPlacementPolicy.Name, 0, 1)

		BeforeAll(func() {
			configMapNames = createLabeledConfigMaps(configMapNamePrefix, 4, groupLabels)
			Expect(hubClient.Create(ctx, placementPolicy)).To(Succeed(), "Failed to create the placement policy")
			Expect(hubClient.Create(ctx, singleGroupPlacementPolicy)).To(Succeed(), "Failed to create the placement policy")
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
		wantSnapshotNames := []string{
			uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicy.Name, 0),
			uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicy.Name, 0, 1),
		}

		var originalPrimarySnapshotUID types.UID

		BeforeAll(func() {
			configMapNames = createLabeledConfigMaps(configMapNamePrefix, 4, groupLabels)
			Expect(hubClient.Create(ctx, placementPolicy)).To(Succeed(), "Failed to create the placement policy")
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
			return uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicy.GetName(), 0)
		}
		secondaryName := func(placementPolicy placementv1alpha1.PlacementPolicyAccessor) string {
			return uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicy.GetName(), 0, 1)
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
					createDecoySnapshot(appNamespaceName, decoy.name, decoy.labels, decoy.annotations, nil)
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
		wantSnapshotNames0 := []string{
			uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicy.Name, 0),
			uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicy.Name, 0, 1),
		}
		wantSnapshotNames1 := []string{
			uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicy.Name, 1),
			uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicy.Name, 1, 1),
		}

		BeforeAll(func() {
			configMapNames = createLabeledConfigMaps(configMapNamePrefix, 4, groupLabels)
			Expect(hubClient.Create(ctx, placementPolicy)).To(Succeed(), "Failed to create the placement policy")
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
			desc                string
			placementPolicy     placementv1alpha1.PlacementPolicyAccessor
			wantLabelTruncated  bool
			wantPrimaryHashed   bool
			wantSecondaryHashed bool
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
				desc:               "the longest name with plain snapshot names (229 characters)",
				placementPolicy:    newNamedPlacementPolicy(229),
				wantLabelTruncated: true,
			},
			{
				desc:                "the shortest name with a hashed secondary snapshot name (230 characters)",
				placementPolicy:     newNamedPlacementPolicy(230),
				wantLabelTruncated:  true,
				wantSecondaryHashed: true,
			},
			{
				desc:                "the longest name with a plain primary snapshot name (231 characters)",
				placementPolicy:     newNamedPlacementPolicy(231),
				wantLabelTruncated:  true,
				wantSecondaryHashed: true,
			},
			{
				desc:                "the shortest name with a hashed primary snapshot name (232 characters)",
				placementPolicy:     newNamedPlacementPolicy(232),
				wantLabelTruncated:  true,
				wantPrimaryHashed:   true,
				wantSecondaryHashed: true,
			},
			{
				desc:                "the longest valid name (253 characters)",
				placementPolicy:     newNamedPlacementPolicy(253),
				wantLabelTruncated:  true,
				wantPrimaryHashed:   true,
				wantSecondaryHashed: true,
			},
			{
				desc:                "the longest valid name of a cluster placement policy (253 characters)",
				placementPolicy:     newNamedClusterPlacementPolicy(253),
				wantLabelTruncated:  true,
				wantPrimaryHashed:   true,
				wantSecondaryHashed: true,
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
					uniqueNameForPrimaryPlacementResourceSnapshot(name, 0),
					uniqueNameForSecondaryPlacementResourceSnapshot(name, 0, 1),
				}

				gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
				verifyReturnedSnapshots(gotSnapshots, isUpToDate, err, wantSnapshotNames)

				verifySnapshotName(wantSnapshotNames[0], fmt.Sprintf(PrimaryPlacementResourceSnapshotNameFmt, name, 0),
					wantHashedPrimaryNamePattern, tc.wantPrimaryHashed)
				verifySnapshotName(wantSnapshotNames[1], fmt.Sprintf(SecondaryPlacementResourceSnapshotNameFmt, name, 0, 1),
					wantHashedSecondaryNamePattern, tc.wantSecondaryHashed)
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
				snapshotNS := tc.placementPolicy.GetNamespace()
				objs = append(objs,
					placementResourceSnapshotObj(snapshotNS, uniqueNameForPrimaryPlacementResourceSnapshot(name, 0)),
					placementResourceSnapshotObj(snapshotNS, uniqueNameForSecondaryPlacementResourceSnapshot(name, 0, 1)),
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

		wantSnapshotNames0 := []string{
			uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicy.Name, 0),
			uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicy.Name, 0, 1),
		}
		wantSnapshotNames1 := []string{
			uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicy.Name, 1),
			uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicy.Name, 1, 1),
		}

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
		})

		It("can create a primary and a secondary snapshot with hashed names", func() {
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
			verifyReturnedSnapshots(gotSnapshots, isUpToDate, err, wantSnapshotNames0)
			verifySnapshotName(wantSnapshotNames0[0], fmt.Sprintf(PrimaryPlacementResourceSnapshotNameFmt, placementPolicy.Name, 0),
				wantHashedPrimaryNamePattern, true)
			verifySnapshotName(wantSnapshotNames0[1], fmt.Sprintf(SecondaryPlacementResourceSnapshotNameFmt, placementPolicy.Name, 0, 1),
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
			wantHashed      bool
			// The wanted prefix of the (hashed) primary snapshot name.
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
				wantHashed:            true,
				wantPrimaryNamePrefix: "policy" + suffix + "v1-resource-snapshot-0-",
			},
			{
				placementPolicy:       newNamedPlacementPolicy("ab.c" + suffix),
				wantHashed:            true,
				wantPrimaryNamePrefix: "abc" + suffix + "-resource-snapshot-0-",
			},
			{
				placementPolicy:       newNamedPlacementPolicy("a.bc" + suffix),
				wantHashed:            true,
				wantPrimaryNamePrefix: "abc" + suffix + "-resource-snapshot-0-",
			},
			{
				placementPolicy: newNamedPlacementPolicy("abc" + suffix),
			},
		}

		wantSnapshotNamesOf := func(placementPolicy placementv1alpha1.PlacementPolicyAccessor) []string {
			return []string{
				uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicy.GetName(), 0),
				uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicy.GetName(), 0, 1),
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

				verifySnapshotName(wantSnapshotNames[0], fmt.Sprintf(PrimaryPlacementResourceSnapshotNameFmt, placementPolicy.Name, 0),
					wantHashedPrimaryNamePattern, tc.wantHashed)
				verifySnapshotName(wantSnapshotNames[1], fmt.Sprintf(SecondaryPlacementResourceSnapshotNameFmt, placementPolicy.Name, 0, 1),
					wantHashedSecondaryNamePattern, tc.wantHashed)
				if tc.wantHashed {
					Expect(wantSnapshotNames[0]).To(HavePrefix(tc.wantPrimaryNamePrefix), "Unexpected prefix of the hashed snapshot name")
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
				verifySnapshotName(gotSnapshots[0].GetName(), fmt.Sprintf(PrimaryPlacementResourceSnapshotNameFmt, placementPolicy.Name, 0),
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
				wantSnapshotNames := []string{uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicy.Name, 0)}
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
		primarySnapshotName0 := uniqueNameForPrimaryPlacementResourceSnapshot(name, 0)

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
			// The snapshots share the same name, but live in different namespaces (or at the cluster scope).
			for _, placementPolicy := range placementPolicies {
				gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfNoSnapshotExists(ctx, placementPolicy)
				verifyReturnedSnapshots(gotSnapshots, isUpToDate, err, []string{primarySnapshotName0})
				getPrimarySnapshotFromCache(placementPolicy, 0)
			}
		})

		It("can update the config map in the app namespace", func() {
			updateConfigMapData(appNamespaceName, configMapName, "changed")
		})

		It("should create a new snapshot only for the placement policy that selects the updated config map", func() {
			gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfStale(ctx, appPlacementPolicy)
			verifyReturnedSnapshots(gotSnapshots, isUpToDate, err, []string{uniqueNameForPrimaryPlacementResourceSnapshot(name, 1)})

			for _, placementPolicy := range []placementv1alpha1.PlacementPolicyAccessor{nsPlacementPolicy, clusterPlacementPolicy} {
				gotSnapshots, isUpToDate, err := resourceSnapshotManager.SnapshotResourcesIfStale(ctx, placementPolicy)
				verifyReturnedSnapshots(gotSnapshots, isUpToDate, err, []string{primarySnapshotName0})
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

		wantSnapshotNamesA0 := []string{
			uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyA.Name, 0),
			uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicyA.Name, 0, 1),
		}
		wantSnapshotNamesA1 := []string{
			uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyA.Name, 1),
			uniqueNameForSecondaryPlacementResourceSnapshot(placementPolicyA.Name, 1, 1),
		}
		wantSnapshotNamesB0 := []string{uniqueNameForPrimaryPlacementResourceSnapshot(placementPolicyB.Name, 0)}

		BeforeAll(func() {
			configMapNames = append(createLabeledConfigMaps(configMapNamePrefixA, 4, groupALabels),
				createLabeledConfigMaps(configMapNamePrefixB, 1, groupBLabels)...)
			Expect(hubClient.Create(ctx, placementPolicyA)).To(Succeed(), "Failed to create placement policy A")
			Expect(hubClient.Create(ctx, placementPolicyB)).To(Succeed(), "Failed to create placement policy B")
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
