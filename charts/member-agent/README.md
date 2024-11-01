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

| Parameter          | Description                                                                                                                                                                                                                                    | Default                                         |
|:-------------------|:-----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|:------------------------------------------------|
| replicaCount       | The number of member-agent replicas to deploy                                                                                                                                                                                                  | `1`                                             |
| image.repository   | Image repository                                                                                                                                                                                                                               | `ghcr.io/azure/azure/fleet/member-agent`        |
| image.pullPolicy   | Image pullPolicy                                                                                                                                                                                                                               | `IfNotPresent`                                  |
| image.tag          | The image tag to use                                                                                                                                                                                                                           | `v0.1.0`                                        |
| affinity           | The node affinity to use for pod scheduling                                                                                                                                                                                                    | `{}`                                            |
| tolerations        | The toleration to use for pod scheduling                                                                                                                                                                                                       | `[]`                                            |
| resources          | The resource request/limits for the container image                                                                                                                                                                                            | limits: "2" CPU, 4Gi, requests: 100m CPU, 128Mi |
| namespace          | Namespace that this Helm chart is installed on.                                                                                                                                                                                                | `fleet-system`                                  |
| logVerbosity       | Log level. Uses V logs (klog)                                                                                                                                                                                                                  | `3`                                             |
| propertyProvider   | The property provider to use with the member agent; if none is specified, the Fleet member agent will start with no property provider (i.e., the agent will expose no cluster properties, and collect only limited resource usage information) | ``                                              |
| region             | The region where the member cluster resides                                                                                                                                                                                                    | ``                                              |
| config.cloudConfig | The cloud provider configuration                                                                                                                                                                                                               | **required if property provider is enabled**    |

## Override Azure cloud config

**If PropertyProvider feature is enabled, then a cloud configuration is required.** 
Cloud configuration provides resource metadata and credentials for `fleet-member-agent` to manipulate Azure resources. 
It's embedded into a Kubernetes secret and mounted to the pods. 
The values can be modified under `config.cloudConfig` section in values.yaml or can be provided as a separate file.


| configuration value                                   | description | Remark                                                                    |
|-------------------------------------------------------| --- |---------------------------------------------------------------------------|
| `cloud`                       | The cloud where resources belong. | Required.                                                                 |
| `tenantId`                    | The AAD Tenant ID for the subscription where the Azure resources are deployed. |                                                                           |
| `subscriptionId`              | The ID of the subscription where resources are deployed. |                                                                           |
| `useManagedIdentityExtension` | Boolean indicating whether or not to use a managed identity. | `true` or `false`                                                         |
| `userAssignedIdentityID`      | ClientID of the user-assigned managed identity with RBAC access to resources. | Required for UserAssignedIdentity and omitted for SystemAssignedIdentity. |
| `aadClientId`                 | The ClientID for an AAD application with RBAC access to resources. | Required if `useManagedIdentityExtension` is set to `false`.              |
| `aadClientSecret`             | The ClientSecret for an AAD application with RBAC access to resources. | Required if `useManagedIdentityExtension` is set to `false`.              |
| `resourceGroup`               | The name of the resource group where cluster resources are deployed. |                                                                           |
| `userAgent`                   | The userAgent provided when accessing resources. |                                                                           |
| `location`                    | The region where resource group and its resources is deployed. |                                                                           |
| `clusterName`                 | The name of the cluster where the agent is running. |                                                                           |
| `clusterResourceGroup`        | The resource group where the cluster is deployed. |                                                                           |
| `vnetName`                    | The name of the virtual network where the cluster is deployed. |                                                                           |
| `vnetResourceGroup`           | The resource group where the virtual network is deployed. |                                                                           |

You can create a file `azure.yaml` with the following content, and pass it to `helm install` command: `helm install <release-name> <chart-name> -f azure.yaml`

```yaml
config:
  cloudConfig:
    cloud: "AzurePublicCloud"
    tenantId: "00000000-0000-0000-0000-000000000000"
    subscriptionId: "00000000-0000-0000-0000-000000000000"
    useManagedIdentityExtension: false
    userAssignedIdentityID: "00000000-0000-0000-0000-000000000000"
    aadClientId: "00000000-0000-0000-0000-000000000000"
    aadClientSecret: "<your secret>"
    userAgent: "fleet-member-agent"
    resourceGroup: "<resource group name>"
    location: "<resource group location>"
    clusterName: "<cluster name>"
    clusterResourceGroup: "<cluster resource group>"
    vnetName: "<vnet name>"
    vnetResourceGroup: "<vnet resource group>"
```

## Contributing Changes
