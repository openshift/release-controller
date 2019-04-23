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

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"
	"k8s.io/klog"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	informers "k8s.io/client-go/informers"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/version"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"

	imageclientset "github.com/openshift/client-go/image/clientset/versioned"
	imageinformers "github.com/openshift/client-go/image/informers/externalversions"
	imagelisters "github.com/openshift/client-go/image/listers/image/v1"
	"github.com/openshift/library-go/pkg/serviceability"
	prowapiv1 "github.com/openshift/release-controller/pkg/prow/apiv1"

	"github.com/openshift/release-controller/pkg/signer"
)

type options struct {
	ReleaseNamespaces []string
	JobNamespace      string
	ProwNamespace     string

	ProwConfigPath string
	JobConfigPath  string

	ListenAddr    string
	ArtifactsHost string

	AuditStorage           string
	AuditGCSServiceAccount string
	SigningKeyring         string
	CLIImageForAudit       string

	DryRun       bool
	LimitSources []string
}

func main() {
	serviceability.StartProfiler()
	defer serviceability.Profile(os.Getenv("OPENSHIFT_PROFILE")).Stop()

	original := flag.CommandLine
	klog.InitFlags(original)
	original.Set("alsologtostderr", "true")
	original.Set("v", "2")

	opt := &options{
		ListenAddr: ":8080",
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
	flag := cmd.Flags()
	flag.BoolVar(&opt.DryRun, "dry-run", opt.DryRun, "Perform no actions on the release streams")
	flag.StringVar(&opt.AuditStorage, "audit", opt.AuditStorage, "A storage location to report audit logs to, if specified. The location may be a file://path or gs:// GCS bucket and path.")
	flag.StringVar(&opt.AuditGCSServiceAccount, "audit-gcs-service-account", opt.AuditGCSServiceAccount, "An optional path to a service account file that should be used for uploading audit information to GCS.")
	flag.StringSliceVar(&opt.LimitSources, "only-source", opt.LimitSources, "The names of the image streams to operate on. Intended for testing.")
	flag.StringVar(&opt.SigningKeyring, "sign", opt.SigningKeyring, "The OpenPGP keyring to sign releases with. Only releases that can be verified will be signed.")
	flag.StringVar(&opt.CLIImageForAudit, "audit-cli-image", opt.CLIImageForAudit, "The command line image pullspec to use for audit and signing. This should be set to a digest under the signers control to prevent attackers from forging verification. If you pass 'local' the oc binary on the path will be used instead of running a job.")

	var ignored string
	flag.StringVar(&ignored, "to", ignored, "REMOVED: The image stream in the release namespace to push releases to.")
	flag.StringVar(&opt.JobNamespace, "job-namespace", opt.JobNamespace, "The namespace to execute jobs and hold temporary objects.")
	flag.StringSliceVar(&opt.ReleaseNamespaces, "release-namespace", opt.ReleaseNamespaces, "The namespace where the source image streams are located and where releases will be published to.")
	flag.StringVar(&opt.ProwNamespace, "prow-namespace", opt.ProwNamespace, "The namespace where the Prow jobs will be created (defaults to --job-namespace).")
	flag.StringVar(&opt.ProwConfigPath, "prow-config", opt.ProwConfigPath, "A config file containing the prow configuration.")
	flag.StringVar(&opt.JobConfigPath, "job-config", opt.JobConfigPath, "A config file containing the jobs to run against releases.")

	flag.StringVar(&opt.ArtifactsHost, "artifacts", opt.ArtifactsHost, "The public hostname of the artifacts server.")

	flag.StringVar(&opt.ListenAddr, "listen", opt.ListenAddr, "The address to serve release information on")

	flag.AddGoFlag(original.Lookup("v"))

	if err := cmd.Execute(); err != nil {
		klog.Exitf("error: %v", err)
	}
}

func (o *options) Run() error {
	if len(o.ReleaseNamespaces) == 0 {
		return fmt.Errorf("no namespace set, use --release-namespace")
	}
	if len(o.JobNamespace) == 0 {
		return fmt.Errorf("no job namespace set, use --job-namespace")
	}
	if len(o.ProwNamespace) == 0 {
		o.ProwNamespace = o.JobNamespace
	}

	config, _, _, err := loadClusterConfig()
	if err != nil {
		return err
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

	client, err := clientset.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("unable to create client: %v", err)
	}
	releaseNamespace := o.ReleaseNamespaces[0]
	if _, err := client.Core().Namespaces().Get(releaseNamespace, metav1.GetOptions{}); err != nil {
		return fmt.Errorf("unable to find release namespace: %v", err)
	}
	if o.JobNamespace != releaseNamespace {
		if _, err := client.Core().Namespaces().Get(o.JobNamespace, metav1.GetOptions{}); err != nil {
			return fmt.Errorf("unable to find job namespace: %v", err)
		}
		klog.Infof("Releases will be published to namespace %s, jobs will be created in namespace %s", releaseNamespace, o.JobNamespace)
	} else {
		klog.Infof("Release will be published to namespace %s and jobs will be in the same namespace", releaseNamespace)
	}

	imageClient, err := imageclientset.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("unable to create client: %v", err)
	}

	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("unable to create prow client: %v", err)
	}
	prowClient := dynamicClient.Resource(schema.GroupVersionResource{Group: "prow.k8s.io", Version: "v1", Resource: "prowjobs"})

	stopCh := wait.NeverStop
	var hasSynced []cache.InformerSynced

	batchFactory := informers.NewSharedInformerFactoryWithOptions(client, 10*time.Minute, informers.WithNamespace(o.JobNamespace))
	jobs := batchFactory.Batch().V1().Jobs()
	hasSynced = append(hasSynced, jobs.Informer().HasSynced)

	configAgent := &prowapiv1.Agent{}
	if len(o.ProwConfigPath) > 0 {
		if err := configAgent.Start(o.ProwConfigPath, o.JobConfigPath); err != nil {
			return err
		}
	}

	imageCache := newLatestImageCache("tests")
	execReleaseInfo := NewExecReleaseInfo(client, config, o.JobNamespace, fmt.Sprintf("%s", releaseNamespace), imageCache.Get)
	releaseInfo := NewCachingReleaseInfo(execReleaseInfo, 64*1024*1024)

	execReleaseFiles := NewExecReleaseFiles(client, config, o.JobNamespace, fmt.Sprintf("%s", releaseNamespace), imageCache.Get)

	graph := NewUpgradeGraph()

	c := NewController(
		client.Core(),
		imageClient.Image(),
		client.Batch(),
		jobs,
		client.Core(),
		configAgent,
		prowClient.Namespace(o.ProwNamespace),
		releaseNamespace,
		o.JobNamespace,
		o.ArtifactsHost,
		releaseInfo,
		graph,
	)

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
				return err
			}
			if err := store.Refresh(context.Background()); err != nil {
				return err
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
		http.DefaultServeMux.HandleFunc("/graph", c.graphHandler)
		http.DefaultServeMux.Handle("/", c.userInterfaceHandler())
		go func() {
			klog.Infof("Listening on %s for UI and metrics", o.ListenAddr)
			if err := http.ListenAndServe(o.ListenAddr, nil); err != nil {
				klog.Exitf("Server exited: %v", err)
			}
		}()
	}

	batchFactory.Start(stopCh)

	// register image streams
	for _, ns := range o.ReleaseNamespaces {
		factory := imageinformers.NewSharedInformerFactoryWithOptions(imageClient, 10*time.Minute, imageinformers.WithNamespace(ns))
		streams := factory.Image().V1().ImageStreams()
		c.AddNamespacedImageStreamInformer(ns, streams)
		hasSynced = append(hasSynced, streams.Informer().HasSynced)
		factory.Start(stopCh)
	}
	imageCache.SetLister(c.imageStreamLister.ImageStreams(releaseNamespace))

	if len(o.ProwConfigPath) > 0 {
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
					from, ok := job.GetAnnotations()[releaseAnnotationFromTag]
					if !ok {
						continue
					}
					to, ok := job.GetAnnotations()[releaseAnnotationToTag]
					if !ok {
						continue
					}
					status, ok := prowJobVerificationStatus(job)
					if !ok {
						continue
					}
					graph.Add(from, to, UpgradeResult(*status))
				}
			}, 2*time.Minute, stopCh)
		}()
	}

	klog.Infof("Waiting for caches to sync")
	cache.WaitForCacheSync(stopCh, hasSynced...)

	switch {
	case o.DryRun:
		klog.Infof("Dry run mode (no changes will be made)")
		<-stopCh
		return nil
	case len(o.AuditStorage) > 0:
		klog.Infof("Auditing releases to %s", o.AuditStorage)
		c.RunAudit(2, stopCh)
		return nil
	case len(o.LimitSources) > 0:
		klog.Infof("Managing only %s, no garbage collection", o.LimitSources)
		c.RunSync(3, stopCh)
		return nil
	default:
		klog.Infof("Managing releases")

		// keep the graph in a more persistent form
		go syncGraphToSecret(graph, o.DryRun, client.CoreV1().Secrets(releaseNamespace), releaseNamespace, "release-upgrade-graph", stopCh)
		// maintain the release pods
		go refreshReleaseToolsEvery(2*time.Hour, execReleaseInfo, execReleaseFiles, stopCh)

		c.RunSync(3, stopCh)
		return nil
	}
}

// loadClusterConfig loads connection configuration
// for the cluster we're deploying to. We prefer to
// use in-cluster configuration if possible, but will
// fall back to using default rules otherwise.
func loadClusterConfig() (*rest.Config, string, bool, error) {
	cfg := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(clientcmd.NewDefaultClientConfigLoadingRules(), &clientcmd.ConfigOverrides{})
	clusterConfig, err := cfg.ClientConfig()
	if err != nil {
		return nil, "", false, fmt.Errorf("could not load client configuration: %v", err)
	}
	ns, isSet, err := cfg.Namespace()
	if err != nil {
		return nil, "", false, fmt.Errorf("could not load client namespace: %v", err)
	}
	return clusterConfig, ns, isSet, nil
}

func newDynamicSharedIndexInformer(client dynamic.NamespaceableResourceInterface, namespace string, resyncPeriod time.Duration, selector labels.Selector) cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				options.LabelSelector = selector.String()
				return client.Namespace(namespace).List(options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				options.LabelSelector = selector.String()
				return client.Namespace(namespace).Watch(options)
			},
		},
		&unstructured.Unstructured{},
		resyncPeriod,
		cache.Indexers{},
	)
}

type latestImageCache struct {
	tag      string
	interval time.Duration

	lock        sync.Mutex
	lister      imagelisters.ImageStreamNamespaceLister
	last        string
	lastChecked time.Time
}

func newLatestImageCache(tag string) *latestImageCache {
	return &latestImageCache{
		tag:      tag,
		interval: 10 * time.Minute,
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
	items, _ := c.lister.List(labels.Everything())
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	for _, item := range items {
		if _, ok := item.Annotations[releaseAnnotationConfig]; ok {
			if spec := findImagePullSpec(item, c.tag); len(spec) > 0 {
				c.last = spec
				c.lastChecked = time.Now()
				klog.V(4).Infof("Using %s for the %s image", spec, c.tag)
				return spec, nil
			}
		}
	}
	return "", fmt.Errorf("could not find a release image stream with :tests")
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
