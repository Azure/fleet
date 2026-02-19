# KubeFleet Helm Charts

This directory contains Helm charts for deploying KubeFleet components.

## Available Charts

- **hub-agent**: The central controller that runs on the hub cluster, managing placement decisions, scheduling, and cluster inventory
- **member-agent**: The agent that runs on each member cluster, applying workloads and reporting cluster status

## Chart Versioning

**Important:** Chart versions match the KubeFleet release versions. When a KubeFleet release is tagged (e.g., `v0.2.1`), the Helm charts are published with the same version (`0.2.1`).

**Example:** To install KubeFleet v0.2.1, use:
```bash
helm install hub-agent oci://ghcr.io/kubefleet-dev/kubefleet/charts/hub-agent --version 0.2.1
```

This ensures consistency between the application version and the chart version, making it easy to know which chart version to use with each KubeFleet release.

## Using Published Charts

KubeFleet Helm charts are automatically published to both GitHub Container Registry (GHCR) as OCI artifacts and GitHub Pages as a traditional Helm repository.

### Option 1: OCI Registry (Recommended)

Install directly from GitHub Container Registry without adding a repository:

#### Hub Agent

```bash
# Install hub-agent on the hub cluster (replace VERSION with your desired release)
helm install hub-agent oci://ghcr.io/kubefleet-dev/kubefleet/charts/hub-agent \
  --version VERSION \
  --namespace fleet-system \
  --create-namespace
```

#### Member Agent

```bash
# Install member-agent on each member cluster (replace VERSION with your desired release)
helm install member-agent oci://ghcr.io/kubefleet-dev/kubefleet/charts/member-agent \
  --version VERSION \
  --namespace fleet-system \
  --create-namespace
```

### Option 2: Traditional Helm Repository

Add the repository and install from it:

```bash
# Add the KubeFleet Helm repository
helm repo add kubefleet https://kubefleet-dev.github.io/kubefleet/charts

# Update your local Helm chart repository cache
helm repo update

# Install hub-agent
helm install hub-agent kubefleet/hub-agent \
  --namespace fleet-system \
  --create-namespace

# Install member-agent
helm install member-agent kubefleet/member-agent \
  --namespace fleet-system \
  --create-namespace
```

### Installing Specific Versions

#### OCI Registry

```bash
# Install a specific version from OCI registry (e.g., v0.2.1 release)
helm install hub-agent oci://ghcr.io/kubefleet-dev/kubefleet/charts/hub-agent \
  --version 0.2.1 \
  --namespace fleet-system \
  --create-namespace
```

#### Traditional Repository

```bash
# List available versions
helm search repo kubefleet --versions

# Install a specific version (e.g., v0.2.1 release)
helm install hub-agent kubefleet/hub-agent \
  --version 0.2.1 \
  --namespace fleet-system \
  --create-namespace
```

### Upgrading Charts

#### OCI Registry

```bash
# Upgrade to a specific version (e.g., v0.2.1)
helm upgrade hub-agent oci://ghcr.io/kubefleet-dev/kubefleet/charts/hub-agent \
  --version 0.2.1 \
  --namespace fleet-system

helm upgrade member-agent oci://ghcr.io/kubefleet-dev/kubefleet/charts/member-agent \
  --version 0.2.1 \
  --namespace fleet-system
```

#### Traditional Repository

```bash
# Upgrade to latest version
helm upgrade hub-agent kubefleet/hub-agent --namespace fleet-system
helm upgrade member-agent kubefleet/member-agent --namespace fleet-system
```

## Chart Publishing

Charts are automatically published to both locations when:
- Changes are pushed to the `main` branch affecting chart files
- A version tag (e.g., `v1.0.0`) is created

**Published Locations:**
- **OCI Registry**: `oci://ghcr.io/kubefleet-dev/kubefleet/charts/{chart-name}`
- **GitHub Pages**: `https://kubefleet-dev.github.io/kubefleet/charts`

The publishing workflow is defined in `.github/workflows/chart.yml`.

## Development

### Local Installation

For development and testing, you can install charts directly from the local repository:

```bash
# Install from local path
helm install hub-agent ./charts/hub-agent --namespace fleet-system --create-namespace
helm install member-agent ./charts/member-agent --namespace fleet-system --create-namespace
```

### Linting

```bash
# Lint a chart
helm lint charts/hub-agent
helm lint charts/member-agent
```

### Packaging

```bash
# Package charts locally
helm package charts/hub-agent
helm package charts/member-agent
```

## Chart Documentation

For detailed documentation on each chart including configuration parameters, see:
- [Hub Agent Chart](./hub-agent/README.md)
- [Member Agent Chart](./member-agent/README.md)

## Contributing

When making changes to charts:
1. Update the chart version in `Chart.yaml` following [Semantic Versioning](https://semver.org/)
2. Update the `appVersion` if the application version changes
3. Run `helm lint` to validate your changes
4. Update the chart's README.md with any new parameters or changes
5. Test the chart installation locally before submitting a PR

## Support

For issues or questions about KubeFleet Helm charts, please:
- Check the [main documentation](https://kubefleet.dev/docs/)
- Review chart-specific READMEs
- Open an issue in the [GitHub repository](https://github.com/kubefleet-dev/kubefleet/issues)
