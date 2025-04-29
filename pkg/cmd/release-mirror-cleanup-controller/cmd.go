package release_mirror_cleanup_controller

import (
	"context"
	"errors"
	"time"

	imageclientset "github.com/openshift/client-go/image/clientset/versioned"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/openshift/release-controller/pkg/version"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/klog/v2"
)

type Options struct {
	controllerContext *controllercmd.ControllerContext
	namespaces        []string
	dryRun            bool
}

func NewReleaseMirrorCleanupControllerCommand(name string) *cobra.Command {
	o := &Options{}

	ccc := controllercmd.NewControllerCommandConfig("release-mirror-cleanup-controller", version.Get(), func(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
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
	ccc.DisableLeaderElection = true

	cmd := ccc.NewCommandWithContext(context.Background())
	cmd.Use = name
	cmd.Short = "Start the release mirror cleanup controller"

	o.AddFlags(cmd.Flags())

	return cmd
}

func (o *Options) AddFlags(fs *pflag.FlagSet) {
	fs.StringArrayVar(&o.namespaces, "namespaces", []string{}, "Namespaces where releases exist")
	fs.BoolVar(&o.dryRun, "dry-run", false, "Print tags to be deleted without actually committing the changes.")
}

func (o *Options) Validate(ctx context.Context) error {
	if len(o.namespaces) == 0 {
		return errors.New("--namespaces flag must be set")
	}
	return nil
}

func (o *Options) Run(ctx context.Context) error {
	inClusterConfig := o.controllerContext.KubeConfig

	// ImageStream Informers
	imageStreamClient, err := imageclientset.NewForConfig(inClusterConfig)
	if err != nil {
		klog.Fatalf("Error building imagestream clientset: %s", err.Error())
	}

	mirrorCleanupController := NewMirrorCleanupController(imageStreamClient, o.namespaces, o.dryRun)

	go mirrorCleanupController.Run(ctx, 6*time.Hour)

	<-ctx.Done()

	return nil
}
