# Support Before-Stage Tasks on the Delete Stage

## Overview

Allow staged update strategies to gate the delete stage with an Approval task, the same before-stage task the
update stages support, so that removing the resources from the clusters that left a placement can be held until
a sign-off is given.

## Plan

1. Add an optional `deleteStage` to the shared update strategy API, with `beforeStageTasks`.
2. Snapshot the tasks and initialize their statuses as the `beforeStageTaskStatus` of the delete stage.
3. Evaluate the tasks with the same code that evaluates the before-stage tasks of the update stages.
4. Add API validation, unit, integration and E2E coverage for both cluster-scoped and namespaced runs.
5. Regenerate API code and CRDs, then run the tests and the repository quality checks.

## Decisions

- The delete stage is configured as a stage (`deleteStage.beforeStageTasks`) rather than with a delete stage
  specific task list, so that the API, the status and the approval requests use the same vocabulary as the
  update stages, and so that the delete stage can take more configurations later.
- The before-stage tasks of the delete stage follow the update stages: at most one task, and it must be an
  Approval. A soak time before the removal is already possible with a TimedWait in the afterStageTasks of the
  last update stage, as the delete stage only starts once the last update stage, including its after-stage tasks,
  completes.
- The approval request is named `<updateRun>-before-delete-stage` with the generic before-stage name format.
  `delete-stage` also is the stage label value of the approval request, as the name of the delete stage,
  `kubernetes-fleet.io/deleteStage`, is not a valid label value. An update stage cannot have this name.
- The tasks only gate the start of the deletion and they are skipped if there is no cluster to delete, the same
  as an update stage with no clusters.
- An update run that is stopped while it waits for the tasks stops without deleting any binding.

## Success Criteria

- [x] No delete stage configuration preserves immediate deletion.
- [x] The approval must be given before any binding is deleted.
- [x] Cluster-scoped and namespaced runs keep the bindings until the tasks pass.
- [x] Generated API and CRD artifacts are current.

## Follow-ups

- Document the delete stage configuration in the staged update docs (website repository).
