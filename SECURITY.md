# Security

The KubeFleet maintainers takes the security of the project very seriously; we greatly welcomes
and appreciates any responsible disclosures of security vulnerabilities.

If you believe you have found a security vulnerability in the repository, please follow the steps
below to report it to the KubeFleet team.

## Reporting Security Issues

**Please do not report security vulnerabilities through public GitHub issues.** Instead, 
report them to the [KubeFleet maintainers](mailto:kubefleet-maintainers@googlegroups.com).
We prefer all communications to be in English.

You should receive a response as soon as possible. If for some reason you do not, please
follow up via email to ensure we received your original message.

Please include the requested information listed below (as much as you can provide) to help
us better understand the nature and scope of the possible issue:

    * Type of issue (e.g. buffer overflow, SQL injection, cross-site scripting, etc.)
    * Full paths of source file(s) related to the manifestation of the issue
    * The location of the affected source code (tag/branch/commit or direct URL)
    * Any special configuration required to reproduce the issue
    * Step-by-step instructions to reproduce the issue
    * Proof-of-concept or exploit code (if possible)
    * Impact of the issue, including how an attacker might exploit the issue

This information will help us process your report more quickly.

Thanks for helping KubeFleet to become more secure!

## Container Image Security & COPA Integration

### Automated Vulnerability Patching

Azure Fleet uses [COPA (Copacetic)](https://github.com/project-copacetic/copacetic) to automatically patch container images for OS package vulnerabilities. COPA integration is built into our production release pipeline.

#### How COPA Works

1. **Vulnerability Scanning**: Each container image is scanned with [Trivy](https://github.com/aquasecurity/trivy) for OS package vulnerabilities
2. **Conditional Patching**: If fixable OS vulnerabilities are found, COPA automatically creates patched versions
3. **Dual Image Availability**: Both original and patched images are published to Microsoft Container Registry (MCR)

#### Image Availability

When vulnerabilities are found and patched, you'll have access to both versions:

**Original Images:**
- `mcr.microsoft.com/aks/fleet/hub-agent:v1.x.x`
- `mcr.microsoft.com/aks/fleet/member-agent:v1.x.x`
- `mcr.microsoft.com/aks/fleet/refresh-token:v1.x.x`
- `mcr.microsoft.com/aks/fleet/crd-installer:v1.x.x`

**Patched Images (when vulnerabilities exist):**
- `mcr.microsoft.com/aks/fleet/hub-agent:v1.x.x-patched`
- `mcr.microsoft.com/aks/fleet/member-agent:v1.x.x-patched`
- `mcr.microsoft.com/aks/fleet/refresh-token:v1.x.x-patched`
- `mcr.microsoft.com/aks/fleet/crd-installer:v1.x.x-patched`

#### Security Benefits

- **Proactive Protection**: Vulnerabilities are patched immediately upon release
- **No Functionality Changes**: COPA only patches OS packages, application code remains unchanged
- **Choice & Flexibility**: Users can choose between original or patched images based on their security requirements
- **Transparency**: Build logs show exactly which vulnerabilities were patched

#### Base Image Security

All Fleet container images use **Azure Linux distroless base images** (`mcr.microsoft.com/azurelinux/distroless/base:3.0`), which provide:
- Minimal attack surface (fewer packages = fewer vulnerabilities)
- Regular security updates from Microsoft
- Production-ready stability and support

#### COPA Configuration

Our COPA integration follows security best practices:
- **Vulnerability Types**: Only OS package vulnerabilities (what COPA can fix)
- **Severity Levels**: All severity levels (CRITICAL, HIGH, MEDIUM, LOW) are patched
- **Fixable Only**: Only vulnerabilities with available fixes are addressed
- **Multi-Architecture**: Works with both AMD64 and ARM64 architectures

For more information about COPA, visit the [official Copacetic project](https://github.com/project-copacetic/copacetic).
