build:
	go build ./cmd/release-controller
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
	glide update -v --skip-test
.PHONY: vendor