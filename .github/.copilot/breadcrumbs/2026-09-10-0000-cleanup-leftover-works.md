# Cleanup Leftover Works

## Goal

Implement `cleanupLeftOverWorks` so manifests previously applied or prepared by leftover Work objects are removed unless still referenced by the current linked Work set.

## Plan

- [x] Collect manifest identifiers whose Applied condition is true or has the preparing-to-process reason.
- [x] Exclude identifiers present in `seenManifIDs`.
- [x] Remove the collected manifests in parallel through `removeOneLeftOverManifest`.
- [x] Aggregate per-manifest failures and run focused validation.

## Success Criteria

- Current manifests are preserved.
- Only previously applied or pre-processed leftover manifests are selected.
- Selected manifests are removed through the existing ownership-aware helper in one parallel batch.

## Validation

- `gofmt` completed successfully.
- Editor diagnostics report no errors in `cleanup.go`.
- `go test ./pkg/v1/controllers/workapplier` is currently blocked by pre-existing unused `leftOverWorks` and `seenManifestIDs` variables in `controller.go`, where the surrounding cleanup call site is still unfinished.

## Placement Deletion Follow-Up

- [ ] Preserve explicit sync strategy values while applying defaults.
- [ ] Delete linked AppliedWork objects with foreground or orphan propagation according to `WhenPlacementDeleted`.
- [ ] After `foregroundDeletionWaitTime`, explicitly clean tracked manifests blocking foreground deletion.
- [ ] Remove Work cleanup finalizers only after the corresponding AppliedWork is gone.
- [ ] Wire placement and leftover cleanup into reconciliation and validate the package.
