package release_payload_controller

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	"github.com/openshift/release-controller/pkg/client/clientset/versioned/fake"
	releasepayloadinformers "github.com/openshift/release-controller/pkg/client/informers/externalversions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
)

func TestPayloadVerificationSync(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		input    *v1alpha1.ReleasePayload
		expected *v1alpha1.ReleasePayload
	}{{
		name: "ReleasePayloadWithoutVerificationJobs",
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
		name: "ReleasePayloadWithBlockingVerificationJobs",
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
		name: "ReleasePayloadWithInformingVerificationJobs",
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
		name: "ReleasePayloadWithVerificationJobs",
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
		name: "ReleasePayloadWithAnalysisJobs",
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
		name: "ReleasePayloadWithStatus",
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
	}, {
		name: "ReleasePayloadWithUpgradeJobs",
		input: &v1alpha1.ReleasePayload{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "4.11.0-0.nightly-2022-02-09-091559",
				Namespace: "ocp",
			},
			Spec: v1alpha1.ReleasePayloadSpec{
				PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
					UpgradeJobs: []v1alpha1.CIConfiguration{
						{
							CIConfigurationName:    "aws",
							CIConfigurationJobName: "release-openshift-origin-installer-e2e-aws-upgrade",
						},
						{
							CIConfigurationName:    "azure",
							CIConfigurationJobName: "release-openshift-origin-installer-e2e-azure-upgrade",
						},
						{
							CIConfigurationName:    "gcp",
							CIConfigurationJobName: "release-openshift-origin-installer-e2e-gcp-upgrade",
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
					UpgradeJobs: []v1alpha1.CIConfiguration{
						{
							CIConfigurationName:    "aws",
							CIConfigurationJobName: "release-openshift-origin-installer-e2e-aws-upgrade",
						},
						{
							CIConfigurationName:    "azure",
							CIConfigurationJobName: "release-openshift-origin-installer-e2e-azure-upgrade",
						},
						{
							CIConfigurationName:    "gcp",
							CIConfigurationJobName: "release-openshift-origin-installer-e2e-gcp-upgrade",
						},
					},
				},
			},
			Status: v1alpha1.ReleasePayloadStatus{
				UpgradeJobResults: []v1alpha1.JobStatus{
					{
						CIConfigurationName:    "aws",
						CIConfigurationJobName: "release-openshift-origin-installer-e2e-aws-upgrade",
					},
					{
						CIConfigurationName:    "azure",
						CIConfigurationJobName: "release-openshift-origin-installer-e2e-azure-upgrade",
					},
					{
						CIConfigurationName:    "gcp",
						CIConfigurationJobName: "release-openshift-origin-installer-e2e-gcp-upgrade",
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

			c, err := NewPayloadVerificationController(releasePayloadInformer, releasePayloadClient.ReleaseV1alpha1(), events.NewInMemoryRecorder("payload-verification-controller-test"))
			if err != nil {
				t.Fatalf("Failed to create Payload Verification Controller: %v", err)
			}

			releasePayloadInformerFactory.Start(context.Background().Done())

			if !cache.WaitForNamedCacheSync("PayloadVerificationController", context.Background().Done(), c.cachesToSync...) {
				t.Errorf("%s: error waiting for caches to sync", testCase.name)
				return
			}

			if err := c.sync(context.TODO(), fmt.Sprintf("%s/%s", testCase.input.Namespace, testCase.input.Name)); err != nil {
				t.Errorf("%s: unexpected err: %v", testCase.name, err)
			}

			// Performing a live lookup instead of having to wait for the cache to sink (again)...
			output, _ := c.releasePayloadClient.ReleasePayloads(testCase.input.Namespace).Get(context.TODO(), testCase.input.Name, metav1.GetOptions{})
			if !reflect.DeepEqual(output, testCase.expected) {
				t.Errorf("%s: Expected %v, got %v", testCase.name, testCase.expected, output)
			}
		})
	}
}
