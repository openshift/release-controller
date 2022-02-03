FROM registry.ci.openshift.org/origin/centos:stream8
LABEL maintainer="apavel@redhat.com"

RUN yum install -y graphviz
ADD release-controller-api /usr/bin/release-controller-api
ENTRYPOINT ["/usr/bin/release-controller-api"]
