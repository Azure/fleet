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

| Parameter                     | Description                                                                                                                                                  | Default                                          |
|:------------------------------|:-------------------------------------------------------------------------------------------------------------------------------------------------------------|:-------------------------------------------------|
| replicaCount                  | The number of hub-agent replicas to deploy                                                                                                                   | `1`                                              |
| image.repository              | Image repository                                                                                                                                             | `ghcr.io/azure/azure/fleet/hub-agent`            |
| image.pullPolicy              | Image pullPolicy                                                                                                                                             | `Always`                                         |
| image.tag                     | The image release tag to use                                                                                                                                 | `v0.1.0`                                         |
| namespace                     | Namespace that this Helm chart is installed on                                                                                                               | `fleet-system`                                   |
| serviceAccount.create         | Whether to create service account                                                                                                                            | `true`                                           |
| serviceAccount.name           | Service account name                                                                                                                                         | `hub-agent-sa`                                   |
| resources                     | The resource request/limits for the container image                                                                                                          | limits: 500m CPU, 1Gi, requests: 100m CPU, 128Mi |
| affinity                      | The node affinity to use for hubagent pod                                                                                                                    | `{}`                                             |
| tolerations                   | The tolerations to use for hubagent pod                                                                                                                      | `[]`                                             |
| logVerbosity                  | Log level. Uses V logs (klog)                                                                                                                                | `5`                                              |
| enableV1Alpha1APIs            | If set, the agents will watch for the v1alpha1 APIs.                                                                                                         | `false`                                          |
| enableV1Beta1APIs             | If set, the agents will watch for the v1beta1 APIs.                                                                                                          | `true`                                           |
| hubAPIQPS                     | QPS to use while talking with fleet-apiserver. Doesn't cover events and node heartbeat apis which rate limiting is controlled by a different set of flags.   | `1000`                                           |
| hubAPIBurst                   | Burst to use while talking with fleet-apiserver. Doesn't cover events and node heartbeat apis which rate limiting is controlled by a different set of flags. | `1000`                                           |
| MaxConcurrentClusterPlacement | The max number of clusterResourcePlacement to run concurrently this fleet supports.                                                                          | `100`                                            |
| ConcurrentResourceChangeSyncs | The number of resourceChange reconcilers that are allowed to run concurrently.                                                                               | `20`                                             |
| logFileMaxSize                | Max size of log file before rotation                                                                                                                         | `1000000`                                        |
| MaxFleetSizeSupported         | The max number of member clusters this fleet supports.                                                                                                       | `100`                                            |