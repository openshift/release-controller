package jira

import (
	"encoding/json"
	"github.com/andygrunwald/go-jira"
	"k8s.io/test-infra/prow/jira/fakejira"
	"reflect"
	"testing"
)

func TestGetPRS(t *testing.T) {
	issue := jira.Issue{ID: "OCPBUGS-0000"}
	removeLinkArray := []jira.RemoteLink{
		{
			ID:           1234,
			Self:         "https://issues.redhat.com/rest/api/2/issue/OCPBUGSM-0000/remotelink/0000",
			GlobalID:     "EXTBZ-14641175-Red Hat Errata Tool-0000",
			Application:  nil,
			Relationship: "external trackers",
			Object: &jira.RemoteLinkObject{
				URL:   "https://errata.devel.redhat.com/advisory/0000",
				Title: "Red Hat Errata Tool 95802",
			},
		},
		{
			ID:           1234,
			Self:         "https://issues.redhat.com/rest/api/2/issue/OCPBUGSM-0000/remotelink/1234",
			GlobalID:     "EXTBZ-14641175-Github-openshift/kube-state-metrics/pull/000",
			Application:  nil,
			Relationship: "external trackers",
			Object: &jira.RemoteLinkObject{
				URL:   "https://github.com/openshift/kube-state-metrics/pull/000",
				Title: "Red Hat Errata Tool 95802",
			},
		},
	}
	remoteLinks := make(map[string][]jira.RemoteLink)
	remoteLinks["OCPBUGS-0000"] = removeLinkArray

	c := &fakejira.FakeClient{Issues: []*jira.Issue{&issue}, RemovedLinks: removeLinkArray, ExistingLinks: remoteLinks}

	extLinks, errors := getPRs([]string{"OCPBUGS-0000"}, c)

	if len(errors) != 0 {
		t.Fatalf("unexpected errors: %s", errors)
	}

	for key, value := range extLinks {
		if key != "OCPBUGS-0000" {
			t.Fatalf("unexpected key for external links: %s", key)
		}
		if len(value) != 1 {
			t.Fatalf("unexpected number of external links: %v", extLinks)
		}
		if !reflect.DeepEqual(value[0], pr{org: "openshift", repo: "kube-state-metrics", prNum: 0}) {
			t.Fatalf("unexpected value for the external links. Expecting: %v but got: %v", pr{org: "openshift", repo: "kube-state-metrics", prNum: 0}, value[0])
		}
	}
}

func TestIssueTargetReleaseCheck(t *testing.T) {
	issueJSON := "{\n \"id\":\"0000\",\n\"key\":\"OCPBUGS-0000\",\n\"fields\":{\n \"customfield_12319940\": [\n{\n\"name\": \"4.11.Z\"\n}\n]\n}\n}"

	var issue jira.Issue
	err := json.Unmarshal([]byte(issueJSON), &issue)
	if err != nil {
		t.Fatalf("failed to unmarshall test issue")
	}
	testCases := []struct {
		name       string
		tagRelease string
		expected   bool
	}{
		{
			name:       "Valid Tag",
			tagRelease: "4.11",
			expected:   false,
		},
		{
			name:       "Invalid tag",
			tagRelease: "4.12",
			expected:   true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			check, err := issueTargetReleaseCheck(&issue, tc.tagRelease, "test")
			if err != nil {
				t.Fatalf("unexpected errors: %s", err)
			}
			if check != tc.expected {
				t.Errorf("expected  %t but got %t for tagVersion: %v", tc.expected, check, tc.tagRelease)
			}
		})
	}
}
