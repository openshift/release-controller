FROM registry.ci.openshift.org/ocp/4.23:tools
LABEL maintainer="apavel@redhat.com"

ADD release-controller /usr/bin/release-controller
COPY rpmdb-cache/rpmdb-cache.tar.zst /usr/share/release-controller/rpmdb-cache.tar.zst
ENTRYPOINT ["/usr/bin/release-controller"]
