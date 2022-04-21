package controller

import (
	"fmt"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
)

// GetNamespaceAndName returns the namespace and name from the runtime.Object
// or an error if either is missing.
func GetNamespaceAndName(object runtime.Object) (namespace, name string, err error) {
	var accessor = meta.NewAccessor()
	namespace, err = accessor.Namespace(object)
	if err != nil {
		return "", "", fmt.Errorf("failed to get namespace for object: %v", err)
	}
	name, err = accessor.Name(object)
	if err != nil {
		return "", "", fmt.Errorf("failed to get name for object: %v", err)
	}
	return namespace, name, nil
}

// GetAnnotation returns the value of the annotation from the runtime.Object
// or an error if either is missing.
func GetAnnotation(object runtime.Object, annotation string) (value string, err error) {
	if len(annotation) == 0 {
		return "", fmt.Errorf("annotation parameter must be set")
	}
	var accessor = meta.NewAccessor()
	annotations, err := accessor.Annotations(object)
	if err != nil {
		return "", fmt.Errorf("failed to get annotations for object: %v", err)
	}
	value, ok := annotations[annotation]
	if !ok {
		return "", fmt.Errorf("annotation %q not found", annotation)
	}
	return value, nil
}

// GetReleasePayloadQueueKeyFromAnnotation returns the "namespace/name" of the
// ReleasePayload to be used as the key for adding an item onto a queue or an
// error if unable to generate the key.
func GetReleasePayloadQueueKeyFromAnnotation(obj interface{}, annotation, releaseNamespace string) (key string, err error) {
	if len(releaseNamespace) == 0 {
		return "", fmt.Errorf("releaseNamespace parameter must be set")
	}
	object, ok := obj.(runtime.Object)
	if !ok {
		return "", fmt.Errorf("unable to cast obj: %v", obj)
	}
	value, err := GetAnnotation(object, annotation)
	if err != nil {
		return "", fmt.Errorf("unable to determine releasePayload: %v", err)
	}
	return fmt.Sprintf("%s/%s", releaseNamespace, value), nil
}
