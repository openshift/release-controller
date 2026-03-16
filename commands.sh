#!/usr/bin/bash

make clean && make

podman build -t release-payload-controller:brawilli -f images/release-payload-controller/Dockerfile .
podman push release-payload-controller:brawilli registry.dptools.openshift.org/ci/release-payload-controller:brawilli

podman build -t release-controller:brawilli -f images/release-controller/Dockerfile .
podman push release-controller:brawilli registry.dptools.openshift.org/ci/release-controller:brawilli

podman build -t release-controller-api:brawilli -f images/release-controller-api/Dockerfile_dev .
podman push release-controller-api:brawilli registry.dptools.openshift.org/ci/release-controller-api:brawilli

exit 0
