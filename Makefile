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

sonar-reports:
	go test ./... -coverprofile=coverage.out -covermode=count -json > report.json
	golangci-lint run ./... --verbose --no-config --out-format checkstyle --issues-exit-code 0 > golangci-lint.out
.PHONY: sonar-reports

# Legacy targets
image:
	imagebuilder -t openshift/release-controller:latest .
.PHONY: image

vendor:
	go mod tidy
	go mod vendor
.PHONY: vendor
