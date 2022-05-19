package release_payload_controller

import (
	"context"
	"fmt"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	"github.com/openshift/release-controller/pkg/client/clientset/versioned/fake"
	releasepayloadinformers "github.com/openshift/release-controller/pkg/client/informers/externalversions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"reflect"
	"testing"
)

func TestPayloadVerificationSync(t *testing.T) {
	testCases := []struct {
		name             string
		releaseNamespace string
		input            *v1alpha1.ReleasePayload
		expected         *v1alpha1.ReleasePayload
	}{{
		name:             "ReleasePayloadWithoutVerificationJobs",
		releaseNamespace: "ocp",
		input: &v1alpha1.ReleasePayload{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "4.11.0-0.nightly-2022-02-09-091559",
				Namespace: "ocp",
			},
			Spec: v1alpha1.ReleasePayloadSpec{
				PayloadCoordinates: v1alpha1.PayloadCoordinates{
					Namespace:          "ocp",
					ImagestreamName:    "release",
					ImagestreamTagName: "4.11.0-0.nightly-2022-02-09-091559",
				},
			},
		},
		expected: &v1alpha1.ReleasePayload{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "4.11.0-0.nightly-2022-02-09-091559",
				Namespace: "ocp",
			},
			Spec: v1alpha1.ReleasePayloadSpec{
				PayloadCoordinates: v1alpha1.PayloadCoordinates{
					Namespace:          "ocp",
					ImagestreamName:    "release",
					ImagestreamTagName: "4.11.0-0.nightly-2022-02-09-091559",
				},
			},
		},
	}, {
		name:             "ReleasePayloadWithBlockingVerificationJobs",
		releaseNamespace: "ocp",
		input: &v1alpha1.ReleasePayload{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "4.11.0-0.nightly-2022-02-09-091559",
				Namespace: "ocp",
			},
			Spec: v1alpha1.ReleasePayloadSpec{
				PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
					BlockingJobs: []v1alpha1.CIConfiguration{
						{
							CIConfigurationName:    "blocking-job",
							CIConfigurationJobName: "blocking-prowjob",
						},
					},
				},
			},
		},
		expected: &v1alpha1.ReleasePayload{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "4.11.0-0.nightly-2022-02-09-091559",
				Namespace: "ocp",
			},
			Spec: v1alpha1.ReleasePayloadSpec{
				PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
					BlockingJobs: []v1alpha1.CIConfiguration{
						{
							CIConfigurationName:    "blocking-job",
							CIConfigurationJobName: "blocking-prowjob",
						},
					},
				},
			},
			Status: v1alpha1.ReleasePayloadStatus{
				BlockingJobResults: []v1alpha1.JobStatus{
					{
						CIConfigurationName:    "blocking-job",
						CIConfigurationJobName: "blocking-prowjob",
					},
				},
			},
		},
	}, {
		name:             "ReleasePayloadWithInformingVerificationJobs",
		releaseNamespace: "ocp",
		input: &v1alpha1.ReleasePayload{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "4.11.0-0.nightly-2022-02-09-091559",
				Namespace: "ocp",
			},
			Spec: v1alpha1.ReleasePayloadSpec{
				PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
					InformingJobs: []v1alpha1.CIConfiguration{
						{
							CIConfigurationName:    "informing-job",
							CIConfigurationJobName: "informing-prowjob",
						},
					},
				},
			},
		},
		expected: &v1alpha1.ReleasePayload{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "4.11.0-0.nightly-2022-02-09-091559",
				Namespace: "ocp",
			},
			Spec: v1alpha1.ReleasePayloadSpec{
				PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
					InformingJobs: []v1alpha1.CIConfiguration{
						{
							CIConfigurationName:    "informing-job",
							CIConfigurationJobName: "informing-prowjob",
						},
					},
				},
			},
			Status: v1alpha1.ReleasePayloadStatus{
				InformingJobResults: []v1alpha1.JobStatus{
					{
						CIConfigurationName:    "informing-job",
						CIConfigurationJobName: "informing-prowjob",
					},
				},
			},
		},
	}, {
		name:             "ReleasePayloadWithVerificationJobs",
		releaseNamespace: "ocp",
		input: &v1alpha1.ReleasePayload{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "4.11.0-0.nightly-2022-02-09-091559",
				Namespace: "ocp",
			},
			Spec: v1alpha1.ReleasePayloadSpec{
				PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
					BlockingJobs: []v1alpha1.CIConfiguration{
						{
							CIConfigurationName:    "blocking-job",
							CIConfigurationJobName: "blocking-prowjob",
						},
					},
					InformingJobs: []v1alpha1.CIConfiguration{
						{
							CIConfigurationName:    "informing-job",
							CIConfigurationJobName: "informing-prowjob",
						},
					},
				},
			},
		},
		expected: &v1alpha1.ReleasePayload{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "4.11.0-0.nightly-2022-02-09-091559",
				Namespace: "ocp",
			},
			Spec: v1alpha1.ReleasePayloadSpec{
				PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
					BlockingJobs: []v1alpha1.CIConfiguration{
						{
							CIConfigurationName:    "blocking-job",
							CIConfigurationJobName: "blocking-prowjob",
						},
					},
					InformingJobs: []v1alpha1.CIConfiguration{
						{
							CIConfigurationName:    "informing-job",
							CIConfigurationJobName: "informing-prowjob",
						},
					},
				},
			},
			Status: v1alpha1.ReleasePayloadStatus{
				BlockingJobResults: []v1alpha1.JobStatus{
					{
						CIConfigurationName:    "blocking-job",
						CIConfigurationJobName: "blocking-prowjob",
					},
				},
				InformingJobResults: []v1alpha1.JobStatus{
					{
						CIConfigurationName:    "informing-job",
						CIConfigurationJobName: "informing-prowjob",
					},
				},
			},
		},
	}, {
		name:             "ReleasePayloadWithAnalysisJobs",
		releaseNamespace: "ocp",
		input: &v1alpha1.ReleasePayload{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "4.11.0-0.nightly-2022-02-09-091559",
				Namespace: "ocp",
			},
			Spec: v1alpha1.ReleasePayloadSpec{
				PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
					BlockingJobs: []v1alpha1.CIConfiguration{
						{
							CIConfigurationName:    "blocking-analysis-job",
							CIConfigurationJobName: "blocking-analysis-prowjob",
							AnalysisJobCount:       10,
						},
					},
					InformingJobs: []v1alpha1.CIConfiguration{
						{
							CIConfigurationName:    "informing-analysis-job",
							CIConfigurationJobName: "informing-analysis-prowjob",
							AnalysisJobCount:       10,
						},
					},
				},
			},
		},
		expected: &v1alpha1.ReleasePayload{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "4.11.0-0.nightly-2022-02-09-091559",
				Namespace: "ocp",
			},
			Spec: v1alpha1.ReleasePayloadSpec{
				PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
					BlockingJobs: []v1alpha1.CIConfiguration{
						{
							CIConfigurationName:    "blocking-analysis-job",
							CIConfigurationJobName: "blocking-analysis-prowjob",
							AnalysisJobCount:       10,
						},
					},
					InformingJobs: []v1alpha1.CIConfiguration{
						{
							CIConfigurationName:    "informing-analysis-job",
							CIConfigurationJobName: "informing-analysis-prowjob",
							AnalysisJobCount:       10,
						},
					},
				},
			},
			Status: v1alpha1.ReleasePayloadStatus{
				BlockingJobResults: []v1alpha1.JobStatus{
					{
						CIConfigurationName:    "blocking-analysis-job",
						CIConfigurationJobName: "blocking-analysis-prowjob",
						AnalysisJobCount:       10,
					},
				},
				InformingJobResults: []v1alpha1.JobStatus{
					{
						CIConfigurationName:    "informing-analysis-job",
						CIConfigurationJobName: "informing-analysis-prowjob",
						AnalysisJobCount:       10,
					},
				},
			},
		},
	}, {
		name:             "ReleasePayloadWithStatus",
		releaseNamespace: "ocp",
		input: &v1alpha1.ReleasePayload{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "4.11.0-0.nightly-2022-02-09-091559",
				Namespace: "ocp",
			},
			Spec: v1alpha1.ReleasePayloadSpec{
				PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
					BlockingJobs: []v1alpha1.CIConfiguration{
						{
							CIConfigurationName:    "blocking-job",
							CIConfigurationJobName: "blocking-prowjob",
						},
					},
					InformingJobs: []v1alpha1.CIConfiguration{
						{
							CIConfigurationName:    "informing-job",
							CIConfigurationJobName: "informing-prowjob",
						},
					},
				},
			},
			Status: v1alpha1.ReleasePayloadStatus{
				BlockingJobResults: []v1alpha1.JobStatus{
					{
						CIConfigurationName:    "blocking-job",
						CIConfigurationJobName: "blocking-prowjob",
					},
				},
				InformingJobResults: []v1alpha1.JobStatus{
					{
						CIConfigurationName:    "blocking-job",
						CIConfigurationJobName: "blocking-prowjob",
					},
				},
			},
		},
		expected: &v1alpha1.ReleasePayload{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "4.11.0-0.nightly-2022-02-09-091559",
				Namespace: "ocp",
			},
			Spec: v1alpha1.ReleasePayloadSpec{
				PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
					BlockingJobs: []v1alpha1.CIConfiguration{
						{
							CIConfigurationName:    "blocking-job",
							CIConfigurationJobName: "blocking-prowjob",
						},
					},
					InformingJobs: []v1alpha1.CIConfiguration{
						{
							CIConfigurationName:    "informing-job",
							CIConfigurationJobName: "informing-prowjob",
						},
					},
				},
			},
			Status: v1alpha1.ReleasePayloadStatus{
				BlockingJobResults: []v1alpha1.JobStatus{
					{
						CIConfigurationName:    "blocking-job",
						CIConfigurationJobName: "blocking-prowjob",
					},
				},
				InformingJobResults: []v1alpha1.JobStatus{
					{
						CIConfigurationName:    "blocking-job",
						CIConfigurationJobName: "blocking-prowjob",
					},
				},
			},
		},
	}}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			releasePayloadClient := fake.NewSimpleClientset(testCase.input)

			releasePayloadInformerFactory := releasepayloadinformers.NewSharedInformerFactory(releasePayloadClient, controllerDefaultResyncDuration)
			releasePayloadInformer := releasePayloadInformerFactory.Release().V1alpha1().ReleasePayloads()

			c := &PayloadVerificationController{
				ReleasePayloadController: NewReleasePayloadController("Payload Verification Controller",
					releasePayloadInformer,
					releasePayloadClient.ReleaseV1alpha1(),
					events.NewInMemoryRecorder("payload-verification-controller-test"),
					workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "PayloadVerificationController")),
			}

			releasePayloadInformer.Informer().AddEventHandler(&cache.ResourceEventHandlerFuncs{
				AddFunc: c.Enqueue,
				UpdateFunc: func(oldObj, newObj interface{}) {
					c.Enqueue(newObj)
				},
				DeleteFunc: c.Enqueue,
			})

			releasePayloadInformerFactory.Start(context.Background().Done())

			if !cache.WaitForNamedCacheSync("PayloadVerificationController", context.Background().Done(), c.cachesToSync...) {
				t.Errorf("%s: error waiting for caches to sync", testCase.name)
				return
			}

			err := c.sync(context.TODO(), fmt.Sprintf("%s/%s", testCase.input.Namespace, testCase.input.Name))
			if err != nil {
				t.Errorf("%s: unexpected err: %v", testCase.name, err)
			}

			// Performing a live lookup instead of having to wait for the cache to sink (again)...
			output, err := c.releasePayloadClient.ReleasePayloads(testCase.input.Namespace).Get(context.TODO(), testCase.input.Name, metav1.GetOptions{})
			if !reflect.DeepEqual(output, testCase.expected) {
				t.Errorf("%s: Expected %v, got %v", testCase.name, testCase.expected, output)
			}
		})
	}
}
