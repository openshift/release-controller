package release_payload_controller

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	"github.com/openshift/release-controller/pkg/client/clientset/versioned/fake"
	releasepayloadinformers "github.com/openshift/release-controller/pkg/client/informers/externalversions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
)

func TestReleaseCreationJobSync(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		payload  *v1alpha1.ReleasePayload
		expected *v1alpha1.ReleasePayload
	}{
		{
			name: "ReleaseCreationJobResultNotPresent",
			payload: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCreationConfig: v1alpha1.PayloadCreationConfig{
						ReleaseCreationCoordinates: v1alpha1.ReleaseCreationCoordinates{
							Namespace:              "ci-release",
							ReleaseCreationJobName: "4.11.0-0.nightly-2022-02-09-091559",
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
					PayloadCreationConfig: v1alpha1.PayloadCreationConfig{
						ReleaseCreationCoordinates: v1alpha1.ReleaseCreationCoordinates{
							Namespace:              "ci-release",
							ReleaseCreationJobName: "4.11.0-0.nightly-2022-02-09-091559",
						},
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{
						Coordinates: v1alpha1.ReleaseCreationJobCoordinates{
							Name:      "4.11.0-0.nightly-2022-02-09-091559",
							Namespace: "ci-release",
						},
					},
				},
			},
		},
		{
			name: "ReleaseCreationJobResultPresent",
			payload: &v1alpha1.ReleasePayload{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0-0.nightly-2022-02-09-091559",
					Namespace: "ocp",
				},
				Spec: v1alpha1.ReleasePayloadSpec{
					PayloadCreationConfig: v1alpha1.PayloadCreationConfig{
						ReleaseCreationCoordinates: v1alpha1.ReleaseCreationCoordinates{
							Namespace:              "ci-release",
							ReleaseCreationJobName: "4.11.0-0.nightly-2022-02-09-091559",
						},
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{
						Coordinates: v1alpha1.ReleaseCreationJobCoordinates{
							Name:      "4.11.0-0.nightly-2022-02-09-091559",
							Namespace: "ci-release",
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
					PayloadCreationConfig: v1alpha1.PayloadCreationConfig{
						ReleaseCreationCoordinates: v1alpha1.ReleaseCreationCoordinates{
							Namespace:              "ci-release",
							ReleaseCreationJobName: "4.11.0-0.nightly-2022-02-09-091559",
						},
					},
				},
				Status: v1alpha1.ReleasePayloadStatus{
					ReleaseCreationJobResult: v1alpha1.ReleaseCreationJobResult{
						Coordinates: v1alpha1.ReleaseCreationJobCoordinates{
							Name:      "4.11.0-0.nightly-2022-02-09-091559",
							Namespace: "ci-release",
						},
					},
				},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			releasePayloadClient := fake.NewSimpleClientset(testCase.payload)
			releasePayloadInformerFactory := releasepayloadinformers.NewSharedInformerFactory(releasePayloadClient, controllerDefaultResyncDuration)
			releasePayloadInformer := releasePayloadInformerFactory.Release().V1alpha1().ReleasePayloads()

			c, err := NewReleaseCreationJobController(releasePayloadInformer, releasePayloadClient.ReleaseV1alpha1(), events.NewInMemoryRecorder("release-creation-job-controller-test"))
			if err != nil {
				t.Fatalf("Failed to create Release Creation Job Controller: %v", err)
			}

			releasePayloadInformerFactory.Start(context.Background().Done())

			if !cache.WaitForNamedCacheSync("ReleaseCreationJobController", context.Background().Done(), c.cachesToSync...) {
				t.Errorf("%s: error waiting for caches to sync", testCase.name)
				return
			}

			if err := c.sync(context.TODO(), fmt.Sprintf("%s/%s", testCase.payload.Namespace, testCase.payload.Name)); err != nil {
				t.Errorf("%s: unexpected err: %v", testCase.name, err)
			}

			// Performing a live lookup instead of having to wait for the cache to sink (again)...
			output, _ := c.releasePayloadClient.ReleasePayloads(testCase.payload.Namespace).Get(context.TODO(), testCase.payload.Name, metav1.GetOptions{})
			if !cmp.Equal(output, testCase.expected, cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime")) {
				t.Errorf("%s: Expected %v, got %v", testCase.name, testCase.expected, output)
			}
		})
	}
}
