all: deploy
.PHONY: all

deploy:
	oc create -f deploy.yaml
.PHONY: deploy

undeploy:
	-oc delete -f deploy.yaml
	-oc delete -n release-controller-test-job statefulsets --all
.PHONY: undeploy

clean: undeploy
	-oc delete -n release-controller-test-job jobs --all
	-oc delete -n release-controller-test-job prowjobs --all
	-oc delete -n release-controller-test-release is --all
.PHONY: clean

all-releases:
	./hack/generate-releases.py -a
.PHONY: all-releases

4.1:
	./hack/generate-releases.py -a -v 4.1
.PHONY: 4.1

4.2:
	./hack/generate-releases.py -a -v 4.2
.PHONY: 4.2

4.3:
	./hack/generate-releases.py -a -v 4.3
.PHONY: 4.3

4.4:
	./hack/generate-releases.py -a -v 4.4
.PHONY: 4.4

4.5:
	./hack/generate-releases.py -a -v 4.5
.PHONY: 4.5

4.6:
	./hack/generate-releases.py -a -v 4.6
.PHONY: 4.6

4.7:
	./hack/generate-releases.py -a -v 4.7
.PHONY: 4.7

