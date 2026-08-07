# Release Controller: High-Level Overview

## What is Release Controller?

Release Controller is a Kubernetes-based system that automates the creation, verification, and management of OpenShift release payloads. It monitors image streams, creates release images, runs verification tests, publishes release information, and manages the entire release lifecycle for OpenShift.

## What Problem Does It Solve?

OpenShift releases are complex collections of hundreds of container images that must be:
- **Coordinated**: All images must be built and tested together
- **Verified**: Releases must pass comprehensive test suites
- **Published**: Releases must be made available to users
- **Tracked**: Release status and metadata must be maintained
- **Audited**: Releases must be signed and auditable
- **Managed**: Old releases must be garbage collected

Managing this manually would be:
- Error-prone due to the complexity
- Time-consuming for release engineers
- Difficult to track and audit
- Inconsistent across releases

Release Controller solves these problems by providing automated orchestration of the entire release process, ensuring consistency, reliability, and traceability.

## Who Is It For?

### Primary Users

1. **OpenShift Release Engineering Team**: The team responsible for creating and managing OpenShift releases
2. **OpenShift Test Platform (TP) Team**: Teams maintaining the CI infrastructure that feeds releases
3. **OpenShift Developers**: Developers who need to understand how their changes become part of releases
4. **Release Operators**: Operators who need to troubleshoot release issues

### Skill Level Requirements

- **Basic Users**: Need to understand Kubernetes concepts and YAML configuration
- **Advanced Users**: Should be familiar with Kubernetes controllers, OpenShift ImageStreams, and CI/CD pipelines
- **Contributors**: Need strong Go skills, Kubernetes API knowledge, and understanding of release processes

## Key Features

### 1. Release Creation
- Monitors ImageStreams for new images
- Creates release payloads from image collections
- Generates release metadata and manifests
- Handles multi-architecture releases

### 2. Release Verification
- Launches ProwJobs for release verification
- Tracks verification job results
- Manages release acceptance criteria
- Handles verification failures

### 3. Release Publishing
- Publishes releases to container registries
- Creates release tags and metadata
- Updates release streams
- Manages release visibility

### 4. Release Payload Management
- Manages ReleasePayload Custom Resources
- Tracks payload creation status
- Handles payload acceptance/rejection
- Manages payload lifecycle

### 5. Image Mirroring
- Mirrors source ImageStreams to release streams
- Creates point-in-time snapshots
- Handles multi-registry scenarios
- Manages mirror job execution

### 6. Upgrade Graph Management
- Builds upgrade graphs between releases
- Validates upgrade paths
- Manages upgrade recommendations
- Tracks upgrade compatibility

### 7. Garbage Collection
- Removes old releases based on policies
- Manages release retention
- Cleans up unused resources
- Handles soft deletion

### 8. Auditing and Signing
- Audits release creation process
- Signs releases for security
- Tracks audit logs
- Manages signing keys

### 9. Web Interface
- Web UI for browsing releases
- REST API for programmatic access
- Release comparison tools
- Upgrade graph visualization

### 10. Jira Integration
- Updates Jira tickets for fixed issues
- Tracks release-related issues
- Manages release blockers

## Technology Stack

- **Language**: Go (100%)
- **Platform**: Kubernetes/OpenShift
- **Storage**: Kubernetes ImageStreams, GCS for artifacts
- **CI Integration**: Prow for verification jobs
- **Container Registry**: OpenShift Image Registry, Quay.io
- **Frontend**: HTML templates with JavaScript

## Repository Statistics

- **License**: Apache-2.0
- **Main Components**: 4 main executables
- **Custom Resources**: ReleasePayload CRD
- **Maintainers**: OpenShift Release Engineering Team

## Key Components

### Core Components

1. **release-controller** - Main controller orchestrating releases
2. **release-controller-api** - Web UI and REST API server
3. **release-payload-controller** - Manages ReleasePayload CRs
4. **release-reimport-controller** - Handles failed image imports

### Supporting Components

- **ReleasePayload CRD** - Custom resource for release tracking
- **Jira Integration** - Issue tracking and updates
- **Signer** - Release signing functionality
- **Audit Backends** - File and GCS audit storage

## Related Documentation

- [OpenShift Release Repository](https://github.com/openshift/release/tree/master/core-services/release-controller) - Canonical documentation
- [OpenShift CI Documentation](https://docs.ci.openshift.org/)
- [Prow Documentation](https://docs.prow.k8s.io/)

## Getting Started

For new users, we recommend starting with:
1. [Setup Guide](SETUP.md) - Get your development environment ready
2. [Usage Guide](USAGE.md) - Learn how to use the release controller
3. [Codebase Walkthrough](CODEBASE_WALKTHROUGH.md) - Understand the structure

For contributors:
1. [Contributing Guide](CONTRIBUTING_GUIDE.md) - Learn how to contribute
2. [Onboarding Guide](ONBOARDING.md) - Deep dive into the codebase

