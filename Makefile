build:
	go build -ldflags "-X github.com/openshift/release-controller/vendor/k8s.io/client-go/pkg/version.gitCommit=$$(git rev-parse HEAD) -X github.com/openshift/release-controller/vendor/k8s.io/client-go/pkg/version.gitVersion=v1.0.0+$$(git rev-parse --short=7 HEAD)" ./cmd/release-controller
.PHONY: build

image:
	imagebuilder -t openshift/release-controller:latest .
.PHONY: image

check:
	go test -race ./...
.PHONY: check

test-integration: build
	./test/integration.sh
.PHONY: test-integration

vendor:
	glide update --strip-vendor --skip-test
.PHONY: vendor