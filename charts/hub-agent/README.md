# Hub agent controller Helm Chart

## Install Chart

```console
# Helm install with fleet-system namespace already created
helm install hub-agent ./charts/hub-agent/
```

## Upgrade Chart

```console
helm upgrade hub-agent ./charts/hubagent/ --namespace fleet-system --create-namespace
```

_See [parameters](#parameters) below._

_See [helm install](https://helm.sh/docs/helm/helm_install/) for command documentation._

## Parameters

| Parameter                                 | Description                                                                                | Default                                          |
|:------------------------------------------|:------------------------------------------------------------------------------------------|:-------------------------------------------------|
| `replicaCount`                            | Number of hub-agent replicas to deploy                                                     | `1`                                              |
| `image.repository`                        | Image repository                                                                           | `ghcr.io/azure/azure/fleet/hub-agent`            |
| `image.pullPolicy`                        | Image pull policy                                                                          | `Always`                                         |
| `image.tag`                               | Image release tag                                                                          | `v0.1.0`                                         |
| `namespace`                               | Namespace where this chart is installed                                                    | `fleet-system`                                   |
| `serviceAccount.create`                   | Whether to create a service account                                                        | `true`                                           |
| `serviceAccount.name`                     | Service account name                                                                       | `hub-agent-sa`                                   |
| `resources`                               | Resource requests/limits for the container                                                 | limits: 500m CPU, 1Gi; requests: 100m CPU, 128Mi |
| `affinity`                                | Node affinity for hub-agent pods                                                           | `{}`                                             |
| `tolerations`                             | Tolerations for hub-agent pods                                                             | `[]`                                             |
| `logVerbosity`                            | Log level (klog V logs)                                                                    | `5`                                              |
| `enableV1Beta1APIs`                       | Watch for v1beta1 APIs                                                                     | `true`                                           |
| `hubAPIQPS`                               | QPS for fleet-apiserver (not including events/node heartbeat)                              | `250`                                            |
| `hubAPIBurst`                             | Burst for fleet-apiserver (not including events/node heartbeat)                            | `1000`                                           |
| `MaxConcurrentClusterPlacement`           | Max concurrent ClusterResourcePlacement operations                                         | `100`                                            |
| `ConcurrentResourceChangeSyncs`           | Max concurrent resourceChange reconcilers                                                  | `20`                                             |
| `logFileMaxSize`                          | Max log file size before rotation                                                          | `1000000`                                        |
| `MaxFleetSizeSupported`                   | Max number of member clusters supported                                                    | `100`                                            |
| `resourceSnapshotCreationMinimumInterval` | The minimum interval at which resource snapshots could be created.                         | `30s`                                            |
| `resourceChangesCollectionDuration`       | The duration for collecting resource changes into one snapshot.                            | `15s`                                            |