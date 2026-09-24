# FAQ Section

Frequently asked questions from newcomers to the Release Controller repository.

## General Questions

### What is Release Controller?

Release Controller is a Kubernetes-based system that automates the creation, verification, and management of OpenShift release payloads. It monitors image streams, creates release images, runs verification tests, and publishes release information.

### Who maintains Release Controller?

The OpenShift Release Engineering team maintains Release Controller. See the [OWNERS](../OWNERS) file for the list of maintainers.

### What programming language is Release Controller written in?

Go (100%).

### How do I get started?

1. Read the [Overview](OVERVIEW.md)
2. Set up your environment ([Setup Guide](SETUP.md))
3. Try running release controller ([Usage Guide](USAGE.md))
4. Explore the codebase ([Codebase Walkthrough](CODEBASE_WALKTHROUGH.md))

## Development Questions

### How do I build the project?

```bash
# Build all components
make build

# Build specific component
go build ./cmd/release-controller
```

### How do I run tests?

```bash
# Run all unit tests
go test ./...

# Run specific package tests
go test ./pkg/release-controller/...
```

### How do I contribute?

See the [Contributing Guide](CONTRIBUTING_GUIDE.md) for detailed instructions. In short:
1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Test your changes
5. Create a Pull Request

## Component Questions

### What is release-controller?

The main controller that orchestrates the entire release process. It monitors ImageStreams, creates releases, manages verification, and publishes releases.

### What is release-controller-api?

The web UI and REST API server that provides a user interface for viewing releases and release information.

### What is release-payload-controller?

A controller that manages ReleasePayload Custom Resources, tracking release creation, verification, and acceptance status.

### What is release-reimport-controller?

A controller that handles re-importing ImageStreamTags that failed initial import.

## Configuration Questions

### How do I configure Release Controller?

Release Controller uses configurations stored in the `openshift/release` repository at `core-services/release-controller/_releases/`.

### How do I add a new release stream?

Add a release configuration file to `openshift/release/core-services/release-controller/_releases/`.

### What is a ReleasePayload?

ReleasePayload is a Custom Resource that tracks the state of a release, including creation progress, verification status, and acceptance/rejection.

## Release Questions

### How are releases created?

1. ImageStreams are updated with new images
2. Release Controller detects the changes
3. ReleasePayload is created
4. Creation job is launched
5. Release image is assembled
6. Verification jobs are launched
7. Release is published when accepted

### How do I check release status?

```bash
# Get ReleasePayload
kubectl get releasepayload <release-tag> -n <namespace>

# Check conditions
kubectl get releasepayload <release-tag> -n <namespace> \
  -o jsonpath='{.status.conditions}'
```

### What are verification jobs?

ProwJobs that test releases to ensure they work correctly. They verify installation, upgrade paths, and functionality.

### How do releases get published?

When a release passes all verification jobs and meets acceptance criteria, it is automatically published to the container registry and made available.

## Troubleshooting Questions

### Release is not being created

1. Check ImageStream exists and is updated
2. Verify release configuration is correct
3. Check controller logs for errors
4. Verify namespace permissions

### Verification jobs are failing

1. Check ProwJob status
2. Review job logs
3. Verify test configurations
4. Check cluster resources

### Release is not published

1. Check ReleasePayload acceptance conditions
2. Verify all verification jobs passed
3. Check publish configuration
4. Review controller logs

### API is not responding

1. Check API server is running
2. Verify kubeconfig is correct
3. Check network connectivity
4. Review API server logs

## Architecture Questions

### How does Release Controller work?

1. Monitors ImageStreams for changes
2. Creates ReleasePayload when new images available
3. Launches creation job to assemble release
4. Launches verification jobs to test release
5. Publishes release when accepted

### How are releases verified?

Releases are verified through ProwJobs that test installation, upgrades, and functionality. Results are tracked in the ReleasePayload status.

### How does image mirroring work?

Source ImageStreams are mirrored to release ImageStreams to create point-in-time snapshots. Mirror jobs copy images between streams.

### How does the upgrade graph work?

The upgrade graph tracks valid upgrade paths between releases. It's built from release metadata and validated during release creation.

## Contribution Questions

### How do I find good first issues?

Look for issues labeled:
- `good first issue`
- `help wanted`
- `beginner friendly`

### What makes a good PR?

- Clear description
- Focused changes
- Tests included
- Documentation updated
- All checks passing

### How long does review take?

Typically 1-3 business days, depending on:
- PR size and complexity
- Reviewer availability
- Number of review rounds needed

## Getting Help

### Where can I ask questions?

- **GitHub Issues**: For bugs and feature requests
- **Pull Requests**: For code-related questions
- **Slack**: #forum-testplatform (for OpenShift team members)
- **Documentation**: Check the docs in this directory

### How do I report a bug?

Create a GitHub issue with:
- Clear description
- Steps to reproduce
- Expected vs actual behavior
- Relevant logs/configs
- Environment information

### How do I request a feature?

Create a GitHub issue with:
- Problem description
- Proposed solution
- Use cases
- Any alternatives considered

## Still Have Questions?

- Check the [documentation index](README.md)
- Search existing GitHub issues
- Ask in #forum-testplatform on Slack
- Create a new issue with your question

