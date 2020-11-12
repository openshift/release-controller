module github.com/openshift/release-controller

go 1.13

replace (
	github.com/Azure/go-autorest => github.com/Azure/go-autorest v12.2.0+incompatible
	github.com/golang/glog => github.com/openshift/golang-glog v0.0.0-20190322123450-3c92600d7533
	github.com/openshift/client-go => github.com/openshift/client-go v0.0.0-20200521150516-05eb9880269c
	github.com/openshift/library-go => github.com/openshift/library-go v0.0.0-20200527213645-a9b77f5402e3
	k8s.io/api => k8s.io/api v0.19.3
	k8s.io/apimachinery => k8s.io/apimachinery v0.19.3
	k8s.io/client-go => k8s.io/client-go v0.19.3
)

require (
	cloud.google.com/go/storage v1.10.0
	github.com/awalterschulze/gographviz v0.0.0-20190221210632-1e9ccb565bca
	github.com/blang/semver v3.5.1+incompatible
	github.com/dustin/go-humanize v1.0.0
	github.com/ghodss/yaml v1.0.1-0.20190212211648-25d852aebe32 // indirect
	github.com/golang/groupcache v0.0.0-20200121045136-8c9f03a8e57e
	github.com/google/go-cmp v0.5.2
	github.com/gorilla/mux v1.7.4
	github.com/hashicorp/golang-lru v0.5.4
	github.com/openshift/api v0.0.0-20200521101457-60c476765272
	github.com/openshift/client-go v0.0.0-20200521150516-05eb9880269c
	github.com/openshift/library-go v0.0.0-20190419201117-4bae0f495ea7
	github.com/prometheus/client_golang v1.7.1
	github.com/russross/blackfriday v2.0.0+incompatible
	github.com/spf13/cobra v1.0.0
	golang.org/x/crypto v0.0.0-20200622213623-75b288015ac9
	golang.org/x/time v0.0.0-20200630173020-3af7569d3a1e
	google.golang.org/api v0.29.0
	gopkg.in/robfig/cron.v2 v2.0.0-20150107220207-be2e0b0deed5
	k8s.io/api v0.19.3
	k8s.io/apimachinery v0.19.3
	k8s.io/client-go v11.0.1-0.20190805182717-6502b5e7b1b5+incompatible
	k8s.io/klog v1.0.0
	k8s.io/kube-openapi v0.0.0-20200805222855-6aeccd4b50c6
	k8s.io/test-infra v0.0.0-20201112175450-00537d1bb47c
)
