release-controller
==================

This is part of generating OpenShift release/update payloads. The canonical
documentation for this repo currently lives here:

https://github.com/openshift/release/tree/master/core-services/release-controller

To build and test changes to this repository, ensure your environment has a KUBECONFIG that points to api.ci and run
`make && ./release-controller --release-namespace ocp --job-namespace ci-release -v=4 --dry-run`

Then navigate to `http://localhost:8080` (plus any relevant path)
