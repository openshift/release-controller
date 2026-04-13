package rhcos

import (
	"fmt"
	"strings"
	"sync"

	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
)

// NodeImageSectionMarkdown returns markdown for the Node Image Info block (package lists, RPM diffs,
// extensions, base-layer reprint). It returns ("", nil) when there is nothing to show: no
// #node-image-info anchor and no machine-os streams on the target release (see ListMachineOSStreams).
//
// Older changelogs embedded #node-image-info via the CoreOS infobox in TransformMarkDownOutput.
// Newer oc releases may omit the "* Red Hat Enterprise Linux CoreOS upgraded from …" summary lines,
// so that anchor is absent even when the payload has rhel-coreos* streams—we still render node
// info when streams are discoverable.
func NodeImageSectionMarkdown(info releasecontroller.ReleaseInfo, fromReleasePullSpec, toReleasePullSpec, changelogMarkdown string) (string, error) {
	streams, err := info.ListMachineOSStreams(toReleasePullSpec)
	if err != nil {
		return "", err
	}

	hasAnchor := strings.Contains(changelogMarkdown, "#node-image-info")
	if !hasAnchor && len(streams) == 0 {
		return "", nil
	}

	if len(streams) == 0 {
		rpmlist, err := info.RpmList(toReleasePullSpec)
		if err != nil {
			return "", err
		}

		rpmdiff, err := info.RpmDiff(fromReleasePullSpec, toReleasePullSpec)
		if err != nil {
			return "", err
		}

		return RenderNodeImageInfo(changelogMarkdown, rpmlist, rpmdiff), nil
	}

	// Process all streams in parallel to reduce cache population time
	nodeStreams := make([]CoreOSNodeStream, len(streams))
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error

	for i, ms := range streams {
		wg.Add(1)
		go func(idx int, stream releasecontroller.MachineOSStreamInfo) {
			defer wg.Done()

			ext := stream.Tag + "-extensions"
			list, err := info.RpmListForStream(toReleasePullSpec, stream.Tag, ext)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				return
			}

			var diff releasecontroller.RpmDiff
			if _, errFrom := info.ImageReferenceForComponent(fromReleasePullSpec, stream.Tag); errFrom == nil {
				diff, err = info.RpmDiffForStream(fromReleasePullSpec, toReleasePullSpec, stream.Tag)
				if err != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					mu.Unlock()
					return
				}
			}

			nodeStreams[idx] = CoreOSNodeStream{
				Title:   releasecontroller.MachineOSTitle(stream),
				RpmList: list,
				RpmDiff: diff,
			}
		}(i, ms)
	}

	wg.Wait()

	if firstErr != nil {
		return "", fmt.Errorf("failed to fetch stream info: %w", firstErr)
	}

	return RenderDualNodeImageInfo(changelogMarkdown, nodeStreams), nil
}
