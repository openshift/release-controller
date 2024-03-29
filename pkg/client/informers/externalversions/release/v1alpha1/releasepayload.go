// Code generated by informer-gen. DO NOT EDIT.

package v1alpha1

import (
	"context"
	time "time"

	releasev1alpha1 "github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	versioned "github.com/openshift/release-controller/pkg/client/clientset/versioned"
	internalinterfaces "github.com/openshift/release-controller/pkg/client/informers/externalversions/internalinterfaces"
	v1alpha1 "github.com/openshift/release-controller/pkg/client/listers/release/v1alpha1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	watch "k8s.io/apimachinery/pkg/watch"
	cache "k8s.io/client-go/tools/cache"
)

// ReleasePayloadInformer provides access to a shared informer and lister for
// ReleasePayloads.
type ReleasePayloadInformer interface {
	Informer() cache.SharedIndexInformer
	Lister() v1alpha1.ReleasePayloadLister
}

type releasePayloadInformer struct {
	factory          internalinterfaces.SharedInformerFactory
	tweakListOptions internalinterfaces.TweakListOptionsFunc
	namespace        string
}

// NewReleasePayloadInformer constructs a new informer for ReleasePayload type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewReleasePayloadInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers) cache.SharedIndexInformer {
	return NewFilteredReleasePayloadInformer(client, namespace, resyncPeriod, indexers, nil)
}

// NewFilteredReleasePayloadInformer constructs a new informer for ReleasePayload type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewFilteredReleasePayloadInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers, tweakListOptions internalinterfaces.TweakListOptionsFunc) cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options v1.ListOptions) (runtime.Object, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.ReleaseV1alpha1().ReleasePayloads(namespace).List(context.TODO(), options)
			},
			WatchFunc: func(options v1.ListOptions) (watch.Interface, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.ReleaseV1alpha1().ReleasePayloads(namespace).Watch(context.TODO(), options)
			},
		},
		&releasev1alpha1.ReleasePayload{},
		resyncPeriod,
		indexers,
	)
}

func (f *releasePayloadInformer) defaultInformer(client versioned.Interface, resyncPeriod time.Duration) cache.SharedIndexInformer {
	return NewFilteredReleasePayloadInformer(client, f.namespace, resyncPeriod, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}, f.tweakListOptions)
}

func (f *releasePayloadInformer) Informer() cache.SharedIndexInformer {
	return f.factory.InformerFor(&releasev1alpha1.ReleasePayload{}, f.defaultInformer)
}

func (f *releasePayloadInformer) Lister() v1alpha1.ReleasePayloadLister {
	return v1alpha1.NewReleasePayloadLister(f.Informer().GetIndexer())
}
