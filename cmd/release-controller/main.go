package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"

	_ "net/http/pprof" // until openshift/library-go#309 merges

	"github.com/golang/glog"
	"github.com/spf13/cobra"

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
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	imageclientset "github.com/openshift/client-go/image/clientset/versioned"
	imageinformers "github.com/openshift/client-go/image/informers/externalversions"
	imagelisters "github.com/openshift/client-go/image/listers/image/v1"
	"github.com/openshift/library-go/pkg/serviceability"
	prowapiv1 "github.com/openshift/release-controller/pkg/prow/apiv1"
)

type options struct {
	ReleaseNamespaces []string
	JobNamespace      string
	ProwNamespace     string

	ProwConfigPath string
	JobConfigPath  string

	DryRun       bool
	LimitSources []string
	ListenAddr   string
}

func main() {
	serviceability.StartProfiler()
	defer serviceability.Profile(os.Getenv("OPENSHIFT_PROFILE")).Stop()

	original := flag.CommandLine
	original.Set("alsologtostderr", "true")
	original.Set("v", "2")

	opt := &options{
		ListenAddr: ":8080",
	}
	cmd := &cobra.Command{
		Run: func(cmd *cobra.Command, arguments []string) {
			if err := opt.Run(); err != nil {
				glog.Exitf("error: %v", err)
			}
		},
	}
	flag := cmd.Flags()
	flag.BoolVar(&opt.DryRun, "dry-run", opt.DryRun, "Perform no actions on the release streams")
	flag.StringSliceVar(&opt.LimitSources, "only-source", opt.LimitSources, "The names of the image streams to operate on. Intended for testing.")

	var ignored string
	flag.StringVar(&ignored, "to", ignored, "REMOVED: The image stream in the release namespace to push releases to.")
	flag.StringVar(&opt.JobNamespace, "job-namespace", opt.JobNamespace, "The namespace to execute jobs and hold temporary objects.")
	flag.StringSliceVar(&opt.ReleaseNamespaces, "release-namespace", opt.ReleaseNamespaces, "The namespace where the source image streams are located and where releases will be published to.")
	flag.StringVar(&opt.ProwNamespace, "prow-namespace", opt.ProwNamespace, "The namespace where the Prow jobs will be created (defaults to --job-namespace).")
	flag.StringVar(&opt.ProwConfigPath, "prow-config", opt.ProwConfigPath, "A config file containing the prow configuration.")
	flag.StringVar(&opt.JobConfigPath, "job-config", opt.JobConfigPath, "A config file containing the jobs to run against releases.")

	flag.StringVar(&opt.ListenAddr, "listen", opt.ListenAddr, "The address to serve release information on")

	flag.AddGoFlag(original.Lookup("v"))

	if err := cmd.Execute(); err != nil {
		glog.Exitf("error: %v", err)
	}
}

func (o *options) Run() error {
	config, _, _, err := loadClusterConfig()
	if err != nil {
		return err
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
		glog.Infof("Releases will be published to namespace %s, jobs will be created in namespace %s", releaseNamespace, o.JobNamespace)
	} else {
		glog.Infof("Release will be published to namespace %s and jobs will be in the same namespace", releaseNamespace)
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
		releaseInfo,
		graph,
	)

	if len(o.ListenAddr) > 0 {
		http.DefaultServeMux.Handle("/metrics", promhttp.Handler())
		http.DefaultServeMux.HandleFunc("/graph", c.graphHandler)
		http.DefaultServeMux.Handle("/", c.userInterfaceHandler())
		go func() {
			glog.Infof("Listening on %s for UI and metrics", o.ListenAddr)
			if err := http.ListenAndServe(o.ListenAddr, nil); err != nil {
				glog.Exitf("Server exited: %v", err)
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

	glog.Infof("Waiting for caches to sync")
	cache.WaitForCacheSync(stopCh, hasSynced...)

	// keep the graph in a more persistent form
	go syncGraphToSecret(graph, o.DryRun, client.CoreV1().Secrets(releaseNamespace), releaseNamespace, "release-upgrade-graph", stopCh)

	go wait.Until(func() {
		err := wait.ExponentialBackoff(wait.Backoff{
			Steps:    3,
			Duration: 1 * time.Second,
			Factor:   2,
		}, func() (bool, error) {
			if err := execReleaseInfo.refreshPod(); err != nil {
				glog.Errorf("Unable to refresh git cache, waiting to retry: %v", err)
				return false, nil
			}
			return true, nil
		})
		if err != nil {
			glog.Errorf("Unable to refresh git cache, waiting until next sync time")
		}
	}, 2*time.Hour, stopCh)

	if o.DryRun {
		glog.Infof("Dry run mode (no changes will be made)")
		<-stopCh
	} else {
		glog.Infof("Managing releases")
		c.Run(3, stopCh)
	}
	return nil
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
				glog.V(4).Infof("Using %s for the %s image", spec, c.tag)
				return spec, nil
			}
		}
	}
	return "", fmt.Errorf("could not find a release image stream with :tests")
}
