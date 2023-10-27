package main

import (
	"os"

	releasereimportcontroller "github.com/openshift/release-controller/pkg/cmd/release-reimport-controller"
	"github.com/spf13/cobra"
	"k8s.io/component-base/cli"
)

func main() {
	command := NewReleaseReimportControllerCommand()
	code := cli.Run(command)
	os.Exit(code)
}

func NewReleaseReimportControllerCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "release-reimport-controller",
		Short: "OpenShift Release Reimport Controller",
		Run: func(cmd *cobra.Command, args []string) {
			_ = cmd.Help()
			os.Exit(1)
		},
	}

	cmd.AddCommand(releasereimportcontroller.NewReleaseReimportControllerCommand("start"))
	return cmd
}
