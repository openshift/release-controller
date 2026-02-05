FROM registry.ci.openshift.org/ocp/4.21:tools
LABEL maintainer="apavel@redhat.com"

ADD release-controller /usr/bin/release-controller
ENTRYPOINT ["/usr/bin/release-controller"]
