package main

import (
	releasepayloadcontroller "github.com/openshift/release-controller/pkg/cmd/release-payload-controller"
	"github.com/spf13/cobra"
	"k8s.io/component-base/cli"
	"os"
)

func main() {
	command := NewReleasePayloadControllerCommand()
	code := cli.Run(command)
	os.Exit(code)
}

func NewReleasePayloadControllerCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "release-payload-controller",
		Short: "OpenShift Release Payload Controller",
		Run: func(cmd *cobra.Command, args []string) {
			_ = cmd.Help()
			os.Exit(1)
		},
	}

	cmd.AddCommand(releasepayloadcontroller.NewReleasePayloadControllerCommand("start"))
	return cmd
}
