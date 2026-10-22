# Arc Member Cluster Agents Helm Chart

Packages the Fleet member agent and the Fleet networking agents as a single Helm chart for deployment to
[Azure Arc-enabled Kubernetes](https://learn.microsoft.com/azure/azure-arc/kubernetes/overview) clusters, allowing
an Arc-connected cluster to join a fleet as a member cluster.

This chart is consumed as an Arc cluster extension, not installed directly with `helm install`. Arc Extensions
require every component to ship in one chart, which is why the member agent and the networking agents are bundled
here rather than published separately.

See `values.yaml` for the full set of parameters. This file covers only what that file does not show.

## Contents

Three Deployments, all in the hardcoded `fleet-system` namespace: `fleet-member-agent`,
`fleet-mcs-controller-manager`, and `fleet-member-net-controller-manager`. Each also runs an `addon-token-adapter`
and a `refresh-token` container to obtain hub credentials.

`crdinstaller.enabled` adds a `crd-installer` **init** container to `fleet-member-agent` — it runs to completion
before the agent starts, rather than alongside it.

## Publishing

The chart is built and pushed by the `build-publish-mcr` GitHub workflow. Agent images are built and published to
MCR by a separate pipeline; the workflow only resolves their already-published tags.

Images are published to MCR as `<release tag>-<revision>` (for example `v0.18.16-1`), where the revision is bumped
on every rebuild of the same source. The workflow selects the highest revision and derives the chart version from
it by stripping the leading `v`, so image tag `v0.18.16-1` produces chart version `0.18.16-1`.

The chart is checked out at the plain release tag, which can predate the move to `aks/v2/fleet`. The workflow rewrites
the legacy `mcr.microsoft.com/aks/fleet/` prefix to `MCR_FLEET_REPO`, which is needed only while new image revisions
are published from release tags whose chart still uses that prefix. It then fails before publishing if any image the
rendered chart references is missing from MCR.

The image tags in `values.yaml` are placeholders of the form `${MEMBER_AGENT_IMAGE_VERSION}`, substituted by
`envsubst` during `make helm-package-arc-member-cluster-agents`. That target rewrites `values.yaml` in place, so
after running it locally restore the file with `git checkout charts/member-agent-arc/values.yaml`.

## Values that do not behave as their names suggest

| Parameter | Actual behavior |
|:---|:---|
| `namespace` | Not consumed by any template; all namespaces are hardcoded to `fleet-system`. Setting it has no effect. |
| `Azure.proxySettings.httpsProxy` | Not consumed by any template. Setting it has no effect. |
| `Azure.proxySettings.httpProxy` | Sets the `HTTPS_PROXY` environment variable, not `HTTP_PROXY`. |
| `Azure.proxySettings.proxyCert` | Mounted and used as `SSL_CERT_FILE` only when `isProxyEnabled` is also set. |
| `Azure.proxySettings.isCustomCert` | Absent from `values.yaml`; mounts the proxy certificate for the MCS controller without enabling the proxy. |
| `propertyProvider` | Absent from `values.yaml`; the member agent runs with no property provider when empty. |

## Testing

`charts_test.go` renders each template with a maximal set of values to confirm the output is valid YAML:

```console
go test ./charts/member-agent-arc/...
```
