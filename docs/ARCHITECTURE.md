# Architecture Documentation

## System Architecture

Release Controller is a Kubernetes-native system that uses Custom Resources (ReleasePayloads) and ImageStreams to manage OpenShift releases. The architecture follows a controller pattern where multiple controllers work together to orchestrate the release process.

### High-Level Architecture Diagram

```mermaid
graph TB
    subgraph "Input Sources"
        ART[ART ImageStreams<br/>ocp/4.15-art-latest]
        CI[CI Builds]
    end
    
    subgraph "Release Controller System"
        RC[Release Controller<br/>Core Orchestrator]
        API[Release Controller API<br/>Web UI & REST API]
        RPC[Release Payload Controller<br/>CRD Manager]
        RRC[Release Reimport Controller<br/>Reimport Handler]
    end
    
    subgraph "Kubernetes Cluster"
        IS[ImageStreams<br/>Source & Release]
        RP[ReleasePayloads<br/>CRs]
        JOBS[Kubernetes Jobs<br/>Release Creation]
        PJ[ProwJobs<br/>Verification Tests]
    end
    
    subgraph "Output"
        REGISTRY[Container Registry<br/>Published Releases]
        WEB[Web Interface<br/>Release Information]
        JIRA[Jira<br/>Issue Tracking]
    end
    
    ART -->|Updates| IS
    CI -->|Creates| IS
    RC -->|Monitors| IS
    RC -->|Creates| RP
    RC -->|Launches| JOBS
    RC -->|Creates| PJ
    RC -->|Mirrors| IS
    
    RPC -->|Manages| RP
    RPC -->|Monitors| PJ
    RPC -->|Updates| RP
    
    RRC -->|Reimports| IS
    
    API -->|Reads| IS
    API -->|Reads| RP
    API -->|Serves| WEB
    
    JOBS -->|Publishes| REGISTRY
    RC -->|Updates| JIRA
    
    style RC fill:#e1f5ff
    style RPC fill:#fff4e1
    style API fill:#e8f5e9
```

## Component Architecture

### Release Creation Flow

```mermaid
sequenceDiagram
    participant ART as ART ImageStream
    participant RC as Release Controller
    participant RP as ReleasePayload
    participant Job as Creation Job
    participant Registry as Container Registry
    participant PJ as ProwJobs
    
    ART->>RC: ImageStream Updated
    RC->>RC: Check Release Config
    RC->>RP: Create ReleasePayload
    RC->>Job: Launch Creation Job
    Job->>Job: Assemble Release
    Job->>Registry: Push Release Image
    Job->>RC: Update Status
    RC->>PJ: Launch Verification Jobs
    PJ->>RC: Report Results
    RC->>RP: Update Payload Status
```

### Release Verification Flow

```mermaid
graph TD
    A[Release Created] --> B[ReleasePayload Created]
    B --> C[Verification Jobs Launched]
    C --> D{All Jobs Pass?}
    D -->|Yes| E[Payload Accepted]
    D -->|No| F[Payload Rejected]
    E --> G[Release Published]
    F --> H[Release Blocked]
    
    style E fill:#e8f5e9
    style F fill:#ffebee
```

### Data Flow Diagram

```mermaid
flowchart TD
    subgraph "Input Sources"
        ARTStreams[ART ImageStreams]
        CIBuilds[CI Build Outputs]
        Config[Release Configs]
    end
    
    subgraph "Processing"
        RC[Release Controller]
        RPC[Release Payload Controller]
        RRC[Release Reimport Controller]
    end
    
    subgraph "Storage"
        ImageStreams[ImageStreams]
        ReleasePayloads[ReleasePayloads]
        GCS[GCS Artifacts]
        Audit[Audit Logs]
    end
    
    subgraph "Output"
        Registry[Container Registry]
        WebUI[Web Interface]
        Jira[Jira Issues]
    end
    
    ARTStreams --> RC
    CIBuilds --> RC
    Config --> RC
    
    RC --> ImageStreams
    RC --> ReleasePayloads
    RC --> GCS
    RC --> Audit
    
    RPC --> ReleasePayloads
    RRC --> ImageStreams
    
    ImageStreams --> Registry
    ReleasePayloads --> WebUI
    RC --> Jira
    
    style RC fill:#e1f5ff
    style RPC fill:#fff4e1
```

## Deployment Architecture

### Production Deployment

```mermaid
graph TB
    subgraph "Release Controller Namespace"
        RCPod[Release Controller Pod]
        APIPod[API Server Pod]
        RPCPod[Release Payload Controller Pod]
        RRCPod[Reimport Controller Pod]
    end
    
    subgraph "Kubernetes Cluster"
        ImageStreams[ImageStreams]
        ReleasePayloads[ReleasePayloads]
        Jobs[Kubernetes Jobs]
        ProwJobs[ProwJobs]
    end
    
    subgraph "External Services"
        Registry[Container Registry]
        GCS[Google Cloud Storage]
        Jira[Jira]
        Prow[Prow CI]
    end
    
    RCPod --> ImageStreams
    RCPod --> ReleasePayloads
    RCPod --> Jobs
    RCPod --> ProwJobs
    
    RPCPod --> ReleasePayloads
    RRCPod --> ImageStreams
    
    APIPod --> ImageStreams
    APIPod --> ReleasePayloads
    
    Jobs --> Registry
    RCPod --> GCS
    RCPod --> Jira
    ProwJobs --> Prow
    
    style RCPod fill:#e1f5ff
    style RPCPod fill:#fff4e1
```

### Controller Architecture

The release-payload-controller runs multiple sub-controllers:

```mermaid
graph LR
    subgraph "release-payload-controller"
        PCC[Payload Creation Controller]
        PMC[Payload Mirror Controller]
        PJC[ProwJob Controller]
        PAC[Payload Accepted Controller]
        PRC[Payload Rejected Controller]
        PVC[Payload Verification Controller]
    end
    
    subgraph "Kubernetes API"
        RP[ReleasePayloads]
        Jobs[Jobs]
        PJ[ProwJobs]
    end
    
    PCC --> RP
    PCC --> Jobs
    PMC --> RP
    PMC --> Jobs
    PJC --> PJ
    PJC --> RP
    PAC --> RP
    PRC --> RP
    PVC --> RP
    
    style PCC fill:#e1f5ff
    style PJC fill:#fff4e1
```

## Component Interaction Diagram

```mermaid
graph TB
    subgraph "Monitoring Layer"
        RC[Release Controller]
    end
    
    subgraph "Management Layer"
        RPC[Release Payload Controller]
        RRC[Release Reimport Controller]
    end
    
    subgraph "Presentation Layer"
        API[Release Controller API]
    end
    
    subgraph "Integration Layer"
        Jira[Jira Integration]
        Signer[Release Signer]
        Audit[Audit Backend]
    end
    
    RC --> RPC
    RC --> RRC
    RC --> API
    RC --> Jira
    RC --> Signer
    RC --> Audit
    
    RPC --> RC
    API --> RC
    
    style RC fill:#e1f5ff
    style RPC fill:#fff4e1
```

## Key Design Patterns

### 1. Controller Pattern
All components follow the Kubernetes controller pattern:
- Watch for resource changes
- Reconcile desired state
- Handle errors and retries gracefully
- Update resource status

### 2. Custom Resources
ReleasePayload CRD represents release state:
- Tracks release creation progress
- Manages verification status
- Handles acceptance/rejection
- Stores release metadata

### 3. ImageStream-Based
Uses OpenShift ImageStreams for:
- Source image tracking
- Release image creation
- Image mirroring
- Point-in-time snapshots

### 4. Job-Based Execution
Release operations execute as Kubernetes Jobs:
- Release creation jobs
- Mirror jobs
- Verification jobs
- Independent and retryable

### 5. Event-Driven
System responds to events:
- ImageStream updates trigger releases
- Job completions trigger status updates
- ReleasePayload changes trigger actions

## Security Architecture

```mermaid
graph TB
    subgraph "Authentication"
        ServiceAccounts[K8s Service Accounts]
        RegistryAuth[Registry Authentication]
    end
    
    subgraph "Authorization"
        RBAC[Kubernetes RBAC]
        ImageStreamPermissions[ImageStream Permissions]
    end
    
    subgraph "Security Features"
        Signing[Release Signing]
        Audit[Audit Logging]
        Verification[Release Verification]
    end
    
    ServiceAccounts --> RBAC
    RegistryAuth --> ImageStreamPermissions
    
    RBAC --> Signing
    ImageStreamPermissions --> Audit
    Signing --> Verification
    
    style Signing fill:#e1f5ff
    style Audit fill:#fff4e1
```

## Scalability Considerations

1. **Horizontal Scaling**: Controllers can be scaled horizontally
2. **Job Parallelization**: Multiple release jobs can run concurrently
3. **Caching**: ImageStream informers cache resource state
4. **Resource Management**: Garbage collection manages old releases
5. **Efficient API Usage**: Informers and watches for Kubernetes API

## Monitoring and Observability

```mermaid
graph LR
    subgraph "Metrics Collection"
        Prometheus[Prometheus]
        Metrics[Metrics Endpoints]
    end
    
    subgraph "Logging"
        K8sLogs[Kubernetes Logs]
        AuditLogs[Audit Logs]
    end
    
    subgraph "Tracing"
        OpenTelemetry[OpenTelemetry]
    end
    
    subgraph "Dashboards"
        Grafana[Grafana]
        WebUI[Web UI]
    end
    
    Prometheus --> Grafana
    Metrics --> Prometheus
    K8sLogs --> Grafana
    AuditLogs --> Grafana
    OpenTelemetry --> Grafana
    WebUI --> Metrics
    
    style Prometheus fill:#e1f5ff
    style Grafana fill:#fff4e1
```

