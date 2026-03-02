#!/usr/bin/env bash

# Copyright 2017 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CODEGEN_PKG="${SCRIPT_ROOT}/vendor/k8s.io/code-generator"

# Temporary directory for code-generator binaries
GOBIN=$(mktemp -d)
export PATH="${GOBIN}:${PATH}"
trap 'rm -rf "${GOBIN}"' EXIT

# Install code generators if not already present
install_generator() {
    local tool=$1
    local target="${GOBIN}/${tool}"
    if [[ ! -x "${target}" ]]; then
        echo "Installing ${tool}..."
        GOBIN="${GOBIN}" go install "${CODEGEN_PKG}/cmd/${tool}"
    fi
}

# Install all required generators
install_generator deepcopy-gen
install_generator defaulter-gen
install_generator client-gen
install_generator lister-gen
install_generator informer-gen

BOILERPLATE="${SCRIPT_ROOT}/hack/boilerplate.go.txt"

# Generate deepcopy and defaulter for each package
# Note: We skip validation-gen as it's not needed (no validation markers in codebase)

echo "Generating deepcopy and defaults for pkg/apis..."
deepcopy-gen \
    --go-header-file "${BOILERPLATE}" \
    --output-file zz_generated.deepcopy.go \
    github.com/openshift/release-controller/pkg/apis/release/v1alpha1

defaulter-gen \
    --go-header-file "${BOILERPLATE}" \
    --output-file zz_generated.defaults.go \
    github.com/openshift/release-controller/pkg/apis/release/v1alpha1

echo "Generating deepcopy and defaults for pkg/releasequalifiers..."
deepcopy-gen \
    --go-header-file "${BOILERPLATE}" \
    --output-file zz_generated.deepcopy.go \
    github.com/openshift/release-controller/pkg/releasequalifiers

defaulter-gen \
    --go-header-file "${BOILERPLATE}" \
    --output-file zz_generated.defaults.go \
    github.com/openshift/release-controller/pkg/releasequalifiers

echo "Generating deepcopy for pkg/releasequalifiers/notifications..."
deepcopy-gen \
    --go-header-file "${BOILERPLATE}" \
    --output-file zz_generated.deepcopy.go \
    github.com/openshift/release-controller/pkg/releasequalifiers/notifications

echo "Generating deepcopy for pkg/releasequalifiers/notifications/jira..."
deepcopy-gen \
    --go-header-file "${BOILERPLATE}" \
    --output-file zz_generated.deepcopy.go \
    github.com/openshift/release-controller/pkg/releasequalifiers/notifications/jira

echo "Generating deepcopy for pkg/releasequalifiers/notifications/slack..."
deepcopy-gen \
    --go-header-file "${BOILERPLATE}" \
    --output-file zz_generated.deepcopy.go \
    github.com/openshift/release-controller/pkg/releasequalifiers/notifications/slack

# Generate typed clients (clientset, listers, informers)
echo "Generating clientset..."
client-gen \
    --go-header-file "${BOILERPLATE}" \
    --clientset-name "versioned" \
    --input-base "github.com/openshift/release-controller/pkg/apis" \
    --input "release/v1alpha1" \
    --output-dir "${SCRIPT_ROOT}/pkg/client/clientset" \
    --output-pkg "github.com/openshift/release-controller/pkg/client/clientset"

echo "Generating listers..."
lister-gen \
    --go-header-file "${BOILERPLATE}" \
    --output-dir "${SCRIPT_ROOT}/pkg/client/listers" \
    --output-pkg "github.com/openshift/release-controller/pkg/client/listers" \
    github.com/openshift/release-controller/pkg/apis/release/v1alpha1

echo "Generating informers..."
informer-gen \
    --go-header-file "${BOILERPLATE}" \
    --versioned-clientset-package "github.com/openshift/release-controller/pkg/client/clientset/versioned" \
    --listers-package "github.com/openshift/release-controller/pkg/client/listers" \
    --output-dir "${SCRIPT_ROOT}/pkg/client/informers" \
    --output-pkg "github.com/openshift/release-controller/pkg/client/informers" \
    github.com/openshift/release-controller/pkg/apis/release/v1alpha1

echo "Code generation complete!"
