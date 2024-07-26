# Tutorial: Migrating Applications to Another Cluster When a Cluster Goes Down
This tutorial demonstrates how to move applications from clusters have gone down to other operational clusters using Fleet.

## Scenario
Your fleet consists of the following clusters:

1. Member Cluster 1 & Member Cluster 2 (WestUS, 1 node each)
2. Member Cluster 3 (EastUS2, 2 nodes)
3. Member Cluster 4 & Member Cluster 5 (WestEurope, 3 nodes each)

Due to certain circumstances, Member Cluster 1 and Member Cluster 2 are down, requiring you to migrate your applications from these clusters to other operational ones.

## Current Application Resources
The following resources are currently deployed in Member Cluster 1 and Member Cluster 2 by the ClusterResourcePlacement:

#### Service
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
Summary:
- This defines a Kubernetes Deployment named `nginx-deployment` in the `test-app` namespace.
- It creates 2 replicas of the nginx pod, each running the `nginx:1.16.1` image.
- The deployment ensures that the specified number of pods (replicas) are running and available.
- The pods are labeled with `app: nginx` and expose port 80.

#### ClusterResourcePlacement
```yaml
apiVersion: placement.kubernetes-fleet.io/v1
kind: ClusterResourcePlacement
metadata:
  annotations:
    kubectl.kubernetes.io/last-applied-configuration: |
      {"apiVersion":"placement.kubernetes-fleet.io/v1","kind":"ClusterResourcePlacement","metadata":{"annotations":{},"name":"crp-migration"},"spec":{"policy":{"affinity":{"clusterAffinity":{"requiredDuringSchedulingIgnoredDuringExecution":{"clusterSelectorTerms":[{"labelSelector":{"matchLabels":{"fleet.azure.com/location":"westus"}}}]}}},"numberOfClusters":2,"placementType":"PickN"},"resourceSelectors":[{"group":"","kind":"Namespace","name":"test-app","version":"v1"}],"revisionHistoryLimit":10,"strategy":{"type":"RollingUpdate"}}}
  creationTimestamp: "2024-07-25T21:27:35Z"
  finalizers:
    - kubernetes-fleet.io/crp-cleanup
    - kubernetes-fleet.io/scheduler-cleanup
  generation: 1
  name: crp-migration
  resourceVersion: "22177519"
  uid: 0683cfaa-df24-4b2c-8a3d-07031692da8f
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
    - lastTransitionTime: "2024-07-25T21:27:35Z"
      message: found all cluster needed as specified by the scheduling policy, found
        2 cluster(s)
      observedGeneration: 1
      reason: SchedulingPolicyFulfilled
      status: "True"
      type: ClusterResourcePlacementScheduled
    - lastTransitionTime: "2024-07-25T21:27:35Z"
      message: All 2 cluster(s) start rolling out the latest resource
      observedGeneration: 1
      reason: RolloutStarted
      status: "True"
      type: ClusterResourcePlacementRolloutStarted
    - lastTransitionTime: "2024-07-25T21:27:35Z"
      message: No override rules are configured for the selected resources
      observedGeneration: 1
      reason: NoOverrideSpecified
      status: "True"
      type: ClusterResourcePlacementOverridden
    - lastTransitionTime: "2024-07-25T21:27:35Z"
      message: Works(s) are succcesfully created or updated in 2 target cluster(s)'
        namespaces
      observedGeneration: 1
      reason: WorkSynchronized
      status: "True"
      type: ClusterResourcePlacementWorkSynchronized
    - lastTransitionTime: "2024-07-25T21:27:35Z"
      message: The selected resources are successfully applied to 2 cluster(s)
      observedGeneration: 1
      reason: ApplySucceeded
      status: "True"
      type: ClusterResourcePlacementApplied
    - lastTransitionTime: "2024-07-25T21:27:45Z"
      message: The selected resources in 2 cluster(s) are available now
      observedGeneration: 1
      reason: ResourceAvailable
      status: "True"
      type: ClusterResourcePlacementAvailable
  observedResourceIndex: "0"
  placementStatuses:
    - clusterName: aks-member-2
      conditions:
        - lastTransitionTime: "2024-07-25T21:27:35Z"
          message: 'Successfully scheduled resources for placement in "aks-member-2"
        (affinity score: 0, topology spread score: 0): picked by scheduling policy'
          observedGeneration: 1
          reason: Scheduled
          status: "True"
          type: Scheduled
        - lastTransitionTime: "2024-07-25T21:27:35Z"
          message: Detected the new changes on the resources and started the rollout process
          observedGeneration: 1
          reason: RolloutStarted
          status: "True"
          type: RolloutStarted
        - lastTransitionTime: "2024-07-25T21:27:35Z"
          message: No override rules are configured for the selected resources
          observedGeneration: 1
          reason: NoOverrideSpecified
          status: "True"
          type: Overridden
        - lastTransitionTime: "2024-07-25T21:27:35Z"
          message: All of the works are synchronized to the latest
          observedGeneration: 1
          reason: AllWorkSynced
          status: "True"
          type: WorkSynchronized
        - lastTransitionTime: "2024-07-25T21:27:35Z"
          message: All corresponding work objects are applied
          observedGeneration: 1
          reason: AllWorkHaveBeenApplied
          status: "True"
          type: Applied
        - lastTransitionTime: "2024-07-25T21:27:45Z"
          message: All corresponding work objects are available
          observedGeneration: 1
          reason: AllWorkAreAvailable
          status: "True"
          type: Available
    - clusterName: aks-member-1
      conditions:
        - lastTransitionTime: "2024-07-25T21:27:35Z"
          message: 'Successfully scheduled resources for placement in "aks-member-1"
        (affinity score: 0, topology spread score: 0): picked by scheduling policy'
          observedGeneration: 1
          reason: Scheduled
          status: "True"
          type: Scheduled
        - lastTransitionTime: "2024-07-25T21:27:35Z"
          message: Detected the new changes on the resources and started the rollout process
          observedGeneration: 1
          reason: RolloutStarted
          status: "True"
          type: RolloutStarted
        - lastTransitionTime: "2024-07-25T21:27:35Z"
          message: No override rules are configured for the selected resources
          observedGeneration: 1
          reason: NoOverrideSpecified
          status: "True"
          type: Overridden
        - lastTransitionTime: "2024-07-25T21:27:35Z"
          message: All of the works are synchronized to the latest
          observedGeneration: 1
          reason: AllWorkSynced
          status: "True"
          type: WorkSynchronized
        - lastTransitionTime: "2024-07-25T21:27:35Z"
          message: All corresponding work objects are applied
          observedGeneration: 1
          reason: AllWorkHaveBeenApplied
          status: "True"
          type: Applied
        - lastTransitionTime: "2024-07-25T21:27:45Z"
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
- This defines a ClusterResourcePlacement named `crp-migration`.
- The PickN placement policy selects 2 clusters based on the label `fleet.azure.com/location: westus`. Consequently, it chooses Member Cluster 1 and Member Cluster 2, as they are located in WestUS.
- It targets resources in the `test-app` namespace.

## Migrating Applications to a Cluster to Other Operational Clusters
When the clusters in WestUS go down, update the ClusterResourcePlacement (CRP) to migrate the applications to other clusters. 
In this tutorial, we will move them to Member Cluster 4 and Member Cluster 5, which are located in WestEurope.

#### Update the CRP for Migration to Clusters in WestEurope
```yaml
apiVersion: placement.kubernetes-fleet.io/v1
kind: ClusterResourcePlacement
metadata:
  name: crp-migration
spec:
  policy:
    placementType: PickN
    numberOfClusters: 2
    affinity:
      clusterAffinity:
        requiredDuringSchedulingIgnoredDuringExecution:
          clusterSelectorTerms:
            - labelSelector:
                matchLabels:
                  fleet.azure.com/location: westeurope  # updated label
  resourceSelectors:
  - group: ""
    kind: Namespace
    name: test-app
    version: v1
  revisionHistoryLimit: 10
  strategy:
    type: RollingUpdate
```
Update the `crp.yaml` to reflect the new region and apply it: 
```bash
kubectl apply -f crp.yaml
```

### Results
After applying the updated `crp.yaml`, the Fleet will schedule the application on the available clusters in WestEurope. 
You can check the status of the CRP to ensure that the application has been successfully migrated and is running on the newly selected clusters:
```bash
kubectl get crp crp-migration -o yaml
```
You should see a status indicating that the application is now running in the clusters located in WestEurope, similar to the following:
#### CRP Status
```yaml
...
status:
  conditions:
    - lastTransitionTime: "2024-07-25T21:36:02Z"
      message: found all cluster needed as specified by the scheduling policy, found
        2 cluster(s)
      observedGeneration: 2
      reason: SchedulingPolicyFulfilled
      status: "True"
      type: ClusterResourcePlacementScheduled
    - lastTransitionTime: "2024-07-25T21:36:14Z"
      message: All 2 cluster(s) start rolling out the latest resource
      observedGeneration: 2
      reason: RolloutStarted
      status: "True"
      type: ClusterResourcePlacementRolloutStarted
    - lastTransitionTime: "2024-07-25T21:36:14Z"
      message: No override rules are configured for the selected resources
      observedGeneration: 2
      reason: NoOverrideSpecified
      status: "True"
      type: ClusterResourcePlacementOverridden
    - lastTransitionTime: "2024-07-25T21:36:14Z"
      message: Works(s) are succcesfully created or updated in 2 target cluster(s)'
        namespaces
      observedGeneration: 2
      reason: WorkSynchronized
      status: "True"
      type: ClusterResourcePlacementWorkSynchronized
    - lastTransitionTime: "2024-07-25T21:36:14Z"
      message: The selected resources are successfully applied to 2 cluster(s)
      observedGeneration: 2
      reason: ApplySucceeded
      status: "True"
      type: ClusterResourcePlacementApplied
    - lastTransitionTime: "2024-07-25T21:36:14Z"
      message: The selected resources in 2 cluster(s) are available now
      observedGeneration: 2
      reason: ResourceAvailable
      status: "True"
      type: ClusterResourcePlacementAvailable
  observedResourceIndex: "0"
  placementStatuses:
    - clusterName: aks-member-5
      conditions:
        - lastTransitionTime: "2024-07-25T21:36:02Z"
          message: 'Successfully scheduled resources for placement in "aks-member-5" (affinity
        score: 0, topology spread score: 0): picked by scheduling policy'
          observedGeneration: 2
          reason: Scheduled
          status: "True"
          type: Scheduled
        - lastTransitionTime: "2024-07-25T21:36:14Z"
          message: Detected the new changes on the resources and started the rollout process
          observedGeneration: 2
          reason: RolloutStarted
          status: "True"
          type: RolloutStarted
        - lastTransitionTime: "2024-07-25T21:36:14Z"
          message: No override rules are configured for the selected resources
          observedGeneration: 2
          reason: NoOverrideSpecified
          status: "True"
          type: Overridden
        - lastTransitionTime: "2024-07-25T21:36:14Z"
          message: All of the works are synchronized to the latest
          observedGeneration: 2
          reason: AllWorkSynced
          status: "True"
          type: WorkSynchronized
        - lastTransitionTime: "2024-07-25T21:36:14Z"
          message: All corresponding work objects are applied
          observedGeneration: 2
          reason: AllWorkHaveBeenApplied
          status: "True"
          type: Applied
        - lastTransitionTime: "2024-07-25T21:36:14Z"
          message: All corresponding work objects are available
          observedGeneration: 2
          reason: AllWorkAreAvailable
          status: "True"
          type: Available
    - clusterName: aks-member-4
      conditions:
        - lastTransitionTime: "2024-07-25T21:36:02Z"
          message: 'Successfully scheduled resources for placement in "aks-member-4" (affinity
        score: 0, topology spread score: 0): picked by scheduling policy'
          observedGeneration: 2
          reason: Scheduled
          status: "True"
          type: Scheduled
        - lastTransitionTime: "2024-07-25T21:36:14Z"
          message: Detected the new changes on the resources and started the rollout process
          observedGeneration: 2
          reason: RolloutStarted
          status: "True"
          type: RolloutStarted
        - lastTransitionTime: "2024-07-25T21:36:14Z"
          message: No override rules are configured for the selected resources
          observedGeneration: 2
          reason: NoOverrideSpecified
          status: "True"
          type: Overridden
        - lastTransitionTime: "2024-07-25T21:36:14Z"
          message: All of the works are synchronized to the latest
          observedGeneration: 2
          reason: AllWorkSynced
          status: "True"
          type: WorkSynchronized
        - lastTransitionTime: "2024-07-25T21:36:14Z"
          message: All corresponding work objects are applied
          observedGeneration: 2
          reason: AllWorkHaveBeenApplied
          status: "True"
          type: Applied
        - lastTransitionTime: "2024-07-25T21:36:14Z"
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

## Conclusion
This tutorial demonstrated how to migrate applications using Fleet when clusters in one region go down. 
By updating the ClusterResourcePlacement, you can ensure that your applications are moved to available clusters in another region, 
maintaining availability and resilience.