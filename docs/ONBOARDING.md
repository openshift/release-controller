# Onboarding Guide for New Contributors

Welcome to Release Controller! This guide will help you understand the codebase and get started contributing effectively.

## What to Learn Before Contributing

### Essential Knowledge

1. **Go Programming Language**
   - Go basics (variables, functions, structs, interfaces)
   - Go concurrency (goroutines, channels)
   - Go testing (`testing` package)
   - Go modules and dependency management
   - **Resources**: [A Tour of Go](https://tour.golang.org/), [Effective Go](https://golang.org/doc/effective_go)

2. **Kubernetes/OpenShift**
   - Kubernetes API concepts
   - Custom Resources (CRDs)
   - Pods, Jobs, ImageStreams
   - Controllers and Operators
   - **Resources**: [Kubernetes Documentation](https://kubernetes.io/docs/), [OpenShift Documentation](https://docs.openshift.com/)

3. **OpenShift ImageStreams**
   - ImageStream concepts
   - ImageStreamTags
   - Image mirroring
   - **Resources**: [OpenShift ImageStreams](https://docs.openshift.com/container-platform/latest/openshift_images/image-streams-manage.html)

4. **Release Concepts**
   - What is a release payload
   - Release lifecycle
   - Release verification
   - **Resources**: [OpenShift Release Documentation](https://github.com/openshift/release/tree/master/core-services/release-controller)

### Recommended Knowledge

1. **Kubernetes Controllers**
   - Controller pattern
   - Reconciliation loops
   - Informers and listers
   - **Resources**: [Kubernetes Controller Pattern](https://kubernetes.io/docs/concepts/architecture/controller/)

2. **Prow Integration**
   - ProwJobs
   - Job execution
   - **Resources**: [Prow Documentation](https://docs.prow.k8s.io/)

3. **Container Images**
   - Container image concepts
   - Image registries
   - Image signing
   - **Resources**: [OCI Image Specification](https://github.com/opencontainers/image-spec)

## Important Concepts the Repo Uses

### 1. ReleasePayload Custom Resource

Release Controller uses a Custom Resource to track release state:

```go
// From pkg/apis/release/v1alpha1/types.go
type ReleasePayload struct {
    Spec   ReleasePayloadSpec
    Status ReleasePayloadStatus
}
```

**Key Concepts:**
- ReleasePayloads track release creation and verification
- Status contains conditions for different states
- Controllers reconcile payload state

**Learn More:**
- Study `pkg/apis/release/v1alpha1/types.go`
- Read about Kubernetes CRDs

### 2. Controller Pattern

All components follow the Kubernetes controller pattern:

```go
// Controllers watch resources and reconcile state
func (c *Controller) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    // Fetch resource
    // Reconcile desired state
    // Update status
    return ctrl.Result{}, nil
}
```

**Key Concepts:**
- Watch for resource changes
- Reconcile desired state
- Handle errors gracefully

**Learn More:**
- Study `cmd/release-controller/controller.go`
- Study `pkg/cmd/release-payload-controller/`
- Read [Kubernetes Controller Pattern](https://kubernetes.io/docs/concepts/architecture/controller/)

### 3. ImageStream-Based Releases

Releases are built from ImageStreams:

**Key Concepts:**
- Source ImageStreams contain built images
- Release ImageStreams contain release images
- Mirroring creates point-in-time snapshots
- Tags represent releases

**Learn More:**
- Study `cmd/release-controller/sync_mirror.go`
- Study `pkg/release-controller/mirror.go`
- Read about OpenShift ImageStreams

### 4. Job-Based Execution

Release operations execute as Kubernetes Jobs:

**Key Concepts:**
- Creation jobs assemble releases
- Mirror jobs copy images
- Verification jobs test releases
- Jobs are independent and retryable

**Learn More:**
- Study `cmd/release-controller/sync_release.go`
- Study job creation logic

### 5. Verification System

Releases are verified through ProwJobs:

**Key Concepts:**
- ProwJobs test releases
- Results tracked in ReleasePayload
- Acceptance based on verification results

**Learn More:**
- Study `cmd/release-controller/sync_verify.go`
- Study `pkg/cmd/release-payload-controller/payload_verification_controller.go`

### 6. Sync Pattern

The main controller uses a sync pattern:

**Key Concepts:**
- Main sync function orchestrates operations
- Individual sync functions handle specific tasks
- Errors are handled and logged
- Status is updated after operations

**Learn More:**
- Study `cmd/release-controller/sync.go`
- Study individual sync functions

## Beginner Roadmap for Mastering the Project

### Phase 1: Understanding the Basics (Week 1-2)

**Goal**: Understand what Release Controller does and how it's organized

**Tasks:**
1. Read [Overview](OVERVIEW.md) and [Architecture](ARCHITECTURE.md)
2. Set up development environment ([Setup Guide](SETUP.md))
3. Build and run release controller locally
4. Read through `cmd/release-controller/main.go` to understand entry point
5. Study a simple sync function like `sync_gc.go`

**Deliverable**: You can build and run release controller locally

### Phase 2: Understanding Release Creation (Week 3-4)

**Goal**: Understand how releases are created

**Tasks:**
1. Read release creation documentation
2. Study `cmd/release-controller/sync_release.go` to understand release creation
3. Study `cmd/release-controller/sync_release_payload.go` to understand payload creation
4. Study `pkg/release-controller/release.go` to understand release data structures
5. Trace through a release creation flow
6. Create a test release locally

**Deliverable**: You understand how releases are created

### Phase 3: Understanding Verification (Week 5-6)

**Goal**: Understand how releases are verified

**Tasks:**
1. Study `cmd/release-controller/sync_verify.go` to understand verification
2. Study `pkg/cmd/release-payload-controller/payload_verification_controller.go`
3. Study ProwJob integration
4. Understand verification job lifecycle
5. Make a small change to verification logic

**Deliverable**: You can modify verification logic

### Phase 4: Understanding Controllers (Week 7-8)

**Goal**: Understand the controller architecture

**Tasks:**
1. Study `pkg/cmd/release-payload-controller/` to understand payload controller
2. Study individual sub-controllers
3. Understand controller reconciliation
4. Run a controller locally and observe behavior
5. Make a small change to a controller

**Deliverable**: You understand how controllers work

### Phase 5: Making Your First Contribution (Week 9-10)

**Goal**: Make your first meaningful contribution

**Tasks:**
1. Find a good first issue (labeled "good first issue")
2. Understand the problem and proposed solution
3. Implement the fix or feature
4. Write tests
5. Create a Pull Request
6. Address review feedback

**Deliverable**: Your first merged PR!

## Learning Resources by Topic

### Go

- **Books**: "The Go Programming Language" by Donovan & Kernighan
- **Online**: [Go by Example](https://gobyexample.com/)
- **Practice**: [Exercism Go Track](https://exercism.org/tracks/go)

### Kubernetes

- **Official Docs**: [Kubernetes Documentation](https://kubernetes.io/docs/)
- **Interactive**: [Kubernetes Playground](https://www.katacoda.com/courses/kubernetes)
- **Books**: "Kubernetes: Up and Running" by Hightower et al.

### OpenShift

- **Official Docs**: [OpenShift Documentation](https://docs.openshift.com/)
- **ImageStreams**: [Managing ImageStreams](https://docs.openshift.com/container-platform/latest/openshift_images/image-streams-manage.html)

### Release Management

- **OpenShift Release**: [Release Repository](https://github.com/openshift/release/tree/master/core-services/release-controller)
- **Release Process**: Internal OpenShift documentation

## Common Patterns in the Codebase

### 1. Sync Pattern

```go
func (c *Controller) sync() error {
    // Sync releases
    if err := c.syncReleases(); err != nil {
        return err
    }
    
    // Sync payloads
    if err := c.syncReleasePayloads(); err != nil {
        return err
    }
    
    // Sync verification
    if err := c.syncVerification(); err != nil {
        return err
    }
    
    return nil
}
```

### 2. Controller Reconciliation Pattern

```go
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    // Fetch resource
    payload := &releasev1alpha1.ReleasePayload{}
    if err := r.Get(ctx, req.NamespacedName, payload); err != nil {
        return ctrl.Result{}, err
    }
    
    // Reconcile desired state
    if err := r.reconcilePayload(payload); err != nil {
        return ctrl.Result{}, err
    }
    
    // Update status
    return ctrl.Result{}, r.Status().Update(ctx, payload)
}
```

### 3. Job Creation Pattern

```go
func (c *Controller) createJob(spec *batchv1.JobSpec) (*batchv1.Job, error) {
    job := &batchv1.Job{
        ObjectMeta: metav1.ObjectMeta{
            Name:      generateJobName(),
            Namespace: c.jobNamespace,
        },
        Spec: *spec,
    }
    
    return c.jobClient.Create(context.TODO(), job, metav1.CreateOptions{})
}
```

### 4. Error Handling Pattern

```go
// Wrap errors with context
if err != nil {
    return fmt.Errorf("failed to create release: %w", err)
}

// Use errors.Is for error checking
if errors.Is(err, imagev1.ErrImageStreamNotFound) {
    // handle
}
```

## Tips for Success

1. **Start Small**: Begin with small, focused changes
2. **Read Code**: Spend time reading existing code before writing new code
3. **Ask Questions**: Don't hesitate to ask for help
4. **Write Tests**: Always write tests for new code
5. **Review PRs**: Review other PRs to learn patterns
6. **Be Patient**: Understanding a large codebase takes time

## Getting Help

- **GitHub Issues**: Search existing issues or create new ones
- **Pull Requests**: Ask questions in PR comments
- **Slack**: #forum-testplatform (for OpenShift team members)
- **Documentation**: Read the docs in this directory

## Next Steps

After completing the roadmap:

1. Find areas of interest
2. Look for issues labeled "good first issue"
3. Start contributing regularly
4. Consider becoming a maintainer (after significant contributions)

Welcome to the team! 🚀

