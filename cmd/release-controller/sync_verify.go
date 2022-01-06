package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"

	"github.com/blang/semver"
	imagev1 "github.com/openshift/api/image/v1"
	citools "github.com/openshift/ci-tools/pkg/release/official"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"
)

func (c *Controller) ensureVerificationJobs(release *releasecontroller.Release, releaseTag *imagev1.TagReference) (releasecontroller.VerificationStatusMap, error) {
	var verifyStatus releasecontroller.VerificationStatusMap
	retryQueueDelay := 0 * time.Second
	for name, verifyType := range release.Config.Verify {
		if verifyType.Disabled {
			klog.V(2).Infof("Release verification step %s is disabled, ignoring", name)
			continue
		}

		switch {
		case verifyType.ProwJob != nil:
			if verifyStatus == nil {
				if data := releaseTag.Annotations[releasecontroller.ReleaseAnnotationVerify]; len(data) > 0 {
					verifyStatus = make(releasecontroller.VerificationStatusMap)
					if err := json.Unmarshal([]byte(data), &verifyStatus); err != nil {
						klog.Errorf("Release %s has invalid verification status, ignoring: %v", releaseTag.Name, err)
					}
				}
			}

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
					// find the next time, if ok run.
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

			// if this is an upgrade job, find the appropriate source for the upgrade job
			var previousTag, previousReleasePullSpec string
			if verifyType.Upgrade {
				var err error
				previousTag, previousReleasePullSpec, err = c.getUpgradeTagAndPullSpec(release, releaseTag, name, verifyType.UpgradeFrom, verifyType.UpgradeFromRelease, false)
				if err != nil {
					return nil, err
				}
			}
			jobNameSuffix := ""
			if jobRetries > 0 {
				jobNameSuffix = fmt.Sprintf("%d", jobRetries)
			}
			jobLabels := map[string]string{
				"release.openshift.io/verify": "true",
			}
			if verifyType.AggregatedProwJob != nil {
				err := c.launchAnalysisJobs(release, name, verifyType, releaseTag, previousTag, previousReleasePullSpec)
				if err != nil {
					return nil, err
				}
				jobLabels["release.openshift.io/aggregator"] = releaseTag.Name
			}
			job, err := c.ensureProwJobForReleaseTag(release, name, jobNameSuffix, verifyType, releaseTag, previousTag, previousReleasePullSpec, jobLabels)
			if err != nil {
				return nil, err
			}
			status, ok := releasecontroller.ProwJobVerificationStatus(job)
			if !ok {
				return nil, fmt.Errorf("unexpected error accessing prow job definition")
			}
			if status.State == releasecontroller.ReleaseVerificationStateSucceeded {
				klog.V(2).Infof("Prow job %s for release %s succeeded, logs at %s", name, releaseTag.Name, status.URL)
			}
			if verifyStatus == nil {
				verifyStatus = make(releasecontroller.VerificationStatusMap)
			}
			status.Retries = jobRetries
			verifyStatus[name] = status

			if jobRetries >= verifyType.MaxRetries {
				verifyStatus[name].TransitionTime = nil
				continue
			}

			if status.State == releasecontroller.ReleaseVerificationStateFailed {
				// Queue for retry if at least one retryable job at earliest interval
				backoffDuration := releasecontroller.CalculateBackoff(jobRetries, status.TransitionTime, &metav1.Time{Time: time.Now()})
				if retryQueueDelay == 0 || backoffDuration < retryQueueDelay {
					retryQueueDelay = backoffDuration
				}
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
	return verifyStatus, nil
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
			previousReleasePullSpec = release.Target.Status.PublicDockerImageRepository + ":" + previousTag
		}
	case releasecontroller.ReleaseUpgradeFromPreviousMinus1:
		if tags := releasecontroller.SortedReleaseTags(release, releasecontroller.ReleasePhaseAccepted); len(tags) > 1 {
			previousTag = tags[1].Name
			previousReleasePullSpec = release.Target.Status.PublicDockerImageRepository + ":" + previousTag
		}
	case releasecontroller.ReleaseUpgradeFromPreviousMinor:
		if version, err := semver.Parse(releaseTag.Name); err == nil && version.Minor > 0 {
			version.Minor--
			if ref, err := c.stableReleases(); err == nil {
				for _, stable := range ref.Releases {
					versions := releasecontroller.UnsortedSemanticReleaseTags(stable.Release, releasecontroller.ReleasePhaseAccepted)
					sort.Sort(versions)
					if v := releasecontroller.FirstTagWithMajorMinorSemanticVersion(versions, version); v != nil {
						previousTag = v.Tag.Name
						previousReleasePullSpec = stable.Release.Target.Status.PublicDockerImageRepository + ":" + previousTag
						break
					}
				}
			}
		}
	case releasecontroller.ReleaseUpgradeFromPreviousPatch:
		if version, err := semver.Parse(releaseTag.Name); err == nil {
			if ref, err := c.stableReleases(); err == nil {
				for _, stable := range ref.Releases {
					versions := releasecontroller.UnsortedSemanticReleaseTags(stable.Release, releasecontroller.ReleasePhaseAccepted)
					sort.Sort(versions)
					if v := releasecontroller.FirstTagWithMajorMinorSemanticVersion(versions, version); v != nil {
						previousTag = v.Tag.Name
						previousReleasePullSpec = stable.Release.Target.Status.PublicDockerImageRepository + ":" + previousTag
						break
					}
				}
			}
		}
	default:
		return "", "", fmt.Errorf("release %s has job %s which defines invalid upgradeFrom: %s", release.Config.Name, name, upgradeType)
	}
	return previousTag, previousReleasePullSpec, err
}

func (c *Controller) resolveUpgradeRelease(upgradeRelease *releasecontroller.UpgradeRelease, release *releasecontroller.Release) (string, string, error) {
	if upgradeRelease.Prerelease != nil {
		semverRange, err := semver.ParseRange(upgradeRelease.Prerelease.VersionBounds.Query())
		if err != nil {
			return "", "", fmt.Errorf("invalid semver range `%s`: %w", upgradeRelease.Prerelease.VersionBounds.Query(), err)
		}
		r, latest, err := c.latestForStream("4-stable", semverRange, 0)
		if err != nil {
			return "", "", fmt.Errorf("failed to get latest tag in 4-stable stream: %w", err)
		}
		tag := latest.Name
		pullSpec := r.Target.Status.PublicDockerImageRepository + ":" + tag
		return tag, pullSpec, nil
	} else if upgradeRelease.Candidate != nil {
		// create blank semver.Range
		var constraint semver.Range
		stream := fmt.Sprintf("%s.0-0.%s%s", upgradeRelease.Candidate.Version, upgradeRelease.Candidate.Stream, strings.TrimPrefix(release.Config.To, "release"))
		r, latest, err := c.latestForStream(stream, constraint, upgradeRelease.Candidate.Relative)
		if err != nil {
			return "", "", fmt.Errorf("failed to get latest tag for stream %s: %w", stream, err)
		}
		tag := latest.Name
		pullSpec := r.Target.Status.PublicDockerImageRepository + ":" + tag
		return tag, pullSpec, nil
	} else if upgradeRelease.Official != nil {
		pullspec, version, err := citools.ResolvePullSpecAndVersion(*upgradeRelease.Official)
		if err != nil {
			return "", "", fmt.Errorf("failed to resolve official release: %w", err)
		}
		return pullspec, version, nil
	}
	return "", "", fmt.Errorf("upgradeRelease fields must be set if upgradeRelease is set")
}
