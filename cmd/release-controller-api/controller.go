package main

import (
	releasepayloadlister "github.com/openshift/release-controller/pkg/client/listers/release/v1alpha1"
	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"

	lru "github.com/hashicorp/golang-lru"

	corev1 "k8s.io/api/core/v1"
	kv1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog"

	imagescheme "github.com/openshift/client-go/image/clientset/versioned/scheme"
	imagelisters "github.com/openshift/client-go/image/listers/image/v1"
)

// Controller ensures that OpenShift update payload images (also known as
// release images) are created whenever an image stream representing the images
// in a release is updated. A consumer sets the release.openshift.io/config
// annotation on an image stream in the release namespace and the controller will
//
//  1. Create a tag in the "release" image stream that uses the release name +
//     current timestamp.
//  2. Mirror all of the tags in the input image stream so that they can't be
//     pruned.
//  3. Launch a job in the job namespace to invoke 'oc adm release new' from
//     the mirror pointing to the release tag we created in step 1.
//  4. If the job succeeds in pushing the tag, set an annotation on that tag
//     release.openshift.io/phase = "Ready", indicating that the release can be
//     used by other steps
//
// TODO:
//
//  5. Perform a number of manual and automated tasks on the release - if all are
//     successful, set the phase to "Verified" and then promote the tag to external
//     locations.
//
// Invariants:
//
// 1. ...
type Controller struct {
	eventRecorder record.EventRecorder
	releaseLister *releasecontroller.MultiImageStreamLister

	// artifactsHost if set is the location to build download links for client tools from
	artifactsHost string

	releaseInfo releasecontroller.ReleaseInfo

	graph *releasecontroller.UpgradeGraph

	// parsedReleaseConfigCache caches the parsed release config object for any release
	// config serialized json.
	parsedReleaseConfigCache *lru.Cache

	dashboards []Dashboard

	authenticationMessage string

	architecture string

	artSuffix string

	releasePayloadNamespace string
	releasePayloadLister    releasepayloadlister.ReleasePayloadLister
}

// NewController instantiates a Controller to manage release objects.
func NewController(
	eventsClient kv1core.EventsGetter,
	artifactsHost string,
	releaseInfo releasecontroller.ReleaseInfo,
	graph *releasecontroller.UpgradeGraph,
	authenticationMessage string,
	architecture string,
	artSuffix string,
	releasePayloadNamespace string,
	releasePayloadLister releasepayloadlister.ReleasePayloadLister,
) *Controller {
	// log events at v2 and send them to the server
	broadcaster := record.NewBroadcaster()
	broadcaster.StartLogging(klog.V(2).Infof)
	broadcaster.StartRecordingToSink(&kv1core.EventSinkImpl{Interface: eventsClient.Events("")})
	recorder := broadcaster.NewRecorder(imagescheme.Scheme, corev1.EventSource{Component: "release-controller-api"})

	// we cache parsed release configs to avoid the deserialization cost
	parsedReleaseConfigCache, err := lru.New(50)
	if err != nil {
		panic(err)
	}

	c := &Controller{
		eventRecorder: recorder,

		releaseLister: &releasecontroller.MultiImageStreamLister{Listers: make(map[string]imagelisters.ImageStreamNamespaceLister)},

		artifactsHost: artifactsHost,

		releaseInfo: releaseInfo,

		graph: graph,

		parsedReleaseConfigCache: parsedReleaseConfigCache,

		authenticationMessage: authenticationMessage,

		architecture: architecture,

		artSuffix: artSuffix,

		releasePayloadNamespace: releasePayloadNamespace,
		releasePayloadLister:    releasePayloadLister,
	}

	c.dashboards = []Dashboard{
		{"Index", "/"},
		{"Overview", "/dashboards/overview"},
		{"Compare", "/dashboards/compare"},
	}

	return c
}
