package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	goruntime "runtime"
	"strconv"
	"strings"
	"time"

	_ "net/http/pprof" // until openshift/library-go#309 merges

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"
	"gopkg.in/fsnotify.v1"
	"k8s.io/klog"

	imageclientset "github.com/openshift/client-go/image/clientset/versioned"
	imageinformers "github.com/openshift/client-go/image/informers/externalversions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/version"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/openshift/library-go/pkg/serviceability"
	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
	"k8s.io/test-infra/prow/flagutil"
	"k8s.io/test-infra/prow/interrupts"
	"k8s.io/test-infra/prow/pjutil"
)

type options struct {
	ReleaseNamespaces []string
	JobNamespace      string
	ProwNamespace     string

	ProwJobKubeconfig    string
	NonProwJobKubeconfig string
	ReleasesKubeconfig   string
	ToolsKubeconfig      string
	ToolsImageStreamTag  string

	ListenAddr    string
	ListenPort    int
	ArtifactsHost string

	ReleaseArchitecture string

	AuthenticationMessage string

	ARTSuffix string
}

func main() {
	serviceability.StartProfiler()
	defer serviceability.Profile(os.Getenv("OPENSHIFT_PROFILE")).Stop()
	// prow registers this on init
	interrupts.OnInterrupt(func() { os.Exit(0) })

	original := flag.CommandLine
	klog.InitFlags(original)
	original.Set("alsologtostderr", "true")
	original.Set("v", "2")

	opt := &options{
		ListenAddr:          ":8080",
		ToolsImageStreamTag: ":tests",
	}
	cmd := &cobra.Command{
		Run: func(cmd *cobra.Command, arguments []string) {
			if err := opt.Run(); err != nil {
				klog.Exitf("error: %v", err)
			}
		},
	}
	flagset := cmd.Flags()
	flagset.StringVar(&opt.ToolsImageStreamTag, "tools-image-stream-tag", opt.ToolsImageStreamTag, "An image stream tag pointing to a release stream that contains the oc command and git (usually <master>:tests).")

	flagset.StringVar(&opt.ProwJobKubeconfig, "prow-job-kubeconfig", opt.ProwJobKubeconfig, "The kubeconfig to use for interacting with ProwJobs. Defaults in-cluster config if unset.")
	flagset.StringVar(&opt.NonProwJobKubeconfig, "non-prow-job-kubeconfig", opt.NonProwJobKubeconfig, "The kubeconfig to use for everything that is not prowjobs (namespaced, pods, batchjobs, ....). Falls back to incluster config if unset")
	flagset.StringVar(&opt.ReleasesKubeconfig, "releases-kubeconfig", opt.ReleasesKubeconfig, "The kubeconfig to use for interacting with release imagestreams and jobs. Falls back to non-prow-job-kubeconfig and then incluster config if unset")
	flagset.StringVar(&opt.ToolsKubeconfig, "tools-kubeconfig", opt.ToolsKubeconfig, "The kubeconfig to use for running the release-controller tools. Falls back to non-prow-job-kubeconfig and then incluster config if unset")

	flagset.StringVar(&opt.JobNamespace, "job-namespace", opt.JobNamespace, "The namespace to execute jobs and hold temporary objects.")
	flagset.StringSliceVar(&opt.ReleaseNamespaces, "release-namespace", opt.ReleaseNamespaces, "The namespace where the source image streams are located and where releases will be published to.")
	flagset.StringVar(&opt.ProwNamespace, "prow-namespace", opt.ProwNamespace, "The namespace where the Prow jobs will be created (defaults to --job-namespace).")

	flagset.AddGoFlagSet(flag.CommandLine)

	flagset.StringVar(&opt.ArtifactsHost, "artifacts", opt.ArtifactsHost, "The public hostname of the artifacts server.")

	flagset.StringVar(&opt.ListenAddr, "listen", opt.ListenAddr, "UNUSED: The address to serve release information on")
	flagset.IntVar(&opt.ListenPort, "port", 8080, "Port to run server on")

	flagset.StringVar(&opt.ReleaseArchitecture, "release-architecture", opt.ReleaseArchitecture, "The architecture of the releases to be created (defaults to 'amd64' if not specified).")

	flagset.StringVar(&opt.AuthenticationMessage, "authentication-message", opt.AuthenticationMessage, "HTML formatted string to display a registry authentication message")

	flagset.StringVar(&opt.ARTSuffix, "art-suffix", "", "Suffix for ART imagstreams (eg. `-art-latest`)")

	flagset.AddGoFlag(original.Lookup("v"))
	if err := setupKubeconfigWatches(opt); err != nil {
		klog.Warningf("failed to set up kubeconfig watches: %v", err)
	}

	if err := cmd.Execute(); err != nil {
		klog.Exitf("error: %v", err)
	}
}

func (o *options) Run() error {
	// report liveness on default prow health port 8081
	health := pjutil.NewHealthOnPort(flagutil.DefaultHealthPort)
	tagParts := strings.Split(o.ToolsImageStreamTag, ":")
	if len(tagParts) != 2 || len(tagParts[1]) == 0 {
		return fmt.Errorf("--tools-image-stream-tag must be STREAM:TAG or :TAG (default STREAM is the oldest release stream)")
	}
	if len(o.ReleaseNamespaces) == 0 {
		return fmt.Errorf("no namespace set, use --release-namespace")
	}
	if len(o.JobNamespace) == 0 {
		return fmt.Errorf("no job namespace set, use --job-namespace")
	}
	if len(o.ProwNamespace) == 0 {
		o.ProwNamespace = o.JobNamespace
	}
	var architecture = "amd64"
	if len(o.ReleaseArchitecture) > 0 {
		architecture = o.ReleaseArchitecture
	}

	inClusterCfg, err := loadClusterConfig()
	if err != nil {
		return fmt.Errorf("failed to load incluster config: %w", err)
	}
	config, err := o.nonProwJobKubeconfig(inClusterCfg)
	if err != nil {
		return fmt.Errorf("failed to load config from %s: %w", o.NonProwJobKubeconfig, err)
	}
	releasesConfig, err := o.releasesKubeconfig(inClusterCfg)
	if err != nil {
		return fmt.Errorf("failed to load releases config from %s: %w", o.ReleasesKubeconfig, err)
	}
	toolsConfig, err := o.toolsKubeconfig(inClusterCfg)
	if err != nil {
		return fmt.Errorf("failed to load tools config from %s: %w", o.ToolsKubeconfig, err)
	}
	config.UserAgent = fmt.Sprintf("release-controller-api/%s (%s/%s)", version.Get().GitVersion, goruntime.GOOS, goruntime.GOARCH)
	releasesConfig.UserAgent = fmt.Sprintf("release-controller-api/%s (%s/%s)", version.Get().GitVersion, goruntime.GOOS, goruntime.GOARCH)
	toolsConfig.UserAgent = fmt.Sprintf("release-controller-api/%s (%s/%s)", version.Get().GitVersion, goruntime.GOOS, goruntime.GOARCH)

	client, err := clientset.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("unable to create client: %v", err)
	}
	releasesClient, err := clientset.NewForConfig(releasesConfig)
	if err != nil {
		return fmt.Errorf("unable to create releases client: %v", err)
	}
	toolsClient, err := clientset.NewForConfig(toolsConfig)
	if err != nil {
		return fmt.Errorf("unable to create tools client: %v", err)
	}
	releaseNamespace := o.ReleaseNamespaces[0]
	for _, ns := range o.ReleaseNamespaces {
		if _, err := client.CoreV1().Namespaces().Get(context.TODO(), ns, metav1.GetOptions{}); err != nil {
			return fmt.Errorf("unable to find release namespace: %s: %v", ns, err)
		}
	}
	if o.JobNamespace != releaseNamespace {
		if _, err := client.CoreV1().Namespaces().Get(context.TODO(), o.JobNamespace, metav1.GetOptions{}); err != nil {
			return fmt.Errorf("unable to find job namespace: %v", err)
		}
	}
	imageClient, err := imageclientset.NewForConfig(releasesConfig)
	if err != nil {
		return fmt.Errorf("unable to create image client: %v", err)
	}

	prowClient, err := o.prowJobClient(inClusterCfg)
	if err != nil {
		return fmt.Errorf("failed to create prowjob client: %v", err)
	}

	klog.Infof("%s releases will be sourced from the following namespaces: %s, and jobs will be run in %s", strings.Title(architecture), strings.Join(o.ReleaseNamespaces, " "), o.JobNamespace)

	imageCache := releasecontroller.NewLatestImageCache(tagParts[0], tagParts[1])
	execReleaseInfo := releasecontroller.NewExecReleaseInfo(toolsClient, toolsConfig, o.JobNamespace, releaseNamespace, imageCache.Get)
	releaseInfo := releasecontroller.NewCachingReleaseInfo(execReleaseInfo, 64*1024*1024, architecture)

	graph := releasecontroller.NewUpgradeGraph(architecture)

	c := NewController(
		client.CoreV1(),
		o.ArtifactsHost,
		releaseInfo,
		graph,
		o.AuthenticationMessage,
		architecture,
		o.ARTSuffix,
	)

	var hasSynced []cache.InformerSynced
	stopCh := wait.NeverStop
	for _, ns := range o.ReleaseNamespaces {
		factory := imageinformers.NewSharedInformerFactoryWithOptions(imageClient, 10*time.Minute, imageinformers.WithNamespace(ns))
		streams := factory.Image().V1().ImageStreams()
		c.releaseLister.Listers[ns] = streams.Lister().ImageStreams(ns)
		hasSynced = append(hasSynced, streams.Informer().HasSynced)
		factory.Start(stopCh)
	}
	imageCache.SetLister(c.releaseLister.ImageStreams(releaseNamespace))

	prowInformers := releasecontroller.NewDynamicSharedIndexInformer(prowClient, o.ProwNamespace, 10*time.Minute, labels.SelectorFromSet(labels.Set{releasecontroller.ReleaseLabelVerify: "true"}))
	hasSynced = append(hasSynced, prowInformers.HasSynced)
	go prowInformers.Run(stopCh)

	go func() {
		index := prowInformers.GetIndexer()
		cache.WaitForCacheSync(stopCh, prowInformers.HasSynced)
		wait.Until(func() {
			for _, item := range index.List() {
				job, ok := item.(*unstructured.Unstructured)
				if !ok {
					continue
				}
				annotations := job.GetAnnotations()
				from, ok := annotations[releasecontroller.ReleaseAnnotationFromTag]
				if !ok {
					continue
				}
				to, ok := annotations[releasecontroller.ReleaseAnnotationToTag]
				if !ok {
					continue
				}
				jobArchitecture, ok := annotations[releasecontroller.ReleaseAnnotationArchitecture]
				if !ok {
					continue
				}
				if jobArchitecture != architecture {
					continue
				}
				status, ok := releasecontroller.ProwJobVerificationStatus(job)
				if !ok {
					continue
				}
				graph.Add(from, to, releasecontroller.UpgradeResult{
					State: status.State,
					URL:   status.URL,
				})
			}
		}, 2*time.Minute, stopCh)
	}()

	klog.Infof("Waiting for caches to sync")
	cache.WaitForCacheSync(stopCh, hasSynced...)

	// read the graph
	go releasecontroller.SyncGraphToSecret(graph, false, releasesClient.CoreV1().Secrets(releaseNamespace), releaseNamespace, "release-upgrade-graph", stopCh)

	http.DefaultServeMux.Handle("/metrics", promhttp.Handler())
	http.DefaultServeMux.HandleFunc("/graph", c.graphHandler)
	http.DefaultServeMux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {})
	http.DefaultServeMux.Handle("/", c.userInterfaceHandler())
	klog.Infof("Listening on port %s for UI and metrics", strconv.Itoa(o.ListenPort))
	interrupts.ListenAndServe(&http.Server{Addr: ":" + strconv.Itoa(o.ListenPort)}, time.Second*10)
	// report that this release-controller-api is ready while http server is responding
	health.ServeReady(func() bool {
		resp, err := http.DefaultClient.Get("http://127.0.0.1:" + strconv.Itoa(o.ListenPort) + "/readyz")
		if resp != nil {
			resp.Body.Close()
		}
		return err == nil && resp.StatusCode == 200
	})
	interrupts.WaitForGracefulShutdown()
	return nil
}

func (o *options) nonProwJobKubeconfig(inClusterCfg *rest.Config) (*rest.Config, error) {
	if o.NonProwJobKubeconfig == "" {
		return inClusterCfg, nil
	}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: o.NonProwJobKubeconfig},
		&clientcmd.ConfigOverrides{},
	).ClientConfig()
}

func (o *options) releasesKubeconfig(inClusterCfg *rest.Config) (*rest.Config, error) {
	if o.ReleasesKubeconfig == "" {
		return o.nonProwJobKubeconfig(inClusterCfg)
	}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: o.ReleasesKubeconfig},
		&clientcmd.ConfigOverrides{},
	).ClientConfig()
}

func (o *options) toolsKubeconfig(inClusterCfg *rest.Config) (*rest.Config, error) {
	if o.ToolsKubeconfig == "" {
		return o.nonProwJobKubeconfig(inClusterCfg)
	}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: o.ToolsKubeconfig},
		&clientcmd.ConfigOverrides{},
	).ClientConfig()
}

// loadClusterConfig loads connection configuration
// for the cluster we're deploying to. We prefer to
// use in-cluster configuration if possible, but will
// fall back to using default rules otherwise.
func loadClusterConfig() (*rest.Config, error) {
	cfg := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(clientcmd.NewDefaultClientConfigLoadingRules(), &clientcmd.ConfigOverrides{})
	clusterConfig, err := cfg.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("could not load client configuration: %v", err)
	}
	return clusterConfig, nil
}

func setupKubeconfigWatches(o *options) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to set up watcher: %w", err)
	}
	for _, candidate := range []string{o.ProwJobKubeconfig, o.NonProwJobKubeconfig} {
		if _, err := os.Stat(candidate); err != nil {
			continue
		}
		if err := watcher.Add(candidate); err != nil {
			return fmt.Errorf("failed to watch %s: %w", candidate, err)
		}
	}

	go func() {
		for e := range watcher.Events {
			if e.Op == fsnotify.Chmod {
				// For some reason we get frequent chmod events from Openshift
				continue
			}
			klog.Infof("event: %s, kubeconfig changed, exiting to make the kubelet restart us so we can pick them up", e.String())
			interrupts.Terminate()
		}
	}()

	return nil
}

func (o *options) prowJobClient(cfg *rest.Config) (dynamic.NamespaceableResourceInterface, error) {
	if o.ProwJobKubeconfig != "" {
		var err error
		cfg, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			&clientcmd.ClientConfigLoadingRules{ExplicitPath: o.ProwJobKubeconfig},
			&clientcmd.ConfigOverrides{},
		).ClientConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to load prowjob kubeconfig from path %q: %v", o.ProwJobKubeconfig, err)
		}
	}

	dynamicClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("unable to create prow client: %v", err)
	}

	return dynamicClient.Resource(schema.GroupVersionResource{Group: "prow.k8s.io", Version: "v1", Resource: "prowjobs"}), nil
}
