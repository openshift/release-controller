# Include the library makefile
include $(addprefix ./vendor/github.com/openshift/build-machinery-go/make/, \
	golang.mk \
	lib/tmp.mk \
	targets/openshift/codegen.mk \
	targets/openshift/controller-gen.mk \
)

# Build configuration
git_commit=$(shell git describe --tags --always --dirty)
build_date=$(shell date -u '+%Y%m%d')
version=v${build_date}-${git_commit}

SOURCE_GIT_TAG=v1.0.0+$(shell git rev-parse --short=7 HEAD)

GO_LD_EXTRAFLAGS=-X github.com/openshift/release-controller/vendor/k8s.io/client-go/pkg/version.gitCommit=$(shell git rev-parse HEAD) -X github.com/openshift/release-controller/vendor/k8s.io/client-go/pkg/version.gitVersion=${SOURCE_GIT_TAG} -X k8s.io/test-infra/prow/version.Name=release-controller -X k8s.io/test-infra/prow/version.Version=${version}

# Codegen configuration
CODEGEN_PKG=./vendor/k8s.io/code-generator
CODEGEN_GENERATORS=all
CODEGEN_OUTPUT_PACKAGE=github.com/openshift/release-controller/pkg/client
CODEGEN_API_PACKAGE=github.com/openshift/release-controller/pkg/apis
CODEGEN_GROUPS_VERSION=release:v1alpha1
CODEGEN_GO_HEADER_FILE=./hack/custom-boilerplate.go.txt

# These tagets can be removed if/when openshift/build-machinery-go supports executing the corresponding vendored scripts...
update-codegen-script:
	hack/update-codegen.sh
.PHONY: update-codegen-script

verify-codegen-script:
	hack/verify-codegen.sh
.PHONY: verify-codegen-script

# CRD generation configuration
CONTROLLER_GEN_VERSION :=v0.7.0

crd: ensure-controller-gen
	rm -f ./artifacts/*.yaml
	$(CONTROLLER_GEN) crd paths=./pkg/apis/release/v1alpha1 output:dir=./artifacts
.PHONY: crd

# Ensure codegen is run before generating the CRD, so updates to Godoc are included.
update-crd: update-codegen-script crd

# Legacy targets
image:
	imagebuilder -t openshift/release-controller:latest .
.PHONY: image

vendor:
	go mod tidy
	go mod vendor
.PHONY: vendor

