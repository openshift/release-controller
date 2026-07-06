package release_payload_controller

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	imageclientset "github.com/openshift/client-go/image/clientset/versioned"
	imageinformers "github.com/openshift/client-go/image/informers/externalversions"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/openshift/release-controller/pkg/bigquery"
	releasepayloadclient "github.com/openshift/release-controller/pkg/client/clientset/versioned"
	releasepayloadinformers "github.com/openshift/release-controller/pkg/client/informers/externalversions"
	"github.com/openshift/release-controller/pkg/releasequalifiers"
	"github.com/openshift/release-controller/pkg/version"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"
	prowjobclientset "sigs.k8s.io/prow/pkg/client/clientset/versioned"
	prowjobinformers "sigs.k8s.io/prow/pkg/client/informers/externalversions"
	"sigs.k8s.io/prow/pkg/flagutil"
	jiraclient "sigs.k8s.io/prow/pkg/jira"
)

type Options struct {
	controllerContext           *controllercmd.ControllerContext
	ReleaseQualifiersConfigPath string

	// BigQuery Options
	GoogleProjectID                    string
	GoogleServiceAccountCredentialFile string
	BigQueryCacheTTL                   time.Duration

	// Jira Options
	jira flagutil.JiraOptions
}

func NewReleasePayloadControllerCommand(name string) *cobra.Command {
	o := &Options{
		BigQueryCacheTTL: 10 * time.Minute,
	}

	ccc := controllercmd.NewControllerCommandConfig("release-payload-controller", version.Get(), func(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
		o.controllerContext = controllerContext

		err := o.Validate(ctx)
		if err != nil {
			return err
		}

		err = o.Run(ctx)
		if err != nil {
			return err
		}

		return nil
	}, clock.RealClock{})

	cmd := ccc.NewCommandWithContext(context.Background())
	cmd.Use = name
	cmd.Short = "Start the release payload controller"

	o.AddFlags(cmd.Flags())

	return cmd
}

func (o *Options) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.ReleaseQualifiersConfigPath, "release-qualifiers-config-path", "", "Path to release qualifiers config file (optional, supports live reloading)")
	fs.StringVar(&o.GoogleProjectID, "google-project-id", os.Getenv("GOOGLE_PROJECT_ID"), "Google project name.")
	fs.StringVar(&o.GoogleServiceAccountCredentialFile, "google-service-account-credential-file", os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"), "location of a credential file described by https://cloud.google.com/docs/authentication/production")
	fs.DurationVar(&o.BigQueryCacheTTL, "bigquery-cache-ttl", o.BigQueryCacheTTL, "TTL for cached BigQuery query results (0 to disable caching)")

	goFlagSet := flag.NewFlagSet("jira", flag.ContinueOnError)
	o.jira.AddFlags(goFlagSet)
	fs.AddGoFlagSet(goFlagSet)
}

func (o *Options) Validate(_ context.Context) error {
	if len(o.GoogleProjectID) == 0 {
		return errors.New("--google-project-id flag must be set")
	}
	if len(o.GoogleServiceAccountCredentialFile) == 0 {
		return errors.New("--google-service-account-credential-file flag must be set")
	}
	if err := o.jira.Validate(false); err != nil {
		return fmt.Errorf("invalid jira options: %w", err)
	}
	return nil
}

func (o *Options) Run(ctx context.Context) error {
	inClusterConfig := o.controllerContext.KubeConfig

	kubeClient, err := kubernetes.NewForConfig(inClusterConfig)
	if err != nil {
		return fmt.Errorf("can't build kubernetes client: %w", err)
	}

	// Batch Job Informers
	kubeFactory := informers.NewSharedInformerFactory(kubeClient, controllerDefaultResyncDuration)
	batchJobInformer := kubeFactory.Batch().V1().Jobs()

	// ReleasePayload Informers
	releasePayloadClient, err := releasepayloadclient.NewForConfig(inClusterConfig)
	if err != nil {
		klog.Fatalf("Error building releasePayload clientset: %s", err.Error())
	}

	releasePayloadInformerFactory := releasepayloadinformers.NewSharedInformerFactory(releasePayloadClient, controllerDefaultResyncDuration)
	releasePayloadInformer := releasePayloadInformerFactory.Release().V1alpha1().ReleasePayloads()

	// ProwJob Informers
	prowJobClient, err := prowjobclientset.NewForConfig(inClusterConfig)
	if err != nil {
		klog.Fatalf("Error building prowjob clientset: %s", err.Error())
	}

	prowJobInformerFactory := prowjobinformers.NewSharedInformerFactory(prowJobClient, controllerDefaultResyncDuration)
	prowJobInformer := prowJobInformerFactory.Prow().V1().ProwJobs()

	// ImageStream Informers
	imageStreamClient, err := imageclientset.NewForConfig(inClusterConfig)
	if err != nil {
		klog.Fatalf("Error building imagestream clientset: %s", err.Error())
	}

	imageStreamInformerFactory := imageinformers.NewSharedInformerFactory(imageStreamClient, controllerDefaultResyncDuration)
	imageStreamInformer := imageStreamInformerFactory.Image().V1().ImageStreams()

	// Initialize release qualifiers config loader
	var configAccessor releasequalifiers.ConfigAccessor
	if o.ReleaseQualifiersConfigPath != "" {
		configLoader, err := releasequalifiers.NewConfigLoader(o.ReleaseQualifiersConfigPath)
		if err != nil {
			klog.Warningf("Failed to initialize release qualifiers config loader: %v", err)
			// Continue with nil accessor - controllers will use empty config
		} else {
			configAccessor = configLoader
			// Start watching config file for changes
			if err := configLoader.StartWatching(ctx); err != nil {
				klog.Warningf("Failed to start watching release qualifiers config: %v", err)
			}
			klog.Infof("Release qualifiers config loaded from: %s", o.ReleaseQualifiersConfigPath)
		}
	}

	// BigQuery Client
	bqc, err := bigquery.NewBigQueryClient(o.GoogleProjectID, o.GoogleServiceAccountCredentialFile)
	if err != nil {
		klog.Fatalf("Unable to configure bigquery client: %v", err)
	}
	defer bqc.Close()

	var bqClient bigquery.ClientInterface = bqc
	if o.BigQueryCacheTTL > 0 {
		bqClient = bigquery.NewCachedClient(bqc, o.BigQueryCacheTTL)
		klog.Infof("BigQuery caching enabled with TTL: %s", o.BigQueryCacheTTL)
	}

	// Jira Client (optional)
	var jiraClient jiraclient.Client
	jiraClient, err = o.jira.Client()
	if err != nil {
		klog.Infof("Jira client not configured, escalations will be logged only: %v", err)
		jiraClient = nil
	}

	// Payload Verification Controller
	payloadVerificationController, err := NewPayloadVerificationController(releasePayloadInformer, releasePayloadClient.ReleaseV1alpha1(), o.controllerContext.EventRecorder)
	if err != nil {
		return err
	}

	// Release Creation Status Controller
	releaseCreationStatusController, err := NewReleaseCreationStatusController(releasePayloadInformer, releasePayloadClient.ReleaseV1alpha1(), batchJobInformer, o.controllerContext.EventRecorder)
	if err != nil {
		return err
	}

	// Release Creation Jobs Controller
	releaseCreationJobsController, err := NewReleaseCreationJobController(releasePayloadInformer, releasePayloadClient.ReleaseV1alpha1(), o.controllerContext.EventRecorder)
	if err != nil {
		return err
	}

	// Payload Creation Controller
	payloadCreationController, err := NewPayloadCreationController(releasePayloadInformer, releasePayloadClient.ReleaseV1alpha1(), o.controllerContext.EventRecorder)
	if err != nil {
		return err
	}

	// Payload Accepted Controller
	payloadAcceptedController, err := NewPayloadAcceptedController(releasePayloadInformer, releasePayloadClient.ReleaseV1alpha1(), o.controllerContext.EventRecorder)
	if err != nil {
		return err
	}

	// Payload Rejected Controller
	payloadRejectedController, err := NewPayloadRejectedController(releasePayloadInformer, releasePayloadClient.ReleaseV1alpha1(), o.controllerContext.EventRecorder)
	if err != nil {
		return err
	}

	// Aggregated State Controller
	aggregateStateController, err := NewJobStateController(releasePayloadInformer, releasePayloadClient.ReleaseV1alpha1(), o.controllerContext.EventRecorder)
	if err != nil {
		return err
	}

	// Job Qualifiers Summary Controller
	jobQualifiersSummaryController, err := NewJobQualifiersSummaryController(releasePayloadInformer, releasePayloadClient.ReleaseV1alpha1(), o.controllerContext.EventRecorder, configAccessor)
	if err != nil {
		return err
	}

	// ProwJob Controller
	pjController, err := NewProwJobStatusController(releasePayloadInformer, releasePayloadClient.ReleaseV1alpha1(), prowJobInformer, o.controllerContext.EventRecorder)
	if err != nil {
		return err
	}

	// ProwJob Controller
	legacyResultsController, err := NewLegacyJobStatusController(releasePayloadInformer, releasePayloadClient.ReleaseV1alpha1(), imageStreamInformer, o.controllerContext.EventRecorder)
	if err != nil {
		return err
	}

	// Payload Mirror Controller
	payloadMirrorController, err := NewPayloadMirrorController(releasePayloadInformer, releasePayloadClient.ReleaseV1alpha1(), o.controllerContext.EventRecorder)
	if err != nil {
		return err
	}

	// Release Mirror Jobs Controller
	releaseMirrorJobsController, err := NewReleaseMirrorJobController(releasePayloadInformer, releasePayloadClient.ReleaseV1alpha1(), o.controllerContext.EventRecorder)
	if err != nil {
		return err
	}

	// Release Mirror Jobs Status Controller
	releaseMirrorJobStatusController, err := NewReleaseMirrorJobStatusController(releasePayloadInformer, releasePayloadClient.ReleaseV1alpha1(), batchJobInformer, o.controllerContext.EventRecorder)
	if err != nil {
		return err
	}

	// Jira Escalations Controller
	jiraEscalationsController, err := NewJiraEscalationsController(releasePayloadInformer, releasePayloadClient.ReleaseV1alpha1(), o.controllerContext.EventRecorder, configAccessor, bqClient, jiraClient)
	if err != nil {
		return err
	}

	// Start the informers
	kubeFactory.Start(ctx.Done())
	releasePayloadInformerFactory.Start(ctx.Done())
	prowJobInformerFactory.Start(ctx.Done())
	imageStreamInformerFactory.Start(ctx.Done())

	// Run the Controllers
	go payloadVerificationController.RunWorkers(ctx, 10)
	go releaseCreationStatusController.RunWorkers(ctx, 10)
	go releaseCreationJobsController.RunWorkers(ctx, 10)
	go payloadCreationController.RunWorkers(ctx, 10)
	go payloadAcceptedController.RunWorkers(ctx, 10)
	go payloadRejectedController.RunWorkers(ctx, 10)
	go pjController.RunWorkers(ctx, 10)
	go aggregateStateController.RunWorkers(ctx, 10)
	go jobQualifiersSummaryController.RunWorkers(ctx, 10)
	go legacyResultsController.RunWorkers(ctx, 10)
	go payloadMirrorController.RunWorkers(ctx, 10)
	go releaseMirrorJobsController.RunWorkers(ctx, 10)
	go releaseMirrorJobStatusController.RunWorkers(ctx, 10)
	go jiraEscalationsController.RunWorkers(ctx, 10)

	<-ctx.Done()

	return nil
}
