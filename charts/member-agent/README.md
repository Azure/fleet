# Azure Fleet Member Agent Helm Chart

## Get Repo

```console
helm repo add member-agent https://azure.github.io/fleet/charts/member-agent
helm repo update
```

## Install Chart

```console
# Go to `charts` folder inside the repo
cd <REPO_DIRECTORY>/fleet/charts
# Helm install
helm install member-agent member-agent/
```

_See [helm install](https://helm.sh/docs/helm/helm_install/) for command documentation._

## Upgrade Chart

```console
# Go to `charts` folder inside the repo
cd <REPO_DIRECTORY>/fleet/charts
# Helm upgrade
helm upgrade member-agent member-agent/ --namespace fleet-system
```

## Parameters

| Parameter                | Description                                           | Default                                         |
|:-------------------------|:------------------------------------------------------|:------------------------------------------------|
| replicaCount             | The number of member-agent replicas to deploy         | `1`                                             |
| image.repository         | Image repository                                      | `ghcr.io/azure/azure/fleet/member-agent`        |
| image.pullPolicy         | Image pullPolicy                                      | `IfNotPresent`                                  |
| image.tag                | The image tag to use                                  | `v0.1.0`                                        |
| affinity                 | The node affinity to use for pod scheduling           | `{}`                                            |
| tolerations              | The toleration to use for pod scheduling              | `[]`                                            |
| resources                | The resource request/limits for the container image   | limits: "2" CPU, 4Gi, requests: 100m CPU, 128Mi |
| namespace                | Namespace that this Helm chart is installed on.       | `fleet-system`                                  |
| logVerbosity             | Log level. Uses V logs (klog)                         | `3`                                             |

## Contributing Changes
