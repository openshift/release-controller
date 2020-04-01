build:
	hack/build.sh
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
