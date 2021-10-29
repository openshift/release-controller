package main

import (
	"github.com/openshift/release-controller/pkg/release-controller"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	v1 "github.com/openshift/api/image/v1"
)

func TestGetNonVerifiedTags(t *testing.T) {
	testCases := []struct {
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
				release_controller.ReleaseAnnotationBugsVerified: "true",
			},
		}},
		expectedCurrent: &v1.TagReference{Name: "test2"},
		expectedPrevious: &v1.TagReference{
			Name: "test1",
			Annotations: map[string]string{
				release_controller.ReleaseAnnotationBugsVerified: "true",
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
				release_controller.ReleaseAnnotationBugsVerified: "true",
			},
		}, {
			Name: "test2",
			Annotations: map[string]string{
				release_controller.ReleaseAnnotationBugsVerified: "true",
			},
		}, {
			Name: "test1",
			Annotations: map[string]string{
				release_controller.ReleaseAnnotationBugsVerified: "true",
			},
		}},
		expectedCurrent: &v1.TagReference{
			Name:        "test4",
			Annotations: map[string]string{},
		},
		expectedPrevious: &v1.TagReference{
			Name: "test3",
			Annotations: map[string]string{
				release_controller.ReleaseAnnotationBugsVerified: "true",
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
				release_controller.ReleaseAnnotationBugsVerified: "true",
			},
		}, {
			Name:        "test2",
			Annotations: map[string]string{},
		}, {
			Name: "test1",
			Annotations: map[string]string{
				release_controller.ReleaseAnnotationBugsVerified: "true",
			},
		}},
		expectedCurrent: &v1.TagReference{
			Name:        "test2",
			Annotations: map[string]string{},
		},
		expectedPrevious: &v1.TagReference{
			Name: "test1",
			Annotations: map[string]string{
				release_controller.ReleaseAnnotationBugsVerified: "true",
			},
		},
	}}
	for _, testCase := range testCases {
		current, previous := getNonVerifiedTags(testCase.acceptedTags)
		if !reflect.DeepEqual(testCase.expectedCurrent, current) {
			t.Errorf("%s: actual current not equal to expected current: %s", testCase.name, cmp.Diff(testCase.expectedCurrent, current))
		}
		if !reflect.DeepEqual(testCase.expectedPrevious, previous) {
			t.Errorf("%s: actual previous not equal to expected previous: %s", testCase.name, cmp.Diff(testCase.expectedPrevious, previous))
		}
	}
}
