# Azure Fleet Member Agent Helm Chart

## Chart Versioning

Chart versions match the KubeFleet release versions. For example, to install KubeFleet v0.3.0, use chart version `0.3.0`.

## Install Chart

### Prerequisites

Before installing, collect the following values from the **hub** cluster:

**`config.hubURL`** — the hub cluster's API server endpoint:

```console
kubectl config view --raw -o jsonpath='{.clusters[0].cluster.server}'
```

**`config.hubCA`** — the hub cluster's certificate authority data (base64-encoded):

```console
kubectl config view --raw -o jsonpath='{.clusters[0].cluster.certificate-authority-data}'
```

If your hub kubeconfig uses a CA file path instead of inline data, encode the file:

```console
cat /path/to/hub-ca.crt | base64 -w0
```

**`config.memberClusterName`** — the name you want this member cluster to be registered as in the hub. This must match the `MemberCluster` resource name on the hub.

### Using Published Chart (Recommended)

The member-agent chart is published to both GitHub Container Registry (OCI) and GitHub Pages.

#### Option 1: OCI Registry (Recommended)

```console
# Install directly from OCI registry (replace VERSION with the desired release)
helm install member-agent oci://ghcr.io/kubefleet-dev/kubefleet/charts/member-agent \
  --version VERSION \
  --namespace fleet-system \
  --create-namespace \
  --set config.hubURL=https://<hub-api-server> \
  --set config.hubCA=<base64-encoded-hub-ca> \
  --set config.memberClusterName=<member-cluster-name>
```

#### Option 2: Traditional Helm Repository

```console
# Add the KubeFleet Helm repository
helm repo add kubefleet https://kubefleet-dev.github.io/kubefleet/charts
helm repo update

# Install member-agent (specify --version to pin to a specific release)
helm install member-agent kubefleet/member-agent \
  --namespace fleet-system \
  --create-namespace \
  --set config.hubURL=https://<hub-api-server> \
  --set config.hubCA=<base64-encoded-hub-ca> \
  --set config.memberClusterName=<member-cluster-name>
```

### From Local Source

```console
helm install member-agent ./charts/member-agent/ \
  --namespace fleet-system \
  --create-namespace \
  --set config.hubURL=https://<hub-api-server> \
  --set config.hubCA=<base64-encoded-hub-ca> \
  --set config.memberClusterName=<member-cluster-name>
```

_See [helm install](https://helm.sh/docs/helm/helm_install/) for command documentation._

## Upgrade Chart

```console
# Using OCI registry (specify VERSION)
helm upgrade member-agent oci://ghcr.io/kubefleet-dev/kubefleet/charts/member-agent \
  --version VERSION \
  --namespace fleet-system

# Using traditional repository
helm upgrade member-agent kubefleet/member-agent --namespace fleet-system
```

## Parameters

| Parameter               | Description                                                                                                                                                                                                                                    | Default                                              |
|:------------------------|:-----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|:-----------------------------------------------------|
| replicaCount            | The number of member-agent replicas to deploy                                                                                                                                                                                                  | `1`                                                  |
| image.repository        | Image repository                                                                                                                                                                                                                               | `ghcr.io/azure/azure/fleet/member-agent`             |
| image.pullPolicy        | Image pullPolicy                                                                                                                                                                                                                               | `IfNotPresent`                                       |
| image.tag               | The image tag to use                                                                                                                                                                                                                           | `v0.1.0`                                             |
| affinity                | The node affinity to use for pod scheduling                                                                                                                                                                                                    | `{}`                                                 |
| tolerations             | The toleration to use for pod scheduling                                                                                                                                                                                                       | `[]`                                                 |
| resources               | The resource request/limits for the container image                                                                                                                                                                                            | limits: "2" CPU, 4Gi, requests: 100m CPU, 128Mi      |
| namespace               | Namespace that this Helm chart is installed on.                                                                                                                                                                                                | `fleet-system`                                       |
| logVerbosity            | Log level. Uses V logs (klog)                                                                                                                                                                                                                  | `3`                                                  |
| tlsClientInsecure       | Skip TLS server certificate verification when the member agent connects to the hub cluster. Leave this `false` unless you explicitly trust the endpoint and understand the risk.                                                            | `false`                                              |
| useCAAuth               | Use certificate-based authentication for the hub connection instead of the token-based path.                                                                                                                                                  | `false`                                              |
| propertyProvider        | The property provider to use with the member agent; if none is specified, the Fleet member agent will start with no property provider (i.e., the agent will expose no cluster properties, and collect only limited resource usage information) | ``                                                   |
| region                  | The region where the member cluster resides                                                                                                                                                                                                    | ``                                                   |
| enableNamespaceCollectionInPropertyProvider | Enable namespace collection in the property provider; when enabled, the member agent will collect and report the list of namespaces present in the member cluster to the hub cluster for use in scheduling decisions | `false` |
| workApplierRequeueRateLimiterAttemptsWithFixedDelay | This parameter is a set of values to control how frequent KubeFleet should reconcile (processed) manifests; it specifies then number of attempts to requeue with fixed delay before switching to exponential backoff | `1` |
| workApplierRequeueRateLimiterFixedDelaySeconds | This parameter is a set of values to control how frequent KubeFleet should reconcile (process) manifests; it specifies the fixed delay in seconds for initial requeue attempts | `5` |
| workApplierRequeueRateLimiterExponentialBaseForSlowBackoff | This parameter is a set of values to control how frequent KubeFleet should reconcile (process) manifests; it specifies the exponential base for the slow backoff stage | `1.2` |
| workApplierRequeueRateLimiterInitialSlowBackoffDelaySeconds | This parameter is a set of values to control how frequent KubeFleet should reconcile (process) manifests; it specifies the initial delay in seconds for the slow backoff stage | `2` |
| workApplierRequeueRateLimiterMaxSlowBackoffDelaySeconds | This parameter is a set of values to control how frequent KubeFleet should reconcile (process) manifests; it specifies the maximum delay in seconds for the slow backoff stage | `15` |
| workApplierRequeueRateLimiterExponentialBaseForFastBackoff | This parameter is a set of values to control how frequent KubeFleet should reconcile (process) manifests; it specifies the exponential base for the fast backoff stage | `1.5` |
| workApplierRequeueRateLimiterMaxFastBackoffDelaySeconds | This parameter is a set of values to control how frequent KubeFleet should reconcile (process) manifests; it specifies the maximum delay in seconds for the fast backoff stage | `900` |
| workApplierRequeueRateLimiterSkipToFastBackoffForAvailableOrDiffReportedWorkObjs | This parameter is a set of values to control how frequent KubeFleet should reconcile (process) manifests; it specifies whether to skip the slow backoff stage and start fast backoff immediately for available or diff-reported work objects | `true` |
| config.azureCloudConfig | The cloud provider configuration                                                                                                                                                                                                               | **required if property provider is set to azure**    |


## Hub TLS configuration

By default, the chart keeps TLS server certificate verification enabled for the member agent's connection to the hub API server (`tlsClientInsecure=false`). This requires a valid `config.hubCA` value — the placeholder in `values.yaml` will cause the agent to fail at startup. See [Prerequisites](#prerequisites) for how to obtain the hub CA data.

Set `tlsClientInsecure=true` only for explicitly trusted test environments where certificate verification cannot be configured.

## Override Azure cloud config

**If PropertyProvider feature is set to azure, then a cloud configuration is required.**
Cloud configuration provides resource metadata and credentials for `fleet-member-agent` to manipulate Azure resources. 
It's embedded into a Kubernetes secret and mounted to the pods. 
The values can be modified under `config.azureCloudConfig` section in values.yaml or can be provided as a separate file.


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
| `vnetName`                    | The name of the virtual network where the cluster is deployed. |                                                                           |
| `vnetResourceGroup`           | The resource group where the virtual network is deployed. |                                                                           |

You can create a file `azure.yaml` with the following content, and pass it to `helm install` command: `helm install <release-name> <chart-name> --set propertyProvider=azure -f azure.yaml`

```yaml
config:
  azureCloudConfig:
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
    vnetName: "<vnet name>"
    vnetResourceGroup: "<vnet resource group>"
```

## Contributing Changes
