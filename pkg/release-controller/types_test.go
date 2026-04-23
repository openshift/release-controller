package releasecontroller

import (
	"strings"
	"testing"

	imagev1 "github.com/openshift/api/image/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestIncomplete(t *testing.T) {
	testCases := []struct {
		name               string
		status             VerificationStatusMap
		required           map[string]ReleaseVerification
		expectIncomplete   bool
		expectIncludesName string
		expectExcludesName string
	}{
		{
			name: "async job not pending is not counted as incomplete",
			status: VerificationStatusMap{
				"blocking-job": {State: ReleaseVerificationStateSucceeded},
			},
			required: map[string]ReleaseVerification{
				"blocking-job": {},
				"async-job":    {Optional: true, Async: true},
			},
			expectIncomplete: false,
		},
		{
			name:   "async job with no status is not counted as incomplete",
			status: VerificationStatusMap{},
			required: map[string]ReleaseVerification{
				"async-job": {Optional: true, Async: true},
			},
			expectIncomplete: false,
		},
		{
			name:   "non-async optional job still counts as incomplete",
			status: VerificationStatusMap{},
			required: map[string]ReleaseVerification{
				"optional-job": {Optional: true},
			},
			expectIncomplete:   true,
			expectIncludesName: "optional-job",
		},
		{
			name:   "disabled job is not counted as incomplete",
			status: VerificationStatusMap{},
			required: map[string]ReleaseVerification{
				"disabled-job": {Disabled: true},
			},
			expectIncomplete: false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			names, incomplete := tc.status.Incomplete(tc.required)
			if incomplete != tc.expectIncomplete {
				t.Errorf("expected incomplete=%v, got %v (names: %v)", tc.expectIncomplete, incomplete, names)
			}
			if tc.expectIncludesName != "" {
				found := false
				for _, n := range names {
					if n == tc.expectIncludesName {
						found = true
					}
				}
				if !found {
					t.Errorf("expected names to include %q, got %v", tc.expectIncludesName, names)
				}
			}
			if tc.expectExcludesName != "" {
				for _, n := range names {
					if n == tc.expectExcludesName {
						t.Errorf("expected names to exclude %q, but it was present", tc.expectExcludesName)
					}
				}
			}
		})
	}
}

func TestVerificationJobsWithRetries_Async(t *testing.T) {
	testCases := []struct {
		name                    string
		jobs                    map[string]ReleaseVerification
		result                  VerificationStatusMap
		expectRetryNames        []string
		expectBlockingJobFailed bool
	}{
		{
			name: "async job failure does not cause blocking failure",
			jobs: map[string]ReleaseVerification{
				"async-job": {Optional: true, Async: true},
			},
			result: VerificationStatusMap{
				"async-job": {State: ReleaseVerificationStateFailed},
			},
			expectRetryNames:        nil,
			expectBlockingJobFailed: false,
		},
		{
			name: "async job not started is not added to retry list",
			jobs: map[string]ReleaseVerification{
				"async-job": {Optional: true, Async: true},
			},
			result:                  VerificationStatusMap{},
			expectRetryNames:        nil,
			expectBlockingJobFailed: false,
		},
		{
			name: "non-async blocking job failure is still detected",
			jobs: map[string]ReleaseVerification{
				"blocking-job": {MaxRetries: 0},
				"async-job":    {Optional: true, Async: true},
			},
			result: VerificationStatusMap{
				"blocking-job": {State: ReleaseVerificationStateFailed, Retries: 0},
				"async-job":    {State: ReleaseVerificationStateFailed},
			},
			expectBlockingJobFailed: true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			names, blockingFailed := VerificationJobsWithRetries(tc.jobs, tc.result)
			if blockingFailed != tc.expectBlockingJobFailed {
				t.Errorf("expected blockingJobFailed=%v, got %v", tc.expectBlockingJobFailed, blockingFailed)
			}
			if len(tc.expectRetryNames) == 0 && len(names) > 0 {
				t.Errorf("expected no retry names, got %v", names)
			}
		})
	}
}

func TestHasReferenceSpecTags(t *testing.T) {
	testCases := []struct {
		name   string
		is     *imagev1.ImageStream
		expect bool
	}{
		{
			name: "no tags returns false",
			is: &imagev1.ImageStream{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
			},
			expect: false,
		},
		{
			name: "tags without reference returns false",
			is: &imagev1.ImageStream{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Spec: imagev1.ImageStreamSpec{
					Tags: []imagev1.TagReference{
						{Name: "component-a"},
						{Name: "component-b"},
					},
				},
			},
			expect: false,
		},
		{
			name: "single tag with reference true returns true",
			is: &imagev1.ImageStream{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Spec: imagev1.ImageStreamSpec{
					Tags: []imagev1.TagReference{
						{Name: "component-a", Reference: true},
					},
				},
			},
			expect: true,
		},
		{
			name: "mixed tags with one reference true returns true",
			is: &imagev1.ImageStream{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Spec: imagev1.ImageStreamSpec{
					Tags: []imagev1.TagReference{
						{Name: "component-a"},
						{Name: "component-b", Reference: true},
						{Name: "component-c"},
					},
				},
			},
			expect: true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := HasReferenceSpecTags(tc.is)
			if got != tc.expect {
				t.Errorf("expected %v, got %v", tc.expect, got)
			}
		})
	}
}

func TestHashSpecTagImageDigests(t *testing.T) {
	testCases := []struct {
		name       string
		is         *imagev1.ImageStream
		expectSame *imagev1.ImageStream
		expectDiff *imagev1.ImageStream
	}{
		{
			name: "non-reference imagestream hashes from status tags",
			is: &imagev1.ImageStream{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Spec: imagev1.ImageStreamSpec{
					Tags: []imagev1.TagReference{
						{Name: "component-a"},
					},
				},
				Status: imagev1.ImageStreamStatus{
					Tags: []imagev1.NamedTagEventList{
						{Tag: "component-a", Items: []imagev1.TagEvent{{Image: "sha256:aaa"}}},
					},
				},
			},
			expectSame: &imagev1.ImageStream{
				ObjectMeta: metav1.ObjectMeta{Name: "test-copy"},
				Spec: imagev1.ImageStreamSpec{
					Tags: []imagev1.TagReference{
						{Name: "component-a"},
					},
				},
				Status: imagev1.ImageStreamStatus{
					Tags: []imagev1.NamedTagEventList{
						{Tag: "component-a", Items: []imagev1.TagEvent{{Image: "sha256:aaa"}}},
					},
				},
			},
			expectDiff: &imagev1.ImageStream{
				ObjectMeta: metav1.ObjectMeta{Name: "test-diff"},
				Spec: imagev1.ImageStreamSpec{
					Tags: []imagev1.TagReference{
						{Name: "component-a"},
					},
				},
				Status: imagev1.ImageStreamStatus{
					Tags: []imagev1.NamedTagEventList{
						{Tag: "component-a", Items: []imagev1.TagEvent{{Image: "sha256:bbb"}}},
					},
				},
			},
		},
		{
			name: "reference imagestream hashes from spec tags",
			is: &imagev1.ImageStream{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Spec: imagev1.ImageStreamSpec{
					Tags: []imagev1.TagReference{
						{
							Name:      "component-a",
							Reference: true,
							From:      &corev1.ObjectReference{Kind: "DockerImage", Name: "quay.io/org/repo@sha256:aaa"},
						},
					},
				},
			},
			expectSame: &imagev1.ImageStream{
				ObjectMeta: metav1.ObjectMeta{Name: "test-copy"},
				Spec: imagev1.ImageStreamSpec{
					Tags: []imagev1.TagReference{
						{
							Name:      "component-a",
							Reference: true,
							From:      &corev1.ObjectReference{Kind: "DockerImage", Name: "quay.io/org/repo@sha256:aaa"},
						},
					},
				},
			},
			expectDiff: &imagev1.ImageStream{
				ObjectMeta: metav1.ObjectMeta{Name: "test-diff"},
				Spec: imagev1.ImageStreamSpec{
					Tags: []imagev1.TagReference{
						{
							Name:      "component-a",
							Reference: true,
							From:      &corev1.ObjectReference{Kind: "DockerImage", Name: "quay.io/org/repo@sha256:bbb"},
						},
					},
				},
			},
		},
		{
			name: "reference imagestream ignores tags without From",
			is: &imagev1.ImageStream{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Spec: imagev1.ImageStreamSpec{
					Tags: []imagev1.TagReference{
						{Name: "no-from", Reference: true},
						{
							Name:      "has-from",
							Reference: true,
							From:      &corev1.ObjectReference{Kind: "DockerImage", Name: "quay.io/org/repo@sha256:aaa"},
						},
					},
				},
			},
			expectSame: &imagev1.ImageStream{
				ObjectMeta: metav1.ObjectMeta{Name: "test-same"},
				Spec: imagev1.ImageStreamSpec{
					Tags: []imagev1.TagReference{
						{Name: "no-from", Reference: true},
						{
							Name:      "has-from",
							Reference: true,
							From:      &corev1.ObjectReference{Kind: "DockerImage", Name: "quay.io/org/repo@sha256:aaa"},
						},
					},
				},
			},
			expectDiff: &imagev1.ImageStream{
				ObjectMeta: metav1.ObjectMeta{Name: "test-diff"},
				Spec: imagev1.ImageStreamSpec{
					Tags: []imagev1.TagReference{
						{Name: "no-from", Reference: true},
						{
							Name:      "has-from",
							Reference: true,
							From:      &corev1.ObjectReference{Kind: "DockerImage", Name: "quay.io/org/repo@sha256:ccc"},
						},
					},
				},
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			hash := HashSpecTagImageDigests(tc.is)
			if hash == "" {
				t.Fatal("expected non-empty hash")
			}
			if tc.expectSame != nil {
				sameHash := HashSpecTagImageDigests(tc.expectSame)
				if hash != sameHash {
					t.Errorf("expected same hash, got %q vs %q", hash, sameHash)
				}
			}
			if tc.expectDiff != nil {
				diffHash := HashSpecTagImageDigests(tc.expectDiff)
				if hash == diffHash {
					t.Errorf("expected different hash, both got %q", hash)
				}
			}
		})
	}
}
func TestReferencePayloadTag(t *testing.T) {
	testCases := []struct {
		name   string
		input  string
		expect string
	}{
		{
			name:   "nightly release name",
			input:  "4.17.0-0.nightly-2024-08-30-110931",
			expect: "rc_payload__4.17.0-0.nightly-2024-08-30-110931",
		},
		{
			name:   "ci release name",
			input:  "4.17.0-0.ci-2024-08-30-110931",
			expect: "rc_payload__4.17.0-0.ci-2024-08-30-110931",
		},
		{
			name:   "simple name",
			input:  "test",
			expect: "rc_payload__test",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := ReferencePayloadTag(tc.input)
			if got != tc.expect {
				t.Errorf("expected %q, got %q", tc.expect, got)
			}
			if !strings.HasPrefix(got, ReferencePayloadTagPrefix) {
				t.Errorf("expected prefix %q, got %q", ReferencePayloadTagPrefix, got)
			}
		})
	}
}

func TestReferenceRemovalTag(t *testing.T) {
	testCases := []struct {
		name   string
		input  string
		expect string
	}{
		{
			name:   "ci release name",
			input:  "4.22.0-0.ci-2026-01-30-070825",
			expect: "remove__rc_payload__4.22.0-0.ci-2026-01-30-070825",
		},
		{
			name:   "nightly release name",
			input:  "4.17.0-0.nightly-2024-08-30-110931",
			expect: "remove__rc_payload__4.17.0-0.nightly-2024-08-30-110931",
		},
		{
			name:   "simple name",
			input:  "test",
			expect: "remove__rc_payload__test",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := ReferenceRemovalTag(tc.input)
			if got != tc.expect {
				t.Errorf("expected %q, got %q", tc.expect, got)
			}
			if !strings.HasPrefix(got, ReferenceRemovalTagPrefix) {
				t.Errorf("expected prefix %q, got %q", ReferenceRemovalTagPrefix, got)
			}
			if !strings.HasPrefix(got, ReferenceRemovalTagPrefix+ReferencePayloadTagPrefix) {
				t.Errorf("expected combined prefix %q, got %q", ReferenceRemovalTagPrefix+ReferencePayloadTagPrefix, got)
			}
		})
	}
}
