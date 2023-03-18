package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:subresource:status

// ReleasePayload encapsulates the information for the creation of a ReleasePayload
// and aggregates the results of its respective verification tests.
//
// The release-controller is configured to monitor imagestreams, in a specific namespace, that are annotated with a
// ReleaseConfig.  The ReleaseConfig is a definition of how releases are calculated.  When a ReleasePayload is
// generated, it will be generated in the same namespace as the imagstream that produced it. If/when an update
// occurs, to one of these imagestreams, the release-controller will:
//  1. Create a point-in-time mirror of the updated imagestream
//  2. Create a new Release from the mirror
//     - Any errors before this point will cause the release to marked `Failed`
//  3. Launches a set of release analysis jobs
//  4. Launches an aggregation job
//  5. Launches a set of release verification jobs
//     - These can either be `Blocking Jobs` which will prevent release acceptance or `Informing Jobs` which will
//     not prevent release acceptance.
//  6. For Stable releases, launches a set of jobs to test a subset of the supported upgrades defined inside the release
//     image itself.  While these jobs do not have any real bearing on the acceptance or rejection of a ReleasePayload,
//     they will be monitored and their respective results captured.  The hope would be to use these results to
//     provide a convenient way to override a "Rejected" release caused by a blocking upgrade job.
//  7. Monitors for job completions
//     - If all `Blocking Jobs` complete successfully, then the release is `Accepted`.  If any `Blocking Jobs` fail,
//     the release will be marked `Rejected`
//  8. Publishes all results to the respective webpage
//
// Example:
// ART:
//  1. Publishes an update to the `ocp/4.9-art-latest` imagestream
//
// Release-controller:
//  1. Creates a mirror named: `ocp/4.9-art-latest-2021-09-27-105859`
//  2. Creates a ReleasePayload: `ocp/4.9.0-0.nightly-2021-09-27-105859`
//     -Labels:
//     release.openshift.io/imagestream=release
//     release.openshift.io/imagestreamtag-name=4.9.0-0.nightly-2021-09-27-105859
//  3. Creates an OpenShift Release: `ocp/release:4.9.0-0.nightly-2021-09-27-105859`
//  4. Update ReleasePayload conditions with results of release creation job
//     If the release was created successfully, the release-controller:
//  5. Launches: 4.9.0-0.nightly-2021-09-27-105859-aggregated-<name>-analysis-<count>
//  6. Launches: 4.9.0-0.nightly-2021-09-27-105859-aggregated-<name>-aggregator
//  7. Launches: 4.9.0-0.nightly-2021-09-27-105859-<name>
//
// When ART promotes a GA release, they will assemble releases themselves, publish it to quay.io, and then update
// the "stable" release stream (i.e. ocp/release) with the corresponding payload.  In this scenario, the
// release-controller will perform all the same steps, mentioned above, but the mirror will be named after
// the "official" release (i.e. 4.9.7) and not not contain a timestamp.  Likewise, any verification tests will
// only contain the release name and the name of the verification test as defined in the ReleaseConfig.  The
// release-controller will also launch a small sample of jobs to test upgrades from the list of upgrade versions
// defined inside the release image itself:
//
// For example:
// $ oc adm release info quay.io/openshift-release-dev/ocp-release:4.11.22-x86_64
//
// The list of supported upgrades is:
// 4.10.16, 4.10.17, 4.10.18, 4.10.20, 4.10.21, 4.10.22, 4.10.23, 4.10.24, 4.10.25, 4.10.26, 4.10.27, 4.10.28, 4.10.29,
// 4.10.30, 4.10.31, 4.10.32, 4.10.33, 4.10.34, 4.10.35, 4.10.36, 4.10.37, 4.10.38, 4.10.39, 4.10.40, 4.10.41, 4.10.42,
// 4.10.43, 4.10.44, 4.10.45, 4.10.46, 4.10.47, 4.11.0, 4.11.1, 4.11.2, 4.11.3, 4.11.4, 4.11.5, 4.11.6, 4.11.7, 4.11.8,
// 4.11.9, 4.11.10, 4.11.11, 4.11.12, 4.11.13, 4.11.14, 4.11.16, 4.11.17, 4.11.18, 4.11.19, 4.11.20, 4.11.21
//
// From the list above, the release-controller will launch a subset of jobs named like:
// 4.11.22-upgrade-from-4.10.16-<platform>
// 4.11.22-upgrade-from-4.11.0-<platform>
//
// Where <platform> is defined in the Release Configuration definitions in the openshift/release repo:
// https://github.com/openshift/release/blob/e5c9122144c09c4095f0f87888b9685712dc7b1e/core-services/release-controller/_releases/release-ocp-4.y-stable.json#L20-L30
//
// Mapping from a Release to ReleasePayload:
// A ReleasePayload will always be named after the Release that it corresponds to, with the addition of a
// random string suffix.  Both objects will reside in the same namespace.
//
//	For a release: `ocp/release:4.9.0-0.nightly-2021-09-27-105859`
//	A corresponding ReleasePayload will exist: `ocp/4.9.0-0.nightly-2021-09-27-105859`
//
// Mapping from ReleasePayload to Release:
// A ReleasePayload is decorated with a couple labels that will point back to the Release that it corresponds to:
//   - release.openshift.io/imagestream=release
//   - release.openshift.io/imagestreamtag-name=4.9.0-0.nightly-2021-09-27-105859
//
// Because the ReleasePayload and the Release will both reside in the same namespace, the release that created the
// ReleasePayload will be located here:
//
//	<namespace>/<release.openshift.io/imagestream>:<release.openshift.io/imagestreamtag-name>
//
// Similarly, the ReleasePayload object itself also has the PayloadCoordinates (.spec.payloadCoordinates) that point
// back to the Release as well:
//
//	spec:
//	  payloadCoordinates:
//	    imagestreamName: release
//	    imagestreamTagName: 4.9.0-0.nightly-2021-09-27-105859
//	    namespace: ocp
//
// The release that created the ReleasePayload will be located here:
//
//	<namespace>/<imagestreamName>:<imagestreamTagName>
//
// Compatibility level 4: No compatibility is provided, the API can change at any point for any reason. These capabilities should not be used by applications needing long term support.
// +openshift:compatibility-gen:level=4
type ReleasePayload struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec the inputs used to create the ReleasePayload
	Spec ReleasePayloadSpec `json:"spec,omitempty"`

	// Status is the current status of the ReleasePayload
	Status ReleasePayloadStatus `json:"status,omitempty"`
}

// ReleasePayloadSpec has the information to represent a ReleasePayload
type ReleasePayloadSpec struct {
	// PayloadCoordinates the coordinates of the imagestreamtag that this ReleasePayload was created from
	PayloadCoordinates PayloadCoordinates `json:"payloadCoordinates,omitempty"`
	// PayloadCreationConfig the configuration used when creating the ReleasePayload
	PayloadCreationConfig PayloadCreationConfig `json:"payloadCreationConfig,omitempty"`
	// PayloadOverride specified when manual intervention is required to manually Accept or Reject a ReleasePayload
	PayloadOverride ReleasePayloadOverride `json:"payloadOverride,omitempty"`
	// PayloadVerificationConfig the configuration that will be used to verify this ReleasePayload
	PayloadVerificationConfig PayloadVerificationConfig `json:"payloadVerificationConfig,omitempty"`
}

// PayloadCoordinates houses the information pointing to the location of the imagesteamtag that this ReleasePayload
// is verifying.
//
// Example:
// For a ReleasePayload named: "4.9.0-0.nightly-2021-09-27-105859" in the "ocp" namespace, and configured
// to be written into the "release" imagestream, we expect:
//  1. Namespace to equal "ocp
//  2. ImagestreamName to equal "release"
//  3. ImagestreamTagName to equal "4.9.0-0.nightly-2021-09-27-105859", which will also serves as the prefix of the ReleasePayload
//
// These coordinates can then be used to get the release imagestreamtag itself:
//
//	# oc -n ocp get imagestreamtag release:4.9.0-0.nightly-2021-09-27-105859
type PayloadCoordinates struct {
	// Namespace must match that of the ReleasePayload
	Namespace string `json:"namespace,omitempty"`

	// ImagestreamName is the location of the configured "release" imagestream
	//   - This is a configurable parameter ("to") passed into the release-controller via the ReleaseConfig's defined here:
	//     https://github.com/openshift/release/blob/master/core-services/release-controller/_releases
	ImagestreamName string `json:"imagestreamName,omitempty"`

	// ImagestreamTagName is the name of the actual release
	ImagestreamTagName string `json:"imagestreamTagName,omitempty"`
}

// PayloadCreationConfig the configuration used to create the ReleasePayload
type PayloadCreationConfig struct {
	// ReleaseCreationCoordinates houses the configuration of the release creation job
	ReleaseCreationCoordinates ReleaseCreationCoordinates `json:"releaseCreationCoordinates,omitempty"`

	// ProwCoordinates houses the configuration for Prow
	ProwCoordinates ProwCoordinates `json:"prowCoordinates,omitempty"`
}

// ReleaseCreationCoordinates houses the information pointing to the location of the release creation job
// responsible for creating this ReleasePayload.
type ReleaseCreationCoordinates struct {
	// Namespace the namespace where the release creation batchv1.Jobs are created
	Namespace string `json:"namespace"`

	// ReleaseCreationJobName the name the release creation batchv1.Job
	ReleaseCreationJobName string `json:"releaseCreationJobName"`
}

// ProwCoordinates houses the information pointing to the location where Prow creates the release
// verification prowv1.ProwJobs.
type ProwCoordinates struct {
	// Namespace the namespace where Prow is configured to run prowv1.ProwJobs
	Namespace string `json:"namespace"`
}

type ReleasePayloadOverrideType string

// These are the supported ReleasePayloadOverride values.
const (
	// ReleasePayloadOverrideAccepted enables the manual Acceptance of a ReleasePayload.
	ReleasePayloadOverrideAccepted ReleasePayloadOverrideType = "Accepted"

	// ReleasePayloadOverrideRejected enables the manual Rejection of a ReleasePayload.
	ReleasePayloadOverrideRejected ReleasePayloadOverrideType = "Rejected"
)

// ReleasePayloadOverride provides the ability to manually Accept/Reject a ReleasePayload
// ART, occasionally, needs the ability to manually accept/reject a Release that, for some reason or another:
//   - won't pass one or more of it's blocking jobs.
//   - shouldn't proceed with the normal release verification processing
//
// This would be the one scenario where another party, besides the release-controller, would update a
// ReleasePayload instance.  Upon doing so, the release-controller should see that an update occurred and make all
// the necessary changes to formally accept/reject the respective release.
type ReleasePayloadOverride struct {
	// Override specifies the ReleasePayloadOverride to apply to the ReleasePayload
	Override ReleasePayloadOverrideType `json:"override"`

	// Reason is a human-readable string that specifies the reason for manually overriding the
	// Acceptance/Rejections of a ReleasePayload
	Reason string `json:"reason,omitempty"`
}

// PayloadVerificationConfig specifies the configuration used to verify the ReleasePayload
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.payloadVerificationDataSource) || has(self.payloadVerificationDataSource)", message="PayloadVerificationDataSource is required once set"
type PayloadVerificationConfig struct {
	// BlockingJobs are release verification jobs that will prevent a ReleasePayload from being Accepted if the job fails
	BlockingJobs []CIConfiguration `json:"blockingJobs,omitempty"`
	// InformingJobs are release verification jobs used to execute tests against a ReleasePayload
	InformingJobs []CIConfiguration `json:"informingJobs,omitempty"`
	// UpgradeJobs are automatically generated jobs used to execute upgrade tests against a ReleasePayload
	UpgradeJobs []CIConfiguration `json:"upgradeJobs,omitempty"`
	// PayloadVerificationDataSource where JobRunResult will be collected from.
	// +kubebuilder:default=BuildFarmLookup
	// +optional
	PayloadVerificationDataSource PayloadVerificationDataSource `json:"payloadVerificationDataSource,omitempty"`
}

// PayloadVerificationDataSource specifies the location where JobRunResult will be collected from
// If BuildFarmLookup, the results are expected to be picked up, in realtime, as ProwJobs execute on the various
// CI build farms.  This is the default value if not specified.
// If ImageStreamTagAnnotation, the results will be scrapped from the ImageStreamTag's annotations of the
// respective release.
// +kubebuilder:validation:Optional
// +kubebuilder:validation:Enum=BuildFarmLookup;ImageStreamTagAnnotation
// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="PayloadVerificationDataSource is immutable"
type PayloadVerificationDataSource string

const (
	// PayloadVerificationDataSourceBuildFarm payload verification results will be collected from the ProwJobs running
	// on the various build farms
	PayloadVerificationDataSourceBuildFarm PayloadVerificationDataSource = "BuildFarmLookup"
	// PayloadVerificationDataSourceImageStream payload verification results will be collected from respective release's
	// ImageStream Annotation
	PayloadVerificationDataSourceImageStream PayloadVerificationDataSource = "ImageStreamTagAnnotation"
)

// CIConfiguration is an Openshift CI system's job definition of a verification test to run against a ReleasePayload
type CIConfiguration struct {
	// CIConfigurationName the unique name given to a verification test.  This value will be used as the key to look up
	// the configuration and the results of the respective verification test
	CIConfigurationName string `json:"ciConfigurationName"`
	// CIConfigurationJobName is the actual name of the prowjob definition as stored in the CI Job Configuration.  This
	// value is used to lookup and read in the prowjob for processing by the release-controller
	CIConfigurationJobName string `json:"ciConfigurationJobName"`
	// MaxRetries Maximum retry attempts for the job. Defaults to 0 - do not retry on fail
	MaxRetries int `json:"maxRetries,omitempty"`
	// AnalysisJobCount Number of asynchronous jobs to execute for release analysis.
	AnalysisJobCount int `json:"analysisJobCount,omitempty"`
}

// ReleasePayloadStatus the status of all the promotion test jobs
type ReleasePayloadStatus struct {
	// Conditions communicates the state of the ReleasePayload.
	// Supported conditions include PayloadCreated, PayloadFailed, PayloadAccepted, and PayloadRejected.
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ReleaseCreationJobResult stores the coordinates and status of the release creation job that is
	// created, by the release-controller, to create the release imagestream defined by the PayloadCoordinates
	// in the ReleasePayloadSpec.  If the release creation job fails to get created or completes unsuccessfully,
	// the ReleasePayload will automatically be "Rejected".  If the release creation job is successful,
	// the release-controller will then begin the validation process.
	ReleaseCreationJobResult ReleaseCreationJobResult `json:"releaseCreationJobResult,omitempty"`

	// BlockingJobResults stores the results of all blocking jobs
	BlockingJobResults []JobStatus `json:"blockingJobResults,omitempty"`

	// InformingJobResults stores the results of all informing jobs
	InformingJobResults []JobStatus `json:"informingJobResults,omitempty"`

	// UpgradeJobResults stores the results of generated upgrade jobs
	UpgradeJobResults []JobStatus `json:"upgradeJobResults,omitempty"`
}

// These are valid condition types for ReleasePayloadStatus.
const (
	// ConditionPayloadCreated if false, the ReleasePayload is waiting for a release image to be created and pushed to the
	// TargetImageStream.  If PayloadCreated is true, a release image has been created and pushed to the TargetImageStream.
	// Verification jobs should begin and will update the status as they complete.
	ConditionPayloadCreated string = "PayloadCreated"

	// ConditionPayloadFailed is true if a ReleasePayload image cannot be created for the given set of image mirrors
	// This condition is terminal
	ConditionPayloadFailed string = "PayloadFailed"

	// ConditionPayloadAccepted is true if the ReleasePayload has passed its verification criteria and can safely
	// be promoted to an external location
	// This condition is terminal
	ConditionPayloadAccepted string = "PayloadAccepted"

	// ConditionPayloadRejected is true if the ReleasePayload has failed one or more of its verification criteria
	// The release-controller will take no more action in this phase.
	ConditionPayloadRejected string = "PayloadRejected"
)

// ReleaseCreationJobResult houses the information about the Release creation batch/v1 Job.  The release
// creation Job creates the actual release, via an `oc adm release` command.  The release-controller is
// responsible for launching the Job, in the --job-namespace, on the same cluster that the release-controller
// is running on.
type ReleaseCreationJobResult struct {
	// Coordinates the location of the batch/v1 Job
	Coordinates ReleaseCreationJobCoordinates `json:"coordinates,omitempty"`
	// Status is the current status of the release creation job
	Status ReleaseCreationJobStatus `json:"status,omitempty"`
	// Message is a human-readable message indicating details about the result of the release creation job
	Message string `json:"message,omitempty"`
}

// ReleaseCreationJobCoordinates houses the information necessary to locate the job execution
type ReleaseCreationJobCoordinates struct {
	Name      string `json:"name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
}

type ReleaseCreationJobStatus string

const (
	// ReleaseCreationJobUnknown means the job's current state is not known
	ReleaseCreationJobUnknown ReleaseCreationJobStatus = "Unknown"
	// ReleaseCreationJobSuccess means the job has completed its execution successfully
	ReleaseCreationJobSuccess ReleaseCreationJobStatus = "Success"
	// ReleaseCreationJobFailed means the job has failed its execution
	ReleaseCreationJobFailed ReleaseCreationJobStatus = "Failed"
)

// JobState the aggregate state of the job
// Supported values include Pending, Failed, Success, and Ignored.
type JobState string

const (
	// JobStatePending not all job runs have completed
	// Transitions to Failed or Success
	JobStatePending JobState = "Pending"

	// JobStateFailure failed job aggregation
	JobStateFailure JobState = "Failure"

	// JobStateSuccess successful job aggregation
	JobStateSuccess JobState = "Success"

	// JobStateUnknown unable to determine the JobState
	JobStateUnknown JobState = "Unknown"
)

// JobStatus encapsulates the name of the job, all the results of the jobs, and an aggregated
// result of all the jobs
type JobStatus struct {
	// CIConfigurationName the unique name given to a verification test
	CIConfigurationName string `json:"ciConfigurationName,omitempty"`

	// CIConfigurationJobName is the name of the prowjob definition as stored in the CI Job Configuration
	CIConfigurationJobName string `json:"ciConfigurationJobName,omitempty"`

	// MaxRetries maximum times to retry a job
	MaxRetries int `json:"maxRetries,omitempty"`

	// AnalysisJobCount Number of asynchronous jobs to execute for release analysis.
	AnalysisJobCount int `json:"analysisJobCount,omitempty"`

	// AggregateState is the overall success/failure of all the executed jobs
	AggregateState JobState `json:"state,omitempty"`

	// JobRunResults contains the links for individual jobs
	JobRunResults []JobRunResult `json:"results,omitempty"`
}

// JobRunState the status of a job
type JobRunState string

const (
	// JobRunStateTriggered job has been created but not scheduled
	JobRunStateTriggered JobRunState = "Triggered"

	// JobRunStatePending job is running and awaiting completion
	JobRunStatePending JobRunState = "Pending"

	// JobRunStateFailure job completed with errors
	JobRunStateFailure JobRunState = "Failure"

	// JobRunStateSuccess job completed without errors
	JobRunStateSuccess JobRunState = "Success"

	// JobRunStateAborted job was terminated early
	JobRunStateAborted JobRunState = "Aborted"

	// JobRunStateError job could not be scheduled
	JobRunStateError JobRunState = "Error"

	// JobRunStateUnknown unable to determine job state
	JobRunStateUnknown JobRunState = "Unknown"
)

// JobRunCoordinates houses the information necessary to locate individual job executions
type JobRunCoordinates struct {
	Name      string `json:"name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Cluster   string `json:"cluster,omitempty"`
}

// JobRunUpgradeType the type of upgrade performed via this job
type JobRunUpgradeType string

const (
	// JobRunUpgradeTypeUpgrade an upgrade from a release in the same Z stream (i.e. 4.11.0 to 4.11.22)
	JobRunUpgradeTypeUpgrade JobRunUpgradeType = "upgrade"

	// JobRunUpgradeTypeUpgradeMinor an upgrade from a previous minor release  (i.e. 4.11.22 to 4.12.0)
	JobRunUpgradeTypeUpgradeMinor JobRunUpgradeType = "upgrade-minor"
)

// JobRunResult the results of a prowjob run
// The release-controller creates ProwJobs (prowv1.ProwJob) during the sync_ready control loop and relies on an informer
// to process jobs, that it created, as they are completed. The JobRunResults will be created, by the release-controller
// during the sync_ready loop and updated whenever any changes, to the respective job is received by the informer.
type JobRunResult struct {
	// Coordinates the location of the job
	Coordinates JobRunCoordinates `json:"coordinates,omitempty"`

	// StartTime timestamp for when the prowjob was created
	StartTime metav1.Time `json:"startTime,omitempty"`

	// CompletionTime timestamp for when the prow pipeline controller observes the final state of the ProwJob
	// For instance, if a client Aborts a ProwJob, the Pipeline controller will receive notification of the change
	// and update the PtowJob's Status accordingly.
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// State the current state of the job run
	State JobRunState `json:"state,omitempty"`

	// HumanProwResultsURL the html link to the prow results
	HumanProwResultsURL string `json:"humanProwResultsURL,omitempty"`

	// UpgradeType the type of upgrade performed via this job
	UpgradeType JobRunUpgradeType `json:"upgradeType,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ReleasePayloadList is a list of ReleasePayloads
//
// Compatibility level 4: No compatibility is provided, the API can change at any point for any reason. These capabilities should not be used by applications needing long term support.
// +openshift:compatibility-gen:level=4
type ReleasePayloadList struct {
	metav1.TypeMeta `json:",inline"`

	// Standard list metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	// List of ReleasePayloads
	Items []ReleasePayload `json:"items"`
}
