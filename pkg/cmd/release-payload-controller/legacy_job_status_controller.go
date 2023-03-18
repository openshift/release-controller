package release_payload_controller

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/go-cmp/cmp"
	v1 "github.com/openshift/api/image/v1"
	imagev1informer "github.com/openshift/client-go/image/informers/externalversions/image/v1"
	imagev1lister "github.com/openshift/client-go/image/listers/image/v1"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	releasepayloadclient "github.com/openshift/release-controller/pkg/client/clientset/versioned/typed/release/v1alpha1"
	releasepayloadinformer "github.com/openshift/release-controller/pkg/client/informers/externalversions/release/v1alpha1"
	"github.com/openshift/release-controller/pkg/prow"
	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
	releasepayloadhelpers "github.com/openshift/release-controller/pkg/releasepayload/v1alpha1helpers"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"reflect"
	"strings"
)

// LegacyJobStatusController is responsible for handling the job results of releases that
// pre-date ReleasePayloads.  These releases have their job results stored in the
// "release.openshift.io/verify" annotation of their respective imagestreamtag.
//
// The results are a json encoded string of the VerificationStatusMap.  The key represents
// the CIConfigurationName of the job.  This is our only way of mapping the individual
// results back to their respective CIConfiguration.
//
// The LegacyJobStatusController operates only on the ReleasePayloads that specify:
//   - .spec.payloadVerificationConfig.legacyResults = True
//
// and writes to the following locations:
//   - .status.blockingJobResults[].results
//   - .status.informingJobResults[].results
//
// The legacy results are very much married to the ReleaseConfig that's defined on the
// "Source" imagestream when the respective release is created.  Processing of these
// results are only concerned with the Blocking and Informing jobs defined in the
// ReleaseConfig.  Therefore, this controller will not process anything related to the
// automatic upgrade jobs produced by the release-controller.
type LegacyJobStatusController struct {
	*ReleasePayloadController

	imageStreamLister imagev1lister.ImageStreamLister
}

func NewLegacyJobStatusController(
	releasePayloadInformer releasepayloadinformer.ReleasePayloadInformer,
	releasePayloadClient releasepayloadclient.ReleaseV1alpha1Interface,
	imageStreamInformer imagev1informer.ImageStreamInformer,
	eventRecorder events.Recorder,
) (*LegacyJobStatusController, error) {
	c := &LegacyJobStatusController{
		ReleasePayloadController: NewReleasePayloadController("Legacy Job Status Controller",
			releasePayloadInformer,
			releasePayloadClient,
			eventRecorder.WithComponentSuffix("legacy-job-status-controller"),
			workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "LegacyJobStatusController")),
		imageStreamLister: imageStreamInformer.Lister(),
	}

	c.syncFn = c.sync

	releasePayloadInformer.Informer().AddEventHandler(&cache.ResourceEventHandlerFuncs{
		AddFunc:    c.Enqueue,
		UpdateFunc: func(old, new interface{}) { c.Enqueue(new) },
		DeleteFunc: c.Enqueue,
	})

	return c, nil
}

func (c *LegacyJobStatusController) sync(ctx context.Context, key string) error {
	klog.V(4).Infof("Starting LegacyJobStatusController sync")
	defer klog.V(4).Infof("LegacyJobStatusController sync done")

	// Convert the namespace/name string into a distinct namespace and name
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	klog.V(4).Infof("Processing ReleasePayload: '%s/%s' from workQueue", namespace, name)

	// Get the ReleasePayload resource with this namespace/name
	originalReleasePayload, err := c.releasePayloadLister.ReleasePayloads(namespace).Get(name)
	// The ReleasePayload resource may no longer exist, in which case we stop processing.
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	// If PayloadVerificationDataSource is not the ImageStream Annotation, then we don't have anything to do...
	if originalReleasePayload.Spec.PayloadVerificationConfig.PayloadVerificationDataSource != v1alpha1.PayloadVerificationDataSourceImageStream {
		return nil
	}

	// Grab the verification results from the payload's imagestream...
	imageStream, err := c.imageStreamLister.ImageStreams(originalReleasePayload.Spec.PayloadCoordinates.Namespace).Get(originalReleasePayload.Spec.PayloadCoordinates.ImagestreamName)
	if err != nil {
		return err
	}
	verificationStatusMap, err := getVerificationJobResults(imageStream, originalReleasePayload.Spec.PayloadCoordinates.ImagestreamTagName)
	if err != nil {
		return err
	}
	// This should only ever happen if the verificationStatusMap is "null", which should never happen, but just in-case
	// we'll log a warning and move on...
	if verificationStatusMap == nil {
		klog.Warningf("unable to process verificationStatusMap for imagestreamtag: %s/%s:%s", imageStream.Namespace, imageStream.Name, originalReleasePayload.Spec.PayloadCoordinates.ImagestreamTagName)
		verificationStatusMap = make(releasecontroller.VerificationStatusMap)
	}

	// Process the results
	var updates []*jobRunResultUpdate

	for ciConfigurationName := range verificationStatusMap {
		status := verificationStatusMap[ciConfigurationName]

		klog.V(4).Infof("Processing legacy result for: %q", ciConfigurationName)
		current := &v1alpha1.JobRunResult{
			State:               getLegacyJobRunState(status.State),
			HumanProwResultsURL: status.URL,
		}

		jobStatus, err := findLegacyJobStatus(originalReleasePayload.Name, &originalReleasePayload.Status, ciConfigurationName)
		if err != nil {
			klog.Warning(fmt.Sprintf("unable to locate legacy jobStatus for releasepayload %q: %v", originalReleasePayload.Name, err))
			continue
		}

		klog.V(4).Infof("Processing legacy JobRunResults for: %s", jobStatus.CIConfigurationName)
		found := false
		for _, result := range jobStatus.JobRunResults {
			if result.State == current.State && result.HumanProwResultsURL == current.HumanProwResultsURL {
				found = true
				if !cmp.Equal(result, current) {
					updates = append(updates, &jobRunResultUpdate{
						ciConfigurationName: ciConfigurationName,
						result:              current,
					})
				}
			}
		}
		if !found {
			updates = append(updates, &jobRunResultUpdate{
				ciConfigurationName: ciConfigurationName,
				result:              current,
			})
		}
	}

	if len(updates) > 0 {
		releasePayload := originalReleasePayload.DeepCopy()

		for _, update := range updates {
			jobStatus, err := findLegacyJobStatus(releasePayload.Name, &releasePayload.Status, update.ciConfigurationName)
			if err != nil {
				klog.Warning(fmt.Sprintf("unable to update legacy jobStatus for releasepayload %q: %v", originalReleasePayload.Name, err))
				continue
			}
			setLegacyJobRunResult(&jobStatus.JobRunResults, *update.result)
			setLegacyJobStatus(&releasePayload.Status, *jobStatus)
		}

		releasepayloadhelpers.CanonicalizeReleasePayloadStatus(releasePayload)

		if reflect.DeepEqual(originalReleasePayload, releasePayload) {
			return nil
		}

		klog.V(4).Infof("Syncing legacy results for ReleasePayload: %s/%s", releasePayload.Namespace, releasePayload.Name)
		_, err = c.releasePayloadClient.ReleasePayloads(releasePayload.Namespace).UpdateStatus(ctx, releasePayload, metav1.UpdateOptions{})
		if errors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
	}

	return nil
}

func getVerificationJobResults(imageStream *v1.ImageStream, tagName string) (releasecontroller.VerificationStatusMap, error) {
	for i := range imageStream.Spec.Tags {
		tag := imageStream.Spec.Tags[i]

		if tag.Name == tagName {
			results, ok := tag.Annotations[releasecontroller.ReleaseAnnotationVerify]
			if !ok || len(results) == 0 {
				return nil, fmt.Errorf("unable to determine job status results for imagestreamtag: %s/%s:%s", imageStream.Namespace, imageStream.Name, tag.Name)
			}
			var status releasecontroller.VerificationStatusMap
			if err := json.Unmarshal([]byte(results), &status); err != nil {
				return nil, fmt.Errorf("unable to unmarshal VerificationStatusMap for imagestreamtag: %s/%s:%s", imageStream.Namespace, imageStream.Name, tag.Name)
			}
			return status, nil
		}
	}
	return nil, nil
}

func getLegacyJobRunState(state string) v1alpha1.JobRunState {
	switch state {
	case releasecontroller.ReleaseVerificationStateFailed:
		return v1alpha1.JobRunStateFailure
	case releasecontroller.ReleaseVerificationStatePending:
		return v1alpha1.JobRunStatePending
	case releasecontroller.ReleaseVerificationStateSucceeded:
		return v1alpha1.JobRunStateSuccess
	default:
		return v1alpha1.JobRunStateUnknown
	}
}

func findLegacyJobStatus(payload string, status *v1alpha1.ReleasePayloadStatus, ciConfigurationName string) (*v1alpha1.JobStatus, error) {
	jobStatus := getLegacyJobStatus(payload, ciConfigurationName, status.BlockingJobResults)
	if jobStatus != nil {
		return jobStatus, nil
	}
	jobStatus = getLegacyJobStatus(payload, ciConfigurationName, status.InformingJobResults)
	if jobStatus != nil {
		return jobStatus, nil
	}
	return nil, fmt.Errorf("unable to locate legacy job results for %s", ciConfigurationName)
}

func getLegacyJobStatus(payload, ciConfigurationName string, results []v1alpha1.JobStatus) *v1alpha1.JobStatus {
	for _, jobStatus := range results {
		hash := prow.ProwjobSafeHash(fmt.Sprintf("%s-%s", payload, jobStatus.CIConfigurationName))
		if (jobStatus.CIConfigurationName == ciConfigurationName) || strings.Contains(ciConfigurationName, hash) {
			return &jobStatus
		}
	}
	return nil
}

func setLegacyJobStatus(status *v1alpha1.ReleasePayloadStatus, jobStatus v1alpha1.JobStatus) {
	for i, job := range status.BlockingJobResults {
		if job.CIConfigurationName == jobStatus.CIConfigurationName {
			status.BlockingJobResults[i] = jobStatus
			return
		}
	}
	for i, job := range status.InformingJobResults {
		if job.CIConfigurationName == jobStatus.CIConfigurationName {
			status.InformingJobResults[i] = jobStatus
			return
		}
	}
}

func setLegacyJobRunResult(results *[]v1alpha1.JobRunResult, newResult v1alpha1.JobRunResult) {
	if results == nil {
		results = &[]v1alpha1.JobRunResult{}
	}

	existingResult := findLegacyJobRunResult(*results, newResult)
	if existingResult == nil {
		*results = append(*results, newResult)
		return
	}

	existingResult.State = newResult.State
	existingResult.HumanProwResultsURL = newResult.HumanProwResultsURL
}

func findLegacyJobRunResult(results []v1alpha1.JobRunResult, result v1alpha1.JobRunResult) *v1alpha1.JobRunResult {
	for i := range results {
		if results[i].State == result.State && results[i].HumanProwResultsURL == result.HumanProwResultsURL {
			return &results[i]
		}
	}
	return nil
}
