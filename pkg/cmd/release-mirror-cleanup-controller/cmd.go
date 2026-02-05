package release_mirror_cleanup_controller

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	imageclientset "github.com/openshift/client-go/image/clientset/versioned"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/openshift/release-controller/pkg/version"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

type Options struct {
	controllerContext    *controllercmd.ControllerContext
	namespaces           []string
	credentialsNamespace string
	credentialsName      string
	minimumAge           time.Duration
	interval             time.Duration
	dryRun               bool
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
	// the same secret is distributed across all job namespaces, so we can just use the one in ci-release for everything
	fs.StringVar(&o.credentialsNamespace, "credentials-namespace", "ci-release", "Namespace where repo credentials secret is located")
	fs.StringVar(&o.credentialsName, "credentials-secret", "release-controller-quay-mirror-secret", "Namespace where repo credentials secret is located")
	// default to 30 days minimum age
	fs.DurationVar(&o.minimumAge, "minimum-age", 720*time.Hour, "Only delete tags older than this duration")
	fs.DurationVar(&o.interval, "interval", 24*time.Hour, "How often to run the cleaner")
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
		return fmt.Errorf("error building imagestream clientset: %s", err.Error())
	}

	kubeClient, err := clientset.NewForConfig(inClusterConfig)
	if err != nil {
		return fmt.Errorf("error building generic clientset: %s", err.Error())
	}
	credsSecret, err := kubeClient.CoreV1().Secrets(o.credentialsNamespace).Get(context.TODO(), o.credentialsName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to retrieve secret %s/%s: %v", o.credentialsNamespace, o.credentialsName, err)
	}
	data, ok := credsSecret.Data["config.json"]
	if !ok {
		return fmt.Errorf("secret %s/%s does not contain config.json", o.credentialsNamespace, o.credentialsName)
	}
	// regclient can only read a file for credentials, so we need to write the credentials to a file for it
	if err := os.WriteFile("/tmp/config.json", data, 0644); err != nil {
		return fmt.Errorf("failed to write secret to /tmp/config.json: %v", err)
	}

	mirrorCleanupController := NewMirrorCleanupController(imageStreamClient, "/tmp/config.json", o.namespaces, o.dryRun, o.minimumAge)

	go mirrorCleanupController.Run(ctx, o.interval)

	<-ctx.Done()

	return nil
}
