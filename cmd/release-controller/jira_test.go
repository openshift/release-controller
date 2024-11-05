package main

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	v1 "github.com/openshift/api/image/v1"
	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
)

func TestJiraGetNonVerifiedTags(t *testing.T) {
	var testCases = []struct {
		name             string
		acceptedTags     []*v1.TagReference
		expectedCurrent  *v1.TagReference
		expectedPrevious *v1.TagReference
	}{{
		name:             "no tags",
		acceptedTags:     []*v1.TagReference{},
		expectedCurrent:  nil,
		expectedPrevious: nil,
	}, {
		name: "2 unverified tags",
		acceptedTags: []*v1.TagReference{{
			Name: "test2",
		}, {
			Name: "test1",
		}},
		expectedCurrent:  &v1.TagReference{Name: "test1"},
		expectedPrevious: nil,
	}, {
		name: "2 tags, 1 verified",
		acceptedTags: []*v1.TagReference{{
			Name: "test2",
		}, {
			Name: "test1",
			Annotations: map[string]string{
				releasecontroller.ReleaseAnnotationIssuesVerified: "true",
			},
		}},
		expectedCurrent: &v1.TagReference{Name: "test2"},
		expectedPrevious: &v1.TagReference{
			Name: "test1",
			Annotations: map[string]string{
				releasecontroller.ReleaseAnnotationIssuesVerified: "true",
			},
		},
	}, {
		name: "Multiple tags",
		acceptedTags: []*v1.TagReference{{
			Name:        "test4",
			Annotations: map[string]string{},
		}, {
			Name: "test3",
			Annotations: map[string]string{
				releasecontroller.ReleaseAnnotationIssuesVerified: "true",
			},
		}, {
			Name: "test2",
			Annotations: map[string]string{
				releasecontroller.ReleaseAnnotationIssuesVerified: "true",
			},
		}, {
			Name: "test1",
			Annotations: map[string]string{
				releasecontroller.ReleaseAnnotationIssuesVerified: "true",
			},
		}},
		expectedCurrent: &v1.TagReference{
			Name:        "test4",
			Annotations: map[string]string{},
		},
		expectedPrevious: &v1.TagReference{
			Name: "test3",
			Annotations: map[string]string{
				releasecontroller.ReleaseAnnotationIssuesVerified: "true",
			},
		},
	}, {
		name: "Multiple tags with unverified in between",
		acceptedTags: []*v1.TagReference{{
			Name:        "test4",
			Annotations: map[string]string{},
		}, {
			Name: "test3",
			Annotations: map[string]string{
				releasecontroller.ReleaseAnnotationIssuesVerified: "true",
			},
		}, {
			Name:        "test2",
			Annotations: map[string]string{},
		}, {
			Name: "test1",
			Annotations: map[string]string{
				releasecontroller.ReleaseAnnotationIssuesVerified: "true",
			},
		}},
		expectedCurrent: &v1.TagReference{
			Name:        "test2",
			Annotations: map[string]string{},
		},
		expectedPrevious: &v1.TagReference{
			Name: "test1",
			Annotations: map[string]string{
				releasecontroller.ReleaseAnnotationIssuesVerified: "true",
			},
		},
	}}
	for _, testCase := range testCases {
		current, previous := getNonVerifiedTagsJira(testCase.acceptedTags)
		if diff := cmp.Diff(testCase.expectedCurrent, current); diff != "" {
			t.Errorf("unexpected difference between actual and expected: %s", diff)
		}
		if diff := cmp.Diff(testCase.expectedPrevious, previous); diff != "" {
			t.Errorf("unexpected difference between actual and previous: %s", diff)
		}
	}
}
