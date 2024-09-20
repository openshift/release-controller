module github.com/openshift/release-controller

go 1.22.4

replace (
	github.com/Azure/go-autorest => github.com/Azure/go-autorest v14.2.0+incompatible
	github.com/bombsimon/logrusr => github.com/stevekuznetsov/logrusr v1.1.1-0.20210709145202-301b9fbb8872
	github.com/containerd/containerd => github.com/containerd/containerd v0.2.10-0.20180716142608-408d13de2fbb
	github.com/docker/docker => github.com/openshift/moby-moby v1.4.2-0.20190308215630-da810a85109d
	github.com/emicklei/go-restful => github.com/emicklei/go-restful v2.16.0+incompatible
	github.com/golang/glog => github.com/openshift/golang-glog v0.0.0-20190322123450-3c92600d7533

	github.com/google/cel-go => github.com/google/cel-go v0.17.8

	github.com/hashicorp/golang-lru => github.com/hashicorp/golang-lru v0.6.0
	github.com/moby/buildkit => github.com/dmcgowan/buildkit v0.0.0-20170731200553-da2b9dc7dab9

	github.com/openshift/client-go => github.com/openshift/client-go v0.0.0-20211209144617-7385dd6338e3

	github.com/prometheus/client_golang => github.com/prometheus/client_golang v1.16.0
	github.com/prometheus/common => github.com/prometheus/common v0.44.0

	go.opentelemetry.io/otel/exporters/otlp/otlptrace => go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.17.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc => go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.17.0
)

require (
	cloud.google.com/go/storage v1.40.0
	github.com/andygrunwald/go-jira v1.15.1
	github.com/awalterschulze/gographviz v0.0.0-20190221210632-1e9ccb565bca
	github.com/blang/semver v3.5.1+incompatible
	github.com/dustin/go-humanize v1.0.1
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da
	github.com/google/go-cmp v0.6.0
	github.com/gorilla/mux v1.8.0
	github.com/hashicorp/go-retryablehttp v0.7.7
	github.com/hashicorp/golang-lru v1.0.2
	github.com/opencontainers/go-digest v1.0.0
	github.com/openshift-eng/jira-lifecycle-plugin v0.0.0-20240710130641-608a3c0b33ae
	github.com/openshift/api v0.0.0-20240522145529-93d6bda14341
	github.com/openshift/build-machinery-go v0.0.0-20220913142420-e25cf57ea46d
	github.com/openshift/ci-tools v0.0.0-20240710031808-de122ac79fa9
	github.com/openshift/client-go v3.9.0+incompatible
	github.com/openshift/library-go v0.0.0-20231017173800-126f85ed0cc7
	github.com/prometheus/client_golang v1.19.1
	github.com/russross/blackfriday v2.0.0+incompatible
	github.com/spf13/cobra v1.8.0
	github.com/spf13/pflag v1.0.5
	golang.org/x/crypto v0.24.0
	golang.org/x/time v0.5.0
	google.golang.org/api v0.181.0
	gopkg.in/fsnotify.v1 v1.4.7
	gopkg.in/robfig/cron.v2 v2.0.0-20150107220207-be2e0b0deed5
	k8s.io/api v0.30.2
	k8s.io/apimachinery v0.30.2
	k8s.io/client-go v0.30.2
	k8s.io/code-generator v0.29.6
	k8s.io/component-base v0.30.0
	k8s.io/klog v1.0.0
	k8s.io/klog/v2 v2.120.1
	sigs.k8s.io/prow v0.0.0-20240917051714-0ac691ea5cae
)

require (
	cloud.google.com/go v0.113.0 // indirect
	cloud.google.com/go/auth v0.4.1 // indirect
	cloud.google.com/go/auth/oauth2adapt v0.2.2 // indirect
	cloud.google.com/go/compute/metadata v0.3.0 // indirect
	cloud.google.com/go/iam v1.1.8 // indirect
	contrib.go.opencensus.io/exporter/ocagent v0.7.1-0.20200907061046-05415f1de66d // indirect
	contrib.go.opencensus.io/exporter/prometheus v0.4.2 // indirect
	github.com/Azure/azure-pipeline-go v0.2.2 // indirect
	github.com/GoogleCloudPlatform/testgrid v0.0.123 // indirect
	github.com/NYTimes/gziphandler v1.1.1 // indirect
	github.com/antlr/antlr4/runtime/Go/antlr/v4 v4.0.0-20230305170008-8188dc5388df // indirect
	github.com/asaskevich/govalidator v0.0.0-20190424111038-f61b66f89f4a // indirect
	github.com/aws/aws-sdk-go v1.44.116 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/blang/semver/v4 v4.0.0 // indirect
	github.com/blendle/zapdriver v1.3.1 // indirect
	github.com/bombsimon/logrusr/v4 v4.1.0 // indirect
	github.com/bwmarrin/snowflake v0.0.0 // indirect
	github.com/cenkalti/backoff/v4 v4.3.0 // indirect
	github.com/census-instrumentation/opencensus-proto v0.4.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/cjwagner/httpcache v0.0.0-20230907212505-d4841bbad466 // indirect
	github.com/clarketm/json v1.13.4 // indirect
	github.com/coreos/go-semver v0.3.1 // indirect
	github.com/coreos/go-systemd/v22 v22.5.0 // indirect
	github.com/danwakefield/fnmatch v0.0.0-20160403171240-cbb64ac3d964 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/denormal/go-gitignore v0.0.0-20180930084346-ae8ad1d07817 // indirect
	github.com/dgrijalva/jwt-go/v4 v4.0.0-preview1 // indirect
	github.com/emicklei/go-restful/v3 v3.11.0 // indirect
	github.com/evanphx/json-patch v5.6.0+incompatible // indirect
	github.com/evanphx/json-patch/v5 v5.9.0 // indirect
	github.com/fatih/structs v1.1.0 // indirect
	github.com/felixge/fgprof v0.9.1 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/fsnotify/fsnotify v1.7.0 // indirect
	github.com/fvbommel/sortorder v1.1.0 // indirect
	github.com/ghodss/yaml v1.0.1-0.20190212211648-25d852aebe32 // indirect
	github.com/go-kit/log v0.2.1 // indirect
	github.com/go-logfmt/logfmt v0.5.1 // indirect
	github.com/go-logr/logr v1.4.1 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-openapi/jsonpointer v0.19.6 // indirect
	github.com/go-openapi/jsonreference v0.20.2 // indirect
	github.com/go-openapi/swag v0.22.10 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang-jwt/jwt/v4 v4.5.0 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/gomodule/redigo v1.8.5 // indirect
	github.com/google/btree v1.0.1 // indirect
	github.com/google/cel-go v0.20.1 // indirect
	github.com/google/gnostic-models v0.6.8 // indirect
	github.com/google/go-querystring v1.1.0 // indirect
	github.com/google/gofuzz v1.2.1-0.20210504230335-f78f29fc09ea // indirect
	github.com/google/pprof v0.0.0-20210720184732-4bb14d4b1be1 // indirect
	github.com/google/s2a-go v0.1.7 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/google/wire v0.4.0 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.2 // indirect
	github.com/googleapis/gax-go v2.0.2+incompatible // indirect
	github.com/googleapis/gax-go/v2 v2.12.4 // indirect
	github.com/grpc-ecosystem/go-grpc-prometheus v1.2.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.20.0 // indirect
	github.com/hashicorp/go-cleanhttp v0.5.2 // indirect
	github.com/imdario/mergo v0.3.16 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/jmespath/go-jmespath v0.4.0 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/mattn/go-ieproxy v0.0.1 // indirect
	github.com/mattn/go-zglob v0.0.2 // indirect
	github.com/matttproud/golang_protobuf_extensions v1.0.4 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/peterbourgon/diskv v2.0.1+incompatible // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pkg/profile v1.3.0 // indirect
	github.com/prometheus/client_model v0.6.1 // indirect
	github.com/prometheus/common v0.55.0 // indirect
	github.com/prometheus/procfs v0.15.1 // indirect
	github.com/prometheus/statsd_exporter v0.22.7 // indirect
	github.com/shurcooL/githubv4 v0.0.0-20220520033151-0b4e3294ff00 // indirect
	github.com/shurcooL/graphql v0.0.0-20181231061246-d48a9a75455f // indirect
	github.com/shurcooL/sanitized_anchor_name v1.0.0 // indirect
	github.com/sirupsen/logrus v1.9.3 // indirect
	github.com/stoewer/go-strcase v1.2.0 // indirect
	github.com/tektoncd/pipeline v0.61.0 // indirect
	github.com/trivago/tgo v1.0.7 // indirect
	go.etcd.io/etcd/api/v3 v3.5.10 // indirect
	go.etcd.io/etcd/client/pkg/v3 v3.5.10 // indirect
	go.etcd.io/etcd/client/v3 v3.5.10 // indirect
	go.opencensus.io v0.24.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.49.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.49.0 // indirect
	go.opentelemetry.io/otel v1.27.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.27.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.19.0 // indirect
	go.opentelemetry.io/otel/metric v1.27.0 // indirect
	go.opentelemetry.io/otel/sdk v1.27.0 // indirect
	go.opentelemetry.io/otel/trace v1.27.0 // indirect
	go.opentelemetry.io/proto/otlp v1.2.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.27.0 // indirect
	go4.org v0.0.0-20201209231011-d4a079459e60 // indirect
	gocloud.dev v0.19.0 // indirect
	golang.org/x/exp v0.0.0-20240222234643-814bf88cf225 // indirect
	golang.org/x/mod v0.17.0 // indirect
	golang.org/x/net v0.26.0 // indirect
	golang.org/x/oauth2 v0.21.0 // indirect
	golang.org/x/sync v0.7.0 // indirect
	golang.org/x/sys v0.21.0 // indirect
	golang.org/x/term v0.21.0 // indirect
	golang.org/x/text v0.16.0 // indirect
	golang.org/x/tools v0.21.1-0.20240508182429-e35e4ccd0d2d // indirect
	golang.org/x/xerrors v0.0.0-20231012003039-104605ab7028 // indirect
	gomodules.xyz/jsonpatch/v2 v2.4.0 // indirect
	google.golang.org/genproto v0.0.0-20240401170217-c3f982113cda // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20240528184218-531527333157 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240528184218-531527333157 // indirect
	google.golang.org/grpc v1.65.0 // indirect
	google.golang.org/protobuf v1.34.2 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/ini.v1 v1.63.2 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/apiserver v0.30.0 // indirect
	k8s.io/gengo v0.0.0-20240129211411-f967bbeff4b4 // indirect
	k8s.io/gengo/v2 v2.0.0-20240228010128-51d4e06bde70 // indirect
	k8s.io/kms v0.30.0 // indirect
	k8s.io/kube-openapi v0.0.0-20240228011516-70dd3763d340 // indirect
	k8s.io/utils v0.0.0-20240102154912-e7106e64919e // indirect
	knative.dev/pkg v0.0.0-20240416145024-0f34a8815650 // indirect
	sigs.k8s.io/apiserver-network-proxy/konnectivity-client v0.29.0 // indirect
	sigs.k8s.io/controller-runtime v0.18.0 // indirect
	sigs.k8s.io/json v0.0.0-20221116044647-bc3834ca7abd // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.4.1 // indirect
	sigs.k8s.io/yaml v1.4.0 // indirect
)
