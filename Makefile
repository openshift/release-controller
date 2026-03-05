# Include the library makefile
include $(addprefix ./vendor/github.com/openshift/build-machinery-go/make/, \
	golang.mk \
	lib/tmp.mk \
	targets/openshift/crd-schema-gen.mk \
)

$(call add-crd-gen,releasepayload,./pkg/apis/release/v1alpha1,./artifacts,./artifacts)

# Build configuration
git_commit=$(shell git describe --tags --always --dirty)
build_date=$(shell date -u '+%Y%m%d')
version=v${build_date}-${git_commit}

SOURCE_GIT_TAG=v1.0.0+$(shell git rev-parse --short=7 HEAD)

GOLINT=golangci-lint

GO_LD_EXTRAFLAGS=-X github.com/openshift/release-controller/vendor/k8s.io/client-go/pkg/version.gitCommit=$(shell git rev-parse HEAD) -X github.com/openshift/release-controller/vendor/k8s.io/client-go/pkg/version.gitVersion=${SOURCE_GIT_TAG} -X k8s.io/test-infra/prow/version.Name=release-controller -X k8s.io/test-infra/prow/version.Version=${version}
GO_TEST_FLAGS=
export CGO_ENABLED := 0

update-codegen-script:
	hack/update-codegen.sh
.PHONY: update-codegen-script

verify-codegen-script:
	hack/verify-codegen.sh
.PHONY: verify-codegen-script

# Ensure codegen is run before generating the CRD, so updates to Godoc are included.
update-crd: update-codegen-script update-codegen-crds
.PHONY: update-crd

# Create CRD from scratch using the 'crd' generator instead of 'schemapatch'
# This target can regenerate the CRD even if it doesn't exist, unlike update-crd
# which uses 'schemapatch' and only patches existing CRDs.
create-crd: update-codegen-script ensure-controller-gen
	'$(CONTROLLER_GEN)' \
		crd \
		paths="./pkg/apis/release/v1alpha1" \
		'output:crd:dir=./artifacts'
.PHONY: create-crd

sonar-reports:
	go test ./... -coverprofile=coverage.out -covermode=count -json > report.json
	golangci-lint run ./... --verbose --no-config --out-format checkstyle --issues-exit-code 0 > golangci-lint.out
.PHONY: sonar-reports

lint:
	@echo "Running linter..."
	@if command -v $(GOLINT) >/dev/null 2>&1; then \
		$(GOLINT) run ./$(BINARY_PATH)/...; \
	else \
		echo "golangci-lint not installed. Install with: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
	fi
.PHONY: lint

# Legacy targets
image:
	imagebuilder -t openshift/release-controller:latest .
.PHONY: image

vendor:
	go mod tidy
	go mod vendor
.PHONY: vendor

# Override the vulncheck target from build-machinery-go to use our wrapper
# that supports ignoring vulnerabilities with no available fix
vulncheck:
	./hack/govulncheck-wrapper.sh
.PHONY: vulncheck
