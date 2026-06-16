FROM registry.ci.openshift.org/ocp/5.0:cli
LABEL maintainer="apavel@redhat.com"

ADD release-reimport-controller /usr/bin/release-reimport-controller
ENTRYPOINT ["/usr/bin/release-reimport-controller"]
