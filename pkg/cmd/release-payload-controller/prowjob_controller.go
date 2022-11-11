package release_payload_controller

import (
	"context"
	"fmt"
	"github.com/google/go-cmp/cmp"
	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	releasepayloadclient "github.com/openshift/release-controller/pkg/client/clientset/versioned/typed/release/v1alpha1"
	releasepayloadinformer "github.com/openshift/release-controller/pkg/client/informers/externalversions/release/v1alpha1"
	"github.com/openshift/release-controller/pkg/prow"
	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
	"github.com/openshift/release-controller/pkg/releasepayload/controller"
	"github.com/openshift/release-controller/pkg/releasepayload/utils"
	releasepayloadhelpers "github.com/openshift/release-controller/pkg/releasepayload/v1alpha1helpers"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	v1 "k8s.io/test-infra/prow/apis/prowjobs/v1"
	prowjobinformer "k8s.io/test-infra/prow/client/informers/externalversions/prowjobs/v1"
	prowjoblister "k8s.io/test-infra/prow/client/listers/prowjobs/v1"
	"k8s.io/test-infra/prow/kube"
	"reflect"
	"strings"

	"github.com/openshift/library-go/pkg/operator/events"
)

// ProwJobStatusController is responsible for watching ProwJobs, in the prow-namespace, and
// updating the respective ReleasePayload with the status, of the job, when it completes.
// The ProwJobStatusController watches for changes to the following resources:
//   - ProwJob
//
// and writes to the following locations:
//   - .status.blockingJobResults[].results
//   - .status.informingJobResults[].results
type ProwJobStatusController struct {
	*ReleasePayloadController

	prowJobLister prowjoblister.ProwJobLister
}

func NewProwJobStatusController(
	releasePayloadInformer releasepayloadinformer.ReleasePayloadInformer,
	releasePayloadClient releasepayloadclient.ReleaseV1alpha1Interface,
	prowJobInformer prowjobinformer.ProwJobInformer,
	eventRecorder events.Recorder,
) (*ProwJobStatusController, error) {
	c := &ProwJobStatusController{
		ReleasePayloadController: NewReleasePayloadController("ProwJob Status Controller",
			releasePayloadInformer,
			releasePayloadClient,
			eventRecorder.WithComponentSuffix("prowjob-status-controller"),
			workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "ProwJobStatusController")),
		prowJobLister: prowJobInformer.Lister(),
	}

	c.syncFn = c.sync
	c.cachesToSync = append(c.cachesToSync, prowJobInformer.Informer().HasSynced)

	prowJobFilter := func(obj interface{}) bool {
		if prowJob, ok := obj.(*v1.ProwJob); ok {
			if _, ok := prowJob.Labels[releasecontroller.ReleaseLabelVerify]; ok {
				if _, ok := prowJob.Labels[releasecontroller.ReleaseLabelPayload]; ok {
					return true
				}
			}
		}
		return false
	}

	prowJobInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: prowJobFilter,
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    c.lookupReleasePayload,
			UpdateFunc: func(old, new interface{}) { c.lookupReleasePayload(new) },
			DeleteFunc: c.lookupReleasePayload,
		},
	})

	releasePayloadInformer.Informer().AddEventHandler(&cache.ResourceEventHandlerFuncs{
		AddFunc:    c.Enqueue,
		UpdateFunc: func(old, new interface{}) { c.Enqueue(new) },
		DeleteFunc: c.Enqueue,
	})

	return c, nil
}

func (c *ProwJobStatusController) lookupReleasePayload(obj interface{}) {
	object, ok := obj.(runtime.Object)
	if !ok {
		utilruntime.HandleError(fmt.Errorf("unable to cast obj: %v", obj))
		return
	}
	source, err := controller.GetAnnotation(object, releasecontroller.ReleaseAnnotationSource)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("unable to determine releasepayload key: %v", err))
		return
	}
	parts := strings.Split(source, "/")
	if len(parts) != 2 {
		utilruntime.HandleError(fmt.Errorf("invalid source with %d parts: %q", len(parts), source))
		return
	}
	release, err := controller.GetAnnotation(object, releasecontroller.ReleaseAnnotationToTag)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("unable to determine releasepayload key: %v", err))
		return
	}
	releasePayloadKey := fmt.Sprintf("%s/%s", parts[0], release)
	klog.V(4).Infof("Queueing ReleasePayload: %s", releasePayloadKey)
	c.queue.Add(releasePayloadKey)
}

type jobRunResultUpdate struct {
	ciConfigurationName    string
	ciConfigurationJobName string
	result                 *v1alpha1.JobRunResult
}

func (c *ProwJobStatusController) sync(ctx context.Context, key string) error {
	klog.V(4).Infof("Starting ProwJobStatusController sync")
	defer klog.V(4).Infof("ProwJobStatusController sync done")

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

	// If the prow coordinates are not set, then we cannot proceed...
	if len(originalReleasePayload.Spec.PayloadCreationConfig.ProwCoordinates.Namespace) == 0 {
		klog.Warning(fmt.Sprintf("unable to process prowjobs for releasepayload %q", originalReleasePayload.Name))
		return nil
	}

	labelSelector := labels.Set{
		releasecontroller.ReleaseAnnotationVerify: "true",
		releasecontroller.ReleaseLabelPayload:     originalReleasePayload.Name,
	}

	prowjobs, err := c.prowJobLister.ProwJobs(originalReleasePayload.Spec.PayloadCreationConfig.ProwCoordinates.Namespace).List(labels.SelectorFromSet(labelSelector))
	if err != nil {
		return err
	}

	var updates []*jobRunResultUpdate

	for _, prowJob := range prowjobs {
		details, err := utils.ParseReleaseVerificationJobName(prowJob.Name)
		if err != nil {
			klog.Warning(fmt.Sprintf("unable to parse prowjob name %q for releasepayload %q: %v", prowJob.Name, originalReleasePayload.Name, err))
			continue
		}
		ciConfigurationName := details.PreReleaseDetails.CIConfigurationName
		ciConfigurationJobName, ok := prowJob.Annotations[kube.ProwJobAnnotation]
		if !ok {
			klog.Warning(fmt.Sprintf("unable to process prowjob %q for releasepayload %q", prowJob.Name, originalReleasePayload.Name))
			continue
		}

		klog.V(4).Infof("Processing prowjob: %s", prowJob.Name)
		current := &v1alpha1.JobRunResult{
			Coordinates: v1alpha1.JobRunCoordinates{
				Name:      prowJob.Name,
				Namespace: prowJob.Namespace,
				Cluster:   prowJob.Spec.Cluster,
			},
			StartTime:           prowJob.Status.StartTime,
			CompletionTime:      prowJob.Status.CompletionTime,
			State:               getJobRunState(prowJob.Status.State),
			HumanProwResultsURL: prowJob.Status.URL,
		}

		jobStatus, err := findJobStatus(originalReleasePayload.Name, &originalReleasePayload.Status, ciConfigurationName, ciConfigurationJobName)
		if err != nil {
			klog.Warning(fmt.Sprintf("unable to locate jobStatus for releasepayload %q: %v", originalReleasePayload.Name, err))
			continue
		}

		klog.V(4).Infof("Processing JobRunResults for: %s (%s)", jobStatus.CIConfigurationName, jobStatus.CIConfigurationJobName)
		found := false
		for _, result := range jobStatus.JobRunResults {
			if result.Coordinates.Name == current.Coordinates.Name && result.Coordinates.Namespace == current.Coordinates.Namespace && result.Coordinates.Cluster == current.Coordinates.Cluster {
				found = true
				if !cmp.Equal(result, current) {
					updates = append(updates, &jobRunResultUpdate{
						ciConfigurationName:    ciConfigurationName,
						ciConfigurationJobName: ciConfigurationJobName,
						result:                 current,
					})
				}
			}
		}
		if !found {
			updates = append(updates, &jobRunResultUpdate{
				ciConfigurationName:    ciConfigurationName,
				ciConfigurationJobName: ciConfigurationJobName,
				result:                 current,
			})
		}
	}

	if len(updates) > 0 {
		releasePayload := originalReleasePayload.DeepCopy()

		for _, update := range updates {
			jobStatus, err := findJobStatus(releasePayload.Name, &releasePayload.Status, update.ciConfigurationName, update.ciConfigurationJobName)
			if err != nil {
				klog.Warning(fmt.Sprintf("unable to update jobStatus for releasepayload %q: %v", originalReleasePayload.Name, err))
				continue
			}
			setJobRunResult(&jobStatus.JobRunResults, *update.result)
			setJobStatus(&releasePayload.Status, *jobStatus)
		}

		releasepayloadhelpers.CanonicalizeReleasePayloadStatus(releasePayload)

		if reflect.DeepEqual(originalReleasePayload, releasePayload) {
			return nil
		}

		klog.V(4).Infof("Syncing prowjob results for ReleasePayload: %s/%s", releasePayload.Namespace, releasePayload.Name)
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

func getJobRunState(prowState v1.ProwJobState) v1alpha1.JobRunState {
	switch prowState {
	case v1.AbortedState:
		return v1alpha1.JobRunStateAborted
	case v1.ErrorState:
		return v1alpha1.JobRunStateError
	case v1.FailureState:
		return v1alpha1.JobRunStateFailure
	case v1.PendingState:
		return v1alpha1.JobRunStatePending
	case v1.SuccessState:
		return v1alpha1.JobRunStateSuccess
	case v1.TriggeredState:
		return v1alpha1.JobRunStateTriggered
	default:
		return v1alpha1.JobRunStateUnknown
	}
}

func setJobStatus(status *v1alpha1.ReleasePayloadStatus, jobStatus v1alpha1.JobStatus) {
	for i, job := range status.BlockingJobResults {
		if job.CIConfigurationName == jobStatus.CIConfigurationName && job.CIConfigurationJobName == jobStatus.CIConfigurationJobName {
			status.BlockingJobResults[i] = jobStatus
			return
		}
	}
	for i, job := range status.InformingJobResults {
		if job.CIConfigurationName == jobStatus.CIConfigurationName && job.CIConfigurationJobName == jobStatus.CIConfigurationJobName {
			status.InformingJobResults[i] = jobStatus
			return
		}
	}
}

func getJobStatus(payload, ciConfigurationName, ciConfigurationJobName string, results []v1alpha1.JobStatus) *v1alpha1.JobStatus {
	for _, jobStatus := range results {
		hash := prow.ProwjobSafeHash(fmt.Sprintf("%s-%s", payload, jobStatus.CIConfigurationName))
		if (jobStatus.CIConfigurationName == ciConfigurationName && jobStatus.CIConfigurationJobName == ciConfigurationJobName) || (strings.Contains(ciConfigurationName, hash) && jobStatus.CIConfigurationJobName == ciConfigurationJobName) {
			return &jobStatus
		}
	}
	return nil
}

func findJobStatus(payload string, status *v1alpha1.ReleasePayloadStatus, ciConfigurationName, ciConfigurationJobName string) (*v1alpha1.JobStatus, error) {
	jobStatus := getJobStatus(payload, ciConfigurationName, ciConfigurationJobName, status.BlockingJobResults)
	if jobStatus != nil {
		return jobStatus, nil
	}
	jobStatus = getJobStatus(payload, ciConfigurationName, ciConfigurationJobName, status.InformingJobResults)
	if jobStatus != nil {
		return jobStatus, nil
	}
	return nil, fmt.Errorf("unable to locate job results for %s (%s)", ciConfigurationName, ciConfigurationJobName)
}

func setJobRunResult(results *[]v1alpha1.JobRunResult, newResult v1alpha1.JobRunResult) {
	if results == nil {
		results = &[]v1alpha1.JobRunResult{}
	}

	existingResult := findJobRunResult(*results, newResult.Coordinates)
	if existingResult == nil {
		*results = append(*results, newResult)
		return
	}

	existingResult.StartTime = newResult.StartTime
	existingResult.CompletionTime = newResult.CompletionTime
	existingResult.State = newResult.State
	existingResult.HumanProwResultsURL = newResult.HumanProwResultsURL
}

func findJobRunResult(results []v1alpha1.JobRunResult, coordinates v1alpha1.JobRunCoordinates) *v1alpha1.JobRunResult {
	for i := range results {
		if results[i].Coordinates.Namespace == coordinates.Namespace &&
			results[i].Coordinates.Name == coordinates.Name &&
			results[i].Coordinates.Cluster == coordinates.Cluster {
			return &results[i]
		}
	}
	return nil
}
