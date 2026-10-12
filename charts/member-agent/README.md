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
| useKubeconfig        | Authenticate to the hub cluster via a full kubeconfig instead of the refresh-token sidecar or `useCAAuth`. The vendor-neutral way to authenticate via a federated identity. Mutually exclusive with `useCAAuth`. See [Hub connection via kubeconfig](#hub-connection-via-kubeconfig-federated-identity-any-vendor). | `false`                                              |
| config.hubKubeconfigSecretName | The name of a Secret (key `kubeconfig`) containing a full kubeconfig for the hub connection, mounted and passed to the process via the `KUBE_CONFIG_PATH` environment variable. Required (the chart fails to render otherwise) when `useKubeconfig` is `true`. | ``                                                   |
| podLabels               | Extra labels applied to the member-agent pod template — e.g. workload-identity labels a `hubKubeconfigSecretName` exec plugin needs.                                                                                                          | `{}`                                                 |
| serviceAccountAnnotations | Extra annotations applied to the member-agent ServiceAccount — e.g. workload-identity annotations a `hubKubeconfigSecretName` exec plugin needs.                                                                                            | `{}`                                                 |
| extraVolumes / extraVolumeMounts / extraInitContainers | Standard passthroughs (rendered via `toYaml`) for getting an exec plugin binary, or a workload-API socket, into the pod without a custom image. `extraVolumeMounts` applies to the member-agent container.               | `[]`                                                 |
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

## Hub connection via kubeconfig (federated identity, any vendor)

As an alternative to the refresh-token sidecar, the member agent can load a full kubeconfig for the
hub connection (`useKubeconfig` + `config.hubKubeconfigSecretName`, i.e. `--use-kubeconfig` plus the
`KUBE_CONFIG_PATH` environment variable), the same way `kubectl` does.
This mode requires no sidecar container and no shared token volume — `client-go` handles token
caching and refresh itself, whatever credential mechanism the kubeconfig's `users[].user.exec`
stanza invokes.

This chart, and KubeFleet's own code, have **no vendor-specific knowledge** of any cloud's identity
system — the kubeconfig you supply can invoke whatever exec-credential plugin your platform
provides (`client.authentication.k8s.io`, the same mechanism `kubectl` itself uses). Everything
vendor-specific — the plugin binary, its arguments, and any workload-identity metadata — lives
entirely in the kubeconfig you write and the `podLabels`/`serviceAccountAnnotations` values you set;
none of it is baked into this chart.

**Kubeconfig mode is fully authoritative for the hub connection.** Once `useKubeconfig` is set, the
loaded kubeconfig's own server URL, TLS configuration, and authentication are used as-is; none of
the following are consulted, even if also set: `config.hubURL`/`HUB_SERVER_URL`, `useCAAuth`,
`tlsClientInsecure`/`--tls-insecure`, `config.CABundle`/`CA_BUNDLE`, `config.hubCA`/
`HUB_CERTIFICATE_AUTHORITY`, and the refresh-token sidecar's `CONFIG_PATH`/`IDENTITY_KEY`/
`IDENTITY_CERT`. If you need TLS verification disabled for testing, set
`insecure-skip-tls-verify: true` on the kubeconfig's own `cluster` entry rather than
`tlsClientInsecure` — the latter has no effect in this mode.

**Security considerations**: the hub kubeconfig can carry — directly or via its exec plugin's
own credentials — everything needed to authenticate to the hub cluster, so treat the Secret
holding it like any other high-value credential (restrict who can `get`/`list` it via RBAC, as
you would `hub-kubeconfig-secret` or `hub-identity` for the other modes). Prefer mounting the
kubeconfig and any exec-plugin binary via Secrets/`extraVolumes` rather than embedding secret
material in Helm values or `--set` flags — values and `--set` arguments can end up in shell
history, `helm get values` output, or process listings, none of which apply to a Secret mounted
as a file.

Three worked examples:

### Azure / AKS (kubelogin)

`kubelogin` covers three Azure identity mechanisms — federated (workload) identity, managed
identity, and a service principal's client secret or certificate — selected by the `--login` mode
in its exec stanza. All three use the same `--use-kubeconfig`/`KUBE_CONFIG_PATH` mechanism and
differ only in the kubeconfig content and prerequisites shown below.

However you build the kubeconfig for any of the three, package it into a Secret the same way
(`kubectl create secret generic hub-kubeconfig --from-file=kubeconfig=./kubeconfig.yaml`), shown in
each example below.

`kubelogin` also needs to be present in the member-agent image for all three — either build a small
custom image on top of the official one:
```dockerfile
FROM ghcr.io/kubefleet-dev/kubefleet/member-agent:<version>
COPY --from=ghcr.io/azure/kubelogin:<version> /usr/local/bin/kubelogin /usr/local/bin/kubelogin
```
or use `extraInitContainers`/`extraVolumes`/`extraVolumeMounts` to place it on a shared volume at
runtime instead — the official `ghcr.io/azure/kubelogin` image is `scratch`-based (no shell), so the
init container has to be a different, shell-capable image that fetches the release binary itself:

```yaml
# values-kubelogin-init.yaml
extraVolumes:
- name: kubelogin-bin
  emptyDir: {}
extraVolumeMounts:
- name: kubelogin-bin
  mountPath: /usr/local/bin
extraInitContainers:
- name: install-kubelogin
  image: busybox:1.36
  # Match the release asset to the node architecture (kubelogin-linux-arm64.zip on arm64 nodes).
  command:
  - sh
  - -c
  - |
    wget -qO /tmp/kubelogin.zip https://github.com/Azure/kubelogin/releases/download/v0.2.19/kubelogin-linux-amd64.zip
    unzip -oq /tmp/kubelogin.zip -d /tmp
    cp /tmp/bin/linux_amd64/kubelogin /kubelogin-bin/kubelogin
    chmod +x /kubelogin-bin/kubelogin
  volumeMounts:
  - name: kubelogin-bin
    mountPath: /kubelogin-bin
```
```console
kubectl create secret generic hub-kubeconfig --namespace fleet-system --from-file=kubeconfig=./kubeconfig.yaml

helm install member-agent ./charts/member-agent/ \
  --namespace fleet-system \
  --create-namespace \
  -f values-kubelogin-init.yaml \
  --set config.memberClusterName=<member-cluster-name> \
  --set useKubeconfig=true \
  --set config.hubKubeconfigSecretName=hub-kubeconfig \
  --set-string podLabels."azure\.workload\.identity/use"=true \
  --set serviceAccountAnnotations."azure\.workload\.identity/client-id"=<uami-client-id>
```
Equivalent `member-agent` invocation: `KUBE_CONFIG_PATH=/etc/kubefleet/hub-kubeconfig/kubeconfig --use-kubeconfig=true`

`kubelogin-bin` is mounted at `/usr/local/bin` on the member-agent container too, so it works with
the bare `command: kubelogin` shown in every example below — no image build required, at the cost
of a network fetch on every pod start.

#### Federated identity (`--login workloadidentity`)

Set up [AKS workload identity](https://learn.microsoft.com/en-us/azure/aks/workload-identity-overview) on the member cluster, and grant the federated managed identity the appropriate RBAC permissions on the hub cluster (an AKS cluster with Azure AD authentication enabled). Then:

```yaml
# kubeconfig.yaml
apiVersion: v1
kind: Config
clusters:
- cluster:
    certificate-authority-data: <base64-encoded-hub-ca>
    server: https://<hub-api-server>
  name: hub
contexts:
- context:
    cluster: hub
    user: fed
  name: hub
current-context: hub
users:
- name: fed
  user:
    exec:
      apiVersion: client.authentication.k8s.io/v1
      command: kubelogin
      args:
      - get-token
      - --environment
      - AzurePublicCloud
      - --server-id
      - <aad-server-application-id>
      - --client-id
      - <uami-client-id>
      - --tenant-id
      - <aad-tenant-id>
      - --login
      - workloadidentity
      provideClusterInfo: false
      interactiveMode: Never
```

```console
kubectl create secret generic hub-kubeconfig --namespace fleet-system --from-file=kubeconfig=./kubeconfig.yaml

helm install member-agent ./charts/member-agent/ \
  --namespace fleet-system \
  --create-namespace \
  -f values-kubelogin-init.yaml \
  --set config.memberClusterName=<member-cluster-name> \
  --set useKubeconfig=true \
  --set config.hubKubeconfigSecretName=hub-kubeconfig \
  --set-string podLabels."azure\.workload\.identity/use"=true \
  --set serviceAccountAnnotations."azure\.workload\.identity/client-id"=<uami-client-id>
```
Equivalent `member-agent` invocation: `KUBE_CONFIG_PATH=/etc/kubefleet/hub-kubeconfig/kubeconfig --use-kubeconfig=true`

#### Managed identity (`--login msi`)

An alternative to federated identity: uses the IMDS-based managed identity available on any Azure
VM/node — so it needs no AKS workload identity setup, no `podLabels`, and no
`serviceAccountAnnotations`. Grant the managed identity the appropriate RBAC permissions on the hub
cluster, then:

```yaml
users:
- name: fed
  user:
    exec:
      apiVersion: client.authentication.k8s.io/v1
      command: kubelogin
      args:
      - get-token
      - --server-id
      - <aad-server-application-id>
      - --login
      - msi
      - --client-id       # omit this flag entirely to use the system-assigned identity instead
      - <uami-client-id>
      provideClusterInfo: false
      interactiveMode: Never
```

```console
kubectl create secret generic hub-kubeconfig --namespace fleet-system --from-file=kubeconfig=./kubeconfig.yaml

helm install member-agent ./charts/member-agent/ \
  --namespace fleet-system \
  --create-namespace \
  -f values-kubelogin-init.yaml \
  --set config.memberClusterName=<member-cluster-name> \
  --set useKubeconfig=true \
  --set config.hubKubeconfigSecretName=hub-kubeconfig
```
Equivalent `member-agent` invocation: `KUBE_CONFIG_PATH=/etc/kubefleet/hub-kubeconfig/kubeconfig --use-kubeconfig=true`

#### Client secret / client certificate (`--login spn`)

Authenticates as an Azure AD app registration (service principal) using a client secret or a client
certificate, via `--client-id`/`--client-secret`/`--client-certificate`/`--tenant-id` — the same
flag-based style as the federated-identity and managed-identity variants above.

**Client secret flow:**
```yaml
users:
- name: fed
  user:
    exec:
      apiVersion: client.authentication.k8s.io/v1
      command: kubelogin
      args:
      - get-token
      - --server-id
      - <aad-server-application-id>
      - --login
      - spn
      - --client-id
      - <spn-client-id>
      - --client-secret
      - <spn-client-secret>
      - --tenant-id
      - <aad-tenant-id>
      provideClusterInfo: false
      interactiveMode: Never
```
```console
kubectl create secret generic hub-kubeconfig --namespace fleet-system --from-file=kubeconfig=./kubeconfig.yaml

helm install member-agent ./charts/member-agent/ \
  --namespace fleet-system \
  --create-namespace \
  -f values-kubelogin-init.yaml \
  --set config.memberClusterName=<member-cluster-name> \
  --set useKubeconfig=true \
  --set config.hubKubeconfigSecretName=hub-kubeconfig
```
Equivalent `member-agent` invocation: `KUBE_CONFIG_PATH=/etc/kubefleet/hub-kubeconfig/kubeconfig --use-kubeconfig=true`

**Client certificate flow:** `--client-certificate` must point to a file on disk, so the
certificate needs its own mount via `extraVolumes`/`extraVolumeMounts` rather than living inline in
the kubeconfig:
```yaml
users:
- name: fed
  user:
    exec:
      apiVersion: client.authentication.k8s.io/v1
      command: kubelogin
      args:
      - get-token
      - --server-id
      - <aad-server-application-id>
      - --login
      - spn
      - --client-id
      - <spn-client-id>
      - --client-certificate
      - /etc/kubefleet/spn-cert/tls.crt
      - --tenant-id
      - <aad-tenant-id>
      provideClusterInfo: false
      interactiveMode: Never
```
This flow also needs its own Secret for the certificate:
```console
kubectl create secret tls spn-cert --namespace fleet-system --cert=/path/to/spn.crt --key=/path/to/spn.key
kubectl create secret generic hub-kubeconfig --namespace fleet-system --from-file=kubeconfig=./kubeconfig.yaml

helm install member-agent ./charts/member-agent/ \
  --namespace fleet-system \
  --create-namespace \
  -f values-kubelogin-init.yaml \
  --set config.memberClusterName=<member-cluster-name> \
  --set useKubeconfig=true \
  --set config.hubKubeconfigSecretName=hub-kubeconfig \
  --set 'extraVolumes[1].name=spn-cert' \
  --set 'extraVolumes[1].secret.secretName=spn-cert' \
  --set 'extraVolumeMounts[1].name=spn-cert' \
  --set 'extraVolumeMounts[1].mountPath=/etc/kubefleet/spn-cert' \
  --set 'extraVolumeMounts[1].readOnly=true'
```
`values-kubelogin-init.yaml` already occupies index `0` of `extraVolumes`/`extraVolumeMounts` (the
`kubelogin-bin` `emptyDir`), so the certificate Secret's volume/mount use index `1` here to avoid
overwriting it.
Equivalent `member-agent` invocation: `KUBE_CONFIG_PATH=/etc/kubefleet/hub-kubeconfig/kubeconfig --use-kubeconfig=true`

### AWS / EKS (IRSA)

Set up [IAM Roles for Service Accounts](https://docs.aws.amazon.com/eks/latest/userguide/iam-roles-for-service-accounts.html) on the member cluster, and grant the IAM role access to the hub EKS cluster (an EKS Access Entry or the `aws-auth` ConfigMap). The kubeconfig's exec stanza runs the small, purpose-built `aws-iam-authenticator` binary (lighter to bundle than the full `aws` CLI):

```yaml
users:
- name: fed
  user:
    exec:
      apiVersion: client.authentication.k8s.io/v1
      command: aws-iam-authenticator
      args:
      - token
      - -i
      - <hub-cluster-id>
      provideClusterInfo: false
      interactiveMode: Never
```

```console
kubectl create secret generic hub-kubeconfig --namespace fleet-system --from-file=kubeconfig=./kubeconfig.yaml

helm install member-agent ./charts/member-agent/ \
  --namespace fleet-system \
  --create-namespace \
  --set config.memberClusterName=<member-cluster-name> \
  --set useKubeconfig=true \
  --set config.hubKubeconfigSecretName=hub-kubeconfig \
  --set serviceAccountAnnotations."eks\.amazonaws\.com/role-arn"=<iam-role-arn>
```
Equivalent `member-agent` invocation: `KUBE_CONFIG_PATH=/etc/kubefleet/hub-kubeconfig/kubeconfig --use-kubeconfig=true`

No `podLabels` needed — IRSA only requires the ServiceAccount annotation.

### GCP / GKE (Workload Identity)

Set up [GKE Workload Identity](https://cloud.google.com/kubernetes-engine/docs/how-to/workload-identity) on the member cluster (binds the pod's ServiceAccount to a Google service account), and grant that service account the appropriate IAM role on the hub GKE cluster. The kubeconfig's exec stanza runs `gke-gcloud-auth-plugin`:

```yaml
users:
- name: fed
  user:
    exec:
      apiVersion: client.authentication.k8s.io/v1beta1
      command: gke-gcloud-auth-plugin
      args:
      - --use_application_default_credentials
      provideClusterInfo: true
```

```console
kubectl create secret generic hub-kubeconfig --namespace fleet-system --from-file=kubeconfig=./kubeconfig.yaml

helm install member-agent ./charts/member-agent/ \
  --namespace fleet-system \
  --create-namespace \
  --set config.memberClusterName=<member-cluster-name> \
  --set useKubeconfig=true \
  --set config.hubKubeconfigSecretName=hub-kubeconfig \
  --set serviceAccountAnnotations."iam\.gke\.io/gcp-service-account"=<gsa-email>
```
Equivalent `member-agent` invocation: `KUBE_CONFIG_PATH=/etc/kubefleet/hub-kubeconfig/kubeconfig --use-kubeconfig=true`

No `podLabels` needed here either — Workload Identity is resolved automatically for any pod running
under the annotated ServiceAccount.

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
