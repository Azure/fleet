# Hub agent controller Helm Chart

## Install Chart

### Default Installation (Self-Signed Certificates)

```console
# Helm install with fleet-system namespace already created
helm install hub-agent ./charts/hub-agent/
```

### Installation with cert-manager

When using cert-manager for certificate management, install cert-manager as a prerequisite first:

```console
# Install cert-manager (omit --version to get latest, or specify a version like --version v1.16.2)
# Note: See CERT_MANAGER_VERSION in .github/workflows/ci.yml for the version tested in CI
helm repo add jetstack https://charts.jetstack.io
helm repo update
helm install cert-manager jetstack/cert-manager \
  --namespace cert-manager \
  --create-namespace \
  --set crds.enabled=true

# Then install hub-agent with cert-manager enabled
helm install hub-agent ./charts/hub-agent --set useCertManager=true --set enableWorkload=true --set enableWebhook=true
```

This configures cert-manager to manage webhook certificates.

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
| `enableWebhook`                           | Enable webhook server                                                                      | `true`                                           |
| `webhookServiceName`                      | Webhook service name                                                                       | `fleetwebhook`                                   |
| `enableGuardRail`                         | Enable guard rail webhook configurations                                                   | `true`                                           |
| `webhookClientConnectionType`             | Connection type for webhook client (service or url)                                        | `service`                                        |
| `useCertManager`                          | Use cert-manager for webhook certificate management (requires `enableWorkload=true`) | `false`                                          |
| `webhookCertSecretName`                   | Name of the Secret where cert-manager stores the certificate                               | `fleet-webhook-server-cert`                      |
| `enableV1Beta1APIs`                       | Watch for v1beta1 APIs                                                                     | `true`                                           |
| `hubAPIQPS`                               | QPS for fleet-apiserver (not including events/node heartbeat)                              | `250`                                            |
| `hubAPIBurst`                             | Burst for fleet-apiserver (not including events/node heartbeat)                            | `1000`                                           |
| `MaxConcurrentClusterPlacement`           | Max concurrent ClusterResourcePlacement operations                                         | `100`                                            |
| `ConcurrentResourceChangeSyncs`           | Max concurrent resourceChange reconcilers                                                  | `20`                                             |
| `logFileMaxSize`                          | Max log file size before rotation                                                          | `1000000`                                        |
| `MaxFleetSizeSupported`                   | Max number of member clusters supported                                                    | `100`                                            |
| `resourceSnapshotCreationMinimumInterval` | The minimum interval at which resource snapshots could be created.                         | `30s`                                            |
| `resourceChangesCollectionDuration`       | The duration for collecting resource changes into one snapshot.                            | `15s`                                            |
| `enableWorkload`                          | Enable kubernetes builtin workload to run in hub cluster.                                  | `false`                                          |

## Certificate Management

The hub-agent supports two modes for webhook certificate management:

### Automatic Certificate Generation (Default)

By default, the hub-agent generates certificates automatically at startup. This mode:
- Requires no external dependencies
- Works out of the box
- Certificates are valid for 10 years
- **Limitation: Only supports single replica deployment** (replicaCount must be 1)

### cert-manager (Optional)

When `useCertManager=true`, certificates are managed by cert-manager. This mode:
- Requires cert-manager to be installed as a prerequisite
- Requires `enableWorkload=true` to allow cert-manager pods to run in the hub cluster (without this, pod creation would be blocked by the webhook)
- Requires `enableWebhook=true` because cert-manager is only used for webhook certificate management
- Handles certificate rotation automatically (90-day certificates)
- Follows industry-standard certificate management practices
- **Supports high availability with multiple replicas** (replicaCount > 1)
- Suitable for production environments

To switch to cert-manager mode:
```console
# Install cert-manager first (omit --version to get latest, or specify a version like --version v1.16.2)
# Note: See CERT_MANAGER_VERSION in .github/workflows/ci.yml for the version tested in CI
helm repo add jetstack https://charts.jetstack.io
helm repo update
helm install cert-manager jetstack/cert-manager \
  --namespace cert-manager \
  --create-namespace \
  --set crds.enabled=true

# Then install hub-agent with cert-manager enabled
helm install hub-agent ./charts/hub-agent --set useCertManager=true --set enableWorkload=true --set enableWebhook=true
```

The `webhookCertSecretName` parameter specifies the Secret name for the certificate:
- Default: `fleet-webhook-server-cert`
- When using cert-manager, this is where cert-manager stores the certificate
- Must match the secret name referenced in the deployment volume mount

Example with custom secret name:
```console
helm install hub-agent ./charts/hub-agent \
  --set useCertManager=true \
  --set enableWorkload=true \
  --set webhookCertSecretName=my-webhook-secret
```