# Troubleshooting guide

## Overview:

This TSG is meant to help you troubleshoot issues with the Fleet APIs.

## Cluster Resource Placement:

Internal Objects to keep in mind when troubleshooting CRP related errors on the hub cluster:
 - **ClusterResourceSnapshot**
 - **ClusterSchedulingPolicySnapshot**
 - **ClusterResourceBinding**
 - **Work** 

please read the API reference for more details about ech object https://github.com/Azure/fleet/blob/main/docs/api-references.md

### How can I debug when my CRP status is ClusterResourcePlacementScheduled "Failed" or false?

We need to take a look at the **ClusterSchedulingPolicySnapshot** status to figure out why the scheduler could not schedule the resource for the placement policy specified.

### How to find the latest ClusterSchedulingSnapshot resource?

### How can I debug when my CRP status is synchronized "Failed"?

In the **ClusterResourcePlacement** status section check to see which **ResourcePlacementStatuses** also has WorkSynchronized condition as false.

From the ResourcePlacementStatus we can get the ClusterName and then check the fleet-member-{ClusterName} namespace to see if a work objects exists/updated in this case it won't as WorkSynchronized has failed.

We need to find the corresponding latest ClusterResourceBinding which should have the status of work create/update. 

### How to find the latest ClusterResourceBinding resource?

### How can I debug when my CRP status is applied "Failed"?

In the **ClusterResourcePlacement** status section check to see which ResourcePlacementStatuses also has ResourceApplied condition as false.

From the ResourcePlacementStatus we can get the ClusterName and then use it to find the work object associated with the member cluster in the fleet-member-{ClusterName} namespace in the hub cluster and check its status to figure out what's wrong.

### How and where to find the correct Work resource?

### How can I debug when some clusters are not selected as expected?

Check the status of the **ClusterSchedulingPolicySnapshot** to determine which clusters were selected along with the reason

### How can I debug when a selected cluster does not have the expected resources on it?

We need to take a look at the ResourcePlacementStatus for that particular cluster in CRP status. In ResourcePlacementStatus we would find failedPlacements which should have the reason

### How can I debug when my CRP doesn't pick up the latest change?