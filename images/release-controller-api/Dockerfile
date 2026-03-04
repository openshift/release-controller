FROM registry.ci.openshift.org/ocp/4.23:tools
LABEL maintainer="apavel@redhat.com"

RUN yum install -y graphviz
ADD release-controller-api /usr/bin/release-controller-api
ENTRYPOINT ["/usr/bin/release-controller-api"]
