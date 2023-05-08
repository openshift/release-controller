FROM registry.ci.openshift.org/origin/centos:stream9
LABEL maintainer="brawilli@redhat.com"

ADD release-payload-controller /usr/bin/release-payload-controller
ENTRYPOINT ["/usr/bin/release-payload-controller"]
