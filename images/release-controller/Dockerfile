FROM registry.ci.openshift.org/openshift/release:golang-1.17
WORKDIR /go/src/github.com/openshift/release-controller
COPY . .
RUN make build

FROM registry.ci.openshift.org/origin/centos:8
COPY --from=0 /go/src/github.com/openshift/release-controller/release-controller /usr/bin/
RUN yum install -y graphviz
