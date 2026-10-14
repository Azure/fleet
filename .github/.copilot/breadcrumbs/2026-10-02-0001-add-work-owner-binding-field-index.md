# Add Work Owner Binding Field Index

**Date**: October 2, 2026 00:01 AEST
**Task**: Add a composite field index for Work objects keyed by owner binding namespace and name.

## Requirements

1. Add the `workOwnedByBinding` custom field.
2. Format indexed values as `%s/%s`.
3. Extract the owner namespace from `WorkOwnerNamespaceLabelKey`.
4. Extract the owner binding name from `WorkOwnedByPlacementBindingLabelKey`.
5. Register the Work field extractor in `SetupWithHubControllerManager`.
6. Use the custom field index when listing Work objects by owner binding.
7. Use the existing owner-and-index custom field when listing secondary placement resource snapshots.

## Plan

1. Add exported field-name and value-format constants to `pkg/v1/utils/fieldindexers/hub.go`.
2. Add a Work owner-binding field extractor using the owner namespace and binding labels, while distinguishing a missing owner-namespace label from a valid empty cluster-scoped namespace.
3. Register the extractor for `placementv1alpha1.Work` in `SetupWithHubControllerManager`.
4. Update `listWorksByOwnerBinding` to query the composite custom field while retaining the Work namespace filter.
5. Update secondary placement resource snapshot retrieval to query the existing composite owner-and-index field.
6. Run the targeted field-indexer and workgenerator package tests and formatting.

## Decisions

1. Use the owner-binding label as the indexed binding-name source.
2. Permit an empty owner namespace only when the owner-namespace label is present, preserving support for cluster-scoped bindings.
3. Unit tests are omitted for now at the user's request.

## Status

Implementation complete.

## Validation

- `gofmt -w pkg/v1/utils/fieldindexers/hub.go pkg/v1/controllers/workgenerator/retrieval.go`
- `go test ./pkg/v1/utils/fieldindexers ./pkg/v1/controllers/workgenerator` (passed; packages currently have no test files)
- `git diff --check` (passed)
