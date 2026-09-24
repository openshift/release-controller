# Codebase Walkthrough

## Repository Structure

The Release Controller repository follows a standard Go project layout:

```
release-controller/
├── cmd/                    # Command-line tools and applications
├── pkg/                    # Shared libraries and packages
├── images/                 # Container image definitions
├── hack/                   # Build scripts and utilities
├── docs/                   # Documentation (this directory)
├── test/                   # Test files and test data
├── artifacts/              # Generated artifacts (CRDs)
├── vendor/                 # Vendored dependencies
├── Makefile               # Build automation
├── go.mod                  # Go module definition
├── go.sum                  # Go module checksums
├── README.md              # Main README
└── OWNERS                 # Code owners file
```

## Directory Details

### `/cmd` - Command-Line Tools

This directory contains all executable components.

#### Core Components

**Release Controller (`cmd/release-controller/`):**
- `main.go` - Entry point, sets up clients and informers
- `controller.go` - Main controller structure and event handlers
- `sync.go` - Core sync logic orchestrating all operations
- `sync_release.go` - Creates and manages release tags
- `sync_release_payload.go` - Manages ReleasePayload creation
- `sync_mirror.go` - Handles image stream mirroring
- `sync_verify.go` - Manages verification jobs
- `sync_verify_prow.go` - Prow job verification logic
- `sync_upgrade.go` - Upgrade graph management
- `sync_gc.go` - Garbage collection of old releases
- `sync_tags.go` - Tag management
- `sync_publish.go` - Release publishing
- `sync_analysis.go` - Release analysis
- `audit.go` - Release auditing functionality
- `jira.go` - Jira integration
- `periodic.go` - Periodic job management
- `prune_graph.go` - Upgrade graph pruning
- `expectations.go` - Release expectations management
- `cluster_distribution.go` - Cluster distribution logic

**Release Controller API (`cmd/release-controller-api/`):**
- `main.go` - HTTP server setup
- `http.go` - Main HTTP handlers
- `controller.go` - API controller logic
- `http_candidate.go` - Candidate release endpoints
- `http_changelog.go` - Changelog generation
- `http_compare.go` - Release comparison
- `http_upgrade_graph.go` - Upgrade graph visualization
- `http_helper.go` - HTTP helper functions
- `static/` - Static web assets (HTML, CSS, JS)

**Release Payload Controller (`cmd/release-payload-controller/`):**
- `main.go` - Entry point for payload controller

**Release Reimport Controller (`cmd/release-reimport-controller/`):**
- `main.go` - Entry point for reimport controller

### `/pkg` - Shared Packages

**Core Release Logic:**
- `pkg/release-controller/` - Core release controller logic
  - `release.go` - Release data structures and logic
  - `release_info.go` - Release information management
  - `release_payload.go` - ReleasePayload handling
  - `prowjob.go` - ProwJob integration
  - `upgrades.go` - Upgrade path logic
  - `mirror.go` - Image mirroring logic
  - `cache.go` - Caching utilities
  - `types.go` - Type definitions
  - `semver.go` - Semantic versioning
  - `error.go` - Error handling
  - `listers.go` - Kubernetes listers

**ReleasePayload Management:**
- `pkg/releasepayload/` - ReleasePayload CRD logic
  - `utils.go` - Utility functions
  - `conditions/` - Condition management
  - `controller/` - Controller helpers
  - `jobstatus/` - Job status tracking
  - `jobrunresult/` - Job run results
  - `v1alpha1helpers/` - API version helpers

**Release Payload Controller:**
- `pkg/cmd/release-payload-controller/` - Payload controller implementation
  - `controller.go` - Main controller
  - `cmd.go` - Command setup
  - `config.go` - Configuration
  - `payload_creation_controller.go` - Payload creation
  - `payload_mirror_controller.go` - Payload mirroring
  - `payload_verification_controller.go` - Verification
  - `payload_accepted_controller.go` - Acceptance logic
  - `payload_rejected_controller.go` - Rejection logic
  - `prowjob_controller.go` - ProwJob management
  - `job_state_controller.go` - Job state tracking
  - `release_creation_job_controller.go` - Creation jobs
  - `release_mirror_job_controller.go` - Mirror jobs
  - `legacy_job_status_controller.go` - Legacy job status

**Release Reimport Controller:**
- `pkg/cmd/release-reimport-controller/` - Reimport controller
  - `controller.go` - Reimport logic
  - `cmd.go` - Command setup

**APIs:**
- `pkg/apis/release/` - API definitions
  - `v1alpha1/` - ReleasePayload CRD
    - `types.go` - CRD type definitions
    - `register.go` - API registration

**Integrations:**
- `pkg/jira/` - Jira integration
- `pkg/signer/` - Release signing
- `pkg/prow/` - Prow integration utilities
- `pkg/rhcos/` - RHCOS image handling

**Client Libraries:**
- `pkg/client/` - Generated Kubernetes clients
  - `clientset/` - Client set
  - `informers/` - Informers for watching
  - `listers/` - Listers for reading

### `/images` - Container Images

Container image definitions for each component:
- `release-controller/Dockerfile`
- `release-controller-api/Dockerfile`
- `release-payload-controller/Dockerfile`
- `release-reimport-controller/Dockerfile`

### `/hack` - Build Scripts

Utility scripts:
- `update-codegen.sh` - Code generation
- `verify-codegen.sh` - Verify generated code
- `release-tool.py` - Release tooling
- `tools.go` - Build tools

### `/test` - Test Files

- `test/testdata/` - Test data files
  - `jobs.yaml` - Test job definitions
  - `prow.yaml` - Test Prow configs

## Key Files and Modules

### Main Entry Points

**Release Controller (`cmd/release-controller/main.go`):**
- Initializes Kubernetes clients
- Sets up informers for ImageStreams and ReleasePayloads
- Starts controller loops
- Sets up HTTP server for metrics

**Release Controller API (`cmd/release-controller-api/main.go`):**
- Sets up HTTP server
- Registers route handlers
- Serves static files
- Provides REST API endpoints

**Release Payload Controller (`cmd/release-payload-controller/main.go`):**
- Initializes controller manager
- Registers sub-controllers
- Starts reconciliation loops

### Core Components

**Sync Logic (`cmd/release-controller/sync.go`):**
- Main sync function orchestrating all operations
- Calls individual sync functions
- Handles errors and retries

**Release Management (`cmd/release-controller/sync_release.go`):**
- Creates release tags
- Manages release lifecycle
- Handles release configuration

**ReleasePayload Management (`cmd/release-controller/sync_release_payload.go`):**
- Creates ReleasePayload CRs
- Updates payload status
- Manages payload conditions

**Mirroring (`cmd/release-controller/sync_mirror.go`):**
- Mirrors source ImageStreams
- Creates point-in-time snapshots
- Manages mirror jobs

**Verification (`cmd/release-controller/sync_verify.go`):**
- Launches verification jobs
- Tracks job results
- Updates verification status

## Component Interactions

### How Release Controller Works

1. **ImageStream Monitoring** (`cmd/release-controller/controller.go`):
   - Watches ImageStreams via informers
   - Detects new images or updates
   - Triggers sync operations

2. **Release Creation** (`cmd/release-controller/sync_release.go`):
   - Checks release configuration
   - Creates ReleasePayload if needed
   - Launches creation job

3. **Payload Management** (`cmd/release-controller/sync_release_payload.go`):
   - Creates ReleasePayload CR
   - Updates payload status
   - Manages payload lifecycle

4. **Verification** (`cmd/release-controller/sync_verify.go`):
   - Launches ProwJobs for verification
   - Monitors job results
   - Updates payload status

5. **Publishing** (`cmd/release-controller/sync_publish.go`):
   - Publishes releases to registry
   - Updates release tags
   - Makes releases available

### How Release Payload Controller Works

1. **Payload Creation Controller**:
   - Watches ReleasePayloads
   - Monitors creation jobs
   - Updates creation conditions

2. **ProwJob Controller**:
   - Watches ProwJobs
   - Updates payload with job results
   - Manages verification status

3. **Payload Accepted Controller**:
   - Checks acceptance criteria
   - Updates accepted condition
   - Triggers publishing

4. **Payload Rejected Controller**:
   - Checks rejection criteria
   - Updates rejected condition
   - Blocks publishing

## Key Classes and Functions

### Release Controller Core

**Main Function** (`cmd/release-controller/main.go:main`):
- Entry point for release controller
- Parses flags and configuration
- Initializes clients
- Starts controller

**Sync Function** (`cmd/release-controller/sync.go:sync`):
- Main sync orchestration
- Calls individual sync functions
- Handles errors

**Release Creation** (`cmd/release-controller/sync_release.go`):
- `syncRelease()` - Creates releases
- `createRelease()` - Creates release tags
- `shouldCreateRelease()` - Determines if release needed

**Payload Management** (`cmd/release-controller/sync_release_payload.go`):
- `syncReleasePayload()` - Syncs payload state
- `createReleasePayload()` - Creates payload CR
- `updateReleasePayload()` - Updates payload status

### Release Payload Controller

**Controller Manager** (`pkg/cmd/release-payload-controller/controller.go`):
- Manages multiple sub-controllers
- Coordinates reconciliation
- Handles errors

**Payload Creation** (`pkg/cmd/release-payload-controller/payload_creation_controller.go`):
- `Reconcile()` - Reconciles payload creation
- Monitors creation jobs
- Updates conditions

## API Surface

### ReleasePayload API

**Custom Resource** (`pkg/apis/release/v1alpha1/types.go`):
```go
type ReleasePayload struct {
    Spec   ReleasePayloadSpec
    Status ReleasePayloadStatus
}

type ReleasePayloadSpec struct {
    ReleaseTag string
    // ...
}

type ReleasePayloadStatus struct {
    Conditions []metav1.Condition
    // ...
}
```

### Release Controller API

**HTTP Endpoints** (`cmd/release-controller-api/http.go`):
- `/` - Main dashboard
- `/releases` - Release listings
- `/release/{tag}` - Release details
- `/changelog` - Changelog generation
- `/compare` - Release comparison
- `/upgrade-graph` - Upgrade graph

## Data Flow

1. **Release Creation Flow:**
   - ImageStream updated
   - Controller detects change
   - ReleasePayload created
   - Creation job launched
   - Release image created
   - Verification jobs launched
   - Payload status updated

2. **Verification Flow:**
   - ProwJobs created
   - Jobs execute
   - Results collected
   - Payload status updated
   - Acceptance/rejection determined

3. **Publishing Flow:**
   - Release accepted
   - Release published to registry
   - Tags updated
   - Web UI updated

## Testing Structure

- **Unit Tests**: `*_test.go` files alongside source
- **Integration Tests**: Test data in `test/testdata/`
- **Test Utilities**: Helper functions in test files

## Build System

- **Makefile**: Main build automation
- **Go Modules**: Dependency management
- **Code Generation**: CRD and client generation
- **Container Builds**: Dockerfile-based

## Extension Points

1. **Custom Controllers**: Add to controller manager
2. **Custom Sync Functions**: Add to sync.go
3. **Custom API Endpoints**: Add to API server
4. **Custom Verification**: Extend verification logic

