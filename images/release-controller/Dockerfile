FROM registry.ci.openshift.org/origin/centos:stream8
LABEL maintainer="apavel@redhat.com"

RUN yum install -y graphviz
ADD release-controller /usr/bin/release-controller
ENTRYPOINT ["/usr/bin/release-controller"]
