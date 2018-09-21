package main

import (
	"bytes"
	"fmt"
	"time"

	imagev1 "github.com/openshift/api/image/v1"
)

// Release holds information about the release used during processing.
type Release struct {
	// Source is the image stream that the Config was loaded from and holds all
	// images that will compose the release.
	Source *imagev1.ImageStream
	// Target is the image stream that the release tag will be pushed to. It is
	// modified and updated during processing by this controller to allow multiple
	// release modifications in a single 'sync' call.
	Target *imagev1.ImageStream
	// Config holds the release configuration parsed off of Source.
	Config *ReleaseConfig
}

// ReleaseConfig is serialized in JSON as the release.openshift.io/config annotation
// on image streams that wish to have release payloads generated from them. It modifies
// how the release is calculated.
type ReleaseConfig struct {
	// Name is a required field and is used to associate release tags back to the input.
	// TODO: determining how naming should work.
	Name string `json:"name"`

	// Expires is the amount of time as a golang duration before Accepted release tags
	// should be expired and removed. If unset, tags are not expired.
	Expires Duration `json:"expires"`

	// Verify is a map of short names to verification steps that must succeed before the
	// release is Accepted. Failures for some job types will cause the release to be
	// rejected.
	Verify map[string]ReleaseVerification `json:"verify"`

	// Publish is a map of short names to publish steps that will be performed after
	// the release is Accepted. Some publish steps are continuously maintained, others
	// may only be performed once.
	Publish map[string]ReleasePublish `json:"publish"`
}

// ReleasePublish defines one action to take when a release is Accepted.
type ReleasePublish struct {
	// TagRef updates the named tag in the release image stream to point at the release.
	TagRef *PublishTagReference `json:"tagRef"`
}

// PublishTagReference ensures that the release image stream has a tag that points to
// the most recent release.
type PublishTagReference struct {
	// Name is the name of the release image stream tag that will be updated to point to
	// (reference) the release tag.
	Name string `json:"name"`
}

// ReleaseVerification is a task that must be completed before a release is marked
// as Accepted. When some tasks fail the release will be marked as Rejected.
type ReleaseVerification struct {
	// Disabled will prevent this verification from being considered as blocking
	Disabled bool `json:"disabled"`
	// ProwJob requires that the named ProwJob from the prow config pass before the
	// release is accepted. The job is run only one time and if it fails the release
	// is rejected.
	ProwJob *ProwJobVerification `json:"prowJob"`
}

// ProwJobVerification identifies the name of a prow job that will be used to
// validate the release.
type ProwJobVerification struct {
	// Name of the prow job to verify
	Name string `json:"name"`
}

const (
	// releasePhasePending is assigned to release tags that are waiting for an update
	// payload image to be created and pushed.
	//
	// This phase may transition to Failed or Ready.
	releasePhasePending = "Pending"
	// releasePhaseFailed occurs when an update payload image cannot be created for
	// a given set of image mirrors.
	//
	// This phase is a terminal phase. Pending is the only input phase.
	releasePhaseFailed = "Failed"
	// releasePhaseReady represents an image tag that has a valid update payload image
	// created and pushed to the release image stream. It may not have completed all
	// possible verification.
	//
	// This phase may transition to Accepted or Rejected. Pending is the only input phase.
	releasePhaseReady = "Ready"
	// releasePhaseAccepted represents an image tag that has passed its verification
	// criteria and can safely be promoted to an external location.
	//
	// This phase is a terminal phase. Ready is the only input phase.
	releasePhaseAccepted = "Accepted"
	// releasePhaseRejected represents an image tag that has failed one or more of the
	// verification criteria.
	//
	// The controller will take no more action in this phase, but a human may set the
	// phase back to Ready to retry and the controller will attempt verification again.
	releasePhaseRejected = "Rejected"

	// releaseAnnotationConfig is the JSON serialized representation of the ReleaseConfig
	// struct. It is only accepted on image streams. An image stream with this annotation
	// is considered an input image stream for creating releases.
	releaseAnnotationConfig = "release.openshift.io/config"

	releaseAnnotationGeneration        = "release.openshift.io/generation"
	releaseAnnotationSource            = "release.openshift.io/source"
	releaseAnnotationName              = "release.openshift.io/name"
	releaseAnnotationReleaseTag        = "release.openshift.io/releaseTag"
	releaseAnnotationImageHash         = "release.openshift.io/hash"
	releaseAnnotationPhase             = "release.openshift.io/phase"
	releaseAnnotationCreationTimestamp = "release.openshift.io/creationTimestamp"
	releaseAnnotationVerifyURLs        = "release.openshift.io/verifyURLs"

	releaseAnnotationReason  = "release.openshift.io/reason"
	releaseAnnotationMessage = "release.openshift.io/message"
)

type Duration time.Duration

func (d *Duration) UnmarshalJSON(data []byte) error {
	if len(data) == 4 && bytes.Equal(data, []byte("null")) {
		return nil
	}
	if len(data) < 2 {
		return fmt.Errorf("invalid duration")
	}
	if data[0] != '"' || data[len(data)-1] != '"' {
		return fmt.Errorf("duration must be a string")
	}
	value, err := time.ParseDuration(string(data[1 : len(data)-1]))
	if err != nil {
		return err
	}
	*d = Duration(value)
	return nil
}

func (d Duration) Duration() time.Duration {
	return time.Duration(d)
}

type tagReferencesByAge []*imagev1.TagReference

func (a tagReferencesByAge) Less(i, j int) bool {
	return a[j].Annotations[releaseAnnotationCreationTimestamp] < a[i].Annotations[releaseAnnotationCreationTimestamp]
}
func (a tagReferencesByAge) Len() int      { return len(a) }
func (a tagReferencesByAge) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
