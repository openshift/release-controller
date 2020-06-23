package bugzilla

import (
	"testing"
	"time"

	"k8s.io/test-infra/prow/github"
)

func TestPRApprovedByQA(t *testing.T) {
	testCases := []struct {
		name         string
		comments     []github.IssueComment
		reviews      []github.Review
		reviewAsLGTM bool
		approved     bool
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
			reviewAsLGTM: true,
			approved:     false,
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
			reviewAsLGTM: true,
			approved:     true,
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
					Body:      "/lgtm",
					User:      github.User{Login: "exampleUser3"},
					UpdatedAt: time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC),
				},
				{
					Body:      "/lgtm cancel",
					User:      github.User{Login: "exampleUser3"},
					UpdatedAt: time.Date(2020, time.January, 2, 0, 0, 0, 0, time.UTC),
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
					State:       github.ReviewStateApproved,
					User:        github.User{Login: "exampleUser3"},
					SubmittedAt: time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC),
				},
				{
					State:       github.ReviewStateChangesRequested,
					User:        github.User{Login: "exampleUser3"},
					SubmittedAt: time.Date(2020, time.January, 2, 0, 0, 0, 0, time.UTC),
				},
			},
			reviewAsLGTM: true,
			approved:     false,
		},
		{
			name: "assigned qa reviewed on repo without reviewAsLGTM",
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
			reviewAsLGTM: false,
			approved:     false,
		},
		{
			name: "assigned qa lgmt'd inside review on repo without reviewAsLGTM",
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
					Body:  "/lgtm",
					User:  github.User{Login: "exampleUser3"},
				},
			},
			reviewAsLGTM: false,
			approved:     true,
		},
	}
	for _, testCase := range testCases {
		approved := prReviewedByQA(testCase.comments, testCase.reviews, testCase.reviewAsLGTM)
		if approved != testCase.approved {
			t.Errorf("%s: Expected %t, got %t", testCase.name, testCase.approved, approved)
		}
	}
}
