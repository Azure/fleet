# Azure Fleet Member Agent Helm Chart

## Get Repo

```console
helm repo add member-agent https://azure.github.io/fleet/charts/member-agent
helm repo update
```

## Install Chart

```console
# Helm install
helm install -n member-agent member-agent/
```

_See [helm install](https://helm.sh/docs/helm/helm_install/) for command documentation._

## Upgrade Chart

```console
helm upgrade -n member-agent member-agent/
```

## Parameters

| Parameter                | Description                                                                                                                                                                                  | Default                                         |
|:-------------------------|:---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|:------------------------------------------------|
| replicaCount             | The number of member-agent replicas to deploy                                                                                                                                                | `1`                                             |
| image.repository         | Image repository                                                                                                                                                                             | `ghcr.io/azure/azure/fleet/member-agent`        |
| image.pullPolicy         | Image pullPolicy                                                                                                                                                                             | `IfNotPresent`                                  |
| image.tag                | The image tag to use                                                                                                                                                                         | `v0.1.0`                                        |
| affinity                 | The node affinity to use for pod scheduling                                                                                                                                                  | `{}`                                            |
| tolerations              | The toleration to use for pod scheduling                                                                                                                                                     | `[]`                                            |
| resources                | The resource request/limits for the container image                                                                                                                                          | limits: "2" CPU, 4Gi, requests: 100m CPU, 128Mi |
| clusterIdentity          | Identity of the cluster that member-agent being installed on. It would authenticate calls from this cluster to the hub cluster if the chosen authentication preference is `Managed Identity` | `""`                                            |
| authenticationPreference | Flow that the cluster, on which this member agent is being installed, would be authenticated. Currently supported authentication flows are `Managed Identity` and `Secret`                   | `"Managed Identity"`                            |

## Contributing Changes
