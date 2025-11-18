# Usage Guide

This guide provides practical examples and step-by-step instructions for using Release Controller.

## Table of Contents

- [Release Controller](#release-controller)
- [Release Controller API](#release-controller-api)
- [Release Payload Controller](#release-payload-controller)
- [Configuration](#configuration)
- [Common Workflows](#common-workflows)

## Release Controller

### Basic Usage

```bash
# Dry-run mode (recommended for testing)
./release-controller \
  --release-namespace ocp \
  --job-namespace ci-release \
  --dry-run \
  -v=4

# Production mode
./release-controller \
  --release-namespace ocp \
  --job-namespace ci-release \
  --releases-kubeconfig ~/.kube/config \
  --prow-job-kubeconfig ~/.kube/config
```

### Common Options

```bash
./release-controller \
  --release-namespace ocp \              # Namespace for releases
  --publish-namespace ocp \              # Namespace for publishing
  --job-namespace ci-release \           # Namespace for jobs
  --prow-namespace ci \                  # Prow namespace
  --releases-kubeconfig ~/.kube/config \  # Kubeconfig for releases
  --prow-job-kubeconfig ~/.kube/config \ # Kubeconfig for Prow jobs
  --listen-addr :8080 \                   # Listen address
  --artifacts-host artifacts.example.com \ # Artifacts host
  --dry-run \                             # Dry-run mode
  -v=4                                    # Verbosity level
```

### Advanced Options

```bash
./release-controller \
  --audit-storage gs://bucket/audit \     # Audit storage location
  --signing-keyring /path/to/keyring \    # Signing keyring
  --verify-jira \                         # Verify Jira integration
  --soft-delete-release-tags \            # Soft delete tags
  --release-architecture amd64            # Release architecture
```

## Release Controller API

### Starting the API Server

```bash
# Basic usage
./release-controller-api \
  --release-namespace ocp \
  --releases-kubeconfig ~/.kube/config

# With custom port
./release-controller-api \
  --release-namespace ocp \
  --releases-kubeconfig ~/.kube/config \
  --listen-addr :9090
```

### Accessing the Web UI

```bash
# Start API server
./release-controller-api --release-namespace ocp

# Access web UI
# Navigate to http://localhost:8080
```

### API Endpoints

- `GET /` - Main dashboard
- `GET /releases` - List all releases
- `GET /release/{tag}` - Get release details
- `GET /changelog?from={tag}&to={tag}` - Get changelog
- `GET /compare?from={tag}&to={tag}` - Compare releases
- `GET /upgrade-graph` - View upgrade graph

### Example API Usage

```bash
# Get release list
curl http://localhost:8080/releases

# Get specific release
curl http://localhost:8080/release/4.15.0-rc.0

# Get changelog
curl "http://localhost:8080/changelog?from=4.14.0&to=4.15.0"

# Compare releases
curl "http://localhost:8080/compare?from=4.14.0&to=4.15.0"
```

## Release Payload Controller

### Running the Controller

```bash
# Basic usage
./release-payload-controller \
  --kubeconfig ~/.kube/config

# With namespace
./release-payload-controller \
  --kubeconfig ~/.kube/config \
  --namespace ocp
```

## Configuration

### Release Configuration

Release configurations are stored in the `openshift/release` repository at:
`core-services/release-controller/_releases/{release-name}.json`

Example configuration:
```json
{
  "name": "4.15",
  "to": "release",
  "message": "OpenShift 4.15",
  "mirrorPrefix": "quay.io/openshift-release-dev/ocp-release",
  "check": {
    "bugs": {
      "enabled": true
    },
    "upgrade": {
      "enabled": true
    }
  },
  "verify": {
    "prow": {
      "enabled": true
    }
  },
  "publish": {
    "quay": {
      "enabled": true
    }
  }
}
```

### Environment Variables

Common environment variables:
- `KUBECONFIG` - Kubernetes config file path
- `RELEASE_NAMESPACE` - Default release namespace
- `JOB_NAMESPACE` - Default job namespace
- `ARTIFACTS_HOST` - Artifacts host URL

## Common Workflows

### Example 1: Creating a Release

1. **Ensure ImageStreams are updated:**
   ```bash
   oc get imagestream 4.15-art-latest -n ocp
   ```

2. **Release Controller detects changes:**
   - Controller monitors ImageStreams
   - Detects new images
   - Creates ReleasePayload

3. **Release is created:**
   - Creation job launched
   - Release image assembled
   - ReleasePayload status updated

4. **Verification runs:**
   - ProwJobs launched
   - Tests execute
   - Results collected

5. **Release published:**
   - Release accepted
   - Published to registry
   - Made available

### Example 2: Viewing Releases

```bash
# Via Web UI
# Navigate to http://localhost:8080

# Via API
curl http://localhost:8080/releases

# Via kubectl
kubectl get releasepayloads -n ocp
```

### Example 3: Checking Release Status

```bash
# Get ReleasePayload
kubectl get releasepayload 4.15.0-rc.0 -n ocp -o yaml

# Check conditions
kubectl get releasepayload 4.15.0-rc.0 -n ocp \
  -o jsonpath='{.status.conditions}'

# Check verification jobs
kubectl get prowjobs -n ci-release \
  -l release.openshift.io/release=4.15.0-rc.0
```

### Example 4: Troubleshooting a Release

```bash
# Check ReleasePayload status
kubectl describe releasepayload 4.15.0-rc.0 -n ocp

# Check creation job
kubectl get jobs -n ci-release \
  -l release.openshift.io/release=4.15.0-rc.0

# Check job logs
kubectl logs -n ci-release job/<job-name>

# Check verification jobs
kubectl get prowjobs -n ci-release \
  -l release.openshift.io/release=4.15.0-rc.0
```

## Best Practices

1. **Always use `--dry-run` first** when testing
2. **Test locally** before deploying
3. **Monitor ReleasePayload status** for issues
4. **Check logs** regularly for errors
5. **Verify ImageStreams** before expecting releases
6. **Use proper namespaces** for different environments
7. **Monitor verification jobs** for failures

## Troubleshooting

### Release Not Created

- Check ImageStream exists and is updated
- Verify release configuration is correct
- Check controller logs for errors
- Verify namespace permissions

### Verification Jobs Failing

- Check ProwJob status
- Review job logs
- Verify test configurations
- Check cluster resources

### Release Not Published

- Check ReleasePayload acceptance conditions
- Verify all verification jobs passed
- Check publish configuration
- Review controller logs

### API Not Responding

- Check API server is running
- Verify kubeconfig is correct
- Check network connectivity
- Review API server logs

## Getting Help

- Check component help: `./release-controller --help`
- Review logs with `-v=4` for debug output
- Check [FAQ](FAQ.md) for common issues
- Search GitHub issues
- Ask in #forum-testplatform on Slack

