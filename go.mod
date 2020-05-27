module github.com/openshift/release-controller

go 1.13

replace (
	github.com/golang/glog => github.com/openshift/golang-glog v0.0.0-20190322123450-3c92600d7533
	k8s.io/api => k8s.io/api v0.0.0-20190918195907-bd6ac527cfd2
	k8s.io/apimachinery => k8s.io/apimachinery v0.0.0-20190817020851-f2f3a405f61d
	k8s.io/client-go => k8s.io/client-go v0.0.0-20190918200256-06eb1244587a
)

require (
	cloud.google.com/go v0.45.1
	github.com/awalterschulze/gographviz v0.0.0-20190221210632-1e9ccb565bca
	github.com/blang/semver v3.5.1+incompatible
	github.com/certifi/gocertifi v0.0.0-20180905225744-ee1a9a0726d2 // indirect
	github.com/dustin/go-humanize v1.0.0
	github.com/getsentry/raven-go v0.0.0-20171206001108-32a13797442c // indirect
	github.com/ghodss/yaml v1.0.1-0.20190212211648-25d852aebe32 // indirect
	github.com/golang/glog v0.0.0-20160126235308-23def4e6c14b
	github.com/golang/groupcache v0.0.0-20190702054246-869f871628b6
	github.com/google/go-cmp v0.3.1
	github.com/gorilla/mux v1.7.1
	github.com/hashicorp/golang-lru v0.5.3
	github.com/matttproud/golang_protobuf_extensions v1.0.2-0.20181231171920-c182affec369 // indirect
	github.com/openshift/api v3.9.1-0.20190419161300-aae63d5f0f50+incompatible
	github.com/openshift/client-go v0.0.0-20190412095722-0255926f5393
	github.com/openshift/library-go v0.0.0-20190419201117-4bae0f495ea7
	github.com/pkg/profile v1.3.0 // indirect
	github.com/prometheus/client_golang v1.0.0
	github.com/russross/blackfriday v2.0.0+incompatible
	github.com/shurcooL/sanitized_anchor_name v1.0.0 // indirect
	github.com/spf13/cobra v0.0.5
	golang.org/x/crypto v0.0.0-20190611184440-5c40567a22f8
	golang.org/x/time v0.0.0-20190308202827-9d24e82272b4
	google.golang.org/api v0.13.0
	k8s.io/api v0.0.0-20190918195907-bd6ac527cfd2
	k8s.io/apimachinery v0.0.0-20190817020851-f2f3a405f61d
	k8s.io/client-go v11.0.1-0.20190805182717-6502b5e7b1b5+incompatible
	k8s.io/klog v1.0.0
	k8s.io/test-infra v0.0.0-20191114175535-e3e01110acb8
)
