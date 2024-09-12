package release_payload_controller

import (
	"context"
	"fmt"

	imageclientset "github.com/openshift/client-go/image/clientset/versioned"
	imageinformers "github.com/openshift/client-go/image/informers/externalversions"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	releasepayloadclient "github.com/openshift/release-controller/pkg/client/clientset/versioned"
	releasepayloadinformers "github.com/openshift/release-controller/pkg/client/informers/externalversions"
	"github.com/openshift/release-controller/pkg/version"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	prowjobclientset "sigs.k8s.io/prow/pkg/client/clientset/versioned"
	prowjobinformers "sigs.k8s.io/prow/pkg/client/informers/externalversions"
)

type Options struct {
	controllerContext *controllercmd.ControllerContext
}

func NewReleasePayloadControllerCommand(name string) *cobra.Command {
	o := &Options{}

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
	})

	cmd := ccc.NewCommandWithContext(context.Background())
	cmd.Use = name
	cmd.Short = "Start the release payload controller"

	o.AddFlags(cmd.Flags())

	return cmd
}

func (o *Options) AddFlags(fs *pflag.FlagSet) {
}

func (o *Options) Validate(ctx context.Context) error {
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
	go legacyResultsController.RunWorkers(ctx, 10)
	go payloadMirrorController.RunWorkers(ctx, 10)
	go releaseMirrorJobsController.RunWorkers(ctx, 10)
	go releaseMirrorJobStatusController.RunWorkers(ctx, 10)

	<-ctx.Done()

	return nil
}
