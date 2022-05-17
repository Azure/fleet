# Azure Fleet Member Agent Helm Chart

## Get Repo

```console
helm repo add member-agent https://azure.github.io/fleet/charts/member-agent
helm repo update
```

## Install Chart

```console
# Helm install
helm install member-agent member-agent/ --namespace member-agent
```

**Note**: If you want to use a different value for namespace other than `member-agent`, you need to set `namespace` params to your desired namespace value. 

_See [helm install](https://helm.sh/docs/helm/helm_install/) for command documentation._

## Upgrade Chart

```console
helm upgrade member-agent member-agent/ --namespace member-agent
```

## Parameters

| Parameter                | Description                                                                                                                                                                | Default                                         |
|:-------------------------|:---------------------------------------------------------------------------------------------------------------------------------------------------------------------------|:------------------------------------------------|
| replicaCount             | The number of member-agent replicas to deploy                                                                                                                              | `1`                                             |
| image.repository         | Image repository                                                                                                                                                           | `ghcr.io/azure/azure/fleet/member-agent`        |
| image.pullPolicy         | Image pullPolicy                                                                                                                                                           | `IfNotPresent`                                  |
| image.tag                | The image tag to use                                                                                                                                                       | `v0.1.0`                                        |
| serviceAccount.create    | Whether to create service account                                                                                                                                          | `true`                                          |
| serviceAccount.name      | Service account name                                                                                                                                                       | `member-agent`                                  |
| affinity                 | The node affinity to use for pod scheduling                                                                                                                                | `{}`                                            |
| tolerations              | The toleration to use for pod scheduling                                                                                                                                   | `[]`                                            |
| resources                | The resource request/limits for the container image                                                                                                                        | limits: "2" CPU, 4Gi, requests: 100m CPU, 128Mi |
| namespace                | Namespace that this Helm chart is installed on.                                                                                                                            | `member-agent`                                  |
| authenticationPreference | Flow that the cluster, on which this member agent is being installed, would be authenticated. Currently supported authentication flows are `Managed Identity` and `Secret` | `Managed Identity`                              |

## Contributing Changes
