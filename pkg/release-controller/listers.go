package releasecontroller

import (
	imagev1 "github.com/openshift/api/image/v1"
	imagelisters "github.com/openshift/client-go/image/listers/image/v1"
	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	releasepayloadlisters "github.com/openshift/release-controller/pkg/client/listers/release/v1alpha1"
	"k8s.io/apimachinery/pkg/labels"
)

// MultiImageStreamLister uses multiple independent namespace listers
// to simulate a full lister so that multiple namespaces can be watched
// for image streams.
type MultiImageStreamLister struct {
	Listers map[string]imagelisters.ImageStreamNamespaceLister
}

func (l *MultiImageStreamLister) List(label labels.Selector) ([]*imagev1.ImageStream, error) {
	var streams []*imagev1.ImageStream
	for _, ns := range l.Listers {
		is, err := ns.List(label)
		if err != nil {
			return nil, err
		}
		streams = append(streams, is...)
	}
	return streams, nil
}

func (l *MultiImageStreamLister) ImageStreams(ns string) imagelisters.ImageStreamNamespaceLister {
	return l.Listers[ns]
}

// MultiReleasePayloadLister uses multiple independent namespace listers
// to simulate a full lister so that multiple namespaces can be watched
// for releasepayloads.
type MultiReleasePayloadLister struct {
	Listers map[string]releasepayloadlisters.ReleasePayloadNamespaceLister
}

func (l *MultiReleasePayloadLister) List(label labels.Selector) ([]*v1alpha1.ReleasePayload, error) {
	var releasePayloads []*v1alpha1.ReleasePayload
	for _, ns := range l.Listers {
		is, err := ns.List(label)
		if err != nil {
			return nil, err
		}
		releasePayloads = append(releasePayloads, is...)
	}
	return releasePayloads, nil
}

func (l *MultiReleasePayloadLister) ReleasePayloads(ns string) releasepayloadlisters.ReleasePayloadNamespaceLister {
	return l.Listers[ns]
}
