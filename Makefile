build:
	GOFLAGS="-mod=vendor" go build -ldflags "-X github.com/openshift/release-controller/vendor/k8s.io/client-go/pkg/version.gitCommit=$$(git rev-parse HEAD) -X github.com/openshift/release-controller/vendor/k8s.io/client-go/pkg/version.gitVersion=v1.0.0+$$(git rev-parse --short=7 HEAD)" ./cmd/release-controller
.PHONY: build

image:
	imagebuilder -t openshift/release-controller:latest .
.PHONY: image

test:
	GOFLAGS="-mod=vendor" go test -race ./...
.PHONY: test

vendor:
	go mod vendor
.PHONY: vendor
