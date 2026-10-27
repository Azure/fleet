# Fix: Preserve UpdateRun Condition Transition Timestamp

## Overview

Prevent hub-agent reconciliation and restarts from restamping the UpdateRun status metric when the selected Kubernetes condition has not transitioned.

## Plan

1. Trace UpdateRun condition reconciliation and metric emission.
2. Emit the selected condition's persisted `LastTransitionTime` instead of reconciliation time.
3. Add focused unit tests for unchanged failed conditions and actual status transitions.
4. Run the smallest relevant UpdateRun unit test suite.

## Success Criteria

- [x] Re-emitting an unchanged failed condition preserves the metric timestamp.
- [x] A condition status transition emits the new transition timestamp.
- [x] Focused UpdateRun tests pass.

## Implementation Notes

- Kubernetes `meta.SetStatusCondition` already preserves `LastTransitionTime` when `Status` is unchanged and advances it when `Status` changes.
- The restamping occurred in metric emission through `SetToCurrentTime()`, independently of the persisted condition.
- `emitUpdateRunStatusMetric` now sets the gauge from the selected condition's `LastTransitionTime`.
- Added table-driven unit coverage for failed, initialized, executing, and succeeded runs. Each case emits twice to prove reconciliation does not restamp the selected condition's timestamp.
- Validation: `go test ./pkg/controllers/updaterun -run 'TestEmitUpdateRunStatusMetricUsesConditionTransitionTime|TestDetermineFailureType|TestIsFailureReason' -count=1`.
