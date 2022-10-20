package jira

import (
	"fmt"
	"strconv"
	"strings"

	jiraBaseClient "github.com/andygrunwald/go-jira"
	"github.com/openshift-eng/jira-lifecycle-plugin/pkg/helpers"
	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
	"k8s.io/klog"
	"k8s.io/test-infra/prow/github"
	"k8s.io/test-infra/prow/jira"
	"k8s.io/test-infra/prow/plugins"
)

type githubClient interface {
	GetIssueLabels(org, repo string, number int) ([]github.Label, error)
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

func (c *Verifier) verifyExtPRs(issue *jiraBaseClient.Issue, extPRs []pr, errs []error, tagName string) (bool, string, []error, bool) {
	var success bool
	message := fmt.Sprintf("Fix included in accepted release %s", tagName)
	var unlabeledPRs []pr
	var issueErrs []error
	if !strings.EqualFold(issue.Fields.Status.Name, jira.StatusOnQA) {
		// In case bug has already been moved to VERIFIED, completely ignore
		if strings.EqualFold(issue.Fields.Status.Name, jira.StatusVerified) {
			klog.V(4).Infof("Issue %s already in VERIFIED status", issue.Key)
			return true, "", nil, false
		}
		issueErrs = append(issueErrs, fmt.Errorf("issue is not in %s status", jira.StatusOnQA))
	} else {
		for _, extPR := range extPRs {
			var newErr error
			unlabeledPRs, newErr = c.ghUnlabeledPRs(extPR)
			if newErr != nil {
				errs = append(errs, newErr)
				issueErrs = append(issueErrs, newErr)
			}
		}
	}
	if len(unlabeledPRs) > 0 || len(issueErrs) > 0 {
		message = fmt.Sprintf("%s\nJira issue will not be automatically moved to %s for the following reasons:", message, jira.StatusVerified)
		for _, extPR := range unlabeledPRs {
			message = fmt.Sprintf("%s\n- PR %s/%s#%d not approved by the QA Contact", message, extPR.org, extPR.repo, extPR.prNum)
		}
		for _, err := range issueErrs {
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
	return false, message, errs, success
}

// VerifyIssues takes a list of jira issues IDs and for each issue changes the status to VERIFIED if the issue was
// reviewed and lgtm'd by the bug's QA Contact

func (c *Verifier) VerifyIssues(issues []string, tagName string) []error {
	tagSemVer, err := releasecontroller.SemverParseTolerant(tagName)
	if err != nil {
		return []error{fmt.Errorf("failed to parse tag `%s` semver: %w", tagName, err)}
	}
	tagRelease := releasecontroller.SemverToMajorMinor(tagSemVer)
	jiraPRs, errs := getPRs(issues, c.jiraClient)
	for issueID, extPRs := range jiraPRs {
		issue, err := c.jiraClient.GetIssue(issueID)
		if err != nil {
			errs = append(errs, fmt.Errorf("unable to get jira ID %s: %w", issueID, err))
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
		checkVerified, message, newErr, success := c.verifyExtPRs(issue, extPRs, errs, tagName)
		if checkVerified {
			// the issue is already verified
			continue
		}
		errs = append(errs, newErr...)
		if message != "" {
			// TODO - use the GetIssueWithOptions method when the PR is merged comments
			// err := c.jiraClient.GetIssueWithOptions(issueID, &jiraBaseClient.GetQueryOptions{Fields: "comment"})
			comments, err := c.jiraClient.GetIssue(issueID)
			if err != nil {
				errs = append(errs, fmt.Errorf("failed to get comments on issue %s: %w", issue.ID, err))
				continue
			}
			var alreadyCommented bool
			for _, comment := range comments.Fields.Comments.Comments {
				if comment.Body == message && (comment.Author.Name == "openshift-crt-jira-release-controller" || comment.Author.EmailAddress == "brawilli+openshift-crt-jira-release-controller@redhat.com") {
					alreadyCommented = true
					break
				}
			}
			if !alreadyCommented {
				restrictedComment := &jiraBaseClient.CommentVisibility{
					Type:  "group",
					Value: "Red Hat Employee",
				}
				if _, err := c.jiraClient.AddComment(issueID, &jiraBaseClient.Comment{Body: message, Visibility: *restrictedComment}); err != nil {
					errs = append(errs, fmt.Errorf("failed to comment on issue %s: %w", issue.ID, err))
				}
			}
		}
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
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to get external bugs for jira issue %s: %w", jiraID, err))
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
