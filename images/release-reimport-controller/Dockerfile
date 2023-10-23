FROM registry.ci.openshift.org/ocp/4.13.9:cli
LABEL maintainer="apavel@redhat.com"

ADD release-reimport-controller /usr/bin/release-reimport-controller
ENTRYPOINT ["/usr/bin/release-reimport-controller"]
