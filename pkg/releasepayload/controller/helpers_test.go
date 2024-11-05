package controller

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestGetNamespaceAndName(t *testing.T) {
	testCases := []struct {
		name              string
		input             runtime.Object
		expectedNamespace string
		expectedName      string
		expectedError     string
	}{
		{
			name:              "EmptyRuntimeObject",
			input:             &batchv1.Job{},
			expectedNamespace: "",
			expectedName:      "",
			expectedError:     "",
		},
		{
			name: "SimpleRuntimeObject",
			input: &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0",
					Namespace: "ci-release",
				},
			},
			expectedNamespace: "ci-release",
			expectedName:      "4.11.0",
			expectedError:     "",
		},
		{
			name:              "NilRuntimeObject",
			input:             nil,
			expectedNamespace: "",
			expectedName:      "",
			expectedError:     "failed to get namespace for object: object does not implement the Object interfaces",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			namespace, name, err := GetNamespaceAndName(testCase.input)
			if !cmp.Equal(namespace, testCase.expectedNamespace) {
				t.Fatalf("%s: Expected namespace %v, got %v", testCase.name, testCase.expectedNamespace, namespace)
			}
			if !cmp.Equal(name, testCase.expectedName) {
				t.Fatalf("%s: Expected name %v, got %v", testCase.name, testCase.expectedName, name)
			}
			if err != nil && !cmp.Equal(err.Error(), testCase.expectedError) {
				t.Fatalf("%s: Expected error %v, got %v", testCase.name, testCase.expectedError, err)
			}
		})
	}
}

func TestGetAnnotation(t *testing.T) {
	testCases := []struct {
		name          string
		input         runtime.Object
		annotation    string
		expectedValue string
		expectedError string
	}{
		{
			name:          "AnnotationParameterNotSet",
			input:         &batchv1.Job{},
			expectedError: "annotation parameter must be set",
		},
		{
			name:          "RuntimeObjectWithoutAnnotations",
			input:         &batchv1.Job{},
			annotation:    releasecontroller.ReleaseAnnotationReleaseTag,
			expectedValue: "",
			expectedError: fmt.Sprintf("annotation %q not found", releasecontroller.ReleaseAnnotationReleaseTag),
		},
		{
			name: "RuntimeObjectWithAnnotations",
			input: &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "4.11.0",
					Namespace: "ci-release",
					Annotations: map[string]string{
						releasecontroller.ReleaseAnnotationReleaseTag: "4.11.0",
					},
				},
			},
			annotation:    releasecontroller.ReleaseAnnotationReleaseTag,
			expectedValue: "4.11.0",
			expectedError: "",
		},
		{
			name:          "NilRuntimeObject",
			input:         nil,
			annotation:    releasecontroller.ReleaseAnnotationReleaseTag,
			expectedValue: "",
			expectedError: "failed to get annotations for object: object does not implement the Object interfaces",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result, err := GetAnnotation(testCase.input, testCase.annotation)
			if !cmp.Equal(result, testCase.expectedValue) {
				t.Fatalf("%s: Expected %v, got %v", testCase.name, testCase.expectedValue, result)
			}
			if err != nil && !cmp.Equal(err.Error(), testCase.expectedError) {
				t.Fatalf("%s: Expected error %v, got %v", testCase.name, testCase.expectedError, err)
			}
		})
	}
}
