package jira

import (
	"fmt"
	"strconv"
	"strings"

	jiraBaseClient "github.com/andygrunwald/go-jira"
	"github.com/openshift-eng/jira-lifecycle-plugin/pkg/helpers"
	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog"
	"k8s.io/test-infra/prow/github"
	"k8s.io/test-infra/prow/jira"
	"k8s.io/test-infra/prow/plugins"
)

type githubClient interface {
	GetIssueLabels(org, repo string, number int) ([]github.Label, error)
	CreateComment(org, repo string, number int, comment string) error
	ListIssueComments(org, repo string, number int) ([]github.IssueComment, error)
}
type Verifier struct {
	// jiraClient is used to retrieve external issue links and mark QA reviewed issues as VERIFIED
	jiraClient jira.Client
	// ghClient is used to retrieve comments on a bug's PR
	ghClient githubClient
	// pluginConfig is used to check whether a repository allows approving reviews as LGTM
	pluginConfig *plugins.Configuration
}

// NewVerifier returns a Verifier configured with the provided github and jira clients and the provided pluginConfig
func NewVerifier(jiraClient jira.Client, ghClient githubClient, pluginConfig *plugins.Configuration) *Verifier {
	return &Verifier{
		jiraClient:   jiraClient,
		ghClient:     ghClient,
		pluginConfig: pluginConfig,
	}
}

type pr struct {
	org   string
	repo  string
	prNum int
}

func issueTargetReleaseCheck(issue *jiraBaseClient.Issue, tagRelease string, tagName string) (bool, error) {
	targetVersion, err := helpers.GetIssueTargetVersion(issue)
	if err != nil {
		klog.Warningf("Failed to get the target version for issue: %s", issue.Key)
		return true, nil
	}
	if targetVersion == nil {
		klog.Warningf("Issue %s does not have a target release", issue.Key)
		return true, nil
	}
	for _, element := range targetVersion {
		issueSplitVer := strings.Split(element.Name, ".")
		if len(issueSplitVer) < 2 {
			return true, fmt.Errorf("issue %s: length of target release `%s` after split by `.` is less than 2", issue.ID, element.Name)
		}
		issueRelease := fmt.Sprintf("%s.%s", issueSplitVer[0], issueSplitVer[1])
		if issueRelease != tagRelease {
			klog.Infof("Issue %s is in different release (%s) than tag %s", issue.Key, issueRelease, tagName)
			return true, nil
		}
		break
	}
	return false, nil
}

func (c *Verifier) ghUnlabeledPRs(extPR pr) ([]pr, error) {
	var unlabeledPRs []pr
	labels, err := c.ghClient.GetIssueLabels(extPR.org, extPR.repo, extPR.prNum)
	if err != nil {
		return unlabeledPRs, fmt.Errorf("unable to get labels for github pull %s/%s#%d: %w", extPR.org, extPR.repo, extPR.prNum, err)
	}
	var hasLabel bool
	for _, label := range labels {
		if label.Name == "qe-approved" {
			hasLabel = true
			break
		}
	}
	if !hasLabel {
		unlabeledPRs = append(unlabeledPRs, extPR)
	}
	return unlabeledPRs, nil
}

func (c *Verifier) commentOnPR(extPR pr, message string) (error, bool) {
	// Get the comments from that PR
	comments, err := c.ghClient.ListIssueComments(extPR.org, extPR.repo, extPR.prNum)
	if err != nil {
		return err, false
	}
	// Check to see if the same message has already been posted.
	for _, comment := range comments {
		if strings.Contains(comment.Body, message) {
			return nil, true
		}
	}
	// If the message hasn't already been posted, post it.
	err = c.ghClient.CreateComment(extPR.org, extPR.repo, extPR.prNum, message)
	if err != nil {
		return err, false
	}
	return nil, true
}

func (c *Verifier) verifyExtPRs(issue *jiraBaseClient.Issue, extPRs []pr, errs *[]error, tagName string) (ticketMessage string, isSuccess bool) {
	var success bool
	message := fmt.Sprintf("Fix included in accepted release %s", tagName)
	var unlabeledPRs []pr
	if !strings.EqualFold(issue.Fields.Status.Name, jira.StatusOnQA) {
		klog.V(4).Infof("Issue %s is in %s status; ignoring", issue.Key, issue.Fields.Status.Name)
		return message, false
	} else {
		for _, extPR := range extPRs {
			var newErr error
			unlabeledPRs, newErr = c.ghUnlabeledPRs(extPR)
			if newErr != nil {
				exists := false
				for _, tmpErr := range *errs {
					if tmpErr.Error() == "Internal Error on Automation" {
						exists = true
						break
					}
				}
				if !exists {
					*errs = append(*errs, fmt.Errorf("Internal Error on Automation"))
				}
				klog.Warning(newErr)
				return "", false
			}
			// Comment on the PR saying that this PR is included in the release
			prError, prSuccess := c.commentOnPR(extPR, message)
			if !prSuccess {
				klog.Warningf("Failed to comment to PR [%s/%s#%d]: %v", extPR.org, extPR.repo, extPR.prNum, prError)
			}
		}
	}
	if len(unlabeledPRs) > 0 || len(*errs) > 0 {
		message = fmt.Sprintf("%s\nJira issue will not be automatically moved to %s for the following reasons:", message, jira.StatusVerified)
		for _, extPR := range unlabeledPRs {
			message = fmt.Sprintf("%s\n- PR %s/%s#%d not approved by the QA Contact", message, extPR.org, extPR.repo, extPR.prNum)
		}
		for _, err := range *errs {
			message = fmt.Sprintf("%s\n- %s", message, err)
		}
		message = fmt.Sprintf("%s\n\nThis issue must now be manually moved to VERIFIED", message)
		qaContact, err := helpers.GetIssueQaContact(issue)
		if qaContact != nil && err == nil {
			message = fmt.Sprintf("%s by %s", message, qaContact.DisplayName)
		}
	} else {
		success = true
	}
	if success {
		message = fmt.Sprintf("%s\nAll linked GitHub PRs have been approved by a QA contact; updating bug status to VERIFIED", message)
	}
	return message, success
}

// VerifyIssues takes a list of jira issues IDs and for each issue changes the status to VERIFIED if the issue was
// reviewed and lgtm'd by the bug's QA Contact

func (c *Verifier) commentIssue(errs *[]error, issue *jiraBaseClient.Issue, message string) {
	if message == "" {
		return
	}
	comments, err := c.jiraClient.GetIssue(issue.ID)
	if err != nil {
		exists := false
		for _, tmpErr := range *errs {
			if tmpErr.Error() == "Internal Error on Automation" {
				exists = true
				break
			}
		}
		if !exists {
			*errs = append(*errs, fmt.Errorf("Internal Error on Automation"))
		}
		klog.Warningf("failed to get comments on issue %s: %v", issue.ID, err)
		return
	}
	for _, comment := range comments.Fields.Comments.Comments {
		// if a ticket is on the verified state but does not contain a comment from the bot, it will add one
		// if a manually verified ticket is already commented, we won't check the message body
		if (comment.Body == message || strings.EqualFold(issue.Fields.Status.Name, jira.StatusVerified)) && (comment.Author.Name == "openshift-crt-jira-release-controller" || comment.Author.EmailAddress == "brawilli+openshift-crt-jira-release-controller@redhat.com") {
			return
		}
	}

	restrictedComment := &jiraBaseClient.CommentVisibility{
		Type:  "group",
		Value: "Red Hat Employee",
	}
	if _, err := c.jiraClient.AddComment(issue.ID, &jiraBaseClient.Comment{Body: message, Visibility: *restrictedComment}); err != nil {
		*errs = append(*errs, fmt.Errorf("failed to comment on issue %s: %w", issue.ID, err))
	}
}

func (c *Verifier) SetFeatureFixedVersions(issues sets.Set[string], tagName, version string) []error {
	errs := []error{}
	for featureID := range issues {
		feature, err := c.jiraClient.GetIssue(featureID)
		if jira.JiraErrorStatusCode(err) == 403 {
			klog.Warningf("Permissions error getting issue %s; ignoring", featureID)
			continue
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("unable to get jira ID %s: %w", featureID, err))
			continue
		}
		fixVersion, err := c.determineFixVersion(feature, version)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if feature.Fields.FixVersions == nil || len(feature.Fields.FixVersions) == 0 {
			tmpIssue := &jiraBaseClient.Issue{
				Key: feature.Key,
				Fields: &jiraBaseClient.IssueFields{
					FixVersions: []*jiraBaseClient.FixVersion{{Name: fixVersion}},
				},
			}
			if _, err := c.jiraClient.UpdateIssue(tmpIssue); err != nil {
				errs = append(errs, fmt.Errorf("unable to update fix versions for feature %s: %v", featureID, err))
				continue
			}
			c.commentIssue(&errs, feature, fmt.Sprintf("Feature included in release %s; setting FixVersions to %s", tagName, version))
			klog.V(4).Infof("Jira feature issue %s Fix Versions updated to %s", feature.Key, fixVersion)
		} else {
			versions := []string{}
			for _, v := range feature.Fields.FixVersions {
				versions = append(versions, v.Name)
			}
			klog.V(4).Infof("Jira feature issue %s already set to [%s]; not updating", feature.Key, strings.Join(versions, ","))
		}
	}
	return errs
}

func (c *Verifier) VerifyIssues(issues []string, tagName string) []error {
	tagSemVer, err := releasecontroller.SemverParseTolerant(tagName)
	if err != nil {
		return []error{fmt.Errorf("failed to parse tag `%s` semver: %w", tagName, err)}
	}
	tagRelease := releasecontroller.SemverToMajorMinor(tagSemVer)
	jiraPRs, errs := getPRs(issues, c.jiraClient)
	for issueID, extPRs := range jiraPRs {
		issue, err := c.jiraClient.GetIssue(issueID)
		if jira.JiraErrorStatusCode(err) == 403 {
			klog.Warningf("Permissions error getting issue %s; ignoring", issueID)
			continue
		}
		if err != nil {
			exists := false
			for _, tmpErr := range errs {
				if tmpErr.Error() == "Internal Error on Automation" {
					exists = true
					break
				}
			}
			if !exists {
				errs = append(errs, fmt.Errorf("Internal Error on Automation"))
			}
			klog.Warningf("unable to get jira ID %s: %v", issueID, err)
			continue
		}
		checkTargetRelease, tagError := issueTargetReleaseCheck(issue, tagRelease, tagName)
		if checkTargetRelease {
			if tagError == nil {
				// the issue does not have a release tag
				continue
			}
			// the release tag format is not as expected
			errs = append(errs, tagError)
			continue
		}
		message, success := c.verifyExtPRs(issue, extPRs, &errs, tagName)
		if !strings.EqualFold(issue.Fields.Status.Name, jira.StatusOnQA) {
			if strings.EqualFold(issue.Fields.Status.Name, jira.StatusVerified) {
				c.commentIssue(&errs, issue, message)
			}
			continue
		}

		c.commentIssue(&errs, issue, message)

		if success {
			klog.V(4).Infof("Updating issue %s (current status %s) to VERIFIED status", issue.ID, issue.Fields.Status.Name)
			if err := c.jiraClient.UpdateStatus(issue.ID, jira.StatusVerified); err != nil {
				errs = append(errs, fmt.Errorf("failed to update status for issue %s: %w", issue.Key, err))
			}
		} else {
			klog.V(4).Infof("Jira issue %s (current status %s) not approved by QA contact", issue.Key, issue.Fields.Status.Name)
		}
	}
	return errs
}

// TODO - this should be moved to the jira-lifecycle-plugin
type identifierNotForPull struct {
	identifier string
}

// TODO - this should be moved to the jira jira-lifecycle-plugin
func (i identifierNotForPull) Error() string {
	return fmt.Sprintf("identifier %q is not for a pull request", i.identifier)
}

// TODO - this should be moved to the jira-lifecycle-plugin
func PullFromIdentifier(identifier string) (org, repo string, num int, err error) {
	identifier = strings.TrimPrefix(identifier, "https://github.com/")
	parts := strings.Split(identifier, "/")
	if len(parts) >= 3 && parts[2] != "pull" {
		return "", "", 0, &identifierNotForPull{identifier: identifier}
	}
	if len(parts) != 4 && !(len(parts) == 5 && (parts[4] == "" || parts[4] == "files")) && !(len(parts) == 6 && (parts[4] == "files" && parts[5] == "")) {
		return "", "", 0, fmt.Errorf("invalid pull identifier with %d parts: %q", len(parts), identifier)
	}
	number, err := strconv.Atoi(parts[3])
	if err != nil {
		return "", "", 0, fmt.Errorf("invalid pull identifier: could not parse %s as number: %w", parts[3], err)
	}

	return parts[0], parts[1], number, nil
}

// getPRs identifies jira issues and the associated github PRs fixed in a release from
// a given issue-list generated by `oc adm release info --bugs=git-cache-path --ouptut=name from-tag to-tag`
func getPRs(input []string, jiraClient jira.Client) (map[string][]pr, []error) {
	jiraPRs := make(map[string][]pr)
	var errs []error
	for _, jiraID := range input {
		extBugs, err := jiraClient.GetRemoteLinks(jiraID)
		if jira.JiraErrorStatusCode(err) == 403 {
			klog.Warningf("Permissions error getting issue %s; ignoring", jiraID)
			continue
		}
		if err != nil {
			exists := false
			for _, tmpErr := range errs {
				if tmpErr.Error() == "Internal Error on Automation" {
					exists = true
					break
				}
			}
			if !exists {
				errs = append(errs, fmt.Errorf("Internal Error on Automation"))
			}
			klog.Warningf("failed to get external bugs for jira issue %s: %v", jiraID, err)
			continue
		}
		foundPR := false
		for _, extBug := range extBugs {
			if strings.HasPrefix(extBug.Object.URL, "https://github.com/") {
				org, repo, num, err := PullFromIdentifier(extBug.Object.URL)
				if err != nil {
					klog.Warningf("failed to parse PR details from the identifier")
					continue
				}
				if existingPRs, ok := jiraPRs[jiraID]; ok {
					jiraPRs[jiraID] = append(existingPRs, pr{org: org, repo: repo, prNum: num})
				} else {
					jiraPRs[jiraID] = []pr{{org: org, repo: repo, prNum: num}}
				}
				foundPR = true
			}
		}
		if !foundPR {
			// sometimes people ignore the bot and manually change the jira tags, resulting in an issue not being linked; ignore these
			klog.V(5).Infof("Failed to identify associated GitHub PR for jira issue %s", jiraID)
		}
	}
	return jiraPRs, errs
}

func (c *Verifier) determineFixVersion(feature *jiraBaseClient.Issue, version string) (string, error) {
	versions, err := c.jiraClient.GetProjectVersions(feature.Fields.Project.Key)
	if jira.JiraErrorStatusCode(err) == 403 {
		return "", fmt.Errorf("permissions error getting project versions for jira issue: %s; ignoring", feature.Key)
	}
	if err != nil {
		return "", fmt.Errorf("unable to get project versions for jira issue %q: %w", feature.Key, err)
	}
	// Give preference to versions without any prefix...
	for _, v := range versions {
		if v.Name == version {
			return v.Name, nil
		}
	}
	// Then, versions with a prefix...
	for _, v := range versions {
		if v.Name == fmt.Sprintf("openshift-%s", version) {
			return v.Name, nil
		}
	}
	// If no match is found, return the version we calculated
	return version, nil
}
