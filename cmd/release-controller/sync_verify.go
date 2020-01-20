package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/blang/semver"
	"github.com/golang/glog"
	imagev1 "github.com/openshift/api/image/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (c *Controller) ensureVerificationJobs(release *Release, releaseTag *imagev1.TagReference) (VerificationStatusMap, error) {
	var verifyStatus VerificationStatusMap
	retryQueueDelay := 0 * time.Second

	if data := releaseTag.Annotations[releaseAnnotationVerify]; len(data) > 0 {
		verifyStatus = make(VerificationStatusMap)
		if err := json.Unmarshal([]byte(data), &verifyStatus); err != nil {
			glog.Errorf("Release %s has invalid verification status, ignoring: %v", releaseTag.Name, err)
		}
	}

	for name, verifyType := range release.Config.Verify {
		if verifyType.Disabled {
			glog.V(2).Infof("Release verification step %s is disabled, ignoring", name)
			continue
		}

		switch {
		case verifyType.ProwJob != nil:
			var jobRetries int
			if status, ok := verifyStatus[name]; ok {
				jobRetries = status.Retries
				switch status.State {
				case releaseVerificationStateSucceeded:
					continue
				case releaseVerificationStateFailed:
					jobRetries++
					if jobRetries > verifyType.MaxRetries {
						continue
					}
					// find the next time, if ok run.
					if status.TransitionTime != nil {
						backoffDuration := calculateBackoff(jobRetries-1, status.TransitionTime, &metav1.Time{Time: time.Now()})
						if backoffDuration > 0 {
							glog.V(6).Infof("%s: Release verification step %s failed %d times, last failure: %s, backoff till: %s",
								releaseTag.Name, name, jobRetries, status.TransitionTime.Format(time.RFC3339), time.Now().Add(backoffDuration).Format(time.RFC3339))
							if retryQueueDelay == 0 || backoffDuration < retryQueueDelay {
								retryQueueDelay = backoffDuration
							}
							continue
						}
					}
				case releaseVerificationStatePending:
					// we need to process this
				default:
					glog.V(2).Infof("Unrecognized verification status %q for type %s on release %s", status.State, name, releaseTag.Name)
				}
			}

			// if this is an upgrade job, find the appropriate source for the upgrade job
			var previousTag, previousReleasePullSpec string
			if verifyType.Upgrade {
				upgradeSource, err := c.getUpgradeSource(release, releaseTag, name, verifyType.UpgradeFrom)
				if err != nil || len(upgradeSource) == 0 {
					return nil, err
				}
				previousTag = upgradeSource[0].tag
				previousReleasePullSpec = upgradeSource[0].pullSpec
			}
			jobName := name
			if jobRetries > 0 {
				jobName = fmt.Sprintf("%s-%d", jobName, jobRetries)
			}

			job, err := c.ensureProwJobForReleaseTag(release, jobName, verifyType, releaseTag, previousTag, previousReleasePullSpec)
			if err != nil {
				return nil, err
			}
			status, ok := prowJobVerificationStatus(job)
			if !ok {
				return nil, fmt.Errorf("unexpected error accessing prow job definition")
			}
			if status.State == releaseVerificationStateSucceeded {
				glog.V(2).Infof("Prow job %s for release %s succeeded, logs at %s", name, releaseTag.Name, status.URL)
			}
			if verifyStatus == nil {
				verifyStatus = make(VerificationStatusMap)
			}
			status.Retries = jobRetries
			verifyStatus[name] = status

			if jobRetries >= verifyType.MaxRetries {
				continue
			}

			if status.State == releaseVerificationStateFailed {
				// Queue for retry if at least one retryable job at earliest interval
				backoffDuration := calculateBackoff(jobRetries, status.TransitionTime, &metav1.Time{Time: time.Now()})
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

type upgradeSource struct {
	tag      string
	pullSpec string
}

func (c *Controller) getUpgradeSource(release *Release, releaseTag *imagev1.TagReference, verifyName, upgradeFrom string) ([]upgradeSource, error) {
	upgradeType := releaseUpgradeFromPrevious
	if release.Config.As == releaseConfigModeStable {
		upgradeType = releaseUpgradeFromPreviousPatch
	}
	if len(upgradeFrom) > 0 {
		upgradeType = upgradeFrom
	}
	switch upgradeType {
	case releaseUpgradeFromPrevious:
		if tags := sortedReleaseTags(release, releasePhaseAccepted); len(tags) > 0 {
			return []upgradeSource{{
				tag:      tags[0].Name,
				pullSpec: release.Target.Status.PublicDockerImageRepository + ":" + tags[0].Name,
			}}, nil
		}
	case releaseUpgradeFromPreviousMinor:
		if version, err := semver.Parse(releaseTag.Name); err == nil && version.Minor > 0 {
			version.Minor--
			if ref, err := c.stableReleases(); err == nil {
				for _, stable := range ref.Releases {
					versions := unsortedSemanticReleaseTags(stable.Release, releasePhaseAccepted)
					sort.Sort(versions)
					if v := latestTagsWithMajorMinorSemanticVersion(versions, version, 1); v != nil {
						return []upgradeSource{{
							tag:      v[0].Tag.Name,
							pullSpec: stable.Release.Target.Status.PublicDockerImageRepository + ":" + v[0].Tag.Name,
						}}, nil
					}
				}
			}
		}
	case releaseUpgradeFromPreviousPatch:
		if version, err := semver.Parse(releaseTag.Name); err == nil {
			if ref, err := c.stableReleases(); err == nil {
				for _, stable := range ref.Releases {
					versions := unsortedSemanticReleaseTags(stable.Release, releasePhaseAccepted)
					sort.Sort(versions)
					if v := latestTagsWithMajorMinorSemanticVersion(versions, version, 1); v != nil {
						return []upgradeSource{{
							tag:      v[0].Tag.Name,
							pullSpec: stable.Release.Target.Status.PublicDockerImageRepository + ":" + v[0].Tag.Name,
						}}, nil
					}
				}
			}
		}
	case releaseUpgradeFromRallyPoint:
		var sources []upgradeSource
		if version, err := semver.Parse(releaseTag.Name); err == nil {
			if ref, err := c.stableReleases(); err == nil {
				for _, stable := range ref.Releases {
					versions := unsortedSemanticReleaseTags(stable.Release, releasePhaseAccepted)
					sort.Sort(versions)
					if v := latestTagsWithMajorMinorSemanticVersion(versions, version, 10); v != nil {
						sources = make([]upgradeSource, 0)
						for i := range v {
							sources = append(sources, upgradeSource{
								tag:      v[i].Tag.Name,
								pullSpec: stable.Release.Target.Status.PublicDockerImageRepository + ":" + v[i].Tag.Name,
							})
							if isRallyPoint(v[i].Tag) {
								break
							}
						}
						return sources, nil
					}
				}
			}
		}
	}
	return nil, fmt.Errorf("release %s has verify type %s which defines invalid upgradeFrom: %s", release.Config.Name, verifyName, upgradeType)
}

func (c *Controller) ensureAdditionalTests(release *Release, releaseTag *imagev1.TagReference) (map[string]ReleaseAdditionalTest, VerificationStatusList, error) {
	var verifyStatus VerificationStatusList
	retryQueueDelay := 0 * time.Second

	if data := releaseTag.Annotations[releaseAnnotationAdditionalTests]; len(data) > 0 {
		verifyStatus = make(VerificationStatusList)
		if err := json.Unmarshal([]byte(data), &verifyStatus); err != nil {
			glog.Errorf("Release %s has invalid verification status, ignoring: %v", releaseTag.Name, err)
		}
	}

	// additionalTests stores expanded list of jobs that have run for testing status in sync loop
	additionalTests := make(map[string]ReleaseAdditionalTest)

	for name, verifyType := range release.Config.AdditionalTests {
		if verifyType.Disabled {
			glog.V(2).Infof("Release verification step %s is disabled, ignoring", name)
			continue
		}

		switch {
		case verifyType.ProwJob != nil:
			// if this is an upgrade job, find the appropriate source for the upgrade job
			if verifyType.Upgrade {
				upgradeSource, err := c.getUpgradeSource(release, releaseTag, name, verifyType.UpgradeFrom)
				if err != nil || len(upgradeSource) == 0 {
					return nil, nil, err
				}

				for _, src := range upgradeSource {
					srcName := name
					if len(upgradeSource) > 1 {
						srcName = fmt.Sprintf("%s-%s", name, src.tag)
					}
					additionalTests[srcName] = ReleaseAdditionalTest{
						UpgradeTag:          src.tag,
						UpgradeRef:          src.pullSpec,
						RetryStrategy:       verifyType.RetryStrategy,
						ReleaseVerification: verifyType.ReleaseVerification,
					}
				}
			} else {
				additionalTests[name] = verifyType
			}
		default:
			// manual verification
		}
	}

	for name, test := range additionalTests {
		var jobNo int
		if status, ok := verifyStatus[name]; ok {
			for _, jobStatus := range status {
				switch jobStatus.State {
				case releaseVerificationStateSucceeded:
					jobNo++
					if test.RetryStrategy == RetryStrategyFirstSuccess {
						jobNo = test.MaxRetries + 1
						break
					}
					continue
				case releaseVerificationStateFailed:
					jobNo++
					continue
				case releaseVerificationStatePending:
					// we need to process this
				default:
					glog.V(2).Infof("Unrecognized verification status %q for type %s on release %s", jobStatus.State, name, releaseTag.Name)
				}
				break
			}
		}
		if jobNo > test.MaxRetries {
			continue
		}

		jobName := name
		if jobNo > 0 {
			jobName = fmt.Sprintf("%s-%d", jobName, jobNo)
		}

		job, err := c.ensureProwJobForReleaseTag(release, jobName, test.ReleaseVerification, releaseTag, test.UpgradeTag, test.UpgradeRef)
		if err != nil {
			return nil, nil, err
		}
		status, ok := prowJobVerificationStatus(job)
		if !ok {
			return nil, nil, fmt.Errorf("unexpected error accessing prow job definition")
		}
		if status.State == releaseVerificationStateSucceeded {
			glog.V(2).Infof("Prow job %s for release %s succeeded, logs at %s", name, releaseTag.Name, status.URL)
		}
		if verifyStatus == nil {
			verifyStatus = make(VerificationStatusList)
		}
		status.Retries = jobNo
		if len(verifyStatus[name]) < jobNo {
			verifyStatus[name] = append(verifyStatus[name], status)
		} else {
			verifyStatus[name][jobNo] = status
		}
		if jobNo >= test.MaxRetries {
			continue
		}

		if status.State == releaseVerificationStateFailed {
			// Queue for retry if at least one retryable job at earliest interval
			backoffDuration := calculateBackoff(jobNo, status.TransitionTime, &metav1.Time{Time: time.Now()})
			if retryQueueDelay == 0 || backoffDuration < retryQueueDelay {
				retryQueueDelay = backoffDuration
			}
		}
	}

	if retryQueueDelay > 0 {
		key := queueKey{
			name:      release.Source.Name,
			namespace: release.Source.Namespace,
		}
		c.queue.AddAfter(key, retryQueueDelay)
	}
	return additionalTests, verifyStatus, nil
}

func isRallyPoint(v *imagev1.TagReference) bool {
	version, err := semver.Parse(v.Name)
	if err != nil {
		return false
	}
	return version.Patch%10 == 0
}