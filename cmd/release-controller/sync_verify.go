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
				status.TransitionTime = nil
			}
			if verifyStatus == nil {
				verifyStatus = make(VerificationStatusMap)
			}
			status.Retries = jobRetries
			verifyStatus[name] = status

			if jobRetries >= verifyType.MaxRetries {
				verifyStatus[name].TransitionTime = nil
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
			for _, tag := range tags {
				if tag.Name != releaseTag.Name {
					return []upgradeSource{{
						tag:      tag.Name,
						pullSpec: release.Target.Status.PublicDockerImageRepository + ":" + tag.Name,
					}}, nil
				}
			}
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
	default:
		return nil, fmt.Errorf("release %s has verify type %s which defines invalid upgradeFrom: %s", release.Config.Name, verifyName, upgradeType)
	}
	return []upgradeSource{{"", ""}}, nil
}

func (c *Controller) ensureCandidateTests(release *Release, releaseTag *imagev1.TagReference) (map[string]*ReleaseCandidateTest, VerificationStatusList, error) {
	var testStatus VerificationStatusList
	retryQueueDelay := 0 * time.Second

	if len(releaseTag.Annotations) > 0 {
		if data := releaseTag.Annotations[releaseAnnotationCandidateTests]; len(data) > 0 {
			testStatus = make(VerificationStatusList)
			if err := json.Unmarshal([]byte(data), &testStatus); err != nil {
				glog.Errorf("Release %s has invalid verification status, ignoring: %v", releaseTag.Name, err)
			}
		}
	}

	// candidateTests stores expanded list of jobs that have run for testing status in sync loop
	candidateTests, err := c.expandCandidateTests(release, releaseTag)
	if err != nil {
		return nil, nil, err
	}

	for name, test := range candidateTests {
		var jobNo, failed int
		var lastJobFailed bool
		if status, ok := testStatus[name]; ok {
			for _, jobStatus := range status.Status {
				switch jobStatus.State {
				case releaseVerificationStateSucceeded:
					lastJobFailed = false
					jobNo++
					failed = 0
					if test.RetryStrategy == RetryStrategyFirstSuccess {
						jobNo = test.MaxRetries + 1
						break
					}
					continue
				case releaseVerificationStateFailed:
					lastJobFailed = true
					jobNo++
					failed++
					continue
				case releaseVerificationStatePending:
					lastJobFailed = false
					// we need to process this
				default:
					glog.V(2).Infof("Unrecognized verification status %q for type %s on release %s", jobStatus.State, name, releaseTag.Name)
				}
				break
			}
			// find the next time, if ok run.
			if lastJobFailed && status.TransitionTime != nil {
				backoffDuration := calculateBackoff(failed-1, status.TransitionTime, &metav1.Time{Time: time.Now()})
				if backoffDuration > 0 {
					glog.V(6).Infof("%s: Release verification step %s failed %d times, last failure: %s, backoff till: %s",
						releaseTag.Name, name, failed, status.TransitionTime.Format(time.RFC3339), time.Now().Add(backoffDuration).Format(time.RFC3339))
					if retryQueueDelay == 0 || backoffDuration < retryQueueDelay {
						retryQueueDelay = backoffDuration
					}
					continue
				}
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
			status.TransitionTime = nil
		}

		if testStatus == nil {
			testStatus = VerificationStatusList{name: &CandidateTestStatus{Status: make([]*VerifyJobStatus, 0)}}
		} else if testStatus[name] == nil {
			testStatus[name] = &CandidateTestStatus{Status: make([]*VerifyJobStatus, 0)}
		} else if testStatus[name].Status == nil {
			testStatus[name].Status = make([]*VerifyJobStatus, 0)
		}

		if jobNo < len(testStatus[name].Status) {
			testStatus[name].Status[jobNo] = &VerifyJobStatus{State: status.State, URL: status.URL}
		} else {
			testStatus[name].Status = append(testStatus[name].Status, &VerifyJobStatus{State: status.State, URL: status.URL})
		}
		testStatus[name].TransitionTime = status.TransitionTime
		if jobNo >= test.MaxRetries {
			testStatus[name].TransitionTime = nil
			continue
		}

		if status.State == releaseVerificationStateFailed {
			// Queue for retry if at least one retryable job at earliest interval
			backoffDuration := calculateBackoff(failed, status.TransitionTime, &metav1.Time{Time: time.Now()})
			if retryQueueDelay == 0 || backoffDuration < retryQueueDelay {
				retryQueueDelay = backoffDuration
			}
		} else if status.State == releaseVerificationStateSucceeded {
			backoffDuration := time.Minute * 1
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
	return candidateTests, testStatus, nil
}

func isRallyPoint(v *imagev1.TagReference) bool {
	version, err := semver.Parse(v.Name)
	if err != nil {
		return false
	}
	return version.Patch%10 == 0
}

func (c *Controller) expandCandidateTests(release *Release, releaseTag *imagev1.TagReference) (map[string]*ReleaseCandidateTest, error) {
	// candidateTests stores expanded list of jobs that have run for testing status in sync loop
	candidateTests := make(map[string]*ReleaseCandidateTest)

	for name, testType := range release.Config.CandidateTests {
		if testType.Disabled {
			glog.V(2).Infof("Release testing step %s is disabled, ignoring", name)
			continue
		}

		switch {
		case testType.ProwJob != nil:
			if !testType.Upgrade {
				candidateTests[name] = testType
				continue
			}
			// if this is an upgrade job, find the appropriate source for the upgrade job
			upgradeSource, err := c.getUpgradeSource(release, releaseTag, name, testType.UpgradeFrom)
			if err != nil || len(upgradeSource) == 0 {
				return nil, err
			}

			for _, src := range upgradeSource {
				srcName := name
				if len(upgradeSource) > 1 {
					// Specify target for multiple upgrade tests
					srcName = fmt.Sprintf("%s-%s", name, src.tag)
				}
				retryStrategy := testType.RetryStrategy
				candidateTests[srcName] = &ReleaseCandidateTest {
					UpgradeTag:          src.tag,
					UpgradeRef:          src.pullSpec,
					RetryStrategy:       retryStrategy,
					ReleaseVerification: testType.ReleaseVerification,
				}
			}
		default:
			// manual verification
		}
	}
	return candidateTests, nil
}