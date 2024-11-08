# Tutorial: Migrating Application Resources to Clusters with More Availability
This tutorial shows how to migrate applications from clusters with lower availability to those with higher availability, 
while also scaling up the number of replicas, using Fleet.

## Scenario
Your fleet consists of the following clusters:

1. Member Cluster 1 & Member Cluster 2 (WestUS, 1 node each)
2. Member Cluster 3 (EastUS2, 2 nodes)
3. Member Cluster 4 & Member Cluster 5 (WestEurope, 3 nodes each)

Due to a sudden increase in traffic and resource demands in your WestUS clusters, you need to migrate your applications to clusters in EastUS2 or WestEurope that have higher availability and can better handle the increased load.

## Current Application Resources
The following resources are currently deployed in the WestUS clusters:

#### Service

> Note: Service test file located [here](./testfiles/nginx-service.yaml).

```yaml
apiVersion: v1
kind: Service
metadata:
  name: nginx-service
  namespace: test-app
spec:
  selector:
    app: nginx
  ports:
  - protocol: TCP
    port: 80
    targetPort: 80
  type: LoadBalancer
```
Summary:
- This defines a Kubernetes Service named `nginx-svc` in the `test-app` namespace.
- The service is of type LoadBalancer, meaning it exposes the application to the internet.
- It targets pods with the label app: nginx and forwards traffic to port 80 on the pods.

#### Deployment

> Note: Deployment test file located [here](./testfiles/nginx-deployment.yaml).

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: nginx-deployment
  namespace: test-app
spec:
  selector:
    matchLabels:
      app: nginx
  replicas: 2
  template:
    metadata:
      labels:
        app: nginx
    spec:
      containers:
      - name: nginx
        image: nginx:1.16.1 
        ports:
        - containerPort: 80
```
> Note: The current deployment has 2 replicas.

Summary:
- This defines a Kubernetes Deployment named `nginx-deployment` in the `test-app` namespace.
- It creates 2 replicas of the nginx pod, each running the `nginx:1.16.1` image.
- The deployment ensures that the specified number of pods (replicas) are running and available.
- The pods are labeled with `app: nginx` and expose port 80.

#### ClusterResourcePlacement

CRP Availability test file located [here](./testfiles/crp-availability.yaml)

```yaml
apiVersion: placement.kubernetes-fleet.io/v1
kind: ClusterResourcePlacement
metadata:
  annotations:
    kubectl.kubernetes.io/last-applied-configuration: |
      {"apiVersion":"placement.kubernetes-fleet.io/v1","kind":"ClusterResourcePlacement","metadata":{"annotations":{},"name":"crp-availability"},"spec":{"policy":{"affinity":{"clusterAffinity":{"requiredDuringSchedulingIgnoredDuringExecution":{"clusterSelectorTerms":[{"labelSelector":{"matchLabels":{"fleet.azure.com/location":"westus"}}}]}}},"numberOfClusters":2,"placementType":"PickN"},"resourceSelectors":[{"group":"","kind":"Namespace","name":"test-app","version":"v1"}],"revisionHistoryLimit":10,"strategy":{"type":"RollingUpdate"}}}
  creationTimestamp: "2024-07-25T23:00:53Z"
  finalizers:
    - kubernetes-fleet.io/crp-cleanup
    - kubernetes-fleet.io/scheduler-cleanup
  generation: 1
  name: crp-availability
  resourceVersion: "22228766"
  uid: 58dbb5d1-4afa-479f-bf57-413328aa61bd
spec:
  policy:
    affinity:
      clusterAffinity:
        requiredDuringSchedulingIgnoredDuringExecution:
          clusterSelectorTerms:
            - labelSelector:
                matchLabels:
                  fleet.azure.com/location: westus
    numberOfClusters: 2
    placementType: PickN
  resourceSelectors:
    - group: ""
      kind: Namespace
      name: test-app
      version: v1
  revisionHistoryLimit: 10
  strategy:
    type: RollingUpdate
status:
  conditions:
    - lastTransitionTime: "2024-07-25T23:00:53Z"
      message: found all cluster needed as specified by the scheduling policy, found
        2 cluster(s)
      observedGeneration: 1
      reason: SchedulingPolicyFulfilled
      status: "True"
      type: ClusterResourcePlacementScheduled
    - lastTransitionTime: "2024-07-25T23:00:53Z"
      message: All 2 cluster(s) start rolling out the latest resource
      observedGeneration: 1
      reason: RolloutStarted
      status: "True"
      type: ClusterResourcePlacementRolloutStarted
    - lastTransitionTime: "2024-07-25T23:00:53Z"
      message: No override rules are configured for the selected resources
      observedGeneration: 1
      reason: NoOverrideSpecified
      status: "True"
      type: ClusterResourcePlacementOverridden
    - lastTransitionTime: "2024-07-25T23:00:53Z"
      message: Works(s) are succcesfully created or updated in 2 target cluster(s)'
        namespaces
      observedGeneration: 1
      reason: WorkSynchronized
      status: "True"
      type: ClusterResourcePlacementWorkSynchronized
    - lastTransitionTime: "2024-07-25T23:00:53Z"
      message: The selected resources are successfully applied to 2 cluster(s)
      observedGeneration: 1
      reason: ApplySucceeded
      status: "True"
      type: ClusterResourcePlacementApplied
    - lastTransitionTime: "2024-07-25T23:01:02Z"
      message: The selected resources in 2 cluster(s) are available now
      observedGeneration: 1
      reason: ResourceAvailable
      status: "True"
      type: ClusterResourcePlacementAvailable
  observedResourceIndex: "0"
  placementStatuses:
    - clusterName: aks-member-2
      conditions:
        - lastTransitionTime: "2024-07-25T23:00:53Z"
          message: 'Successfully scheduled resources for placement in "aks-member-2"
        (affinity score: 0, topology spread score: 0): picked by scheduling policy'
          observedGeneration: 1
          reason: Scheduled
          status: "True"
          type: Scheduled
        - lastTransitionTime: "2024-07-25T23:00:53Z"
          message: Detected the new changes on the resources and started the rollout process
          observedGeneration: 1
          reason: RolloutStarted
          status: "True"
          type: RolloutStarted
        - lastTransitionTime: "2024-07-25T23:00:53Z"
          message: No override rules are configured for the selected resources
          observedGeneration: 1
          reason: NoOverrideSpecified
          status: "True"
          type: Overridden
        - lastTransitionTime: "2024-07-25T23:00:53Z"
          message: All of the works are synchronized to the latest
          observedGeneration: 1
          reason: AllWorkSynced
          status: "True"
          type: WorkSynchronized
        - lastTransitionTime: "2024-07-25T23:00:53Z"
          message: All corresponding work objects are applied
          observedGeneration: 1
          reason: AllWorkHaveBeenApplied
          status: "True"
          type: Applied
        - lastTransitionTime: "2024-07-25T23:01:02Z"
          message: All corresponding work objects are available
          observedGeneration: 1
          reason: AllWorkAreAvailable
          status: "True"
          type: Available
    - clusterName: aks-member-1
      conditions:
        - lastTransitionTime: "2024-07-25T23:00:53Z"
          message: 'Successfully scheduled resources for placement in "aks-member-1"
        (affinity score: 0, topology spread score: 0): picked by scheduling policy'
          observedGeneration: 1
          reason: Scheduled
          status: "True"
          type: Scheduled
        - lastTransitionTime: "2024-07-25T23:00:53Z"
          message: Detected the new changes on the resources and started the rollout process
          observedGeneration: 1
          reason: RolloutStarted
          status: "True"
          type: RolloutStarted
        - lastTransitionTime: "2024-07-25T23:00:53Z"
          message: No override rules are configured for the selected resources
          observedGeneration: 1
          reason: NoOverrideSpecified
          status: "True"
          type: Overridden
        - lastTransitionTime: "2024-07-25T23:00:53Z"
          message: All of the works are synchronized to the latest
          observedGeneration: 1
          reason: AllWorkSynced
          status: "True"
          type: WorkSynchronized
        - lastTransitionTime: "2024-07-25T23:00:53Z"
          message: All corresponding work objects are applied
          observedGeneration: 1
          reason: AllWorkHaveBeenApplied
          status: "True"
          type: Applied
        - lastTransitionTime: "2024-07-25T23:01:02Z"
          message: All corresponding work objects are available
          observedGeneration: 1
          reason: AllWorkAreAvailable
          status: "True"
          type: Available
  selectedResources:
    - kind: Namespace
      name: test-app
      version: v1
    - group: apps
      kind: Deployment
      name: nginx-deployment
      namespace: test-app
      version: v1
    - kind: Service
      name: nginx-service
      namespace: test-app
      version: v1
```
Summary:
- This defines a ClusterResourcePlacement named `crp-availability`.
- The placement policy PickN selects 2 clusters. The clusters are selected based on the label `fleet.azure.com/location: westus`.
- It targets resources in the `test-app` namespace.

### Identify Clusters with More Availability
To identify clusters with more availability, you can check the member cluster properties.
```bash
kubectl get memberclusters -A -o wide
```
The output will show the availability in each cluster, including the number of nodes, available CPU, and memory.
```bash
NAME                                JOINED   AGE   NODE-COUNT   AVAILABLE-CPU   AVAILABLE-MEMORY   ALLOCATABLE-CPU   ALLOCATABLE-MEMORY
aks-member-1                        True     22d   1            30m             40Ki               1900m             4652296Ki
aks-member-2                        True     22d   1            30m             40Ki               1900m             4652296Ki
aks-member-3                        True     22d   2            2820m           8477196Ki          3800m             9304588Ki
aks-member-4                        True     22d   3            4408m           12896012Ki         5700m             13956876Ki
aks-member-5                        True     22d   3            4408m           12896024Ki         5700m             13956888Ki
```
Based on the available resources, you can see that Member Cluster 3 in EastUS2 and Member Cluster 4 & 5 in WestEurope have more nodes and available resources compared to the WestUS clusters.

## Migrating Applications to a Different Cluster with More Availability While Scaling Up
When the clusters in WestUS are nearing capacity limits and risk becoming overloaded, update the ClusterResourcePlacement (CRP) to migrate the applications to clusters in EastUS2 or WestEurope, which have more available resources and can handle increased demand more effectively. 
For this tutorial, we will move them to WestEurope.

## Create Resource Override
To scale up during migration, apply this override before updating crp:
```yaml
apiVersion: placement.kubernetes-fleet.io/v1alpha1
kind: ResourceOverride
metadata:
  name: ro-1
  namespace: test-app
spec:
  resourceSelectors:
    -  group: apps
       kind: Deployment
       version: v1
       name: nginx-deployment
  policy:
    overrideRules:
      - clusterSelector:
          clusterSelectorTerms:
            - labelSelector:
                matchLabels:
                  fleet.azure.com/location: westeurope
        jsonPatchOverrides:
          - op: replace
            path: /spec/replicas
            value:
              4
```
This override updates the `nginx-deployment` Deployment in the `test-app` namespace by setting the number of replicas to "4" for clusters located in the westeurope region.

#### Update the CRP for Migration
```yaml
apiVersion: placement.kubernetes-fleet.io/v1
kind: ClusterResourcePlacement
metadata:
  name: crp-availability
spec:
  policy:
    placementType: PickN
    numberOfClusters: 2
    affinity:
      clusterAffinity:
        requiredDuringSchedulingIgnoredDuringExecution:
          clusterSelectorTerms:
            - propertySelector:
                matchExpressions:
                  - name: kubernetes-fleet.io/node-count
                    operator: Ge
                    values:
                      - "3"
  resourceSelectors:
    - group: ""
      kind: Namespace
      name: test-app
      version: v1
  revisionHistoryLimit: 10
  strategy:
    type: RollingUpdate
```
Update the [`crp-availability.yaml`](./testfiles/crp-availability.yaml) to reflect selecting clusters with higher node-count and apply it:
```bash
kubectl apply -f crp-availability.yaml
```

### Results
After applying the updated [`crp-availability.yaml`](./testfiles/crp-availability.yaml), the Fleet will schedule the application on the available clusters in WestEurope as they each have 3 nodes.
You can check the status of the CRP to ensure that the application has been successfully migrated and is running in the new region:
```bash
kubectl get crp crp-availability -o yaml
```
You should see a status indicating that the application is now running in the WestEurope clusters, similar to the following:
#### CRP Status
```yaml
...
status:
  conditions:
    - lastTransitionTime: "2024-07-25T23:10:08Z"
      message: found all cluster needed as specified by the scheduling policy, found
        2 cluster(s)
      observedGeneration: 2
      reason: SchedulingPolicyFulfilled
      status: "True"
      type: ClusterResourcePlacementScheduled
    - lastTransitionTime: "2024-07-25T23:10:20Z"
      message: All 2 cluster(s) start rolling out the latest resource
      observedGeneration: 2
      reason: RolloutStarted
      status: "True"
      type: ClusterResourcePlacementRolloutStarted
    - lastTransitionTime: "2024-07-25T23:10:20Z"
      message: The selected resources are successfully overridden in 2 cluster(s)
      observedGeneration: 2
      reason: OverriddenSucceeded
      status: "True"
      type: ClusterResourcePlacementOverridden
    - lastTransitionTime: "2024-07-25T23:10:20Z"
      message: Works(s) are succcesfully created or updated in 2 target cluster(s)'
        namespaces
      observedGeneration: 2
      reason: WorkSynchronized
      status: "True"
      type: ClusterResourcePlacementWorkSynchronized
    - lastTransitionTime: "2024-07-25T23:10:21Z"
      message: The selected resources are successfully applied to 2 cluster(s)
      observedGeneration: 2
      reason: ApplySucceeded
      status: "True"
      type: ClusterResourcePlacementApplied
    - lastTransitionTime: "2024-07-25T23:10:30Z"
      message: The selected resources in 2 cluster(s) are available now
      observedGeneration: 2
      reason: ResourceAvailable
      status: "True"
      type: ClusterResourcePlacementAvailable
  observedResourceIndex: "0"
  placementStatuses:
    - applicableResourceOverrides:
        - name: ro-1-0
          namespace: test-app
      clusterName: aks-member-5
      conditions:
        - lastTransitionTime: "2024-07-25T23:10:08Z"
          message: 'Successfully scheduled resources for placement in "aks-member-5" (affinity
        score: 0, topology spread score: 0): picked by scheduling policy'
          observedGeneration: 2
          reason: Scheduled
          status: "True"
          type: Scheduled
        - lastTransitionTime: "2024-07-25T23:10:20Z"
          message: Detected the new changes on the resources and started the rollout process
          observedGeneration: 2
          reason: RolloutStarted
          status: "True"
          type: RolloutStarted
        - lastTransitionTime: "2024-07-25T23:10:20Z"
          message: Successfully applied the override rules on the resources
          observedGeneration: 2
          reason: OverriddenSucceeded
          status: "True"
          type: Overridden
        - lastTransitionTime: "2024-07-25T23:10:20Z"
          message: All of the works are synchronized to the latest
          observedGeneration: 2
          reason: AllWorkSynced
          status: "True"
          type: WorkSynchronized
        - lastTransitionTime: "2024-07-25T23:10:21Z"
          message: All corresponding work objects are applied
          observedGeneration: 2
          reason: AllWorkHaveBeenApplied
          status: "True"
          type: Applied
        - lastTransitionTime: "2024-07-25T23:10:30Z"
          message: All corresponding work objects are available
          observedGeneration: 2
          reason: AllWorkAreAvailable
          status: "True"
          type: Available
    - applicableResourceOverrides:
        - name: ro-1-0
          namespace: test-app
      clusterName: aks-member-4
      conditions:
        - lastTransitionTime: "2024-07-25T23:10:08Z"
          message: 'Successfully scheduled resources for placement in "aks-member-4" (affinity
        score: 0, topology spread score: 0): picked by scheduling policy'
          observedGeneration: 2
          reason: Scheduled
          status: "True"
          type: Scheduled
        - lastTransitionTime: "2024-07-25T23:10:08Z"
          message: Detected the new changes on the resources and started the rollout process
          observedGeneration: 2
          reason: RolloutStarted
          status: "True"
          type: RolloutStarted
        - lastTransitionTime: "2024-07-25T23:10:08Z"
          message: Successfully applied the override rules on the resources
          observedGeneration: 2
          reason: OverriddenSucceeded
          status: "True"
          type: Overridden
        - lastTransitionTime: "2024-07-25T23:10:08Z"
          message: All of the works are synchronized to the latest
          observedGeneration: 2
          reason: AllWorkSynced
          status: "True"
          type: WorkSynchronized
        - lastTransitionTime: "2024-07-25T23:10:09Z"
          message: All corresponding work objects are applied
          observedGeneration: 2
          reason: AllWorkHaveBeenApplied
          status: "True"
          type: Applied
        - lastTransitionTime: "2024-07-25T23:10:19Z"
          message: All corresponding work objects are available
          observedGeneration: 2
          reason: AllWorkAreAvailable
          status: "True"
          type: Available
  selectedResources:
    - kind: Namespace
      name: test-app
      version: v1
    - group: apps
      kind: Deployment
      name: nginx-deployment
      namespace: test-app
      version: v1
    - kind: Service
      name: nginx-service
      namespace: test-app
      version: v1
```
The status indicates that the application has been successfully migrated to the WestEurope clusters and is now running with 4 replicas, as the resource override has been applied.

To double-check, you can also verify the number of replicas in the `nginx-deployment`:
1. Change context to member cluster 4 or 5:
    ```bash
    kubectl config use-context aks-member-4
    ```
2. Get the deployment:
    ```bash
    kubectl get deployment nginx-deployment -n test-app -o wide
    ```

## Conclusion
This tutorial demonstrated how to migrate applications using Fleet from clusters with lower availability to those with higher availability. 
By updating the ClusterResourcePlacement and applying a ResourceOverride, you can ensure that your applications are moved to clusters with better availability while also scaling up the number of replicas to enhance performance and resilience.