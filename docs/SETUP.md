# Setup Guide (Beginner Friendly)

This guide will help you set up a development environment for working with Release Controller.

## Prerequisites

### Required Software

1. **Go** (version 1.23.2 or later)
   ```bash
   # Check your Go version
   go version
   
   # If not installed, download from https://golang.org/dl/
   ```

2. **Git**
   ```bash
   git --version
   ```

3. **Make**
   ```bash
   make --version
   ```

4. **kubectl** (for interacting with Kubernetes)
   ```bash
   kubectl version --client
   ```

5. **oc** (OpenShift CLI) - Recommended
   ```bash
   oc version
   ```

### Optional but Recommended

- **Docker** - For building container images
- **kind** or **minikube** - For local Kubernetes cluster
- **jq** - For JSON processing
- **yq** - For YAML processing

## Installation Steps

### 1. Clone the Repository

```bash
# Clone the repository
git clone https://github.com/openshift/release-controller.git
cd release-controller

# If you plan to contribute, fork first and clone your fork
```

### 2. Set Up Go Environment

```bash
# Set Go environment variables (if not already set)
export GOPATH=$HOME/go
export PATH=$PATH:$GOPATH/bin

# Verify Go is working
go env
```

### 3. Install Dependencies

```bash
# Download dependencies
go mod download

# Verify dependencies
go mod verify

# Vendor dependencies (if needed)
make vendor
```

### 4. Build the Components

```bash
# Build all components
make build

# Or build specific component
go build ./cmd/release-controller
go build ./cmd/release-controller-api
go build ./cmd/release-payload-controller
go build ./cmd/release-reimport-controller
```

### 5. Verify Installation

```bash
# Check that components are built
ls -la release-controller*
./release-controller --help
```

## Environment Requirements

### Development Environment

- **Operating System**: Linux or macOS (Windows with WSL2)
- **Memory**: Minimum 8GB RAM (16GB recommended)
- **Disk Space**: At least 10GB free space
- **Network**: Internet connection for downloading dependencies

### For Testing with OpenShift

- Access to an OpenShift cluster (or use CodeReady Containers)
- Valid kubeconfig file
- Appropriate cluster permissions
- Access to ImageStreams in release namespaces

### For Integration Testing

- Access to `api.ci` OpenShift cluster (for OpenShift team members)
- Release namespace access
- ImageStream read/write permissions

## How to Run the Project

### Running Release Controller

```bash
# Basic usage with dry-run
./release-controller \
  --release-namespace ocp \
  --job-namespace ci-release \
  --dry-run \
  -v=4

# With kubeconfig
./release-controller \
  --release-namespace ocp \
  --job-namespace ci-release \
  --releases-kubeconfig ~/.kube/config \
  --dry-run

# Production-like (requires cluster access)
./release-controller \
  --release-namespace ocp \
  --job-namespace ci-release \
  --releases-kubeconfig ~/.kube/config \
  --prow-job-kubeconfig ~/.kube/config
```

### Running Release Controller API

```bash
# Start API server
./release-controller-api \
  --release-namespace ocp \
  --releases-kubeconfig ~/.kube/config

# Access at http://localhost:8080
```

### Running Release Payload Controller

```bash
# Run payload controller
./release-payload-controller \
  --kubeconfig ~/.kube/config
```

### Running Release Reimport Controller

```bash
# Run reimport controller
./release-reimport-controller \
  --kubeconfig ~/.kube/config
```

### Running Tests

```bash
# Run all unit tests
go test ./...

# Run specific package tests
go test ./pkg/release-controller/...

# Run with verbose output
go test -v ./pkg/release-controller/...

# Run with coverage
go test -cover ./pkg/...
```

## How to Test It

### Unit Testing

```bash
# Run all unit tests
go test ./...

# Run tests for specific package
go test ./pkg/release-controller/...

# Run tests with coverage
go test -cover ./pkg/...

# Generate coverage report
go test -coverprofile=coverage.out ./pkg/...
go tool cover -html=coverage.out
```

### Manual Testing

1. **Test Release Controller Locally:**
   ```bash
   # Run with dry-run mode
   ./release-controller \
     --release-namespace ocp \
     --job-namespace ci-release \
     --dry-run \
     -v=4
   ```

2. **Test API Server:**
   ```bash
   # Start API server
   ./release-controller-api \
     --release-namespace ocp \
     --releases-kubeconfig ~/.kube/config
   
   # Access web UI
   # Navigate to http://localhost:8080
   ```

3. **Test Configuration:**
   ```bash
   # Validate release configuration
   # (Check release configs in openshift/release repository)
   ```

## Common Errors and Solutions

### Error: "go: cannot find module"

**Solution:**
```bash
# Ensure you're in the repository root
cd /path/to/release-controller

# Download dependencies
go mod download

# Or update dependencies
go mod tidy
```

### Error: "command not found: release-controller"

**Solution:**
```bash
# Build the component
go build ./cmd/release-controller

# Or use make
make build
```

### Error: "permission denied" when accessing cluster

**Solution:**
```bash
# Check kubeconfig
kubectl config current-context

# Verify cluster access
kubectl get nodes

# Check ImageStream access
oc get imagestreams -n ocp
```

### Error: "cannot load package" or import errors

**Solution:**
```bash
# Clean module cache
go clean -modcache

# Re-download dependencies
go mod download

# Verify go.mod is correct
go mod verify
```

### Error: "ImageStream not found"

**Solution:**
- Verify you're using the correct namespace
- Check ImageStream exists: `oc get imagestreams -n <namespace>`
- Verify you have read permissions

### Error: "ReleasePayload CRD not found"

**Solution:**
```bash
# Install CRDs
make update-crd

# Or apply manually
kubectl apply -f artifacts/release.openshift.io_releasepayloads.yaml
```

### Error: Build failures due to missing dependencies

**Solution:**
```bash
# Update dependencies
go mod tidy
go mod vendor
```

## Development Workflow

### 1. Make Changes

```bash
# Create a feature branch
git checkout -b feature/my-feature

# Make your changes
# ...

# Format code
go fmt ./...

# Run tests
go test ./...
```

### 2. Verify Changes

```bash
# Run tests
go test ./...

# Build components
make build

# Verify code generation (if changed APIs)
make verify-codegen
```

### 3. Commit Changes

```bash
# Stage changes
git add .

# Commit with descriptive message
git commit -m "Add feature: description of changes"

# Push to your fork
git push origin feature/my-feature
```

## IDE Setup

### VS Code

Recommended extensions:
- Go extension
- YAML extension
- Kubernetes extension

Settings:
```json
{
  "go.useLanguageServer": true,
  "go.formatTool": "goimports",
  "editor.formatOnSave": true
}
```

### GoLand / IntelliJ

- Install Go plugin
- Configure Go SDK
- Enable goimports on save

## Next Steps

After setting up your environment:

1. Read the [Usage Guide](USAGE.md) to learn how to use release controller
2. Explore the [Codebase Walkthrough](CODEBASE_WALKTHROUGH.md) to understand the structure
3. Check the [Contributing Guide](CONTRIBUTING_GUIDE.md) if you want to contribute
4. Review the [Onboarding Guide](ONBOARDING.md) for deep dive

## Getting Help

- Check existing documentation
- Search existing issues on GitHub
- Ask in #forum-testplatform on Slack (for OpenShift team members)
- Create a new issue with detailed error information

