package main

import (
	"context"
	"flag"
	"fmt"
	releasepayloadinformers "github.com/openshift/release-controller/pkg/client/informers/externalversions"
	"net/http"
	"net/url"
	"os"
	goruntime "runtime"
	"strings"
	"time"

	releasepayloadclient "github.com/openshift/release-controller/pkg/client/clientset/versioned"
	"github.com/openshift/release-controller/pkg/jira"

	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"

	_ "net/http/pprof" // until openshift/library-go#309 merges

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"
	"gopkg.in/fsnotify.v1"
	"k8s.io/klog"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/informers"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/version"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	bugzillaBaseClient "k8s.io/test-infra/prow/bugzilla"
	"k8s.io/test-infra/prow/config/secret"
	configflagutil "k8s.io/test-infra/prow/flagutil/config"
	pluginflagutil "k8s.io/test-infra/prow/flagutil/plugins"
	"k8s.io/test-infra/prow/pjutil"

	imageclientset "github.com/openshift/client-go/image/clientset/versioned"
	imageinformers "github.com/openshift/client-go/image/informers/externalversions"
	"github.com/openshift/library-go/pkg/serviceability"
	prowconfig "k8s.io/test-infra/prow/config"
	"k8s.io/test-infra/prow/flagutil"
	"k8s.io/test-infra/prow/interrupts"
	jiraBaseClient "k8s.io/test-infra/prow/jira"

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

	PluginConfig pluginflagutil.PluginOptions

	githubThrottle int
	github         flagutil.GitHubOptions

	VerifyBugzilla bool
	bugzilla       flagutil.BugzillaOptions

	VerifyJira bool
	jira       flagutil.JiraOptions

	validateConfigs string

	softDeleteReleaseTags bool

	ReleaseArchitecture string

	AuthenticationMessage string

	Registry string

	ClusterGroups []string

	ARTSuffix string

	PruneGraph        bool
	PrintPrunedGraph  string
	ConfirmPruneGraph bool

	ProcessLegacyResults bool
}

// Add metrics for bugzilla verifier errors
var (
	bugzillaErrorMetrics = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "release_controller_bugzilla_errors_total",
			Help: "The total number of errors encountered by the release-controller's bugzilla verifier",
		},
		[]string{"type"},
	)
)

// Add metrics for jira verifier errors
var (
	jiraErrorMetrics = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "release_controller_jira_errors_total",
			Help: "The total number of errors encountered by the release-controller's jira verifier",
		},
		[]string{"type"},
	)
)

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

		Registry: "registry.ci.openshift.org",

		PrintPrunedGraph: releasecontroller.PruneGraphPrintSecret,
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

	flagset.StringVar(&opt.ArtifactsHost, "artifacts", opt.ArtifactsHost, "REMOVED: The public hostname of the artifacts server.")

	flagset.StringVar(&opt.ListenAddr, "listen", opt.ListenAddr, "The address to serve metrics on")

	flagset.BoolVar(&opt.VerifyBugzilla, "verify-bugzilla", opt.VerifyBugzilla, "Update status of bugs fixed in accepted release to VERIFIED if PR was approved by QE.")
	flagset.BoolVar(&opt.VerifyJira, "verify-jira", opt.VerifyJira, "Update status of issues fixed in accepted release to VERIFIED if PR was approved by QE.")
	flagset.IntVar(&opt.githubThrottle, "github-throttle", 0, "Maximum number of GitHub requests per hour. Used by bugzilla and jira verifier.")

	flagset.StringVar(&opt.validateConfigs, "validate-configs", "", "Validate configs at specified directory and exit without running operator")
	flagset.BoolVar(&opt.softDeleteReleaseTags, "soft-delete-release-tags", false, "If set to true, annotate imagestreamtags instead of deleting them")

	flagset.StringVar(&opt.ReleaseArchitecture, "release-architecture", opt.ReleaseArchitecture, "The architecture of the releases to be created (defaults to 'amd64' if not specified).")

	flagset.StringVar(&opt.AuthenticationMessage, "authentication-message", opt.AuthenticationMessage, "HTML formatted string to display a registry authentication message")

	flagset.StringVar(&opt.Registry, "registry", opt.Registry, "Specify the registry, that the artifact server will use, to retrieve release images when located on remote clusters")

	flagset.StringVar(&opt.ARTSuffix, "art-suffix", "", "Suffix for ART imagstreams (eg. `-art-latest`)")

	// This option can be used to group, any number of, similar build cluster names into logical groups that will be used to
	// randomly distribute prowjobs onto.  When the release-controller reads in the prowjob definition, it will check if
	// the defined "cluster:" value exists in one of the cluster groups and then "Get()" the next, random, cluster name
	// from the group and assign the job to that cluster. If the cluster: is not a member of any group, then the
	// release-controller will not make any modifications and the jobs will run on the cluster as it is defined in the
	// job itself. The groupings are intended to be used to pool build clusters of similar configurations (i.e. cloud
	// provider, specific hardware, configurations, etc).  This way, jobs that are intended to be run on the specific
	// configurations can be distributed properly on the environment that they require.
	flagset.StringArrayVar(&opt.ClusterGroups, "cluster-group", opt.ClusterGroups, "A comma seperated list of build cluster names to evenly distribute jobs to.  May be specified multiple times to account for different configurations of build clusters.")

	flagset.BoolVar(&opt.PruneGraph, "prune-graph", opt.PruneGraph, "Reads the upgrade graph, prunes edges, and prints the result")
	flagset.StringVar(&opt.PrintPrunedGraph, "print-pruned-graph", opt.PrintPrunedGraph, "Print the result of pruning the graph.  Valid options are: <|secret|debug>. The default, 'secret', is the base64 encoded secret payload. The 'debug' option will pretty print the json payload")
	flagset.BoolVar(&opt.ConfirmPruneGraph, "confirm-prune-graph", opt.ConfirmPruneGraph, "Persist the pruned graph")

	flagset.BoolVar(&opt.ProcessLegacyResults, "process-legacy-results", opt.ProcessLegacyResults, "enable the migration of imagestream based results to ReleasePayloads")

	goFlagSet := flag.NewFlagSet("prowflags", flag.ContinueOnError)
	opt.github.AddFlags(goFlagSet)
	opt.bugzilla.AddFlags(goFlagSet)
	opt.jira.AddFlags(goFlagSet)
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
	// report liveness on default prow health port 8081
	health := pjutil.NewHealthOnPort(flagutil.DefaultHealthPort)
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
	if len(o.PrintPrunedGraph) > 0 && o.PrintPrunedGraph != releasecontroller.PruneGraphPrintSecret && o.PrintPrunedGraph != releasecontroller.PruneGraphPrintDebug {
		return fmt.Errorf("--print-prune-graph must be \"%s\" or \"%s\"", releasecontroller.PruneGraphPrintSecret, releasecontroller.PruneGraphPrintDebug)
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

	imageClient, err := imageclientset.NewForConfig(releasesConfig)
	if err != nil {
		return fmt.Errorf("unable to create image client: %v", err)
	}

	prowClient, err := o.prowJobClient(inClusterCfg)
	if err != nil {
		return fmt.Errorf("failed to create prowjob client: %v", err)
	}

	stopCh := wait.NeverStop
	var hasSynced []cache.InformerSynced

	batchFactory := informers.NewSharedInformerFactoryWithOptions(releasesClient, 10*time.Minute, informers.WithNamespace(o.JobNamespace))
	jobs := batchFactory.Batch().V1().Jobs()
	hasSynced = append(hasSynced, jobs.Informer().HasSynced)

	configAgent := &prowconfig.Agent{}
	if o.prowconfig.ConfigPath != "" {
		var err error
		configAgent, err = o.prowconfig.ConfigAgent()
		if err != nil {
			return err
		}
	}

	var jiraClient jiraBaseClient.Client
	var bzClient bugzillaBaseClient.Client
	var tokens []string
	// Append the path of github and bugzilla secrets.
	if o.github.TokenPath != "" {
		tokens = append(tokens, o.github.TokenPath)
	}
	if o.github.AppPrivateKeyPath != "" {
		tokens = append(tokens, o.github.AppPrivateKeyPath)
	}
	if o.bugzilla.ApiKeyPath != "" {
		tokens = append(tokens, o.bugzilla.ApiKeyPath)
	}
	if err := secret.Add(tokens...); err != nil {
		return fmt.Errorf("Error starting secrets agent: %w", err)
	}

	ghClient, err := o.github.GitHubClient(false)
	if err != nil {
		return fmt.Errorf("Failed to create github client: %v", err)
	}
	ghClient.Throttle(o.githubThrottle, 0)

	if o.VerifyBugzilla {
		bzClient, err = o.bugzilla.BugzillaClient()
		if err != nil {
			return fmt.Errorf("Failed to create bugzilla client: %v", err)
		}
	}
	if o.VerifyJira {
		jiraClient, err = o.jira.Client()
		if err != nil {
			return fmt.Errorf("Failed to create bugzilla client: %v", err)
		}
	}

	imageCache := releasecontroller.NewLatestImageCache(tagParts[0], tagParts[1])
	execReleaseInfo := releasecontroller.NewExecReleaseInfo(toolsClient, toolsConfig, o.JobNamespace, releaseNamespace, imageCache.Get, jiraClient)
	releaseInfo := releasecontroller.NewCachingReleaseInfo(execReleaseInfo, 64*1024*1024, architecture)

	execReleaseFiles := releasecontroller.NewExecReleaseFiles(toolsClient, toolsConfig, o.JobNamespace, releaseNamespace, releaseNamespace, o.Registry, imageCache.Get)

	graph := releasecontroller.NewUpgradeGraph(architecture)

	releasePayloadClient, err := releasepayloadclient.NewForConfig(inClusterCfg)
	if err != nil {
		klog.Fatal(err)
	}

	c := NewController(
		client.CoreV1(),
		imageClient.ImageV1(),
		releasesClient.BatchV1(),
		jobs,
		client.CoreV1(),
		configAgent,
		prowClient.Namespace(o.ProwNamespace),
		o.JobNamespace,
		releaseInfo,
		graph,
		o.softDeleteReleaseTags,
		o.AuthenticationMessage,
		o.ClusterGroups,
		architecture,
		o.ARTSuffix,
		releasePayloadClient.ReleaseV1alpha1(),
	)

	if o.VerifyBugzilla {
		pluginAgent, err := o.PluginConfig.PluginAgent()
		if err != nil {
			return fmt.Errorf("Failed to create plugin agent: %v", err)
		}
		c.bugzillaVerifier = bugzilla.NewVerifier(bzClient, ghClient, pluginAgent.Config())
		initializeMetrics(bugzillaErrorMetrics)
		c.bugzillaErrorMetrics = bugzillaErrorMetrics
	}
	if o.VerifyJira {
		pluginAgent, err := o.PluginConfig.PluginAgent()
		if err != nil {
			return fmt.Errorf("Failed to create plugin agent: %v", err)
		}
		c.jiraVerifier = jira.NewVerifier(jiraClient, ghClient, pluginAgent.Config())
		initializeJiraMetrics(jiraErrorMetrics)
		c.jiraErrorMetrics = jiraErrorMetrics
	}

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

	if len(o.CLIImageForAudit) > 0 {
		c.cliImageForAudit = o.CLIImageForAudit
	}

	if len(o.SigningKeyring) > 0 {
		signer, err := signer.NewFromKeyring(o.SigningKeyring)
		if err != nil {
			return err
		}
		c.signer = signer
	}

	if len(o.ListenAddr) > 0 {
		http.DefaultServeMux.Handle("/metrics", promhttp.Handler())
		go func() {
			klog.Infof("Listening on %s for metrics", o.ListenAddr)
			if err := http.ListenAndServe(o.ListenAddr, nil); err != nil {
				klog.Exitf("Server exited: %v", err)
			}
		}()
	}

	batchFactory.Start(stopCh)

	// register the releasepayload namespaces
	for _, ns := range o.ReleaseNamespaces {
		releasePayloadInformerFactory := releasepayloadinformers.NewSharedInformerFactoryWithOptions(releasePayloadClient, 24*time.Hour, releasepayloadinformers.WithNamespace(ns))
		releasePayloadInformer := releasePayloadInformerFactory.Release().V1alpha1().ReleasePayloads()
		c.AddReleasePayloadNamespace(ns, releasePayloadInformer)
		hasSynced = append(hasSynced, releasePayloadInformer.Informer().HasSynced)
		releasePayloadInformerFactory.Start(stopCh)
	}

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
		prowInformers := releasecontroller.NewDynamicSharedIndexInformer(prowClient, o.ProwNamespace, 10*time.Minute, labels.SelectorFromSet(labels.Set{releasecontroller.ReleaseLabelVerify: "true"}))
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

		if !o.DryRun {
			go c.syncPeriodicJobs(prowInformers, stopCh)
		}
	}

	klog.Infof("Waiting for caches to sync")
	cache.WaitForCacheSync(stopCh, hasSynced...)

	// always report as ready is this part of the code has been reached; may make sense to add a better ready check in the future
	health.ServeReady(func() bool { return true })

	switch {
	case o.PruneGraph:
		return c.PruneGraph(releasesClient.CoreV1().Secrets(releaseNamespace), o.ReleaseNamespaces[0], "release-upgrade-graph", o.PrintPrunedGraph, o.ConfirmPruneGraph)
	case o.DryRun:
		klog.Infof("Dry run mode (no changes will be made)")

		// read the graph
		go releasecontroller.SyncGraphToSecret(graph, false, releasesClient.CoreV1().Secrets(releaseNamespace), releaseNamespace, "release-upgrade-graph", stopCh)

		<-stopCh
		return nil
	case len(o.AuditStorage) > 0:
		klog.Infof("Auditing releases to %s", o.AuditStorage)

		// read the graph
		go releasecontroller.SyncGraphToSecret(graph, false, releasesClient.CoreV1().Secrets(releaseNamespace), releaseNamespace, "release-upgrade-graph", stopCh)

		c.RunAudit(4, stopCh)
		return nil
	case len(o.LimitSources) > 0:
		klog.Infof("Managing only %s, no garbage collection", o.LimitSources)

		// read the graph
		go releasecontroller.SyncGraphToSecret(graph, false, releasesClient.CoreV1().Secrets(releaseNamespace), releaseNamespace, "release-upgrade-graph", stopCh)

		c.RunSync(3, stopCh)
		return nil
	default:
		klog.Infof("Managing releases")

		// keep the graph in a more persistent form
		go releasecontroller.SyncGraphToSecret(graph, true, releasesClient.CoreV1().Secrets(releaseNamespace), releaseNamespace, "release-upgrade-graph", stopCh)
		// maintain the release pods
		go refreshReleaseToolsEvery(2*time.Hour, execReleaseInfo, execReleaseFiles, stopCh)

		if o.ProcessLegacyResults {
			go c.processLegacyResults(6*time.Hour, stopCh)
		}

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

func refreshReleaseToolsEvery(interval time.Duration, execReleaseInfo *releasecontroller.ExecReleaseInfo, execReleaseFiles *releasecontroller.ExecReleaseFiles, stopCh <-chan struct{}) {
	wait.Until(func() {
		err := wait.ExponentialBackoff(wait.Backoff{
			Steps:    3,
			Duration: 1 * time.Second,
			Factor:   2,
		}, func() (bool, error) {
			success := true
			if err := execReleaseInfo.RefreshPod(); err != nil {
				klog.Errorf("Unable to refresh git cache, waiting to retry: %v", err)
				success = false
			}
			if err := execReleaseFiles.RefreshPod(); err != nil {
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
	for _, candidate := range []string{o.ProwJobKubeconfig, o.NonProwJobKubeconfig, o.ReleasesKubeconfig, o.ToolsKubeconfig} {
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
