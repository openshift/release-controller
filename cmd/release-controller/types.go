package main

import (
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
	// Expires is the amount of time before release tags should be expired and
	// removed.
	Expires time.Duration
}

// ReleaseConfig is serialized in JSON as the release.openshift.io/config annotation
// on image streams that wish to have release payloads generated from them. It modifies
// how the release is calculated.
type ReleaseConfig struct {
	// Name is a required field and is used to associate release tags back to the input.
	// TODO: determining how naming should work.
	Name string `json:"name"`
}

const (
	// releaseImageStreamName is the hardcoded image stream that release images will
	// be pushed to.
	// TODO: make configurable
	releaseImageStreamName = "release"
)

const (
	// releasePhasePending is assigned to release tags that are waiting for an update
	// payload image to be created and pushed.
	//
	// This phase may transition to Aborted, Failed, or Ready
	releasePhasePending = "Pending"
	// releasePhaseAborted is for when multiple release tags are pending at once - all
	// but the newest release tag will be marked aborted.
	//
	// This phase is a terminal phase. Pending is the only input phase.
	releasePhaseAborted = "Aborted"
	// releasePhaseFailed occurs when an update payload image cannot be created for
	// a given set of image mirrors.
	//
	// This phase is a terminal phase. Pending is the only input phase.
	releasePhaseFailed = "Failed"
	// releasePhaseReady represents an image tag that has a valid update payload image
	// created and pushed to the release image stream. It may not have completed all
	// possible validation.
	//
	// This phase is currently a terminal phase. Pending is the only input phase.
	// TODO: transition to Verified.
	releasePhaseReady = "Ready"
	// TODO: must implement
	releasePhaseVerified = "Verified"

	// releaseAnnotationConfig is the JSON serialized representation of the ReleaseConfig
	// struct. It is only accepted on image streams. An image stream with this annotation
	// is considered an input image stream for creating releases.
	releaseAnnotationConfig = "release.openshift.io/config"

	releaseAnnotationGeneration        = "release.openshift.io/generation"
	releaseAnnotationSource            = "release.openshift.io/source"
	releaseAnnotationName              = "release.openshift.io/name"
	releaseAnnotationImageHash         = "release.openshift.io/hash"
	releaseAnnotationPhase             = "release.openshift.io/phase"
	releaseAnnotationCreationTimestamp = "release.openshift.io/creationTimestamp"

	releaseAnnotationReason  = "release.openshift.io/reason"
	releaseAnnotationMessage = "release.openshift.io/message"
)
