FROM registry.ci.openshift.org/ocp/4.19:cli
LABEL maintainer="apavel@redhat.com"

ADD release-mirror-cleanup-controller /usr/bin/release-mirror-cleanup-controller
ENTRYPOINT ["/usr/bin/release-mirror-cleanup-controller"]
