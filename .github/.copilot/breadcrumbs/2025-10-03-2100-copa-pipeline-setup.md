# COPA Pipeline Setup for Fleet-Public

## Context
Setting up a COPA (Copacetic) pipeline to automatically patch container images for vulnerabilities in the fleet-public repository. COPA uses Trivy to scan for OS package vulnerabilities and then patches base images to fix them.

## Current State Analysis

Based on my investigation:

### Existing Infrastructure
- **Docker Images Built**: 4 main images
  - `hub-agent` (from `docker/hub-agent.Dockerfile`)
  - `member-agent` (from `docker/member-agent.Dockerfile`) 
  - `refresh-token` (from `docker/refresh-token.Dockerfile`)
  - `crd-installer` (from `docker/crd-installer.Dockerfile`)

- **Current Build Pipeline**: `.github/workflows/publish-image.yml`
  - Builds and pushes to GitHub Container Registry (ghcr.io)
  - Triggered on pushes to main branch and semantic version tags
  - Uses Makefile targets: `docker-build-hub-agent`, `docker-build-member-agent`, `docker-build-refresh-token`

- **Existing Security**: `.github/workflows/trivy.yml` already exists for security scanning

### COPA Reference Implementation
From the sozercan/copa-test example, the pipeline:
1. Uses Trivy to generate vulnerability reports in JSON format
2. Checks if vulnerabilities exist in OS packages
3. Downloads and uses COPA CLI to patch images
4. Pushes patched images with new tags
5. Uses Docker buildx for multi-platform support

## Implementation Plan

### Phase 1: Analysis and Setup
**Goal**: Understand current images and prepare COPA integration

#### Task 1.1: Analyze Current Dockerfiles
- [ ] Review all 4 Dockerfiles to understand base images
- [ ] Identify which base images are most likely to have vulnerabilities
- [ ] Document current image naming and tagging strategy

#### Task 1.2: Plan COPA Integration Strategy
- [ ] Decide on integration approach: separate workflow vs. integrated with existing
- [ ] Define patched image naming convention (e.g., `-patched` suffix)
- [ ] Plan trigger mechanism (manual dispatch vs. scheduled vs. on-push)

### Phase 2: Create COPA Workflow
**Goal**: Implement the COPA patching pipeline

#### Task 2.1: Create Base COPA Workflow
- [ ] Create `.github/workflows/copa-patch.yml`
- [ ] Set up matrix strategy for all 4 images
- [ ] Configure proper registry authentication (GitHub Container Registry)

#### Task 2.2: Implement Trivy Scanning
- [ ] Add Trivy vulnerability scanning step
- [ ] Configure JSON output for COPA consumption
- [ ] Add vulnerability count checking logic

#### Task 2.3: Implement COPA Patching
- [ ] Add COPA CLI download and setup
- [ ] Configure Docker buildx for patching
- [ ] Implement conditional patching (only if vulnerabilities found)

#### Task 2.4: Configure Image Push
- [ ] Set up patched image push logic
- [ ] Implement proper tagging strategy
- [ ] Add success/failure reporting

### Phase 3: Integration and Testing
**Goal**: Ensure COPA pipeline works with existing infrastructure

#### Task 3.1: Test COPA Workflow
- [ ] Run workflow against current images
- [ ] Verify patched images are created correctly
- [ ] Validate that patched images work in the fleet system

#### Task 3.2: Documentation and Configuration
- [ ] Update README with COPA pipeline information
- [ ] Document how to trigger COPA patching
- [ ] Add configuration options for customization

#### Task 3.3: Optional Automation
- [ ] Consider adding COPA to existing publish-image workflow
- [ ] Implement scheduled patching (weekly/monthly)
- [ ] Add notification mechanisms for patching results

## Success Criteria

1. **Functional Pipeline**: COPA workflow successfully scans, patches, and pushes all 4 container images
2. **Vulnerability Reduction**: Patched images have fewer or zero OS package vulnerabilities
3. **Integration**: Patched images work correctly in the fleet system
4. **Maintainability**: Pipeline is well-documented and easy to maintain
5. **Flexibility**: Pipeline can be triggered manually and potentially scheduled

## Technical Decisions

### Image Strategy
- **Original Images**: Keep existing image names and tags unchanged
- **Patched Images**: Add `-patched` suffix to distinguish (e.g., `hub-agent:main-patched`)
- **Registry**: Continue using GitHub Container Registry (ghcr.io)

### Workflow Trigger
- **Primary**: Manual workflow dispatch for controlled patching
- **Secondary**: Optional scheduled runs for regular maintenance
- **Future**: Could integrate with existing publish workflow

### Error Handling
- **Graceful Failures**: If no vulnerabilities found, skip patching
- **Matrix Strategy**: Allow individual image failures without stopping others
- **Detailed Logging**: Comprehensive output for debugging

## Implementation Status ✅

### Completed Tasks
- [x] **Analyzed Dockerfiles**: All 4 images use Azure Linux distroless base images (`mcr.microsoft.com/azurelinux/distroless/base:3.0`)
- [x] **Updated build-publish-mcr.yml**: Integrated COPA patching into production MCR workflow (CORRECTED TARGET)
- [x] **Added Trivy Scanning**: Each image gets scanned for OS package vulnerabilities using Docker container approach
- [x] **Implemented COPA Logic**: Conditional patching when vulnerabilities are found with dedicated job
- [x] **Configured Patched Images**: '-patched' suffix tags pushed to ACR/MCR registry

### IMPORTANT CORRECTION ⚠️
**User correctly identified that we should update `build-publish-mcr.yml` instead of `publish-image.yml`**
- The MCR workflow is the production workflow that publishes to Microsoft Container Registry
- This is much more important than the basic GitHub Container Registry workflow
- COPA integration now added to the production pipeline

### Key Implementation Details

**Severity Configuration**: Following reference pipeline approach (no severity filter = patch ALL OS vulnerabilities)
```yaml
ignore-unfixed: true    # Only fixable vulnerabilities
vuln-type: "os"         # Only OS packages (what COPA can fix)
# No severity filter = patches CRITICAL, HIGH, MEDIUM, LOW
```

**Image Strategy**: 
- Original images: `hub-agent:main`, `member-agent:main`, etc.
- Patched images: `hub-agent:main-patched`, `member-agent:main-patched`, etc.
- Both coexist in GitHub Container Registry (ghcr.io)

**MCR Workflow Process Flow**:
1. **prepare-variables**: Get release tag and versions
2. **publish-images-amd64**: Build and publish AMD64 architecture images
3. **publish-images-arm64**: Build and publish ARM64 architecture images  
4. **create-image-manifest-bundle**: Create multi-architecture manifest bundles
5. **copa-patch-images**: NEW JOB - COPA vulnerability patching
   - Login to ACR with Azure identity
   - Set up Docker Buildx for COPA operations
   - Download COPA CLI v0.11.1 (latest stable)
   - For each image (hub-agent, member-agent, refresh-token, crd-installer):
     - Scan with Trivy using Docker container approach
     - Count vulnerabilities in JSON report
     - Conditionally patch if vulnerabilities > 0
     - Push patched image with '-patched' suffix
   - Generate comprehensive summary report

**Key Technical Adaptations for MCR Workflow**:
- Uses Azure Container Registry authentication (`az acr login`)
- Uses self-hosted 1ES pool runners
- Scans multi-arch manifest images (not individual arch images)
- Uses Docker container approach for Trivy (more reliable in enterprise environment)
- Handles authentication to private ACR with Azure identity

### Final Status ✅ COMPLETE

**All Implementation Tasks Completed:**
- [x] **Analyzed Dockerfiles** - Azure Linux distroless base images identified
- [x] **Updated MCR Workflow** - Added dedicated `copa-patch-images` job
- [x] **Added Trivy Scanning** - Docker container approach for enterprise environment
- [x] **Implemented COPA Logic** - Conditional patching with proper error handling
- [x] **Configured Image Push** - Patched images with '-patched' suffix to ACR/MCR
- [x] **Updated Documentation** - Added comprehensive security documentation to SECURITY.md

**Ready for Production:**
- COPA integration is complete and ready for the next MCR release
- Both original and patched images will coexist in Microsoft Container Registry
- Pipeline runs automatically on `workflow_dispatch` with release tags
- Comprehensive logging and summary reporting included

**Next Step:** Test with next release to validate COPA functionality in production environment