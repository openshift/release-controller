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
	"context"
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
	includeChangelogs := flag.Bool("include-changelogs", false, "include RPM changelogs for changed packages in the Node Image Info section")
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
		nodeMD, err := rhcos.NodeImageSectionMarkdown(context.Background(), info, *from, *to, out)
		if err != nil {
			fmt.Fprintf(os.Stderr, "NodeImageSectionMarkdown: %v\n", err)
			os.Exit(1)
		}
		if nodeMD != "" {
			out = out + "\n\n## Node Image Info\n\n" + nodeMD
		}
	}

	fmt.Print(out)

	if *includeChangelogs {
		printRpmChangelogs(info, *from, *to)
	}
}

func printRpmChangelogs(info *releasecontroller.ExecReleaseInfo, from, to string) {
	streams, err := info.ListMachineOSStreams(to)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ListMachineOSStreams: %v\n", err)
		return
	}

	type diffEntry struct {
		tag  string
		diff releasecontroller.RpmDiff
	}
	var diffs []diffEntry

	if len(streams) == 0 {
		diff, err := info.RpmDiff(from, to)
		if err != nil {
			fmt.Fprintf(os.Stderr, "RpmDiff: %v\n", err)
			return
		}
		diffs = append(diffs, diffEntry{tag: "rhel-coreos", diff: diff})
	} else {
		for _, stream := range streams {
			if _, errFrom := info.ImageReferenceForComponent(from, stream.Tag); errFrom != nil {
				continue
			}
			diff, err := info.RpmDiffForStream(from, to, stream.Tag)
			if err != nil {
				fmt.Fprintf(os.Stderr, "RpmDiffForStream(%s): %v\n", stream.Tag, err)
				continue
			}
			diffs = append(diffs, diffEntry{tag: stream.Tag, diff: diff})
		}
	}

	for _, de := range diffs {
		if len(de.diff.Changed) == 0 {
			continue
		}
		imageRef, err := info.ImageReferenceForComponent(to, de.tag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ImageReferenceForComponent(%s): %v\n", de.tag, err)
			continue
		}
		packageNames := make([]string, 0, len(de.diff.Changed))
		for pkg := range de.diff.Changed {
			packageNames = append(packageNames, pkg)
		}
		changelogs, err := info.RpmChangelogs(imageRef, packageNames)
		if err != nil {
			fmt.Fprintf(os.Stderr, "RpmChangelogs(%s): %v\n", de.tag, err)
			continue
		}
		fmt.Printf("\n## RPM Changelogs (%s)\n\n", de.tag)
		for pkg, entry := range de.diff.Changed {
			cl, ok := changelogs[pkg]
			if !ok || cl == "" {
				continue
			}
			filtered := releasecontroller.FilterChangelogBetweenVersions(cl, entry.Old)
			if filtered == "" {
				continue
			}
			fmt.Printf("### %s (%s → %s)\n\n%s\n\n", pkg, entry.Old, entry.New, filtered)
		}
	}
}
