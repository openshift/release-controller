package main

import (
	"os"

	releasemirrorcleanupcontroller "github.com/openshift/release-controller/pkg/cmd/release-mirror-cleanup-controller"
	"github.com/spf13/cobra"
	"k8s.io/component-base/cli"
)

func main() {
	command := NewReleaseMirrorCleanupControllerCommand()
	code := cli.Run(command)
	os.Exit(code)
}

func NewReleaseMirrorCleanupControllerCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "release-mirror-cleanup-controller",
		Short: "OpenShift Release Mirror Cleanup Controller",
		Run: func(cmd *cobra.Command, args []string) {
			_ = cmd.Help()
			os.Exit(1)
		},
	}

	cmd.AddCommand(releasemirrorcleanupcontroller.NewReleaseMirrorCleanupControllerCommand("start"))
	return cmd
}
