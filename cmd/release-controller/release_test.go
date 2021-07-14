package main

import (
	"github.com/google/go-cmp/cmp"
	imagev1 "github.com/openshift/api/image/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"reflect"
	"testing"
)

func TestFindImportedCurrentStatusTag(t *testing.T) {
	testCases := []struct {
		name    string
		is      *imagev1.ImageStream
		tagName string
		want    *imagev1.TagEvent
	}{
		{
			name: "ImageStreamWithNoTags",
			is: &imagev1.ImageStream{
				Status: imagev1.ImageStreamStatus{
					Tags: []imagev1.NamedTagEventList{},
				},
			},
			tagName: "4.9.0-0.nightly-2021-07-13-162634",
			want:    nil,
		},
		{
			name: "ImageStreamMissingTag",
			is: &imagev1.ImageStream{
				Status: imagev1.ImageStreamStatus{
					Tags: []imagev1.NamedTagEventList{
						{
							Tag: "4.9",
						},
					},
				},
			},
			tagName: "4.9.0-0.nightly-2021-07-13-162634",
			want:    nil,
		},
		{
			name: "ImageStreamTagWithNoItems",
			is: &imagev1.ImageStream{
				Status: imagev1.ImageStreamStatus{
					Tags: []imagev1.NamedTagEventList{
						{
							Tag:   "4.9",
							Items: []imagev1.TagEvent{},
						},
					},
				},
			},
			tagName: "4.9.0-0.nightly-2021-07-13-162634",
			want:    nil,
		},
		{
			name: "ImageStreamTagWithImportFailureCondition",
			is: &imagev1.ImageStream{
				Status: imagev1.ImageStreamStatus{
					Tags: []imagev1.NamedTagEventList{
						{
							Tag: "4.9.0-0.nightly-2021-07-13-162634",
							Items: []imagev1.TagEvent{
								{
									DockerImageReference: "image-registry.openshift-image-registry.svc:5000/ocp/release@sha256:3b6bcf36253ca4391abd58db311a47d532a00cef26c634983681704f7ed88a57",
									Image:                "sha256:3b6bcf36253ca4391abd58db311a47d532a00cef26c634983681704f7ed88a57",
									Generation:           20,
								},
							},
							Conditions: []imagev1.TagEventCondition{
								{
									Type:   imagev1.ImportSuccess,
									Status: corev1.ConditionFalse,
								},
							},
						},
					},
				},
			},
			tagName: "4.9.0-0.nightly-2021-07-13-162634",
			want:    nil,
		},
		{
			name: "ImageStreamTagWithImportSuccessConditionNoMatchingSpecTag",
			is: &imagev1.ImageStream{
				Spec: imagev1.ImageStreamSpec{Tags: []imagev1.TagReference{}},
				Status: imagev1.ImageStreamStatus{
					Tags: []imagev1.NamedTagEventList{
						{
							Tag: "4.9.0-0.nightly-2021-07-13-162634",
							Items: []imagev1.TagEvent{
								{
									DockerImageReference: "image-registry.openshift-image-registry.svc:5000/ocp/release@sha256:3b6bcf36253ca4391abd58db311a47d532a00cef26c634983681704f7ed88a57",
									Image:                "sha256:3b6bcf36253ca4391abd58db311a47d532a00cef26c634983681704f7ed88a57",
									Generation:           20,
								},
							},
							Conditions: []imagev1.TagEventCondition{
								{
									Type:   imagev1.ImportSuccess,
									Status: corev1.ConditionTrue,
								},
							},
						},
					},
				},
			},
			tagName: "4.9.0-0.nightly-2021-07-13-162634",
			want: &imagev1.TagEvent{
				Created:              metav1.Time{},
				DockerImageReference: "image-registry.openshift-image-registry.svc:5000/ocp/release@sha256:3b6bcf36253ca4391abd58db311a47d532a00cef26c634983681704f7ed88a57",
				Image:                "sha256:3b6bcf36253ca4391abd58db311a47d532a00cef26c634983681704f7ed88a57",
				Generation:           20,
			},
		},
		{
			name: "ImageStreamTagWithImportSuccessConditionMatchingSpecTagGenerationNil",
			is: &imagev1.ImageStream{
				Spec: imagev1.ImageStreamSpec{
					Tags: []imagev1.TagReference{
						{
							Name:       "4.9.0-0.nightly-2021-07-13-162634",
							Generation: nil,
						},
					},
				},
				Status: imagev1.ImageStreamStatus{
					Tags: []imagev1.NamedTagEventList{
						{
							Tag: "4.9.0-0.nightly-2021-07-13-162634",
							Items: []imagev1.TagEvent{
								{
									DockerImageReference: "image-registry.openshift-image-registry.svc:5000/ocp/release@sha256:3b6bcf36253ca4391abd58db311a47d532a00cef26c634983681704f7ed88a57",
									Image:                "sha256:3b6bcf36253ca4391abd58db311a47d532a00cef26c634983681704f7ed88a57",
									Generation:           20,
								},
							},
							Conditions: []imagev1.TagEventCondition{
								{
									Type:   imagev1.ImportSuccess,
									Status: corev1.ConditionTrue,
								},
							},
						},
					},
				},
			},
			tagName: "4.9.0-0.nightly-2021-07-13-162634",
			want:    nil,
		},
		{
			name: "ImageStreamTagWithImportSuccessConditionMatchingSpecTagGenerationGreater",
			is: &imagev1.ImageStream{
				Spec: imagev1.ImageStreamSpec{
					Tags: []imagev1.TagReference{
						{
							Name:       "4.9.0-0.nightly-2021-07-13-162634",
							Generation: func(i int64) *int64 { return &i }(25),
						},
					},
				},
				Status: imagev1.ImageStreamStatus{
					Tags: []imagev1.NamedTagEventList{
						{
							Tag: "4.9.0-0.nightly-2021-07-13-162634",
							Items: []imagev1.TagEvent{
								{
									DockerImageReference: "image-registry.openshift-image-registry.svc:5000/ocp/release@sha256:3b6bcf36253ca4391abd58db311a47d532a00cef26c634983681704f7ed88a57",
									Image:                "sha256:3b6bcf36253ca4391abd58db311a47d532a00cef26c634983681704f7ed88a57",
									Generation:           20,
								},
							},
							Conditions: []imagev1.TagEventCondition{
								{
									Type:   imagev1.ImportSuccess,
									Status: corev1.ConditionTrue,
								},
							},
						},
					},
				},
			},
			tagName: "4.9.0-0.nightly-2021-07-13-162634",
			want:    nil,
		},
		{
			name: "ImageStreamTagWithImportSuccessConditionMatchingSpecTag",
			is: &imagev1.ImageStream{
				Spec: imagev1.ImageStreamSpec{
					Tags: []imagev1.TagReference{
						{
							Name:       "4.9.0-0.nightly-2021-07-13-162634",
							Generation: func(i int64) *int64 { return &i }(20),
						},
					},
				},
				Status: imagev1.ImageStreamStatus{
					Tags: []imagev1.NamedTagEventList{
						{
							Tag: "4.9.0-0.nightly-2021-07-13-162634",
							Items: []imagev1.TagEvent{
								{
									DockerImageReference: "image-registry.openshift-image-registry.svc:5000/ocp/release@sha256:3b6bcf36253ca4391abd58db311a47d532a00cef26c634983681704f7ed88a57",
									Image:                "sha256:3b6bcf36253ca4391abd58db311a47d532a00cef26c634983681704f7ed88a57",
									Generation:           20,
								},
							},
							Conditions: []imagev1.TagEventCondition{
								{
									Type:   imagev1.ImportSuccess,
									Status: corev1.ConditionTrue,
								},
							},
						},
					},
				},
			},
			tagName: "4.9.0-0.nightly-2021-07-13-162634",
			want:    &imagev1.TagEvent{
				Created:              metav1.Time{},
				DockerImageReference: "image-registry.openshift-image-registry.svc:5000/ocp/release@sha256:3b6bcf36253ca4391abd58db311a47d532a00cef26c634983681704f7ed88a57",
				Image:                "sha256:3b6bcf36253ca4391abd58db311a47d532a00cef26c634983681704f7ed88a57",
				Generation:           20,
			},
		},
	}

	t.Parallel()
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := findImportedCurrentStatusTag(tc.is, tc.tagName)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("%s", cmp.Diff(tc.want, got))
			}
		})
	}
}
