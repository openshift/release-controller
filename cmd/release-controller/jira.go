package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	jiraBaseClient "github.com/andygrunwald/go-jira"
	"github.com/blang/semver"
	v1 "github.com/openshift/api/image/v1"
	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
	"github.com/prometheus/client_golang/prometheus"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog"
	"k8s.io/test-infra/prow/jira"
)

const (
	jiraPrevReleaseUnset         = "previous_release_unset"
	jiraPrevImagestreamGetErr    = "previous_imagestream_get_error"
	jiraMissingTag               = "missing_tag"
	jiraNoRegistry               = "no_configured_registry"
	jiraUnableToGenerateBuglist  = "unable_to_generate_buglist"
	jiraVerifier                 = "verifier"
	jiraChangelogGeneration      = "changelog_generation"
	jiraChangelogUnmarshal       = "changelog_unmarshal"
	jiraIssuesParse              = "issues_parse"
	jiraIssuesUnmarshal          = "issues_unmarshal"
	jiraFeatureChildren          = "feature_children"
	jiraFeatureChildrenUnmarshal = "feature_children_unmarshal"
	jiraStableReleases           = "stable_releases"
	jiraFeatureVersion           = "features"
	jiraFailedAnnotation         = "failed_annotation"
	jiraImagestreamGetErr        = "imagestream_get_error"
)

// initializeJiraMetrics initializes all labels used by the jira error metrics to 0. This allows
// prometheus to output a non-zero rate when an error occurs (unset values becoming set is a `0` rate)
func initializeJiraMetrics(*prometheus.CounterVec) {
	jiraErrorMetrics.WithLabelValues(jiraPrevReleaseUnset).Add(0)
	jiraErrorMetrics.WithLabelValues(jiraPrevImagestreamGetErr).Add(0)
	jiraErrorMetrics.WithLabelValues(jiraMissingTag).Add(0)
	jiraErrorMetrics.WithLabelValues(jiraNoRegistry).Add(0)
	jiraErrorMetrics.WithLabelValues(jiraUnableToGenerateBuglist).Add(0)
	jiraErrorMetrics.WithLabelValues(jiraVerifier).Add(0)
	jiraErrorMetrics.WithLabelValues(jiraFailedAnnotation).Add(0)
	jiraErrorMetrics.WithLabelValues(jiraImagestreamGetErr).Add(0)
}

func getNonVerifiedTagsJira(acceptedTags []*v1.TagReference) (current, previous *v1.TagReference) {
	// get oldest non-verified tag to make sure none are missed
	// accepted tags are returned sorted with the latest release first; reverse, so we can get oldest non-verified release
	for i := 0; i < len(acceptedTags)/2; i++ {
		j := len(acceptedTags) - i - 1
		acceptedTags[i], acceptedTags[j] = acceptedTags[j], acceptedTags[i]
	}
	for index, tag := range acceptedTags {
		if anno, ok := tag.Annotations[releasecontroller.ReleaseAnnotationIssuesVerified]; !ok || anno != "true" {
			if index == 0 {
				return tag, nil
			}
			return tag, acceptedTags[index-1]
		}
	}
	return nil, nil
}

// syncJira checks whether fixed bugs in a release had their
// PR reviewed and approved by the QA contact for the bug
func (c *Controller) syncJira(key queueKey) error {
	defer func() {
		if err := recover(); err != nil {
			panic(err)
		}
	}()

	release, err := c.loadReleaseForSync(key.namespace, key.name)
	if err != nil || release == nil {
		klog.V(6).Infof("jira: could not load release for sync for %s/%s", key.namespace, key.name)
		return err
	}

	klog.V(6).Infof("checking if %v (%s) has verifyIssues set", key, release.Config.Name)

	// check if verifyIssues publish step is enabled for release
	var verifyIssues *releasecontroller.PublishVerifyIssues
	for _, publishType := range release.Config.Publish {
		if publishType.Disabled {
			continue
		}
		switch {
		case publishType.VerifyIssues != nil:
			verifyIssues = publishType.VerifyIssues
		}
		if verifyIssues != nil {
			break
		}
	}
	if verifyIssues == nil {
		klog.V(6).Infof("%v (%s) does not have verifyIssues set", key, release.Config.Name)
		return nil
	}

	klog.V(4).Infof("Verifying fixed issues in %s", release.Config.Name)

	// get accepted tags
	acceptedTags := releasecontroller.SortedRawReleaseTags(release, releasecontroller.ReleasePhaseAccepted)
	tag, prevTag := getNonVerifiedTagsJira(acceptedTags)
	if tag == nil {
		klog.V(6).Infof("jira: All accepted tags for %s have already been verified", release.Config.Name)
		return nil
	}
	if prevTag == nil {
		if verifyIssues.PreviousReleaseTag == nil {
			klog.V(2).Infof("jira error: previous release unset for %s", release.Config.Name)
			c.jiraErrorMetrics.WithLabelValues(jiraPrevReleaseUnset).Inc()
			return fmt.Errorf("jira error: previous release unset for %s", release.Config.Name)
		}
		stream, err := c.imageClient.ImageStreams(verifyIssues.PreviousReleaseTag.Namespace).Get(context.TODO(), verifyIssues.PreviousReleaseTag.Name, meta.GetOptions{})
		if err != nil {
			klog.V(2).Infof("jira: failed to get imagestream (%s/%s) when getting previous release for %s: %v", verifyIssues.PreviousReleaseTag.Namespace, verifyIssues.PreviousReleaseTag.Name, release.Config.Name, err)
			c.jiraErrorMetrics.WithLabelValues(jiraPrevImagestreamGetErr).Inc()
			return err
		}
		prevTag = releasecontroller.FindTagReference(stream, verifyIssues.PreviousReleaseTag.Tag)
		if prevTag == nil {
			klog.V(2).Infof("jira: failed to get tag %s in imagestream (%s/%s) when getting previous release for %s", verifyIssues.PreviousReleaseTag.Tag, verifyIssues.PreviousReleaseTag.Namespace, verifyIssues.PreviousReleaseTag.Name, release.Config.Name)
			c.jiraErrorMetrics.WithLabelValues(jiraMissingTag).Inc()
			return fmt.Errorf("failed to find tag %s in imagestream %s/%s", verifyIssues.PreviousReleaseTag.Tag, verifyIssues.PreviousReleaseTag.Namespace, verifyIssues.PreviousReleaseTag.Name)
		}
	}
	// To make sure all tags are up-to-date, requeue when non-verified tags are found; this allows us to make
	// sure tags that may not have been passed to this function (such as older tags) get processed,
	// and it also allows us to handle the case where the imagestream fails to update below.
	defer c.queue.AddAfter(key, time.Second)

	dockerRepo := release.Target.Status.PublicDockerImageRepository
	if len(dockerRepo) == 0 {
		klog.V(4).Infof("jira: release target %s does not have a configured registry", release.Target.Name)
		c.jiraErrorMetrics.WithLabelValues(jiraNoRegistry).Inc()
		return fmt.Errorf("jira: release target %s does not have a configured registry", release.Target.Name)
	}

	issues, err := c.releaseInfo.Bugs(dockerRepo+":"+prevTag.Name, dockerRepo+":"+tag.Name)
	var issueList []string
	for _, issue := range issues {
		if issue.Source == 1 {
			issueList = append(issueList, issue.ID)
		}
	}
	if err != nil {
		klog.V(4).Infof("Jira: Unable to generate bug list from %s to %s: %v", prevTag.Name, tag.Name, err)
		c.jiraErrorMetrics.WithLabelValues(jiraUnableToGenerateBuglist).Inc()
		return fmt.Errorf("jira: unable to generate bug list from %s to %s: %w", prevTag.Name, tag.Name, err)
	}
	var errs []error
	if errs := append(errs, c.jiraVerifier.VerifyIssues(issueList, tag.Name)...); len(errs) != 0 {
		klog.V(4).Infof("Error(s) in jira verifier: %v", utilerrors.NewAggregate(errs))
		c.jiraErrorMetrics.WithLabelValues(jiraVerifier).Inc()
		return utilerrors.NewAggregate(errs)
	}

	// Handle feature tags
	// Generate the change log from image digests; this should be pretty quick since the Bugs function was run recently
	changelogJSON, err := c.releaseInfo.ChangeLog(dockerRepo+":"+prevTag.Name, dockerRepo+":"+tag.Name, true)
	if err != nil {
		klog.V(4).Infof("Jira: Unable to generate changelog from %s to %s: %v", prevTag.Name, tag.Name, err)
		c.jiraErrorMetrics.WithLabelValues(jiraChangelogGeneration).Inc()
		return fmt.Errorf("jira: unable to generate changelog from %s to %s: %w", prevTag.Name, tag.Name, err)
	}
	var changelog releasecontroller.ChangeLog
	if err := json.Unmarshal([]byte(changelogJSON), &changelog); err != nil {
		klog.V(4).Infof("Jira: Unable to unmarshal changelog from %s to %s: %v", prevTag.Name, tag.Name, err)
		c.jiraErrorMetrics.WithLabelValues(jiraChangelogUnmarshal).Inc()
		return fmt.Errorf("jira: unable to unmarshal changelog from %s to %s: %w", prevTag.Name, tag.Name, err)
	}
	// Get issue details
	info, err := c.releaseInfo.IssuesInfo(changelogJSON)
	if err != nil {
		klog.V(4).Infof("Jira: Unable to parse issue info from changelog of %s to %s: %v", prevTag.Name, tag.Name, err)
		c.jiraErrorMetrics.WithLabelValues(jiraIssuesParse).Inc()
		return fmt.Errorf("jira: unable to parse issue info from changelog of %s to %s: %w", prevTag.Name, tag.Name, err)
	}

	var mapIssueDetails map[string]releasecontroller.IssueDetails
	if err := json.Unmarshal([]byte(info), &mapIssueDetails); err != nil {
		klog.V(4).Infof("Jira: Unable to unmarshal issue info from changelog of %s to %s: %v", prevTag.Name, tag.Name, err)
		c.jiraErrorMetrics.WithLabelValues(jiraIssuesUnmarshal).Inc()
		return fmt.Errorf("jira: unable to unmarshal issue info from changelog of %s to %s: %w", prevTag.Name, tag.Name, err)
	}

	parentFeatures := []string{}
	for key, details := range mapIssueDetails {
		if strings.HasPrefix(key, "OSDOCS-") || strings.HasPrefix(key, "PLMCORE-") {
			continue
		}
		if details.IssueType == releasecontroller.JiraTypeFeature {
			parentFeatures = append(parentFeatures, key)
		}
	}

	// this gets all children of the feature that are type `Epic`
	featureChildrenJSON, err := c.releaseInfo.GetFeatureChildren(parentFeatures, 10*time.Minute)
	if err != nil {
		klog.V(4).Infof("Jira: Error getting feature children: %v", err)
		c.jiraErrorMetrics.WithLabelValues(jiraFeatureChildren).Inc()
		return fmt.Errorf("jira: unable to get feature children: %v", err)
	}
	var featureChildren map[string][]jiraBaseClient.Issue
	if err := json.Unmarshal([]byte(featureChildrenJSON), &featureChildren); err != nil {
		klog.V(4).Infof("Jira: Error unmarhsalling feature children: %v", err)
		c.jiraErrorMetrics.WithLabelValues(jiraFeatureChildrenUnmarshal).Inc()
		return fmt.Errorf("jira: unable unmarshal feature children: %v", err)
	}

	fixVersionUpdateList := sets.New[string]()
	for _, issue := range issueList {
		if strings.HasPrefix(issue, "OSDOCS-") || strings.HasPrefix(issue, "PLMCORE-") {
			continue
		}
		fixVersionUpdateList = fixVersionUpdateList.Insert(issue)
		if mapIssueDetails[issue].Epic != "" && !(strings.HasPrefix(mapIssueDetails[issue].Epic, "OSDOCS-") || strings.HasPrefix(mapIssueDetails[issue].Epic, "PLMCORE-")) {
			fixVersionUpdateList = fixVersionUpdateList.Insert(mapIssueDetails[issue].Epic)
		}
	}
	for _, feature := range parentFeatures {
		unsetEpic := false
		for _, epic := range featureChildren[feature] {
			if strings.HasPrefix(epic.Key, "OSDOCS-") || strings.HasPrefix(epic.Key, "PLMCORE-") ||
				fixVersionUpdateList.Has(epic.Key) || (epic.Fields != nil && epic.Fields.FixVersions != nil && len(epic.Fields.FixVersions) > 0) {
				continue
			}
			unsetEpic = true
			break
		}
		if !unsetEpic {
			fixVersionUpdateList.Insert(feature)
		}
	}

	// figure out what fix version should be applied
	tagSemver := semver.MustParse(tag.Name)
	fixVersion := fmt.Sprintf("%d.%d.0", tagSemver.Major, tagSemver.Minor)
	stableReleases, err := releasecontroller.GetStableReleases(c.parsedReleaseConfigCache, c.eventRecorder, c.releaseLister)
	if err != nil {
		klog.V(4).Infof("Jira: Error getting stable releases: %v", err)
		c.jiraErrorMetrics.WithLabelValues(jiraStableReleases).Inc()
		return fmt.Errorf("jira: unable to get stable releases: %v", err)
	}
	stableVersionTag := findStableVersionTag(stableReleases, semver.MustParse(fixVersion))
	if stableVersionTag != nil {
		if timestamp, ok := stableVersionTag.Annotations[releasecontroller.ReleaseAnnotationCreationTimestamp]; ok {
			created, err := time.Parse(time.RFC3339, timestamp)
			if err == nil {
				// if 4.y.0 was published before this tag, update fixVersion to 4.y.z
				if created.Before(changelog.To.Created) {
					fixVersion = fmt.Sprintf("%d.%d.z", tagSemver.Major, tagSemver.Minor)
				}
			}
		}
	}

	if errs := append(errs, c.jiraVerifier.SetFeatureFixedVersions(fixVersionUpdateList, tag.Name, fixVersion)...); len(errs) != 0 {
		klog.V(4).Infof("Error(s) updating versions for completed features: %v", utilerrors.NewAggregate(errs))
		// Temporarily ignore errors in fixversion updating due to issues with various projects
		//c.jiraErrorMetrics.WithLabelValues(jiraFeatureVersion).Inc()
		//return utilerrors.NewAggregate(errs)
	}

	var lastErr error
	err = wait.PollImmediate(15*time.Second, 1*time.Minute, func() (bool, error) {
		// Get the latest version of ImageStream before trying to update annotations
		target, err := c.imageClient.ImageStreams(release.Target.Namespace).Get(context.TODO(), release.Target.Name, meta.GetOptions{})
		if err != nil {
			klog.V(4).Infof("Failed to get latest version of target release stream %s: %v", release.Target.Name, err)
			c.jiraErrorMetrics.WithLabelValues(jiraImagestreamGetErr).Inc()
			return false, err
		}
		tagToBeUpdated := releasecontroller.FindTagReference(target, tag.Name)
		if tagToBeUpdated == nil {
			klog.V(6).Infof("release %s no longer exists, cannot set annotation %s=true", tag.Name, releasecontroller.ReleaseAnnotationIssuesVerified)
			return false, fmt.Errorf("release %s no longer exists, cannot set annotation %s=true", tag.Name, releasecontroller.ReleaseAnnotationIssuesVerified)
		}
		if tagToBeUpdated.Annotations == nil {
			tagToBeUpdated.Annotations = make(map[string]string)
		}
		tagToBeUpdated.Annotations[releasecontroller.ReleaseAnnotationIssuesVerified] = "true"
		klog.V(6).Infof("Setting %s annotation to \"true\" for %s in imagestream %s/%s", releasecontroller.ReleaseAnnotationIssuesVerified, tag.Name, target.GetNamespace(), target.GetName())
		if _, err := c.imageClient.ImageStreams(target.Namespace).Update(context.TODO(), target, meta.UpdateOptions{}); err != nil {
			klog.V(4).Infof("Failed to update Jira annotation for tag %s in imagestream %s/%s: %v", tag.Name, target.GetNamespace(), target.GetName(), err)
			lastErr = err
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		if lastErr != nil && errors.Is(err, wait.ErrWaitTimeout) {
			err = lastErr
		}
		c.jiraErrorMetrics.WithLabelValues(jiraFailedAnnotation).Inc()
		return err
	}

	return nil
}

func findStableVersionTag(ref *releasecontroller.StableReferences, version semver.Version) *v1.TagReference {
	for _, stable := range ref.Releases {
		versions := releasecontroller.UnsortedSemanticReleaseTags(stable.Release, releasecontroller.ReleasePhaseAccepted)
		sort.Sort(versions)

		for _, v := range versions {
			if v.Version == nil {
				continue
			}
			if v.Version.EQ(version) {
				return v.Tag
			}
		}
	}
	return nil
}

// from cmd/release-controller-api/http.go
var statusComplete = sets.NewString(strings.ToLower(jira.StatusOnQA), strings.ToLower(jira.StatusVerified), strings.ToLower(jira.StatusModified), strings.ToLower(jira.StatusClosed))

func statusOnBuild(buildTimeStamp *time.Time, issueTimestamp time.Time, transitions []releasecontroller.Transition) bool {
	if !issueTimestamp.IsZero() && issueTimestamp.Before(*buildTimeStamp) {
		return true
	}
	status := getPastStatus(transitions, buildTimeStamp)
	if statusComplete.Has(strings.ToLower(status)) {
		return true
	}
	return false
}
func getPastStatus(transitions []releasecontroller.Transition, buildTime *time.Time) string {
	status := "New"
	for _, t := range transitions {
		if t.Time.After(*buildTime) {
			break
		}
		status = t.ToStatus
	}
	return status
}
