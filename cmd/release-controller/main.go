package main

import (
	"flag"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"k8s.io/client-go/dynamic"

	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/golang/glog"
	"github.com/spf13/cobra"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"

	informers "k8s.io/client-go/informers"
	clientset "k8s.io/client-go/kubernetes"

	imageclientset "github.com/openshift/client-go/image/clientset/versioned"
	imageinformers "github.com/openshift/client-go/image/informers/externalversions"
	prowapiv1 "github.com/openshift/release-controller/pkg/prow/apiv1"
)

type options struct {
	ReleaseNamespace string
	JobNamespace     string
	ProwNamespace    string

	ProwConfigPath string
	JobConfigPath  string
}

func main() {
	original := flag.CommandLine
	original.Set("alsologtostderr", "true")
	original.Set("v", "2")

	opt := &options{}
	cmd := &cobra.Command{
		Run: func(cmd *cobra.Command, arguments []string) {
			if err := opt.Run(); err != nil {
				glog.Exitf("error: %v", err)
			}
		},
	}
	flag := cmd.Flags()
	flag.StringVar(&opt.JobNamespace, "job-namespace", opt.JobNamespace, "The namespace to execute jobs and hold temporary objects.")
	flag.StringVar(&opt.ReleaseNamespace, "release-namespace", opt.ReleaseNamespace, "The namespace where the releases will be published to.")
	flag.StringVar(&opt.ProwNamespace, "prow-namespace", opt.ProwNamespace, "The namespace where the Prow jobs will be created (defaults to --job-namespace).")
	flag.StringVar(&opt.ProwConfigPath, "prow-config", opt.ProwConfigPath, "A config file containing the prow configuration.")
	flag.StringVar(&opt.JobConfigPath, "job-config", opt.JobConfigPath, "A config file containing the jobs to run against releases.")
	flag.AddGoFlag(original.Lookup("v"))

	if err := cmd.Execute(); err != nil {
		glog.Exitf("error: %v", err)
	}
}

func (o *options) Run() error {
	config, _, _, err := loadClusterConfig()
	if err != nil {
		return fmt.Errorf("unable to load client configuration: %v", err)
	}
	if len(o.ReleaseNamespace) == 0 {
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
	if _, err := client.Core().Namespaces().Get(o.ReleaseNamespace, metav1.GetOptions{}); err != nil {
		return fmt.Errorf("unable to find release namespace: %v", err)
	}
	if o.JobNamespace != o.ReleaseNamespace {
		if _, err := client.Core().Namespaces().Get(o.JobNamespace, metav1.GetOptions{}); err != nil {
			return fmt.Errorf("unable to find job namespace: %v", err)
		}
		glog.Infof("Releases will be published to image stream %s/%s, jobs and mirrors will be created in namespace %s", o.ReleaseNamespace, "release", o.JobNamespace)
	} else {
		glog.Infof("Release will be published to image stream %s/%s and jobs will be in the same namespace", o.ReleaseNamespace, "release")
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
	batchFactory := informers.NewSharedInformerFactoryWithOptions(client, 10*time.Minute, informers.WithNamespace(o.JobNamespace))
	jobs := batchFactory.Batch().V1().Jobs()

	configAgent := &prowapiv1.Agent{}
	if len(o.ProwConfigPath) > 0 {
		if err := configAgent.Start(o.ProwConfigPath, o.JobConfigPath); err != nil {
			return err
		}
	}

	c := NewController(
		client.Core(),
		imageClient.Image(),
		client.Batch(),
		jobs,
		configAgent,
		prowClient.Namespace(o.ProwNamespace),
		o.ReleaseNamespace,
		o.JobNamespace,
	)

	batchFactory.Start(stopCh)
	batchFactory.WaitForCacheSync(stopCh)

	// register image streams
	factory := imageinformers.NewSharedInformerFactoryWithOptions(imageClient, 10*time.Minute, imageinformers.WithNamespace(o.ReleaseNamespace))
	c.AddNamespacedImageStreamInformer(o.ReleaseNamespace, factory.Image().V1().ImageStreams())
	factory.Start(stopCh)
	factory.WaitForCacheSync(stopCh)
	if o.JobNamespace != o.ReleaseNamespace {
		factory = imageinformers.NewSharedInformerFactoryWithOptions(imageClient, 10*time.Minute, imageinformers.WithNamespace(o.JobNamespace))
		c.AddNamespacedImageStreamInformer(c.jobNamespace, factory.Image().V1().ImageStreams())
		factory.Start(stopCh)
		factory.WaitForCacheSync(stopCh)
	}

	if len(o.ProwConfigPath) > 0 {
		prowInformers := newDynamicSharedIndexInformer(prowClient, o.ProwNamespace, 10*time.Minute, labels.SelectorFromSet(labels.Set{"release.openshift.io/verify": "true"}))
		c.AddProwInformer(o.ProwNamespace, prowInformers)
		go prowInformers.Run(stopCh)
		cache.WaitForCacheSync(stopCh, prowInformers.HasSynced)
	}

	glog.Infof("Caches synced")

	c.Run(3, stopCh)
	return nil
}

// loadClusterConfig loads connection configuration
// for the cluster we're deploying to. We prefer to
// use in-cluster configuration if possible, but will
// fall back to using default rules otherwise.
func loadClusterConfig() (*rest.Config, string, bool, error) {
	credentials, err := clientcmd.NewDefaultClientConfigLoadingRules().Load()
	if err != nil {
		return nil, "", false, fmt.Errorf("could not load credentials from config: %v", err)
	}
	cfg := clientcmd.NewDefaultClientConfig(*credentials, &clientcmd.ConfigOverrides{})
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
