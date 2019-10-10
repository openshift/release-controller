FROM openshift/origin-release:golang-1.13
WORKDIR /go/src/github.com/openshift/release-controller
COPY . .
RUN make build

FROM centos:7
COPY --from=0 /go/src/github.com/openshift/release-controller/release-controller /usr/bin/
RUN yum install -y graphviz
