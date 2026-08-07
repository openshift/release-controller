package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"

	"github.com/blang/semver"
	"github.com/hashicorp/go-retryablehttp"
	imagev1 "github.com/openshift/api/image/v1"
	citools "github.com/openshift/ci-tools/pkg/release/official"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"
)

func (c *Controller) ensureVerificationJobs(release *releasecontroller.Release, releaseTag *imagev1.TagReference, verifyStatus releasecontroller.VerificationStatusMap) error {
	retryQueueDelay := 0 * time.Second
	verificationJobs, err := releasecontroller.GetVerificationJobs(c.parsedReleaseConfigCache, c.eventRecorder, c.releaseLister, release, releaseTag, c.artSuffix)
	if err != nil {
		return err
	}
	for name, verifyType := range verificationJobs {
		if verifyType.Disabled {
			klog.V(2).Infof("Release verification step %s is disabled, ignoring", name)
			continue
		}

		switch {
		case verifyType.ProwJob != nil:
			var jobRetries int
			if status, ok := verifyStatus[name]; ok {
				jobRetries = status.Retries
				switch status.State {
				case releasecontroller.ReleaseVerificationStateSucceeded:
					continue
				case releasecontroller.ReleaseVerificationStateFailed:
					jobRetries++
					if jobRetries > verifyType.MaxRetries {
						continue
					}
					if status.TransitionTime != nil {
						backoffDuration := releasecontroller.CalculateBackoff(jobRetries-1, status.TransitionTime, &metav1.Time{Time: time.Now()})
						if backoffDuration > 0 {
							klog.V(6).Infof("%s: Release verification step %s failed %d times, last failure: %s, backoff till: %s",
								releaseTag.Name, name, jobRetries, status.TransitionTime.Format(time.RFC3339), time.Now().Add(backoffDuration).Format(time.RFC3339))
							if retryQueueDelay == 0 || backoffDuration < retryQueueDelay {
								retryQueueDelay = backoffDuration
							}
							continue
						}
					}
				case releasecontroller.ReleaseVerificationStatePending:
					// we need to process this
				default:
					klog.V(2).Infof("Unrecognized verification status %q for type %s on release %s", status.State, name, releaseTag.Name)
				}
			}

			var previousTag, previousReleasePullSpec string
			if verifyType.Upgrade {
				var err error
				previousTag, previousReleasePullSpec, err = c.getUpgradeTagAndPullSpec(release, releaseTag, name, verifyType.UpgradeFrom, verifyType.UpgradeFromRelease, false)
				if err != nil {
					return err
				}
			}
			jobNameSuffix := ""
			if jobRetries > 0 {
				jobNameSuffix = fmt.Sprintf("retry-%d", jobRetries)
			}
			jobLabels := map[string]string{
				releasecontroller.ProwJobLabelCapability: "rce",
				releasecontroller.ReleaseLabelVerify:     "true",
				releasecontroller.ReleaseLabelPayload:    releaseTag.Name,
			}
			if verifyType.AggregatedProwJob != nil {
				err := c.launchAnalysisJobs(release, name, verifyType, releaseTag, previousTag, previousReleasePullSpec)
				if err != nil {
					return err
				}
				jobLabels["release.openshift.io/aggregator"] = releaseTag.Name
			}
			job, err := c.ensureProwJobForReleaseTag(release, name, jobNameSuffix, verifyType, releaseTag, previousTag, previousReleasePullSpec, jobLabels)
			if err != nil {
				return err
			}
			prowStatus, ok := releasecontroller.ProwJobVerificationStatus(job)
			if !ok {
				return fmt.Errorf("unexpected error accessing prow job definition")
			}
			if prowStatus.State == releasecontroller.ReleaseVerificationStateSucceeded {
				klog.V(2).Infof("Prow job %s for release %s succeeded, logs at %s", name, releaseTag.Name, releasecontroller.GenerateProwJobResultsURL(prowStatus.URL))
			}

		default:
			// manual verification
		}
	}
	if retryQueueDelay > 0 {
		key := queueKey{
			name:      release.Source.Name,
			namespace: release.Source.Namespace,
		}
		c.queue.AddAfter(key, retryQueueDelay)
	}
	return nil
}

func (c *Controller) getUpgradeTagAndPullSpec(release *releasecontroller.Release, releaseTag *imagev1.TagReference, name, upgradeFrom string, upgradeFromRelease *releasecontroller.UpgradeRelease, periodic bool) (previousTag, previousReleasePullSpec string, err error) {
	if upgradeFromRelease != nil {
		return c.resolveUpgradeRelease(upgradeFromRelease, release)
	}
	var upgradeType string
	if periodic {
		upgradeType = releasecontroller.ReleaseUpgradeFromPreviousMinus1
	} else {
		upgradeType = releasecontroller.ReleaseUpgradeFromPrevious
	}
	if release.Config.As == releasecontroller.ReleaseConfigModeStable {
		upgradeType = releasecontroller.ReleaseUpgradeFromPreviousPatch
	}
	if len(upgradeFrom) > 0 {
		upgradeType = upgradeFrom
	}
	switch upgradeType {
	case releasecontroller.ReleaseUpgradeFromPrevious:
		if tags := releasecontroller.SortedReleaseTags(release, releasecontroller.ReleasePhaseAccepted); len(tags) > 0 {
			previousTag = tags[0].Name
			previousReleasePullSpec = releasecontroller.ReleasePullSpec(release, tags[0])
		}
	case releasecontroller.ReleaseUpgradeFromPreviousMinus1:
		if tags := releasecontroller.SortedReleaseTags(release, releasecontroller.ReleasePhaseAccepted); len(tags) > 1 {
			previousTag = tags[1].Name
			previousReleasePullSpec = releasecontroller.ReleasePullSpec(release, tags[1])
		}
	case releasecontroller.ReleaseUpgradeFromPreviousMinor:
		if version, err := semver.Parse(releaseTag.Name); err == nil && version.Minor > 0 {
			version.Minor--
			if ref, err := releasecontroller.GetStableReleases(c.parsedReleaseConfigCache, c.eventRecorder, c.releaseLister, c.releasePayloadLister); err == nil {
				previousTag, previousReleasePullSpec = findLatestStableForVersion(ref, version)
			}
		}
	case releasecontroller.ReleaseUpgradeFromPreviousPatch:
		if version, err := semver.Parse(releaseTag.Name); err == nil {
			if ref, err := releasecontroller.GetStableReleases(c.parsedReleaseConfigCache, c.eventRecorder, c.releaseLister, c.releasePayloadLister); err == nil {
				previousTag, previousReleasePullSpec = findLatestStableForVersion(ref, version)
			}
		}
	default:
		return "", "", fmt.Errorf("release %s has job %s which defines invalid upgradeFrom: %s", release.Config.Name, name, upgradeType)
	}
	return previousTag, previousReleasePullSpec, err
}

func findLatestStableForVersion(ref *releasecontroller.StableReferences, version semver.Version) (string, string) {
	var currentLatestRelease *semver.Version
	var latestTag, latestPullSpec string
	for _, stable := range ref.Releases {
		versions := releasecontroller.UnsortedSemanticReleaseTags(stable.Release, releasecontroller.ReleasePhaseAccepted)
		sort.Sort(versions)
		if v := releasecontroller.FirstTagWithMajorMinorSemanticVersion(versions, version); v != nil {
			if currentLatestRelease == nil || v.Version.Compare(*currentLatestRelease) > 0 {
				currentLatestRelease = v.Version
				latestTag = v.Tag.Name
				latestPullSpec = releasecontroller.ReleasePullSpec(stable.Release, v.Tag)
			}
		}
	}
	return latestTag, latestPullSpec
}

func (c *Controller) resolveUpgradeRelease(upgradeRelease *releasecontroller.UpgradeRelease, release *releasecontroller.Release) (string, string, error) {
	if upgradeRelease.Prerelease != nil {
		semverRange, err := semver.ParseRange(upgradeRelease.Prerelease.VersionBounds.Query())
		if err != nil {
			return "", "", fmt.Errorf("invalid semver range `%s`: %w", upgradeRelease.Prerelease.VersionBounds.Query(), err)
		}
		//TODO: Right now, nothing relies on the "prerelease" stanza that would trigger this logic.  If it is ever used, we are going to need to handle "4-stable" and "5-stable"...
		r, latest, err := releasecontroller.LatestForStream(c.parsedReleaseConfigCache, c.eventRecorder, c.releaseLister, c.releasePayloadLister, "4-stable", semverRange, 0, "")
		if err != nil {
			return "", "", fmt.Errorf("failed to get latest tag in 4-stable stream: %w", err)
		}
		tag := latest.Name
		pullSpec := releasecontroller.ReleasePullSpec(r, latest)
		return tag, pullSpec, nil
	} else if upgradeRelease.Candidate != nil {
		// create blank semver.Range
		var constraint semver.Range
		stream := fmt.Sprintf("%s.0-0.%s%s", upgradeRelease.Candidate.Version, upgradeRelease.Candidate.Stream, TrimPrefixes(release.Config.To, "release-5", "release"))
		r, latest, err := releasecontroller.LatestForStream(c.parsedReleaseConfigCache, c.eventRecorder, c.releaseLister, c.releasePayloadLister, stream, constraint, upgradeRelease.Candidate.Relative, "")
		if err != nil {
			return "", "", fmt.Errorf("failed to get latest tag for stream %s: %w", stream, err)
		}
		tag := latest.Name
		pullSpec := releasecontroller.ReleasePullSpec(r, latest)
		return tag, pullSpec, nil
	} else if upgradeRelease.Official != nil {
		httpClient := retryablehttp.NewClient()
		httpClient.Logger = nil
		pullspec, version, err := citools.ResolvePullSpecAndVersion(httpClient.StandardClient(), *upgradeRelease.Official)
		if err != nil {
			return "", "", fmt.Errorf("failed to resolve official release: %w", err)
		}
		return pullspec, version, nil
	}
	return "", "", fmt.Errorf("upgradeRelease fields must be set if upgradeRelease is set")
}

func TrimPrefixes(s string, prefixes ...string) string {
	for _, prefix := range prefixes {
		if after, found := strings.CutPrefix(s, prefix); found {
			return after
		}
	}
	return s
}
