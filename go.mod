module github.com/openshift/release-controller

go 1.13

replace (
	github.com/Azure/go-autorest => github.com/Azure/go-autorest v12.2.0+incompatible
	github.com/golang/glog => github.com/openshift/golang-glog v0.0.0-20190322123450-3c92600d7533
	k8s.io/api => k8s.io/api v0.0.0-20190918195907-bd6ac527cfd2
	k8s.io/apimachinery => k8s.io/apimachinery v0.0.0-20190817020851-f2f3a405f61d
	k8s.io/client-go => k8s.io/client-go v0.0.0-20190918200256-06eb1244587a
)

require (
	cloud.google.com/go/storage v1.0.0
	github.com/awalterschulze/gographviz v0.0.0-20190221210632-1e9ccb565bca
	github.com/blang/semver v3.5.1+incompatible
	github.com/certifi/gocertifi v0.0.0-20180905225744-ee1a9a0726d2 // indirect
	github.com/coreos/etcd v3.3.13+incompatible // indirect
	github.com/dustin/go-humanize v1.0.0
	github.com/getsentry/raven-go v0.0.0-20171206001108-32a13797442c // indirect
	github.com/ghodss/yaml v1.0.1-0.20190212211648-25d852aebe32 // indirect
	github.com/go-yaml/yaml v2.1.0+incompatible // indirect
	github.com/golang/glog v0.0.0-20160126235308-23def4e6c14b
	github.com/golang/groupcache v0.0.0-20190702054246-869f871628b6
	github.com/golang/lint v0.0.0-20180702182130-06c8688daad7 // indirect
	github.com/gorilla/mux v1.7.3
	github.com/gotestyourself/gotestyourself v2.2.0+incompatible // indirect
	github.com/hashicorp/golang-lru v0.5.4
	github.com/klauspost/cpuid v1.2.1 // indirect
	github.com/knative/build v0.1.2 // indirect
	github.com/matttproud/golang_protobuf_extensions v1.0.2-0.20181231171920-c182affec369 // indirect
	github.com/natefinch/lumberjack v2.0.0+incompatible // indirect
	github.com/openshift/api v3.9.1-0.20190419161300-aae63d5f0f50+incompatible
	github.com/openshift/client-go v0.0.0-20190412095722-0255926f5393
	github.com/openshift/library-go v0.0.0-20190419201117-4bae0f495ea7
	github.com/pkg/profile v1.3.0 // indirect
	github.com/prometheus/client_golang v1.5.0
	github.com/russross/blackfriday v2.0.0+incompatible
	github.com/shurcooL/go v0.0.0-20180423040247-9e1955d9fb6e // indirect
	github.com/spf13/cobra v0.0.6
	golang.org/x/crypto v0.0.0-20200302210943-78000ba7a073
	golang.org/x/time v0.0.0-20191024005414-555d28b269f0
	google.golang.org/api v0.13.0
	gopkg.in/yaml.v1 v1.0.0-20140924161607-9f9df34309c0 // indirect
	k8s.io/api v0.17.3
	k8s.io/apimachinery v0.17.3
	k8s.io/client-go v11.0.1-0.20190805182717-6502b5e7b1b5+incompatible
	k8s.io/klog v1.0.0
	k8s.io/kubernetes v1.14.7 // indirect
	k8s.io/test-infra v0.0.0-20200401184823-f28c158fa750
	sigs.k8s.io/testing_frameworks v0.1.1 // indirect
)
