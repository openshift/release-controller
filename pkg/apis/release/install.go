package release

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	releasev1alpha1 "github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
)

const (
	GroupName = "release.openshift.io"
)

var (
	schemeBuilder = runtime.NewSchemeBuilder(releasev1alpha1.Install)
	// Install is a function which adds every version of this group to a scheme
	Install = schemeBuilder.AddToScheme
)

func Resource(resource string) schema.GroupResource {
	return schema.GroupResource{Group: GroupName, Resource: resource}
}

func Kind(kind string) schema.GroupKind {
	return schema.GroupKind{Group: GroupName, Kind: kind}
}
