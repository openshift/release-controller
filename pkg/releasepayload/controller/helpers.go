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

// GetAnnotation returns the value of the annotation from the runtime.Object or an error
// if unable to retrieve annotation.
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
