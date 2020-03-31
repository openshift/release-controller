package main

import (
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/util/diff"
	"k8s.io/test-infra/prow/github"
)

func TestGetBugzillaPRs(t *testing.T) {
	const exampleChangelog1 = `## Changes from 1.2.3-0.ci-1234

### Components

* Kubernetes 1.17.1

### [important-component](https://github.com/example/important-component/tree/shahash)

* [Bug 1234567](https://bugzilla.example.com/show_bug.cgi?id=1234567): Test change [#890](https://github.com/example/important-component/pull/890)
* [Full changelog](https://github.com/example/important-component/compare/shahash1...shahash2)

### [another-important-component](https://github.com/example/another/important-component/tree/shahash)

* [Bug 8901234](https://bugzilla.example.com/show_bug.cgi?id=8901234): Test change [#567](https://github.com/example/another-important-component/pull/567)
* [Full changelog](https://github.com/example/another-important-component/compare/shahash1...shahash2)
`

	expected := []BugzillaPR{
		{
			bugzillaID: 1234567,
			githubPR: GitHubPR{
				Org:  "example",
				Repo: "important-component",
				PR:   890,
			},
		},
		{
			bugzillaID: 8901234,
			githubPR: GitHubPR{
				Org:  "example",
				Repo: "another-important-component",
				PR:   567,
			},
		},
	}

	bugzillaPRs := getBugzillaPRs(exampleChangelog1)
	if !reflect.DeepEqual(bugzillaPRs, expected) {
		t.Errorf("getBugzillaPRs did not return expected result: %s", diff.ObjectReflectDiff(expected, bugzillaPRs))
	}
}

func TestPRApprovedByQA(t *testing.T) {
	testCases := []struct {
		name     string
		comments []github.IssueComment
		reviews  []github.Review
		approved bool
	}{
		{
			name: "no assigned qa",
			comments: []github.IssueComment{
				{
					Body: "Fixed Bug",
					User: github.User{Login: "exampleUser"},
				}, {
					Body: "/lgtm",
					User: github.User{Login: "exampleUser2"},
				},
			},
			reviews:  []github.Review{},
			approved: false,
		},
		{
			name: "assigned qa did not lgtm",
			comments: []github.IssueComment{
				{
					Body: "Fixed Bug",
					User: github.User{Login: "exampleUser"},
				},
				{
					Body: "Requesting review from QA contact:\n/cc @exampleUser3\n\n<details>plugin details</details>",
					User: github.User{Login: "exampleUser"},
				},
				{
					Body: "/lgtm",
					User: github.User{Login: "exampleUser2"},
				},
			},
			reviews:  []github.Review{},
			approved: false,
		},
		{
			name: "approve review not from qa",
			comments: []github.IssueComment{
				{
					Body: "Fixed Bug",
					User: github.User{Login: "exampleUser"},
				}, {
					Body: "Requesting review from QA contact:\n/cc @exampleUser3\n\n<details>plugin details</details>",
					User: github.User{Login: "exampleUser"},
				}, {
					Body: "/lgtm",
					User: github.User{Login: "exampleUser2"},
				},
			},
			reviews: []github.Review{
				{
					State: github.ReviewStateApproved,
					User:  github.User{Login: "exampleUser2"},
				},
			},
			approved: false,
		},
		{
			name: "assigned qa lgtm'd",
			comments: []github.IssueComment{
				{
					Body: "Fixed Bug",
					User: github.User{Login: "exampleUser"},
				},
				{
					Body: "Requesting review from QA contact:\n/cc @exampleUser3\n\n<details>plugin details</details>",
					User: github.User{Login: "exampleUser"},
				},
				{
					Body: "/lgtm",
					User: github.User{Login: "exampleUser3"},
				},
			},
			approved: true,
		},
		{
			name: "assigned qa reviewed",
			comments: []github.IssueComment{
				{
					Body: "Fixed Bug",
					User: github.User{Login: "exampleUser"},
				},
				{
					Body: "Requesting review from QA contact:\n/cc @exampleUser3\n\n<details>plugin details</details>",
					User: github.User{Login: "exampleUser"},
				},
			},
			reviews: []github.Review{
				{
					State: github.ReviewStateApproved,
					User:  github.User{Login: "exampleUser3"},
				},
			},
			approved: true,
		},
	}
	for _, testCase := range testCases {
		approved := prApprovedByQA(testCase.comments, testCase.reviews)
		if approved != testCase.approved {
			t.Errorf("%s: Expected %t, got %t", testCase.name, testCase.approved, approved)
		}
	}
}
