package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	goruntime "runtime"
	"sort"
	"strings"
	"sync"
	"time"

	_ "net/http/pprof" // until openshift/library-go#309 merges

	lru "github.com/hashicorp/golang-lru"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"
	"gopkg.in/fsnotify.v1"
	"k8s.io/klog"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/informers"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/version"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	configflagutil "k8s.io/test-infra/prow/flagutil/config"
	pluginflagutil "k8s.io/test-infra/prow/flagutil/plugins"

	imagev1 "github.com/openshift/api/image/v1"
	imageclientset "github.com/openshift/client-go/image/clientset/versioned"
	imageinformers "github.com/openshift/client-go/image/informers/externalversions"
	imagelisters "github.com/openshift/client-go/image/listers/image/v1"
	"github.com/openshift/library-go/pkg/serviceability"
	prowconfig "k8s.io/test-infra/prow/config"
	"k8s.io/test-infra/prow/config/secret"
	"k8s.io/test-infra/prow/flagutil"
	"k8s.io/test-infra/prow/interrupts"

	"github.com/openshift/release-controller/pkg/bugzilla"
	"github.com/openshift/release-controller/pkg/signer"
)

type options struct {
	ReleaseNamespaces []string
	PublishNamespaces []string
	JobNamespace      string
	ProwNamespace     string

	ProwJobKubeconfig    string
	NonProwJobKubeconfig string
	ReleasesKubeconfig   string
	ToolsKubeconfig      string

	prowconfig configflagutil.ConfigOptions

	ListenAddr    string
	ArtifactsHost string

	AuditStorage           string
	AuditGCSServiceAccount string
	SigningKeyring         string
	CLIImageForAudit       string
	ToolsImageStreamTag    string

	DryRun       bool
	LimitSources []string

	VerifyBugzilla bool
	PluginConfig   pluginflagutil.PluginOptions
	github         flagutil.GitHubOptions
	bugzilla       flagutil.BugzillaOptions
	githubThrottle int

	validateConfigs string

	softDeleteReleaseTags bool

	ReleaseArchitecture string

	AuthenticationMessage string

	Registry string
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
		ListenAddr: ":8080",
		ToolsImageStreamTag: ":tests",
		Registry: "registry.ci.openshift.org",
	}
	cmd := &cobra.Command{
		Run: func(cmd *cobra.Command, arguments []string) {
			if f := cmd.Flags().Lookup("audit"); f.Changed && len(f.Value.String()) == 0 {
				klog.Exitf("error: --audit must include a location to store audit logs")
			}
			if err := opt.Run(); err != nil {
				klog.Exitf("error: %v", err)
			}
		},
	}
	flagset := cmd.Flags()
	flagset.BoolVar(&opt.DryRun, "dry-run", opt.DryRun, "Perform no actions on the release streams")
	flagset.StringVar(&opt.AuditStorage, "audit", opt.AuditStorage, "A storage location to report audit logs to, if specified. The location may be a file://path or gs:// GCS bucket and path.")
	flagset.StringVar(&opt.AuditGCSServiceAccount, "audit-gcs-service-account", opt.AuditGCSServiceAccount, "An optional path to a service account file that should be used for uploading audit information to GCS.")
	flagset.StringSliceVar(&opt.LimitSources, "only-source", opt.LimitSources, "The names of the image streams to operate on. Intended for testing.")
	flagset.StringVar(&opt.SigningKeyring, "sign", opt.SigningKeyring, "The OpenPGP keyring to sign releases with. Only releases that can be verified will be signed.")
	flagset.StringVar(&opt.CLIImageForAudit, "audit-cli-image", opt.CLIImageForAudit, "The command line image pullspec to use for audit and signing. This should be set to a digest under the signers control to prevent attackers from forging verification. If you pass 'local' the oc binary on the path will be used instead of running a job.")

	flagset.StringVar(&opt.ToolsImageStreamTag, "tools-image-stream-tag", opt.ToolsImageStreamTag, "An image stream tag pointing to a release stream that contains the oc command and git (usually <master>:tests).")

	var ignored string
	flagset.StringVar(&ignored, "to", ignored, "REMOVED: The image stream in the release namespace to push releases to.")

	flagset.StringVar(&opt.ProwJobKubeconfig, "prow-job-kubeconfig", opt.ProwJobKubeconfig, "The kubeconfig to use for interacting with ProwJobs. Defaults in-cluster config if unset.")
	flagset.StringVar(&opt.NonProwJobKubeconfig, "non-prow-job-kubeconfig", opt.NonProwJobKubeconfig, "The kubeconfig to use for everything that is not prowjobs (namespaced, pods, batchjobs, ....). Falls back to incluster config if unset")
	flagset.StringVar(&opt.ReleasesKubeconfig, "releases-kubeconfig", opt.ReleasesKubeconfig, "The kubeconfig to use for interacting with release imagestreams and jobs. Falls back to non-prow-job-kubeconfig and then incluster config if unset")
	flagset.StringVar(&opt.ToolsKubeconfig, "tools-kubeconfig", opt.ToolsKubeconfig, "The kubeconfig to use for running the release-controller tools. Falls back to non-prow-job-kubeconfig and then incluster config if unset")

	flagset.StringVar(&opt.JobNamespace, "job-namespace", opt.JobNamespace, "The namespace to execute jobs and hold temporary objects.")
	flagset.StringSliceVar(&opt.ReleaseNamespaces, "release-namespace", opt.ReleaseNamespaces, "The namespace where the source image streams are located and where releases will be published to.")
	flagset.StringSliceVar(&opt.PublishNamespaces, "publish-namespace", opt.PublishNamespaces, "Optional namespaces that the release might publish results to.")
	flagset.StringVar(&opt.ProwNamespace, "prow-namespace", opt.ProwNamespace, "The namespace where the Prow jobs will be created (defaults to --job-namespace).")

	opt.prowconfig.ConfigPathFlagName = "prow-config"
	opt.prowconfig.JobConfigPathFlagName = "job-config"
	opt.prowconfig.AddFlags(flag.CommandLine)
	flagset.AddGoFlagSet(flag.CommandLine)

	flagset.StringVar(&opt.ArtifactsHost, "artifacts", opt.ArtifactsHost, "The public hostname of the artifacts server.")

	flagset.StringVar(&opt.ListenAddr, "listen", opt.ListenAddr, "The address to serve release information on")

	flagset.BoolVar(&opt.VerifyBugzilla, "verify-bugzilla", opt.VerifyBugzilla, "Update status of bugs fixed in accepted release to VERIFIED if PR was approved by QE.")
	flagset.IntVar(&opt.githubThrottle, "github-throttle", 0, "Maximum number of GitHub requests per hour. Used by bugzilla verifier.")

	flagset.StringVar(&opt.validateConfigs, "validate-configs", "", "Validate configs at specified directory and exit without running operator")
	flagset.BoolVar(&opt.softDeleteReleaseTags, "soft-delete-release-tags", false, "If set to true, annotate imagestreamtags instead of deleting them")

	flagset.StringVar(&opt.ReleaseArchitecture, "release-architecture", opt.ReleaseArchitecture, "The architecture of the releases to be created (defaults to 'amd64' if not specified).")

	flagset.StringVar(&opt.AuthenticationMessage, "authentication-message", opt.AuthenticationMessage, "HTML formatted string to display a registry authentication message")

	flagset.StringVar(&opt.Registry, "registry", opt.Registry, "Specify the registry, that the artifact server will use, to retrieve release images when located on remote clusters")

	goFlagSet := flag.NewFlagSet("prowflags", flag.ContinueOnError)
	opt.github.AddFlags(goFlagSet)
	opt.bugzilla.AddFlags(goFlagSet)
	opt.PluginConfig.AddFlags(goFlagSet)
	flagset.AddGoFlagSet(goFlagSet)

	flagset.AddGoFlag(original.Lookup("v"))
	if err := setupKubeconfigWatches(opt); err != nil {
		klog.Warningf("failed to set up kubeconfig watches: %v", err)
	}

	if err := cmd.Execute(); err != nil {
		klog.Exitf("error: %v", err)
	}
}

func (o *options) Run() error {
	if o.validateConfigs != "" {
		return validateConfigs(o.validateConfigs)
	}

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
	if sets.NewString(o.ReleaseNamespaces...).HasAny(o.PublishNamespaces...) {
		return fmt.Errorf("--release-namespace and --publish-namespace may not overlap")
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
	var mode string
	switch {
	case o.DryRun:
		mode = "dry-run"
	case len(o.AuditStorage) > 0:
		mode = "audit"
	case len(o.LimitSources) > 0:
		mode = "manage"
	default:
		mode = "manage"
	}
	config.UserAgent = fmt.Sprintf("release-controller/%s (%s/%s) %s", version.Get().GitVersion, goruntime.GOOS, goruntime.GOARCH, mode)
	releasesConfig.UserAgent = fmt.Sprintf("release-controller/%s (%s/%s) %s", version.Get().GitVersion, goruntime.GOOS, goruntime.GOARCH, mode)
	toolsConfig.UserAgent = fmt.Sprintf("release-controller/%s (%s/%s) %s", version.Get().GitVersion, goruntime.GOOS, goruntime.GOARCH, mode)

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
	klog.Infof("%s releases will be sourced from the following namespaces: %s, and jobs will be run in %s", strings.Title(architecture), strings.Join(o.ReleaseNamespaces, " "), o.JobNamespace)

	start := time.Now()
	imageClient, err := imageclientset.NewForConfig(releasesConfig)
	if err != nil {
		return fmt.Errorf("unable to create image client: %v", err)
	}
	klog.V(4).Infof("1: %v", time.Now().Sub(start))

	start = time.Now()
	prowClient, err := o.prowJobClient(inClusterCfg)
	if err != nil {
		return fmt.Errorf("failed to create prowjob client: %v", err)
	}
	klog.V(4).Infof("2: %v", time.Now().Sub(start))

	stopCh := wait.NeverStop
	var hasSynced []cache.InformerSynced

	start = time.Now()
	batchFactory := informers.NewSharedInformerFactoryWithOptions(releasesClient, 10*time.Minute, informers.WithNamespace(o.JobNamespace))
	jobs := batchFactory.Batch().V1().Jobs()
	hasSynced = append(hasSynced, jobs.Informer().HasSynced)
	klog.V(4).Infof("3: %v", time.Now().Sub(start))

	start = time.Now()
	configAgent := &prowconfig.Agent{}
	if o.prowconfig.ConfigPath != "" {
		var err error
		configAgent, err = o.prowconfig.ConfigAgent()
		if err != nil {
			return err
		}
	}
	klog.V(4).Infof("4: %v", time.Now().Sub(start))

	start = time.Now()
	imageCache := newLatestImageCache(tagParts[0], tagParts[1])
	execReleaseInfo := NewExecReleaseInfo(toolsClient, toolsConfig, o.JobNamespace, releaseNamespace, imageCache.Get)
	releaseInfo := NewCachingReleaseInfo(execReleaseInfo, 64*1024*1024)

	execReleaseFiles := NewExecReleaseFiles(toolsClient, toolsConfig, o.JobNamespace, releaseNamespace, releaseNamespace, o.Registry, imageCache.Get)
	klog.V(4).Infof("5: %v", time.Now().Sub(start))

	start = time.Now()
	graph := NewUpgradeGraph(architecture)
	klog.V(4).Infof("6: %v", time.Now().Sub(start))

	start = time.Now()
	c := NewController(
		client.CoreV1(),
		imageClient.ImageV1(),
		releasesClient.BatchV1(),
		jobs,
		client.CoreV1(),
		configAgent,
		prowClient.Namespace(o.ProwNamespace),
		o.JobNamespace,
		o.ArtifactsHost,
		releaseInfo,
		graph,
		o.softDeleteReleaseTags,
		o.AuthenticationMessage,
	)
	klog.V(4).Infof("7: %v", time.Now().Sub(start))

	start = time.Now()
	if o.VerifyBugzilla {
		var tokens []string

		// Append the path of bugzilla and github secrets.
		if o.github.TokenPath != "" {
			tokens = append(tokens, o.github.TokenPath)
		}

		if o.bugzilla.ApiKeyPath != "" {
			tokens = append(tokens, o.bugzilla.ApiKeyPath)
		}

		secretAgent := &secret.Agent{}
		if err := secretAgent.Start(tokens); err != nil {
			return fmt.Errorf("Error starting secrets agent: %v", err)
		}

		ghClient, err := o.github.GitHubClient(secretAgent, false)
		if err != nil {
			return fmt.Errorf("Failed to create github client: %v", err)
		}
		ghClient.Throttle(o.githubThrottle, 0)
		bzClient, err := o.bugzilla.BugzillaClient(secretAgent)
		if err != nil {
			return fmt.Errorf("Failed to create bugzilla client: %v", err)
		}
		pluginAgent, err := o.PluginConfig.PluginAgent()
		if err != nil {
			return fmt.Errorf("Failed to create plugin agent: %v", err)
		}
		c.bugzillaVerifier = bugzilla.NewVerifier(bzClient, ghClient, pluginAgent.Config())
	}
	klog.V(4).Infof("8: %v", time.Now().Sub(start))

	start = time.Now()
	if len(o.AuditStorage) > 0 {
		u, err := url.Parse(o.AuditStorage)
		if err != nil {
			return fmt.Errorf("--audit must be a valid file:// or gs:// URL: %v", err)
		}
		switch u.Scheme {
		case "file":
			path := u.Path
			if !strings.HasSuffix(path, "/") {
				path += "/"
			}
			store, err := NewFileAuditStore(path)
			if err != nil {
				return err
			}
			if err := store.Refresh(context.Background()); err != nil {
				return err
			}
			c.auditStore = store
		case "gs":
			path := u.Path
			if !strings.HasSuffix(path, "/") {
				path += "/"
			}
			store, err := NewGCSAuditStore(u.Host, path, config.UserAgent, o.AuditGCSServiceAccount)
			if err != nil {
				return fmt.Errorf("unable to initialize audit store: %v", err)
			}
			if err := store.Refresh(context.Background()); err != nil {
				return fmt.Errorf("unable to refresh audit store: %v", err)
			}
			c.auditStore = store
		default:
			return fmt.Errorf("--audit must be a valid file:// or gs:// URL")
		}
	}
	klog.V(4).Infof("9: %v", time.Now().Sub(start))

	if len(o.CLIImageForAudit) > 0 {
		c.cliImageForAudit = o.CLIImageForAudit
	}

	start = time.Now()
	if len(o.SigningKeyring) > 0 {
		signer, err := signer.NewFromKeyring(o.SigningKeyring)
		if err != nil {
			return err
		}
		c.signer = signer
	}
	klog.V(4).Infof("10: %v", time.Now().Sub(start))

	if len(o.ListenAddr) > 0 {
		http.DefaultServeMux.Handle("/metrics", promhttp.Handler())
		http.DefaultServeMux.HandleFunc("/graph", c.graphHandler)
		http.DefaultServeMux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "OK") })
		http.DefaultServeMux.HandleFunc("/healthz/ready", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "OK") })
		http.DefaultServeMux.Handle("/", c.userInterfaceHandler())
		go func() {
			klog.Infof("Listening on %s for UI and metrics", o.ListenAddr)
			if err := http.ListenAndServe(o.ListenAddr, nil); err != nil {
				klog.Exitf("Server exited: %v", err)
			}
		}()
	}

	batchFactory.Start(stopCh)

	// register the publish and release namespaces
	publishNamespaces := sets.NewString(o.PublishNamespaces...)
	for _, ns := range sets.NewString(o.ReleaseNamespaces...).Union(publishNamespaces).List() {
		factory := imageinformers.NewSharedInformerFactoryWithOptions(imageClient, 10*time.Minute, imageinformers.WithNamespace(ns))
		streams := factory.Image().V1().ImageStreams()
		if publishNamespaces.Has(ns) {
			c.AddPublishNamespace(ns, streams)
		} else {
			c.AddReleaseNamespace(ns, streams)
		}
		hasSynced = append(hasSynced, streams.Informer().HasSynced)
		factory.Start(stopCh)
	}
	imageCache.SetLister(c.releaseLister.ImageStreams(releaseNamespace))

	if o.prowconfig.ConfigPath != "" {
		prowInformers := newDynamicSharedIndexInformer(prowClient, o.ProwNamespace, 10*time.Minute, labels.SelectorFromSet(labels.Set{"release.openshift.io/verify": "true"}))
		hasSynced = append(hasSynced, prowInformers.HasSynced)
		c.AddProwInformer(o.ProwNamespace, prowInformers)
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
					from, ok := annotations[releaseAnnotationFromTag]
					if !ok {
						continue
					}
					to, ok := annotations[releaseAnnotationToTag]
					if !ok {
						continue
					}
					jobArchitecture, ok := annotations[releaseAnnotationArchitecture]
					if !ok {
						continue
					}
					if jobArchitecture != architecture {
						continue
					}
					status, ok := prowJobVerificationStatus(job)
					if !ok {
						continue
					}
					graph.Add(from, to, UpgradeResult{
						State: status.State,
						URL:   status.URL,
					})
				}
			}, 2*time.Minute, stopCh)
		}()

		if !o.DryRun {
			go c.syncPeriodicJobs(prowInformers, stopCh)
		}
	}

	klog.Infof("Waiting for caches to sync")
	cache.WaitForCacheSync(stopCh, hasSynced...)

	switch {
	case o.DryRun:
		klog.Infof("Dry run mode (no changes will be made)")

		// read the graph
		go syncGraphToSecret(graph, false, releasesClient.CoreV1().Secrets(releaseNamespace), releaseNamespace, "release-upgrade-graph", stopCh)

		<-stopCh
		return nil
	case len(o.AuditStorage) > 0:
		klog.Infof("Auditing releases to %s", o.AuditStorage)

		// read the graph
		go syncGraphToSecret(graph, false, releasesClient.CoreV1().Secrets(releaseNamespace), releaseNamespace, "release-upgrade-graph", stopCh)

		c.RunAudit(4, stopCh)
		return nil
	case len(o.LimitSources) > 0:
		klog.Infof("Managing only %s, no garbage collection", o.LimitSources)

		// read the graph
		go syncGraphToSecret(graph, false, releasesClient.CoreV1().Secrets(releaseNamespace), releaseNamespace, "release-upgrade-graph", stopCh)

		c.RunSync(3, stopCh)
		return nil
	default:
		klog.Infof("Managing releases")

		// keep the graph in a more persistent form
		go syncGraphToSecret(graph, true, releasesClient.CoreV1().Secrets(releaseNamespace), releaseNamespace, "release-upgrade-graph", stopCh)
		// maintain the release pods
		go refreshReleaseToolsEvery(2*time.Hour, execReleaseInfo, execReleaseFiles, stopCh)

		c.RunSync(3, stopCh)
		return nil
	}
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

func newDynamicSharedIndexInformer(client dynamic.NamespaceableResourceInterface, namespace string, resyncPeriod time.Duration, selector labels.Selector) cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				options.LabelSelector = selector.String()
				return client.Namespace(namespace).List(context.TODO(), options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				options.LabelSelector = selector.String()
				return client.Namespace(namespace).Watch(context.TODO(), options)
			},
		},
		&unstructured.Unstructured{},
		resyncPeriod,
		cache.Indexers{},
	)
}

// latestImageCache tries to find the first valid tag matching
// the requested image stream with the matching name (or the first
// one when looking across all lexigraphically).
type latestImageCache struct {
	imageStream string
	tag         string
	interval    time.Duration

	cache       *lru.Cache
	lock        sync.Mutex
	lister      imagelisters.ImageStreamNamespaceLister
	last        string
	lastChecked time.Time
}

func newLatestImageCache(imageStream string, tag string) *latestImageCache {
	cache, _ := lru.New(64)
	return &latestImageCache{
		imageStream: imageStream,
		tag:         tag,
		interval:    10 * time.Minute,
		cache:       cache,
	}
}

func (c *latestImageCache) SetLister(lister imagelisters.ImageStreamNamespaceLister) {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.lister = lister
}

func (c *latestImageCache) Get() (string, error) {
	c.lock.Lock()
	defer c.lock.Unlock()
	if c.lister == nil {
		return "", fmt.Errorf("not yet started")
	}
	if len(c.last) > 0 && c.lastChecked.After(time.Now().Add(-c.interval)) {
		return c.last, nil
	}

	// Find the first image stream matching the desired stream name name, or the first
	// one that isn't a stable image stream and has the requested tag. Stable image
	// streams
	var preferred *imagev1.ImageStream
	items, _ := c.lister.List(labels.Everything())
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	for _, item := range items {
		if len(c.imageStream) > 0 {
			if c.imageStream == item.Name {
				preferred = item
				break
			}
			continue
		}

		value, ok := item.Annotations[releaseAnnotationConfig]
		if !ok {
			continue
		}
		if spec := findImagePullSpec(item, c.tag); len(spec) == 0 {
			continue
		}
		config, err := parseReleaseConfig(value, c.cache)
		if err != nil {
			continue
		}
		if config.As == releaseConfigModeStable {
			continue
		}

		if preferred == nil {
			preferred = item
			continue
		}
		if len(c.imageStream) > 0 && c.imageStream == item.Name {
			preferred = item
			break
		}
	}

	if preferred != nil {
		if spec := findImagePullSpec(preferred, c.tag); len(spec) > 0 {
			c.last = spec
			c.lastChecked = time.Now()
			klog.V(4).Infof("Resolved %s:%s to %s", c.imageStream, c.tag, spec)
			return spec, nil
		}
	}

	return "", fmt.Errorf("could not find a release image stream with :%s (tools=%s)", c.tag, c.imageStream)
}

func refreshReleaseToolsEvery(interval time.Duration, execReleaseInfo *ExecReleaseInfo, execReleaseFiles *ExecReleaseFiles, stopCh <-chan struct{}) {
	wait.Until(func() {
		err := wait.ExponentialBackoff(wait.Backoff{
			Steps:    3,
			Duration: 1 * time.Second,
			Factor:   2,
		}, func() (bool, error) {
			success := true
			if err := execReleaseInfo.refreshPod(); err != nil {
				klog.Errorf("Unable to refresh git cache, waiting to retry: %v", err)
				success = false
			}
			if err := execReleaseFiles.refreshPod(); err != nil {
				klog.Errorf("Unable to refresh files cache, waiting to retry: %v", err)
				success = false
			}
			return success, nil
		})
		if err != nil {
			klog.Errorf("Unable to refresh git cache, waiting until next sync time")
		}
	}, interval, stopCh)
}

func setupKubeconfigWatches(o *options) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to set up watcher: %w", err)
	}
	for _, candidate := range []string{o.ProwJobKubeconfig, o.NonProwJobKubeconfig, "/var/run/secrets/kubernetes.io/serviceaccount/token"} {
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
