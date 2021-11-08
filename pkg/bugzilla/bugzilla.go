package bugzilla

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"k8s.io/klog"
	"k8s.io/kube-openapi/pkg/util/sets"
	"k8s.io/test-infra/prow/bugzilla"
	"k8s.io/test-infra/prow/github"
	"k8s.io/test-infra/prow/plugins"
)

// Verifier takes a list of bugzilla bugs and uses the Bugzilla client to
// retrieve the associated GitHub PR via the bugzilla bug's external bug links.
// It then uses the github client to read the comments of the associated PR to
// determine whether the bug's QA Contact reviewed the GitHub PR. If yes, the bug
// gets marked as VERIFIED in Bugzilla.
type Verifier struct {
	// bzClient is used to retrieve external bug links and mark QA reviewed bugs as VERIFIED
	bzClient bugzilla.Client
	// ghClient is used to retrieve comments on a bug's PR
	ghClient github.Client
	// pluginConfig is used to check whether a repository allows approving reviews as LGTM
	pluginConfig *plugins.Configuration
}

// NewVerifier returns a Verifier configured with the provided github and bugzilla clients and the provided pluginConfig
func NewVerifier(bzClient bugzilla.Client, ghClient github.Client, pluginConfig *plugins.Configuration) *Verifier {
	return &Verifier{
		bzClient:     bzClient,
		ghClient:     ghClient,
		pluginConfig: pluginConfig,
	}
}

// pr contains the org, repo, and pr number for a pr
type pr struct {
	org   string
	repo  string
	prNum int
}

var (
	// bzAssignRegex matches the QA assignment comment made by the openshift-ci-robot
	bzAssignRegex = regexp.MustCompile(`Requesting review from QA contact:[[:space:]]+/cc @[[:alnum:]]+`)
	// from prow lgtm plugin
	lgtmRe       = regexp.MustCompile(`(?mi)^/lgtm(?: no-issue)?\s*$`)
	lgtmCancelRe = regexp.MustCompile(`(?mi)^/lgtm cancel\s*$`)
)

// VerifyBugs takes a list of bugzilla bug IDs and for each bug changes the bug status to VERIFIED if bug was reviewed and
// lgtm'd by the bug's QA Contect
func (c *Verifier) VerifyBugs(bugs []int, tagName string) []error {
	bzPRs, errs := getPRs(bugs, c.bzClient)
	for bugID, extPRs := range bzPRs {
		bug, err := c.bzClient.GetBug(bugID)
		if err != nil {
			errs = append(errs, fmt.Errorf("Unable to get bugzilla number %d: %v", bugID, err))
			continue
		}
		var success bool
		message := fmt.Sprintf("Bugfix included in accepted release %s", tagName)
		if bug.Status != "ON_QA" {
			// In case bug has already been moved to VERIFIED, completely ignore
			if bug.Status == "VERIFIED" {
				message = ""
			} else {
				message = fmt.Sprintf("%s\nBug is not in ON_QA status; bug will not be automatically moved to VERIFIED", message)
			}
		} else {
			var unapprovedPRs []pr
			var bugErrs []error
			for _, extPR := range extPRs {
				comments, err := c.ghClient.ListIssueComments(extPR.org, extPR.repo, extPR.prNum)
				if err != nil {
					newErr := fmt.Errorf("Unable to get comments for github pull %s/%s#%d: %v", extPR.org, extPR.repo, extPR.prNum, err)
					errs = append(errs, newErr)
					bugErrs = append(bugErrs, newErr)
					continue
				}
				reviews, err := c.ghClient.ListReviews(extPR.org, extPR.repo, extPR.prNum)
				if err != nil {
					newErr := fmt.Errorf("Unable to get reviews for github pull %s/%s#%d: %v", extPR.org, extPR.repo, extPR.prNum, err)
					errs = append(errs, newErr)
					bugErrs = append(bugErrs, newErr)
					continue
				}
				if !prReviewedByQA(comments, reviews, c.pluginConfig.LgtmFor(extPR.org, extPR.repo).ReviewActsAsLgtm) {
					unapprovedPRs = append(unapprovedPRs, extPR)
				}
			}
			if len(unapprovedPRs) > 0 || len(bugErrs) > 0 {
				message = fmt.Sprintf("%s\nBug will not be automatically moved to VERIFIED for the following reasons:", message)
				for _, extPR := range unapprovedPRs {
					message = fmt.Sprintf("%s\n- PR %s/%s#%d not approved by QA contact", message, extPR.org, extPR.repo, extPR.prNum)
				}
				for _, err := range bugErrs {
					message = fmt.Sprintf("%s\n- %s", message, err)
				}
				message = fmt.Sprintf("%s\n\nThis bug must now be manually moved to VERIFIED", message)
				// Sometimes the QAContactDetail is nil; if not nil, include name of QA contact in message
				if bug.QAContactDetail != nil {
					message = fmt.Sprintf("%s by %s", message, bug.QAContactDetail.Name)
				}
			} else {
				success = true
			}
		}
		if success {
			message = fmt.Sprintf("%s\nAll linked GitHub PRs have been approved by a QA contact; updating bug status to VERIFIED", message)
		}
		if message != "" {
			comments, err := c.bzClient.GetComments(bugID)
			if err != nil {
				errs = append(errs, fmt.Errorf("Failed to get comments on bug %d: %v", bug.ID, err))
				continue
			}
			var alreadyCommented bool
			for _, comment := range comments {
				if comment.Text == message && (comment.Creator == "openshift-bugzilla-robot" || comment.Creator == "openshift-bugzilla-robot@redhat.com") {
					alreadyCommented = true
					break
				}
			}
			if !alreadyCommented {
				if _, err := c.bzClient.CreateComment(&bugzilla.CommentCreate{ID: bugID, Comment: message, IsPrivate: true}); err != nil {
					errs = append(errs, fmt.Errorf("Failed to comment on bug %d: %v", bug.ID, err))
				}
			}
		}
		if success {
			klog.V(4).Infof("Updating bug %d (current status %s) to VERIFIED status", bug.ID, bug.Status)
			if err := c.bzClient.UpdateBug(bug.ID, bugzilla.BugUpdate{Status: "VERIFIED"}); err != nil {
				errs = append(errs, fmt.Errorf("Failed to update status for bug %d: %v", bug.ID, err))
			}
		} else {
			klog.V(4).Infof("Bug %d (current status %s) not approved by QA contact", bug.ID, bug.Status)
		}
	}
	return errs
}

// getPRs identifies bugzilla bugs and the associated github PRs fixed in a release from
// a given buglist generated by `oc adm release info --bugs=git-cache-path --ouptut=name from-tag to-tag`
func getPRs(input []int, bzClient bugzilla.Client) (map[int][]pr, []error) {
	bzPRs := make(map[int][]pr)
	var errs []error
	for _, bzID := range input {
		extBugs, err := bzClient.GetExternalBugPRsOnBug(bzID)
		if err != nil {
			// there are a couple of bugs with weird permissions issues that can cause this to fail; simply log instead of generating error
			if bugzilla.IsAccessDenied(err) {
				klog.V(4).Infof("Access denied getting external bugs for bugzilla bug %d: %v", bzID, err)
			} else {
				errs = append(errs, fmt.Errorf("Failed to get external bugs for bugzilla bug %d: %v", bzID, err))
			}
			continue
		}
		foundPR := false
		for _, extBug := range extBugs {
			if extBug.Type.URL == "https://github.com/" {
				if existingPRs, ok := bzPRs[bzID]; ok {
					bzPRs[bzID] = append(existingPRs, pr{org: extBug.Org, repo: extBug.Repo, prNum: extBug.Num})
				} else {
					bzPRs[bzID] = []pr{{org: extBug.Org, repo: extBug.Repo, prNum: extBug.Num}}
				}
				foundPR = true
			}
		}
		if !foundPR {
			// sometimes people ignore the bot and manually change the bugzilla tags, resulting in a bug not being linked; ignore these
			klog.V(5).Infof("Failed to identify associated GitHub PR for bugzilla bug %d", bzID)
		}
	}
	return bzPRs, errs
}

func updateLGTMs(lgtms map[string]time.Time, lgtmCancels map[string]time.Time, body, login string, submitted time.Time) {
	if lgtmRe.MatchString(body) {
		lgtms[login] = submitted
		return
	}
	if lgtmCancelRe.MatchString(body) {
		lgtmCancels[login] = submitted
		return
	}
}

// prReviewedByQA looks through PR comments and identifies if an assigned
// QA contact lgtm'd the PR
func prReviewedByQA(comments []github.IssueComment, reviews []github.Review, reviewAsLGTM bool) bool {
	lgtms := make(map[string]time.Time)
	lgtmCancels := make(map[string]time.Time)
	qaContacts := sets.NewString()
	for _, comment := range comments {
		updateLGTMs(lgtms, lgtmCancels, comment.Body, comment.User.Login, comment.UpdatedAt)
		bz := bzAssignRegex.FindString(comment.Body)
		if bz != "" {
			splitbz := strings.Split(bz, "@")
			if len(splitbz) == 2 {
				qaContacts.Insert(splitbz[1])
			}
		}
	}
	for _, review := range reviews {
		updateLGTMs(lgtms, lgtmCancels, review.Body, review.User.Login, review.SubmittedAt)
		if reviewAsLGTM {
			if review.State == github.ReviewStateApproved {
				lgtms[review.User.Login] = review.SubmittedAt
				continue
			}
			if review.State == github.ReviewStateChangesRequested {
				lgtmCancels[review.User.Login] = review.SubmittedAt
				continue
			}
		}
	}
	finalLGTMs := sets.NewString()
	for user, lgtmTime := range lgtms {
		if cancelTime, ok := lgtmCancels[user]; ok {
			if cancelTime.After(lgtmTime) {
				continue
			}
		}
		finalLGTMs.Insert(user)
	}
	for contact := range qaContacts {
		for lgtm := range finalLGTMs {
			if contact == lgtm {
				klog.V(4).Infof("QA Contact %s lgtm'd this PR", contact)
				return true
			}
		}
	}
	return false
}
