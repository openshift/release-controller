# Release Controller - Beginner's Guide

## Overview

The **release-controller** is a Kubernetes controller system that automates the creation, verification, and management of OpenShift release payloads. It monitors image streams, creates release images, runs verification tests, and publishes release information.

> **Note**: For the canonical documentation, see the [OpenShift Release repository](https://github.com/openshift/release/tree/master/core-services/release-controller).

## What is a Release?

A **release** in OpenShift is a collection of container images that together form a complete, installable version of OpenShift. Each release includes:

- All container images needed to run OpenShift
- Metadata about the release (version, components, etc.)
- Upgrade paths between releases

## Target Audience

This guide is designed for:

- New contributors to the release-controller repository
- Developers who need to understand how releases are created
- Operators who need to troubleshoot release issues
- Anyone wanting to understand the OpenShift release process

## Repository Structure

```text
release-controller/
├── cmd/                          # Main executables
│   ├── release-controller/      # Core controller (manages releases)
│   ├── release-controller-api/  # Web UI and API server
│   ├── release-payload-controller/  # Manages ReleasePayload CRs
│   └── release-reimport-controller/ # Handles re-importing of imagestreamtags that failed initial import
├── pkg/                          # Shared packages
│   ├── release-controller/      # Core release logic
│   ├── releasepayload/          # ReleasePayload CRD logic
│   ├── jira/                    # Jira integration
│   └── signer/                  # Release signing
└── images/                       # Dockerfiles for each component
```

## Main Components

### 1. Release Controller (`cmd/release-controller/`)

**Purpose**: The core controller that orchestrates the entire release process.

**Key Responsibilities**:
- Monitors ImageStreams for changes
- Creates releases when new images are available
- Mirrors the inbound ImageStreams to create a Point-In-Time release
- Launches jobs for release-related tasks
- Manages release verification (ProwJobs)
- Handles garbage collection of old releases
- Audits and signs releases
- Updates Jira tickets for fixed issues

**Key Files**:
- `main.go`: Entry point, sets up clients and informers
- `controller.go`: Main controller structure and event handlers
- `sync.go`: Core sync logic for processing releases
- `sync_release.go`: Creates and manages release tags
- `sync_mirror.go`: Handles image stream mirroring
- `sync_verify.go`: Manages verification jobs
- `sync_gc.go`: Garbage collection of old releases
- `audit.go`: Release auditing functionality

### 2. Release Controller API (`cmd/release-controller-api/`)

**Purpose**: Provides a web interface and REST API for viewing releases.

**Key Responsibilities**:
- Serves web UI for browsing releases
- Provides REST endpoints for release information
- Displays upgrade graphs
- Shows release changelogs
- Compares releases

**Key Files**:
- `main.go`: HTTP server setup
- `http.go`: Main HTTP handlers
- `http_candidate.go`: Candidate release endpoints
- `http_changelog.go`: Changelog generation
- `http_compare.go`: Release comparison
- `http_upgrade_graph.go`: Upgrade graph visualization

### 3. Release Payload Controller (`cmd/release-payload-controller/`)

**Purpose**: Manages ReleasePayload Custom Resources (CRs).

**Key Responsibilities**:
- Updates payload creation conditions
- Monitors release mirror/creation jobs and updates ReleasePayload conditions
- Populates the initial ReleasePayload Status stanza
- Updates the payload accepted condition
- Updates the payload rejected condition
- Tracks verification job results

**Key Files**:
- `payload_creation_controller.go`: Updates the payload creation conditions
- `payload_verification_controller.go`: Populates the initial ReleasePayload Status stanza
- `payload_accepted_controller.go`: Updates the payload accepted condition
- `payload_rejected_controller.go`: Updates the payload rejected condition
- `job_state_controller.go`: Updates job states

### 4. Release Reimport Controller (`cmd/release-reimport-controller/`)

**Purpose**: Handles the re-importing of imagestreamtags that failed their initial import into the inbound ImageStream.

## How It Works

### Release Creation Flow

1. **Image Stream Update**: ART (Automated Release Team) updates an ImageStream (e.g., `ocp/4.15-art-latest`)

2. **Controller Detection**: The release-controller detects the change via Kubernetes informers

3. **Mirror Creation**: Controller creates a point-in-time mirror of the ImageStream to prevent image pruning

4. **Release Tag Creation**: Controller creates a new release tag (e.g., `4.15.0-0.nightly-2024-01-15-123456`)

5. **Release Image Build**: Controller launches a Kubernetes Job that runs `oc adm release new` to create the release image

6. **Verification Jobs**: Once the release image is ready, controller launches ProwJobs for:
   - Release analysis
   - Blocking verification tests
   - Informing verification tests
   - Upgrade tests (for stable releases)

7. **Status Updates**: Controller monitors job results and updates release status:
   - `Ready`: Release image created successfully
   - `Accepted`: All blocking jobs passed
   - `Rejected`: One or more blocking jobs failed
   - `Failed`: Release creation or processing failed

### Key Concepts

#### Image Streams
- **Source ImageStream**: The input ImageStream (e.g., `ocp/4.15-art-latest`) that contains component images
- **Release ImageStream**: The target ImageStream (e.g., `oc/release`) where release tags are created
- **Mirror ImageStream**: A point-in-time copy of the source to prevent image pruning

#### Release Phases
- **Pending**: Release tag created but not yet processed
- **Ready**: Release image created successfully
- **Accepted**: All verification tests passed
- **Rejected**: Verification tests failed
- **Failed**: Release creation or processing failed

#### Release Payloads vs ImageStream Tags

**ImageStream Tags** (Legacy):
- Traditional way of tracking releases
- Stored as tags on ImageStreams
- Used for backward compatibility

**ReleasePayloads** (Modern):
- Kubernetes CRs (Custom Resources) that represent releases
- Created by the release-controller
- Track job status, conditions, and results
- Provide better observability and management
- Migration from ImageStream tags is ongoing

## Configuration

### Release Configuration
Releases are configured via annotations on ImageStreams:

```yaml
apiVersion: image.openshift.io/v1
kind: ImageStream
metadata:
  name: 4.15-art-latest
  namespace: ocp
  annotations:
    release.openshift.io/config: |
      {
        "name": "4.15",
        "verify": {
          "prow": {
            "optional": [...],
            "upgrade": [...]
          }
        }
      }
```

### Controller Flags
Key flags for `release-controller`:
- `--release-namespace`: Namespace containing source ImageStreams
- `--job-namespace`: Namespace for running jobs
- `--prow-namespace`: Namespace for ProwJobs
- `--prow-config`: Path to Prow configuration
- `--release-architecture`: Target architecture (amd64, arm64, etc.)
- `--dry-run`: Test mode without making changes

## Quick Start

### Prerequisites
- Go 1.23+ installed
- Access to a Kubernetes cluster (or `api.ci` for OpenShift CI)
- `kubectl` or `oc` CLI configured
- KUBECONFIG pointing to your cluster

### Building and Running Locally

1. **Build the project**:
   ```bash
   make
   ```

2. **Run in dry-run mode** (recommended for testing):
   ```bash
   ./release-controller \
     --release-namespace ocp \
     --job-namespace ci-release \
     --dry-run \
     -v=4
   ```

3. **Access the web UI**:
   Navigate to `http://localhost:8080` in your browser

4. **View metrics**:
   Metrics are available at `http://localhost:8080/metrics`

### Running the API Server

```bash
./release-controller-api \
  --release-namespace ocp \
  --job-namespace ci-release \
  --port 8080
```

## API Endpoints

The release-controller-api provides several endpoints:

### Web UI Endpoints
- `/` - Main release dashboard
- `/releasestream/{release}` - View a specific release stream
- `/releasestream/{release}/release/{tag}` - View a specific release
- `/releasestream/{release}/latest` - Latest release for a stream
- `/releasestream/{release}/candidates` - Candidate releases
- `/changelog` - Release changelog
- `/graph` - Upgrade graph visualization
- `/dashboards/overview` - Overview dashboard
- `/dashboards/compare` - Compare releases

### REST API Endpoints
- `GET /api/v1/releasestream/{release}/tags` - List all tags for a release stream
- `GET /api/v1/releasestream/{release}/latest` - Get latest release
- `GET /api/v1/releasestream/{release}/candidate` - Get candidate release
- `GET /api/v1/releasestream/{release}/release/{tag}` - Get release info
- `GET /api/v1/releasestream/{release}/config` - Get release configuration
- `GET /api/v1/releasestreams/accepted` - List accepted streams
- `GET /api/v1/releasestreams/rejected` - List rejected streams
- `GET /api/v1/releasestreams/all` - List all streams

### Example API Usage

```bash
# Get latest release for 4.15
curl http://localhost:8080/api/v1/releasestream/4.15/latest

# Get all tags for a release stream
curl http://localhost:8080/api/v1/releasestream/4.15/tags

# Get release information
curl http://localhost:8080/api/v1/releasestream/4.15/release/4.15.0-0.nightly-2024-01-15-123456
```

## Common Tasks

### Viewing Releases
Access the web UI at `http://localhost:8080` (when running locally)

### Checking Release Status
```bash
# Check ImageStream tag
oc get imagestreamtag -n ocp release:4.15.0-0.nightly-2024-01-15-123456

# Check ReleasePayload
oc get releasepayloads -n ocp
oc describe releasepayload 4.15.0-0.nightly-2024-01-15-123456 -n ocp
```

### Viewing Release Payloads
```bash
# List all ReleasePayloads
oc get releasepayloads -n ocp

# Get details of a specific ReleasePayload
oc get releasepayload 4.15.0-0.nightly-2024-01-15-123456 -n ocp -o yaml

# Check conditions
oc get releasepayload 4.15.0-0.nightly-2024-01-15-123456 -n ocp -o jsonpath='{.status.conditions}'
```

### Checking Job Status
```bash
# List jobs in job namespace
oc get jobs -n ci-release

# Check specific job logs
oc logs job/<job-name> -n ci-release

# List ProwJobs
oc get prowjobs -n ci

# Check ProwJob status
oc get prowjob <prowjob-name> -n ci -o yaml
```

### Viewing Controller Logs
```bash
# Release controller logs
oc logs -n <namespace> deployment/release-controller

# Release controller API logs
oc logs -n <namespace> deployment/release-controller-api

# Release payload controller logs
oc logs -n <namespace> deployment/release-payload-controller
```

## Troubleshooting

### Release Not Created

**Symptoms**: New images in source ImageStream but no release tag created

**Check**:
1. Verify ImageStream has the `release.openshift.io/config` annotation:
   ```bash
   oc get imagestream 4.15-art-latest -n ocp -o yaml | grep release.openshift.io/config
   ```
2. Check controller logs for errors:
   ```bash
   oc logs -n <namespace> deployment/release-controller --tail=100
   ```
3. Verify controller is watching the correct namespace
4. Check if release stream has reached end-of-life

### Jobs Failing

**Symptoms**: Release creation job or verification jobs failing

**Check**:
1. View job logs:
   ```bash
   oc logs job/<job-name> -n <namespace>
   ```
2. Check job events:
   ```bash
   oc describe job <job-name> -n <namespace>
   ```
3. Verify image pull permissions
4. Check Prow configuration if ProwJobs are failing
5. Verify cluster resources (CPU, memory)

### Release Stuck in Pending

**Symptoms**: Release tag created but never moves to Ready state

**Check**:
1. Verify release creation job completed:
   ```bash
   oc get jobs -n <job-namespace> | grep <release-tag>
   ```
2. Check if image stream mirror was created:
   ```bash
   oc get imagestreams -n <namespace> | grep mirror
   ```
3. Check controller logs for errors
4. Verify job namespace has sufficient resources

### Release Not Accepted

**Symptoms**: Release is Ready but not Accepted

**Check**:
1. Check ReleasePayload conditions:
   ```bash
   oc get releasepayload <tag> -n ocp -o jsonpath='{.status.conditions}'
   ```
2. Check blocking job status:
   ```bash
   oc get prowjobs -n ci -l release.openshift.io/verify=true
   ```
3. Review job results in the web UI
4. Check if any blocking jobs failed

### Controller Not Responding

**Symptoms**: No updates to releases, logs show no activity

**Check**:
1. Verify controller pod is running:
   ```bash
   oc get pods -n <namespace> | grep release-controller
   ```
2. Check for crash loops:
   ```bash
   oc describe pod <pod-name> -n <namespace>
   ```
3. Verify kubeconfig permissions
4. Check for resource constraints (CPU, memory limits)

## Development

### Building
```bash
make
```

### Testing
```bash
make test
```

### Running Tests
```bash
go test ./...
```

### Code Generation
```bash
make update-codegen
make verify-codegen
```

## Glossary

- **ART (Automated Release Team)**: Team responsible for building and publishing OpenShift component images
- **ImageStream**: OpenShift resource that tracks container images and provides image tags
- **Release Payload**: Kubernetes CRD that represents a release and tracks its status
- **ProwJob**: Job definition used by Prow for running release verification tests
- **Release Tag**: A specific version of a release (e.g., `4.15.0-0.nightly-2024-01-15-123456`)
- **Release Image**: A container image containing all components needed for an OpenShift release
- **Blocking Job**: A verification job that must pass for a release to be accepted
- **Informing Job**: A verification job that provides information but doesn't block acceptance
- **Upgrade Graph**: A graph showing valid upgrade paths between releases
- **Mirror**: A point-in-time copy of an ImageStream to prevent image pruning

## Key Dependencies

- **Kubernetes/OpenShift**: Container orchestration platform
- **Prow**: System for running release verification tests
- **ImageStreams**: OpenShift image management
- **Jira**: Issue tracking (optional, for verification)

## Further Reading

- [OpenShift Release Documentation](https://github.com/openshift/release/tree/master/core-services/release-controller)
- [Release Tool Documentation](./release-tool.md)
- [Supply Chain Security](./supply-chain-security.md)
- [Architecture Diagrams](./ARCHITECTURE_DIAGRAMS.md)

## Getting Help

- Check controller logs: `oc logs -n <namespace> deployment/release-controller`
- Review job logs in the job namespace
- Check ReleasePayload status and conditions
- Review ImageStream annotations and tags
- Check the web UI for visual status information
- Review the [Architecture Diagrams](./ARCHITECTURE_DIAGRAMS.md) for system understanding

## Contributing

When contributing to this repository:
1. Follow the existing code style
2. Add tests for new functionality
3. Update documentation as needed
4. Ensure all tests pass: `make test`
5. Verify code generation: `make verify-codegen`
