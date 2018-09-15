package main

import (
	"flag"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/golang/glog"
	"github.com/spf13/cobra"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	informers "k8s.io/client-go/informers"
	clientset "k8s.io/client-go/kubernetes"

	imageclientset "github.com/openshift/client-go/image/clientset/versioned"
	imageinformers "github.com/openshift/client-go/image/informers/externalversions"
)

type options struct {
	ReleaseNamespace string
	JobNamespace     string
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
	flag.StringVar(&opt.ReleaseNamespace, "release-namespace", opt.ReleaseNamespace, "The namespace to look for release image streams in, will default to the current namespace.")
	flag.AddGoFlag(original.Lookup("v"))

	if err := cmd.Execute(); err != nil {
		glog.Exitf("error: %v", err)
	}
}

func (o *options) Run() error {
	config, ns, _, err := loadClusterConfig()
	if err != nil {
		return fmt.Errorf("unable to load client configuration: %v", err)
	}
	if len(o.ReleaseNamespace) > 0 {
		ns = o.ReleaseNamespace
	}
	if len(ns) == 0 {
		return fmt.Errorf("no namespace set, use --release-namespace")
	}
	if len(o.JobNamespace) == 0 {
		return fmt.Errorf("no job namespace set, use --job-namespace")
	}

	client, err := clientset.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("unable to create client: %v", err)
	}
	namespace, err := client.Core().Namespaces().Get(ns, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("unable to find release namespace: %v", err)
	}
	glog.Infof("Using namespace %s for release namespace", namespace.Name)
	imageClient, err := imageclientset.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("unable to create client: %v", err)
	}

	stopCh := wait.NeverStop
	factory := imageinformers.NewSharedInformerFactoryWithOptions(imageClient, 10*time.Minute, imageinformers.WithNamespace(ns))
	imagestreams := factory.Image().V1().ImageStreams()
	batchFactory := informers.NewSharedInformerFactoryWithOptions(client, 10*time.Minute, informers.WithNamespace(o.JobNamespace))
	jobs := batchFactory.Batch().V1().Jobs()

	c := NewController(client.Core(), imageClient.Image(), imagestreams, client.Batch(), jobs, o.JobNamespace)

	factory.Start(stopCh)
	batchFactory.Start(stopCh)
	factory.WaitForCacheSync(stopCh)
	batchFactory.WaitForCacheSync(stopCh)
	glog.Infof("Caches synced")

	c.Run(3, stopCh)
	return nil
	// load flag
	// parse config
	// check inputs
	// start informers
	//   for each Release
	//     watch for changes to image stream
	//		 level-driven
	//			 if new images
	//         create an image stream tag in openshift/release:<TAG>
	//				 create a release payload and push to that tag
	//					 have a cooldown on changes?
	//			 for any release payload in the image stream missing jobs
	//				 create any necessary jobs
	//			 for any release payload with jobs
	//				 wait till those jobs are complete
	//					 mirror the payload to official locations
	//				 if any jobs are missing - ERROR HANDLING?
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
