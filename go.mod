module github.com/openshift/release-controller

go 1.17

replace (
	github.com/Azure/go-autorest => github.com/Azure/go-autorest v14.2.0+incompatible
	github.com/bombsimon/logrusr => github.com/stevekuznetsov/logrusr v1.1.1-0.20210709145202-301b9fbb8872
	github.com/containerd/containerd => github.com/containerd/containerd v0.2.10-0.20180716142608-408d13de2fbb
	github.com/docker/docker => github.com/openshift/moby-moby v1.4.2-0.20190308215630-da810a85109d
	github.com/golang/glog => github.com/openshift/golang-glog v0.0.0-20190322123450-3c92600d7533
	github.com/moby/buildkit => github.com/dmcgowan/buildkit v0.0.0-20170731200553-da2b9dc7dab9
	github.com/openshift/ci-tools => github.com/openshift/ci-tools v0.0.0-20220203161918-dbab1f148fc5
	github.com/openshift/client-go => github.com/openshift/client-go v0.0.0-20211209144617-7385dd6338e3
	github.com/openshift/library-go => github.com/openshift/library-go v0.0.0-20211220195323-eca2c467c492
	k8s.io/api => k8s.io/api v0.23.0
	k8s.io/apimachinery => k8s.io/apimachinery v0.23.0
	k8s.io/client-go => k8s.io/client-go v0.23.0
	k8s.io/component-base => k8s.io/component-base v0.23.0
	k8s.io/kubectl => k8s.io/kubectl v0.23.0
)

require (
	cloud.google.com/go/storage v1.12.0
	github.com/awalterschulze/gographviz v0.0.0-20190221210632-1e9ccb565bca
	github.com/blang/semver v3.5.1+incompatible
	github.com/dustin/go-humanize v1.0.0
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da
	github.com/google/go-cmp v0.5.7
	github.com/gorilla/mux v1.8.0
	github.com/hashicorp/go-retryablehttp v0.6.6
	github.com/hashicorp/golang-lru v0.5.4
	github.com/openshift/api v0.0.0-20211209135129-c58d9f695577
	github.com/openshift/build-machinery-go v0.0.0-20211213093930-7e33a7eb4ce3
	github.com/openshift/ci-tools v0.0.0-20220207113218-3dbff936afff
	github.com/openshift/client-go v3.9.0+incompatible
	github.com/openshift/library-go v0.0.0-20200127110935-527e40ed17d9
	github.com/prometheus/client_golang v1.12.1
	github.com/russross/blackfriday v2.0.0+incompatible
	github.com/spf13/cobra v1.2.1
	github.com/spf13/pflag v1.0.5
	golang.org/x/crypto v0.0.0-20210817164053-32db794688a5
	golang.org/x/time v0.0.0-20211116232009-f0f3c7e86c11
	google.golang.org/api v0.44.0
	gopkg.in/fsnotify.v1 v1.4.7
	gopkg.in/robfig/cron.v2 v2.0.0-20150107220207-be2e0b0deed5
	k8s.io/api v0.23.3
	k8s.io/apimachinery v0.23.3
	k8s.io/client-go v11.0.1-0.20190805182717-6502b5e7b1b5+incompatible
	k8s.io/code-generator v0.23.3
	k8s.io/component-base v0.23.3
	k8s.io/klog v1.0.0
	k8s.io/klog/v2 v2.40.1
	k8s.io/kubernetes v1.14.7
	k8s.io/test-infra v0.0.0-20220315203149-843d26549556
)

require (
	cloud.google.com/go v0.81.0 // indirect
	github.com/Azure/go-autorest v14.2.0+incompatible // indirect
	github.com/Azure/go-autorest/autorest v0.11.18 // indirect
	github.com/Azure/go-autorest/autorest/adal v0.9.13 // indirect
	github.com/Azure/go-autorest/autorest/date v0.3.0 // indirect
	github.com/Azure/go-autorest/logger v0.2.1 // indirect
	github.com/Azure/go-autorest/tracing v0.6.0 // indirect
	github.com/GoogleCloudPlatform/testgrid v0.0.68 // indirect
	github.com/NYTimes/gziphandler v1.1.1 // indirect
	github.com/PuerkitoBio/purell v1.1.1 // indirect
	github.com/PuerkitoBio/urlesc v0.0.0-20170810143723-de5bf2ad4578 // indirect
	github.com/andygrunwald/go-gerrit v0.0.0-20210709065208-9d38b0be0268 // indirect
	github.com/andygrunwald/go-jira v1.14.0 // indirect
	github.com/aws/aws-sdk-go v1.37.22 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/bwmarrin/snowflake v0.0.0 // indirect
	github.com/cespare/xxhash/v2 v2.1.2 // indirect
	github.com/clarketm/json v1.13.4 // indirect
	github.com/coreos/go-semver v0.3.0 // indirect
	github.com/coreos/go-systemd/v22 v22.3.2 // indirect
	github.com/danwakefield/fnmatch v0.0.0-20160403171240-cbb64ac3d964 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/denormal/go-gitignore v0.0.0-20180930084346-ae8ad1d07817 // indirect
	github.com/dgrijalva/jwt-go/v4 v4.0.0-preview1 // indirect
	github.com/elazarl/goproxy v0.0.0-20191011121108-aa519ddbe484 // indirect
	github.com/emicklei/go-restful v2.11.1+incompatible // indirect
	github.com/evanphx/json-patch v5.6.0+incompatible // indirect
	github.com/fatih/structs v1.1.0 // indirect
	github.com/felixge/fgprof v0.9.1 // indirect
	github.com/felixge/httpsnoop v1.0.1 // indirect
	github.com/form3tech-oss/jwt-go v3.2.3+incompatible // indirect
	github.com/fsnotify/fsnotify v1.5.1 // indirect
	github.com/fvbommel/sortorder v1.0.1 // indirect
	github.com/ghodss/yaml v1.0.1-0.20190212211648-25d852aebe32 // indirect
	github.com/go-logr/logr v1.2.2 // indirect
	github.com/go-openapi/jsonpointer v0.19.5 // indirect
	github.com/go-openapi/jsonreference v0.19.5 // indirect
	github.com/go-openapi/swag v0.19.14 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang-jwt/jwt v3.2.1+incompatible // indirect
	github.com/golang/protobuf v1.5.2 // indirect
	github.com/gomodule/redigo v1.8.5 // indirect
	github.com/google/btree v1.0.1 // indirect
	github.com/google/go-querystring v1.1.0 // indirect
	github.com/google/gofuzz v1.2.1-0.20210504230335-f78f29fc09ea // indirect
	github.com/google/pprof v0.0.0-20210226084205-cbba55b83ad5 // indirect
	github.com/google/uuid v1.3.0 // indirect
	github.com/google/wire v0.4.0 // indirect
	github.com/googleapis/gax-go v2.0.2+incompatible // indirect
	github.com/googleapis/gax-go/v2 v2.0.5 // indirect
	github.com/googleapis/gnostic v0.5.5 // indirect
	github.com/gregjones/httpcache v0.0.0-20190212212710-3befbb6ad0cc // indirect
	github.com/grpc-ecosystem/go-grpc-prometheus v1.2.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway v1.16.0 // indirect
	github.com/hashicorp/go-cleanhttp v0.5.1 // indirect
	github.com/imdario/mergo v0.3.12 // indirect
	github.com/inconshreveable/mousetrap v1.0.0 // indirect
	github.com/jmespath/go-jmespath v0.4.0 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/jstemmer/go-junit-report v0.9.1 // indirect
	github.com/mailru/easyjson v0.7.6 // indirect
	github.com/mattn/go-zglob v0.0.2 // indirect
	github.com/matttproud/golang_protobuf_extensions v1.0.2-0.20181231171920-c182affec369 // indirect
	github.com/moby/spdystream v0.2.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/peterbourgon/diskv v2.0.1+incompatible // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pkg/profile v1.3.0 // indirect
	github.com/prometheus/client_model v0.2.0 // indirect
	github.com/prometheus/common v0.32.1 // indirect
	github.com/prometheus/procfs v0.7.3 // indirect
	github.com/satori/go.uuid v1.2.0 // indirect
	github.com/shurcooL/githubv4 v0.0.0-20210725200734-83ba7b4c9228 // indirect
	github.com/shurcooL/graphql v0.0.0-20181231061246-d48a9a75455f // indirect
	github.com/shurcooL/sanitized_anchor_name v1.0.0 // indirect
	github.com/sirupsen/logrus v1.8.1 // indirect
	github.com/tektoncd/pipeline v0.14.1-0.20200710073957-5eeb17f81999 // indirect
	github.com/trivago/tgo v1.0.7 // indirect
	go.etcd.io/etcd/api/v3 v3.5.0 // indirect
	go.etcd.io/etcd/client/pkg/v3 v3.5.0 // indirect
	go.etcd.io/etcd/client/v3 v3.5.0 // indirect
	go.opencensus.io v0.23.0 // indirect
	go.opentelemetry.io/contrib v0.20.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.20.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.20.0 // indirect
	go.opentelemetry.io/otel v0.20.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp v0.20.0 // indirect
	go.opentelemetry.io/otel/metric v0.20.0 // indirect
	go.opentelemetry.io/otel/sdk v0.20.0 // indirect
	go.opentelemetry.io/otel/sdk/export/metric v0.20.0 // indirect
	go.opentelemetry.io/otel/sdk/metric v0.20.0 // indirect
	go.opentelemetry.io/otel/trace v0.20.0 // indirect
	go.opentelemetry.io/proto/otlp v0.7.0 // indirect
	go.uber.org/atomic v1.7.0 // indirect
	go.uber.org/multierr v1.6.0 // indirect
	go.uber.org/zap v1.19.1 // indirect
	go4.org v0.0.0-20201209231011-d4a079459e60 // indirect
	gocloud.dev v0.19.0 // indirect
	golang.org/x/lint v0.0.0-20210508222113-6edffad5e616 // indirect
	golang.org/x/mod v0.4.2 // indirect
	golang.org/x/net v0.0.0-20220127200216-cd36cc0744dd // indirect
	golang.org/x/oauth2 v0.0.0-20211104180415-d3ed0bb246c8 // indirect
	golang.org/x/sync v0.0.0-20210220032951-036812b2e83c // indirect
	golang.org/x/sys v0.0.0-20220128215802-99c3d69c2c27 // indirect
	golang.org/x/term v0.0.0-20210927222741-03fcf44c2211 // indirect
	golang.org/x/text v0.3.7 // indirect
	golang.org/x/tools v0.1.6-0.20210820212750-d4cc65f0b2ff // indirect
	golang.org/x/xerrors v0.0.0-20200804184101-5ec99f83aff1 // indirect
	gomodules.xyz/jsonpatch/v2 v2.2.0 // indirect
	google.golang.org/appengine v1.6.7 // indirect
	google.golang.org/genproto v0.0.0-20210831024726-fe130286e0e2 // indirect
	google.golang.org/grpc v1.40.0 // indirect
	google.golang.org/protobuf v1.27.1 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/ini.v1 v1.62.0 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.0.0 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.0-20210107192922-496545a6307b // indirect
	k8s.io/apiextensions-apiserver v0.23.3 // indirect
	k8s.io/apiserver v0.23.3 // indirect
	k8s.io/gengo v0.0.0-20210813121822-485abfe95c7c // indirect
	k8s.io/kube-openapi v0.0.0-20220124234850-424119656bbf // indirect
	k8s.io/utils v0.0.0-20220127004650-9b3446523e65 // indirect
	knative.dev/pkg v0.0.0-20200711004937-22502028e31a // indirect
	sigs.k8s.io/apiserver-network-proxy/konnectivity-client v0.0.27 // indirect
	sigs.k8s.io/controller-runtime v0.11.0 // indirect
	sigs.k8s.io/json v0.0.0-20211208200746-9f7c6b3444d2 // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.2.1 // indirect
	sigs.k8s.io/yaml v1.3.0 // indirect
)
