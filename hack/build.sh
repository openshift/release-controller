#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail

git_commit="$( git describe --tags --always --dirty )"
build_date="$( date -u '+%Y%m%d' )"
version="v${build_date}-${git_commit}"

GOFLAGS="-mod=vendor" go build -ldflags "-X github.com/openshift/release-controller/vendor/k8s.io/client-go/pkg/version.gitCommit=$(git rev-parse HEAD) -X github.com/openshift/release-controller/vendor/k8s.io/client-go/pkg/version.gitVersion=v1.0.0+$(git rev-parse --short=7 HEAD) -X 'k8s.io/test-infra/prow/version.Name=release-controller' -X 'k8s.io/test-infra/prow/version.Version=${version}'" ./cmd/release-controller
