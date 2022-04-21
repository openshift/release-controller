package release_payload_controller

import (
	"context"
	"errors"
	"fmt"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	releasepayloadclient "github.com/openshift/release-controller/pkg/client/clientset/versioned"
	releasepayloadinformers "github.com/openshift/release-controller/pkg/client/informers/externalversions"
	"github.com/openshift/release-controller/pkg/version"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	prowjobclientset "k8s.io/test-infra/prow/client/clientset/versioned"
	prowjobinformers "k8s.io/test-infra/prow/client/informers/externalversions"
)

type Options struct {
	controllerContext *controllercmd.ControllerContext
	jobNamespace      string
	releaseNamespace  string
}

func NewReleasePayloadControllerCommand(name string) *cobra.Command {
	o := &Options{
		jobNamespace:     "ci-release",
		releaseNamespace: "ocp",
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
	})

	cmd := ccc.NewCommandWithContext(context.Background())
	cmd.Use = name
	cmd.Short = "Start the release payload controller"

	o.AddFlags(cmd.Flags())

	return cmd
}

func (o *Options) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.releaseNamespace, "release-namespace", o.releaseNamespace, "The namespace where ReleasePayloads are stored.")
	fs.StringVar(&o.jobNamespace, "job-namespace", o.jobNamespace, "The namespace where release creation jobs are configured to run.")
}

func (o *Options) Validate(ctx context.Context) error {
	if len(o.jobNamespace) == 0 {
		return errors.New("required flag --job-namespace not set")
	}
	if len(o.releaseNamespace) == 0 {
		return errors.New("required flag --release-namespace not set")
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

	// Payload Verification Controller
	payloadVerificationController, err := NewPayloadVerificationController(o.releaseNamespace, releasePayloadInformer, releasePayloadClient.ReleaseV1alpha1(), o.controllerContext.EventRecorder)
	if err != nil {
		return err
	}

	// Release Creation Status Controller
	releaseCreationStatusController, err := NewReleaseCreationStatusController(o.releaseNamespace, releasePayloadInformer, releasePayloadClient.ReleaseV1alpha1(), o.jobNamespace, batchJobInformer, o.controllerContext.EventRecorder)
	if err != nil {
		return err
	}

	// Release Creation Jobs Controller
	releaseCreationJobsController, err := NewReleaseCreationJobController(o.releaseNamespace, releasePayloadInformer, releasePayloadClient.ReleaseV1alpha1(), o.jobNamespace, o.controllerContext.EventRecorder)
	if err != nil {
		return err
	}

	// Payload Creation Controller
	payloadCreationController, err := NewPayloadCreationController(o.releaseNamespace, releasePayloadInformer, releasePayloadClient.ReleaseV1alpha1(), o.controllerContext.EventRecorder)
	if err != nil {
		return err
	}

	// Payload Accepted Controller
	payloadAcceptedController, err := NewPayloadAcceptedController(o.releaseNamespace, releasePayloadInformer, releasePayloadClient.ReleaseV1alpha1(), o.controllerContext.EventRecorder)
	if err != nil {
		return err
	}

	// Payload Rejected Controller
	payloadRejectedController, err := NewPayloadRejectedController(o.releaseNamespace, releasePayloadInformer, releasePayloadClient.ReleaseV1alpha1(), o.controllerContext.EventRecorder)
	if err != nil {
		return err
	}

	// ProwJob Controller
	pjController, err := NewProwJobStatusController(o.releaseNamespace, releasePayloadInformer, prowJobInformer, o.controllerContext.EventRecorder)
	if err != nil {
		return err
	}

	// Start the informers
	kubeFactory.Start(ctx.Done())
	releasePayloadInformerFactory.Start(ctx.Done())
	prowJobInformerFactory.Start(ctx.Done())

	// Run the Controllers
	go payloadVerificationController.Run(ctx)
	go releaseCreationStatusController.Run(ctx, 1)
	go releaseCreationJobsController.Run(ctx)
	go payloadCreationController.Run(ctx)
	go payloadAcceptedController.Run(ctx)
	go payloadRejectedController.Run(ctx)
	go pjController.Run(ctx, 1)

	<-ctx.Done()

	return nil
}
