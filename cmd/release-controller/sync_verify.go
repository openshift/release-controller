package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"
	"strings"

	"github.com/blang/semver"
	"github.com/golang/glog"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

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
					if tags := sortedReleaseTags(release, releasePhaseAccepted); len(tags) > 0 {
						previousTag = tags[0].Name
						previousReleasePullSpec = release.Target.Status.PublicDockerImageRepository + ":" + previousTag
					}
				case releaseUpgradeFromPreviousMinor:
					if version, err := semver.Parse(releaseTag.Name); err == nil && version.Minor > 0 {
						version.Minor--
						if ref, err := c.stableReleases(); err == nil {
							for _, stable := range ref.Releases {
								versions := unsortedSemanticReleaseTags(stable.Release, releasePhaseAccepted)
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
								versions := unsortedSemanticReleaseTags(stable.Release, releasePhaseAccepted)
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

type additionalTestInstance struct {
	test  ReleaseAdditionalTest
	name  string
	jobNo int
}

func (c *Controller) ensureAdditionalTests(release *Release, releaseTag *imagev1.TagReference) (map[string]ReleaseAdditionalTest, ValidationStatusMap, error) {
	var verifyStatus ValidationStatusMap
	if verifyStatus == nil {
		if data := releaseTag.Annotations[releaseAnnotationAdditionalTests]; len(data) > 0 {
			verifyStatus = make(ValidationStatusMap)
			if err := json.Unmarshal([]byte(data), &verifyStatus); err != nil {
				glog.Errorf("Release %s has invalid verification status, ignoring: %v", releaseTag.Name, err)
			}
		}
	}

	additionalTests := make(map[string]ReleaseAdditionalTest)

	if strings.Contains(release.Config.Name, "nightly") {
		retryCount := 2
		var err error
		additionalTests, err = c.upgradeJobs(release, releaseTag, retryCount)
		if err != nil {
			return nil, nil, err
		}
	}

	for name, additionalTest := range release.Config.AdditionalTests {
		additionalTests[name] = additionalTest
	}

	jobsToRun := make(map[string]additionalTestInstance)

	for name, testType := range additionalTests {
		if testType.Disabled {
			glog.V(2).Infof("Release additional test step %s is disabled, ignoring", name)
			continue
		}
		switch {
		case testType.ProwJob != nil:
			switch testType.Retry.RetryStrategy {
			case RetryStrategyTillRetryCount, RetryStrategyFirstSuccess, RetryStrategyFirstFailure:
				// process this, ensure minimum number of results
			default:
				glog.Errorf("Release %s has invalid test %s: unrecognized retry strategy %s", releaseTag.Name, name, testType.Retry.RetryStrategy)
				continue
			}
			skipTest := false
			for jobNo := 0; jobNo < testType.Retry.RetryCount; jobNo++ {
				if skipTest {
					break
				}
				if verifyStatus == nil {
					verifyStatus = make(ValidationStatusMap)
				}
				if jobNo < len(verifyStatus[name]) {
					switch verifyStatus[name][jobNo].State {
					case releaseVerificationStateSucceeded:
						if testType.Retry.RetryStrategy == RetryStrategyFirstSuccess {
							skipTest = true
						}
						continue
					case releaseVerificationStateFailed:
						if testType.Retry.RetryStrategy == RetryStrategyFirstFailure {
							skipTest = true
						}
						continue
					case releaseVerificationStatePending:
						// Process this directly
						jobName := fmt.Sprintf("%s-%d", name, jobNo)
						job, err := c.ensureProwJobForAdditionalTest(release, jobName, testType, releaseTag)
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
						if len(verifyStatus[name]) <= jobNo {
							verifyStatus[name] = append(verifyStatus[name], status)
						} else {
							verifyStatus[name][jobNo] = status
						}
						continue
					default:
						glog.V(2).Infof("Unrecognized verification status %q for type %s on release %s", verifyStatus[name][jobNo].State, name, releaseTag.Name)
						skipTest = true
						continue
					}
				}
				jobName := fmt.Sprintf("%s-%d", name, jobNo)
				jobsToRun[jobName] = additionalTestInstance{
					test:  testType,
					name:  name,
					jobNo: jobNo,
				}
			}
		default:
			// manual verification
		}
	}

	totalPendingJobs := 0
	currentStreamPendingJobs := 0
	currentTagPendingJobs := 0
	releaseSource := release.Source.Namespace + "/" + release.Source.Name
	for _, job := range c.prowLister.List() {
		pj, ok := job.(*unstructured.Unstructured)
		if !ok {
			continue
		}
		status, ok := prowJobVerificationStatus(pj)
		if !ok || status.State != releaseVerificationStatePending {
			continue
		}
		totalPendingJobs++
		annotations := pj.GetAnnotations()
		if len(annotations) <= 0 {
			continue
		}
		if annotations[releaseAnnotationSource] == releaseSource {
			currentStreamPendingJobs++
		}
		if annotations[releaseAnnotationToTag] == releaseTag.Name {
			currentTagPendingJobs++
		}
	}
	maxJobs := 50
	maxJobsPerStream := 20
	maxJobsPerTag := 4

	allowedJobCount := maxJobs - totalPendingJobs
	if allowedJobCount > maxJobsPerTag-currentTagPendingJobs {
		allowedJobCount = maxJobsPerTag - currentTagPendingJobs
	}
	if allowedJobCount > maxJobsPerStream-currentStreamPendingJobs {
		allowedJobCount = maxJobsPerStream - currentStreamPendingJobs
	}
	if allowedJobCount < 0 {
		allowedJobCount = 0
	}

	for jobName, test := range jobsToRun {
		if allowedJobCount <= 0 {
			break
		}
		job, err := c.ensureProwJobForAdditionalTest(release, jobName, test.test, releaseTag)
		if err != nil {
			return nil, nil, err
		}
		status, ok := prowJobVerificationStatus(job)
		if !ok {
			return nil, nil, fmt.Errorf("unexpected error accessing prow job definition")
		}
		if status.State == releaseVerificationStateSucceeded {
			glog.V(2).Infof("Prow job %s for release %s succeeded, logs at %s", test.name, releaseTag.Name, status.URL)
		}
		if len(verifyStatus[test.name]) <= test.jobNo {
			verifyStatus[test.name] = append(verifyStatus[test.name], status)
		} else {
			verifyStatus[test.name][test.jobNo] = status
		}
		if status.State == releaseVerificationStatePending {
			allowedJobCount--
		}
	}
	return additionalTests, verifyStatus, nil
}

func (c *Controller) upgradeJobs(release *Release, releaseTag *imagev1.TagReference, retryCount int) (map[string]ReleaseAdditionalTest, error) {
	upgradeTests := make(map[string]ReleaseAdditionalTest)
	if releaseTag == nil || len(releaseTag.Annotations) == 0 || len(releaseTag.Annotations[releaseAnnotationKeep]) == 0 {
		return upgradeTests, nil
	}

	releaseVersion, err := semverParseTolerant(releaseTag.Name)
	if err != nil {
		return upgradeTests, nil
	}
	upgradesFound := make(map[string]int)
	upgrades := c.graph.UpgradesTo(releaseTag.Name)
	for _, u := range upgrades {
		upgradesFound[u.From]++
	}

	// Stable releases after the last rally point
	stable, err := c.stableReleases()
	if err != nil {
		return upgradeTests, err
	}
	prowJobPrefix := "release-openshift-origin-installer-e2e-aws-upgrade-"

	for _, r := range stable.Releases {
		releaseSource := fmt.Sprintf("%s/%s", r.Release.Source.Namespace, r.Release.Source.Name)
		for _, stableTag := range r.Release.Source.Spec.Tags {
			if stableTag.Annotations[releaseAnnotationPhase] != releasePhaseAccepted || stableTag.Annotations[releaseAnnotationSource] != releaseSource {
				continue
			}

			if len(stableTag.Name) == 0 || upgradesFound[stableTag.Name] >= retryCount {
				continue
			}

			stableVersion, err := semverParseTolerant(stableTag.Name)
			if err != nil || len(stableVersion.Pre) != 0 || len(stableVersion.Build) != 0 {
				// Only accept stable releases of the for <Major>.<Minor>.<Patch> for upgrade tests
				continue
			}
			if stableVersion.Major != releaseVersion.Major || stableVersion.Minor != releaseVersion.Minor {
				continue
			}

			fromImageStream, _ := c.findImageStreamByAnnotations(map[string]string{releaseAnnotationReleaseTag: stableTag.Name})
			if fromImageStream == nil {
				glog.Errorf("Unable to find image repository for %s", stableTag.Name)
				continue
			}
			if len(fromImageStream.Status.PublicDockerImageRepository) == 0 {
				continue
			}

			prowJobName := fmt.Sprintf("%s%d.%d", prowJobPrefix, stableVersion.Major, stableVersion.Minor)
			testName := fmt.Sprintf("%s-%s", prowJobPrefix, namespaceSafeHash(releaseTag.Name, stableVersion.String())[:15])

			upgradeTests[testName] = ReleaseAdditionalTest{
				ReleaseVerification: ReleaseVerification{
					Disabled: false,
					Optional: true,
					Upgrade:  true,
					ProwJob:  &ProwJobVerification{Name: prowJobName},
				},
				UpgradeTag: stableTag.Name,
				UpgradeRef: fromImageStream.Status.PublicDockerImageRepository,
				Retry: &RetryPolicy{
					RetryStrategy: RetryStrategyFirstSuccess,
					RetryCount:    retryCount,
				},
			}
		}
	}

	return upgradeTests, nil
}
