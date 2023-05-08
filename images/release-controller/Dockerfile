FROM registry.ci.openshift.org/openshift/centos:stream9
LABEL maintainer="apavel@redhat.com"

RUN yum install -y graphviz
ADD release-controller /usr/bin/release-controller
ENTRYPOINT ["/usr/bin/release-controller"]
