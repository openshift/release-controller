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
	for name, verifyType := range release.Config.Verify {
		if verifyType.Disabled {
			glog.V(2).Infof("Release verification step %s is disabled, ignoring", name)
			continue
		}

		switch {
		case verifyType.ProwJob != nil:
			if verifyStatus == nil {
				if data := releaseTag.Annotations[releaseAnnotationVerify]; len(data) > 0 {
					verifyStatus = make(VerificationStatusMap)
					if err := json.Unmarshal([]byte(data), &verifyStatus); err != nil {
						glog.Errorf("Release %s has invalid verification status, ignoring: %v", releaseTag.Name, err)
					}
				}
			}

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
						backoffDuration := calculateBackoff(jobRetries-1, status.TransitionTime, &metav1.Time{time.Now()})
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
				upgradeType := releaseUpgradeFromPrevious
				if release.Config.As == releaseConfigModeStable {
					upgradeType = releaseUpgradeFromPreviousPatch
				}
				if len(verifyType.UpgradeFrom) > 0 {
					upgradeType = verifyType.UpgradeFrom
				}
				switch upgradeType {
				case releaseUpgradeFromPrevious:
					if tags := tagsForRelease(release, releasePhaseAccepted); len(tags) > 0 {
						previousTag = tags[0].Name
						previousReleasePullSpec = release.Target.Status.PublicDockerImageRepository + ":" + previousTag
					}
				case releaseUpgradeFromPreviousMinor:
					if version, err := semver.Parse(releaseTag.Name); err == nil && version.Minor > 0 {
						version.Minor--
						if ref, err := c.stableReleases(); err == nil {
							for _, stable := range ref.Releases {
								versions := semanticTagsForRelease(stable.Release, releasePhaseAccepted)
								sort.Sort(versions)
								if v := firstTagWithMajorMinorSemanticVersion(versions, version); v != nil {
									previousTag = v.Tag.Name
									previousReleasePullSpec = stable.Release.Target.Status.PublicDockerImageRepository + ":" + previousTag
									break
								}
							}
						}
					}
				case releaseUpgradeFromPreviousPatch:
					if version, err := semver.Parse(releaseTag.Name); err == nil {
						if ref, err := c.stableReleases(); err == nil {
							for _, stable := range ref.Releases {
								versions := semanticTagsForRelease(stable.Release, releasePhaseAccepted)
								sort.Sort(versions)
								if v := firstTagWithMajorMinorSemanticVersion(versions, version); v != nil {
									previousTag = v.Tag.Name
									previousReleasePullSpec = stable.Release.Target.Status.PublicDockerImageRepository + ":" + previousTag
									break
								}
							}
						}
					}
				default:
					return nil, fmt.Errorf("release %s has verify type %s which defines invalid upgradeFrom: %s", release.Config.Name, name, upgradeType)
				}
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
				backoffDuration := calculateBackoff(jobRetries, status.TransitionTime, &metav1.Time{time.Now()})
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
