package bugzilla

import (
	"testing"

	"k8s.io/test-infra/prow/github"
)

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
		{
			name: "assigned qa lgtm'd and then cancelled",
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
				{
					Body: "/lgtm cancel",
					User: github.User{Login: "exampleUser3"},
				},
			},
			approved: false,
		},
		{
			name: "assigned qa approved review then requested changes",
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
				{
					State: github.ReviewStateChangesRequested,
					User:  github.User{Login: "exampleUser3"},
				},
			},
			approved: false,
		},
	}
	for _, testCase := range testCases {
		approved := prReviewedByQA(testCase.comments, testCase.reviews)
		if approved != testCase.approved {
			t.Errorf("%s: Expected %t, got %t", testCase.name, testCase.approved, approved)
		}
	}
}
