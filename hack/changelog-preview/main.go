// changelog-preview runs the same ChangeLog + RHCOS markdown transforms as the release-controller API
// without needing a Kubernetes cluster. Requires `oc` on PATH and registry pull access to the
// release images you pass.
//
// By default it also appends the Node Image Info section (RPM lists and diffs per CoreOS stream
// when applicable), matching the web UI. Use --skip-node-info for changelog-only output.
//
// Example:
//
//	go run ./hack/changelog-preview/ \
//	  --from quay.io/openshift-release-dev/ocp-release@sha256:... \
//	  --to quay.io/openshift-release-dev/ocp-release@sha256:... \
//	  --from-tag 4.20.0-0.nightly-2025-01-01-000000 \
//	  --to-tag 4.21.0-ec.1
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/openshift/release-controller/pkg/rhcos"
	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/prow/pkg/jira"
)

func main() {
	from := flag.String("from", "", "from release image pull spec (digest or tag@repo)")
	to := flag.String("to", "", "to release image pull spec")
	fromTag := flag.String("from-tag", "previous", "from tag name (for markdown link substitution)")
	toTag := flag.String("to-tag", "current", "to tag name (for markdown link substitution)")
	arch := flag.String("arch", "amd64", "release architecture (amd64, arm64, ...)")
	skipNode := flag.Bool("skip-node-info", false, "omit Node Image Info (faster; no extra oc rpmdb/image-for calls)")
	flag.Parse()
	if *from == "" || *to == "" {
		fmt.Fprintf(os.Stderr, "usage: changelog-preview --from <pullspec> --to <pullspec> [flags]\n")
		os.Exit(2)
	}

	var archName, archExt string
	switch *arch {
	case "amd64":
		archName = "x86_64"
	case "arm64":
		archName = "aarch64"
		archExt = fmt.Sprintf("-%s", archName)
	default:
		archName = *arch
		archExt = fmt.Sprintf("-%s", archName)
	}

	var nilClient kubernetes.Interface
	var nilCfg *rest.Config
	info := releasecontroller.NewExecReleaseInfo(nilClient, nilCfg, "", "", func() (string, error) { return "", nil }, jira.Client(nil))

	out, err := info.ChangeLog(*from, *to, false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ChangeLog: %v\n", err)
		os.Exit(1)
	}
	out, err = rhcos.TransformMarkDownOutput(out, *fromTag, *toTag, archName, archExt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "TransformMarkDownOutput: %v\n", err)
		os.Exit(1)
	}

	if !*skipNode {
		nodeMD, err := rhcos.NodeImageSectionMarkdown(info, *from, *to, out)
		if err != nil {
			fmt.Fprintf(os.Stderr, "NodeImageSectionMarkdown: %v\n", err)
			os.Exit(1)
		}
		if nodeMD != "" {
			out = out + "\n\n## Node Image Info\n\n" + nodeMD
		}
	}

	fmt.Print(out)
}
