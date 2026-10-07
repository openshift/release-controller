package release_payload_controller

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	imagev1 "github.com/openshift/api/image/v1"
	imagefake "github.com/openshift/client-go/image/clientset/versioned/fake"
	imageinformers "github.com/openshift/client-go/image/informers/externalversions"
	imagev1informer "github.com/openshift/client-go/image/informers/externalversions/image/v1"
	imagev1lister "github.com/openshift/client-go/image/listers/image/v1"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	"github.com/openshift/release-controller/pkg/client/clientset/versioned/fake"
	releasepayloadinformers "github.com/openshift/release-controller/pkg/client/informers/externalversions"
	releasepayloadinformer "github.com/openshift/release-controller/pkg/client/informers/externalversions/release/v1alpha1"
	releasepayloadlister "github.com/openshift/release-controller/pkg/client/listers/release/v1alpha1"
	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/clock"
)

const (
	legacyLocalConfig     = `{"name":"4-stable","as":"Stable"}`
	legacyReferenceConfig = `{"name":"nightly","to":"release","referenceRelease":{"pullRepository":"quay.io/releases/pull","pushRepository":"quay.io/releases/push"}}`
)

type legacyCoordinatesFixture struct {
	payload *v1alpha1.ReleasePayload
	target  *imagev1.ImageStream
}

func newLegacyCoordinatesFixture() legacyCoordinatesFixture {
	return legacyCoordinatesFixture{
		payload: &v1alpha1.ReleasePayload{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "ocp", Name: "legacy-payload", ResourceVersion: "42",
				Labels: map[string]string{"keep": "label"}, Annotations: map[string]string{"keep": "annotation"},
			},
			Spec: v1alpha1.ReleasePayloadSpec{
				PayloadCoordinates:    v1alpha1.PayloadCoordinates{Namespace: "ocp", ImagestreamName: "release", ImagestreamTagName: "4.12.9", StreamName: "4-stable"},
				PayloadCreationConfig: v1alpha1.PayloadCreationConfig{ProwCoordinates: v1alpha1.ProwCoordinates{Namespace: "ci"}},
				PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
					PayloadVerificationDataSource: v1alpha1.PayloadVerificationDataSourceBuildFarm,
					BlockingJobs:                  []v1alpha1.CIConfiguration{{CIConfigurationName: "job", CIConfigurationJobName: "prow-job"}},
				},
				PayloadOverride: v1alpha1.ReleasePayloadOverride{Override: v1alpha1.ReleasePayloadOverrideAccepted, Reason: "keep"},
				PayloadType:     v1alpha1.PayloadTypeLocal,
			},
			Status: v1alpha1.ReleasePayloadStatus{
				Conditions:         []metav1.Condition{{Type: v1alpha1.ConditionPayloadAccepted, Status: metav1.ConditionTrue, Reason: "keep"}},
				BlockingJobResults: []v1alpha1.JobStatus{{CIConfigurationName: "job", JobRunResults: []v1alpha1.JobRunResult{{State: v1alpha1.JobRunStateSuccess}}}},
			},
		},
		target: &imagev1.ImageStream{
			ObjectMeta: metav1.ObjectMeta{Namespace: "ocp", Name: "release", Annotations: map[string]string{releasecontroller.ReleaseAnnotationConfig: legacyLocalConfig}},
			Spec: imagev1.ImageStreamSpec{Tags: []imagev1.TagReference{
				{Name: "unrelated", Reference: true},
				{Name: "4.12.9", Annotations: map[string]string{}},
			}},
			Status: imagev1.ImageStreamStatus{PublicDockerImageRepository: "registry.example.com/ocp/release"},
		},
	}
}

func legacyReferenceTag(f *legacyCoordinatesFixture) {
	f.target.Spec.Tags[1].Reference = true
}

func TestLegacyReleaseCoordinatesSync(t *testing.T) {
	local := &v1alpha1.ReleaseCoordinates{Repository: "registry.example.com/ocp/release", Tag: "4.12.9"}
	updateFailure := errors.New("update failed")
	conflict := apierrors.NewConflict(schema.GroupResource{Group: v1alpha1.GroupName, Resource: "releasepayloads"}, "legacy-payload", errors.New("stale resource version"))
	deleted := apierrors.NewNotFound(schema.GroupResource{Group: v1alpha1.GroupName, Resource: "releasepayloads"}, "legacy-payload")
	tests := []struct {
		name        string
		modify      func(*legacyCoordinatesFixture)
		want        *v1alpha1.ReleaseCoordinates
		updateError error
		wantError   error
	}{
		{name: "nil coordinates and build farm results", want: local},
		{name: "empty coordinates", modify: func(f *legacyCoordinatesFixture) { f.payload.Spec.ReleaseCoordinates = []v1alpha1.ReleaseCoordinates{} }, want: local},
		{name: "image stream results", modify: func(f *legacyCoordinatesFixture) {
			f.payload.Spec.PayloadVerificationConfig.PayloadVerificationDataSource = v1alpha1.PayloadVerificationDataSourceImageStream
		}, want: local},
		{name: "unspecified verification source and payload type", modify: func(f *legacyCoordinatesFixture) {
			f.payload.Spec.PayloadVerificationConfig.PayloadVerificationDataSource = ""
			f.payload.Spec.PayloadType = ""
		}, want: local},
		{name: "populated coordinates", modify: func(f *legacyCoordinatesFixture) {
			f.payload.Spec.ReleaseCoordinates = []v1alpha1.ReleaseCoordinates{{Repository: "keep", Tag: "one", Digest: "sha256:keep"}, {Repository: "also-keep", Tag: "two"}}
			f.target = nil
		}},
		{name: "nonempty list with empty entry", modify: func(f *legacyCoordinatesFixture) {
			f.payload.Spec.ReleaseCoordinates = []v1alpha1.ReleaseCoordinates{{}}
		}},
		{name: "older local tag in converted stream", modify: func(f *legacyCoordinatesFixture) {
			f.target.Annotations[releasecontroller.ReleaseAnnotationConfig] = legacyReferenceConfig
			f.target.Spec.Tags[1].Annotations[releasecontroller.ReleaseAnnotationSource] = "missing/source"
		}, want: local},
		{name: "local tag without release config", modify: func(f *legacyCoordinatesFixture) { f.target.Annotations = nil }, want: local},
		{name: "reference payload type is skipped", modify: func(f *legacyCoordinatesFixture) {
			f.payload.Spec.PayloadType = v1alpha1.PayloadTypeReference
		}},
		{name: "reference payload type without image stream data is skipped", modify: func(f *legacyCoordinatesFixture) {
			f.payload.Spec.PayloadType = v1alpha1.PayloadTypeReference
			f.payload.Spec.PayloadCoordinates = v1alpha1.PayloadCoordinates{}
			f.target = nil
		}},
		{name: "reference tag is skipped", modify: legacyReferenceTag},
		{name: "reference tag with unspecified payload type is skipped", modify: func(f *legacyCoordinatesFixture) {
			legacyReferenceTag(f)
			f.payload.Spec.PayloadType = ""
		}},
		{name: "reference tag without local repository is skipped", modify: func(f *legacyCoordinatesFixture) {
			legacyReferenceTag(f)
			f.target.Status.PublicDockerImageRepository = ""
		}},
		{name: "reference tag without config is skipped", modify: func(f *legacyCoordinatesFixture) {
			legacyReferenceTag(f)
			f.target.Annotations = nil
		}},
		{name: "reference tag with invalid metadata is skipped", modify: func(f *legacyCoordinatesFixture) {
			legacyReferenceTag(f)
			f.target.Spec.Tags[1].Annotations[releasecontroller.ReleaseAnnotationSource] = "too/many/parts"
			f.target.Annotations[releasecontroller.ReleaseAnnotationConfig] = "{"
		}},
		{name: "missing coordinate namespace", modify: func(f *legacyCoordinatesFixture) { f.payload.Spec.PayloadCoordinates.Namespace = "" }},
		{name: "missing coordinate stream", modify: func(f *legacyCoordinatesFixture) { f.payload.Spec.PayloadCoordinates.ImagestreamName = "" }},
		{name: "missing coordinate tag", modify: func(f *legacyCoordinatesFixture) { f.payload.Spec.PayloadCoordinates.ImagestreamTagName = "" }},
		{name: "missing target stream", modify: func(f *legacyCoordinatesFixture) { f.target = nil }},
		{name: "missing spec tag despite status tag", modify: func(f *legacyCoordinatesFixture) {
			f.target.Spec.Tags = f.target.Spec.Tags[:1]
			f.target.Status.Tags = []imagev1.NamedTagEventList{{Tag: "4.12.9"}}
		}},
		{name: "empty local repository", modify: func(f *legacyCoordinatesFixture) { f.target.Status.PublicDockerImageRepository = "" }},
		{name: "invalid local repository", modify: func(f *legacyCoordinatesFixture) { f.target.Status.PublicDockerImageRepository = "invalid repository" }},
		{name: "repository contains a tag", modify: func(f *legacyCoordinatesFixture) { f.target.Status.PublicDockerImageRepository += ":tag" }},
		{name: "update failure", want: local, updateError: updateFailure, wantError: updateFailure},
		{name: "update conflict", want: local, updateError: conflict, wantError: conflict},
		{name: "deleted during update", want: local, updateError: deleted},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newLegacyCoordinatesFixture()
			if tc.modify != nil {
				tc.modify(&f)
			}
			original := f.payload.DeepCopy()
			c, client, informer := newLegacyCoordinatesTestController(t, f)
			if tc.updateError != nil {
				client.PrependReactor("update", "releasepayloads", func(action clienttesting.Action) (bool, runtime.Object, error) {
					return true, nil, tc.updateError
				})
			}
			err := c.sync(context.Background(), "ocp/legacy-payload")
			if !errors.Is(err, tc.wantError) {
				t.Fatalf("expected error %v, got %v", tc.wantError, err)
			}
			cached, err := informer.Lister().ReleasePayloads("ocp").Get("legacy-payload")
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(original, cached); diff != "" {
				t.Errorf("cached payload was mutated (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(original, f.payload); diff != "" {
				t.Errorf("input payload was mutated (-want +got):\n%s", diff)
			}
			expected := original.DeepCopy()
			if tc.want != nil {
				expected.Spec.ReleaseCoordinates = []v1alpha1.ReleaseCoordinates{*tc.want}
				if len(client.Actions()) != 1 {
					t.Fatalf("expected one update, got %v", client.Actions())
				}
				action, ok := client.Actions()[0].(clienttesting.UpdateAction)
				if !ok || action.GetSubresource() != "" {
					t.Fatalf("expected main resource update, got %v", client.Actions()[0])
				}
				if diff := cmp.Diff(expected, action.GetObject()); diff != "" {
					t.Errorf("unexpected update (-want +got):\n%s", diff)
				}
			} else if len(client.Actions()) != 0 {
				t.Fatalf("expected no writes, got %v", client.Actions())
			}
			if tc.updateError != nil {
				expected = original
			}
			stored, err := client.Tracker().Get(v1alpha1.SchemeGroupVersion.WithResource("releasepayloads"), "ocp", "legacy-payload")
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(expected, stored); diff != "" {
				t.Errorf("unexpected stored payload (-want +got):\n%s", diff)
			}
			if tc.want != nil && tc.updateError == nil {
				// Simulate the informer observing the write, then reconcile again.
				if err := informer.Informer().GetIndexer().Update(stored); err != nil {
					t.Fatal(err)
				}
				client.ClearActions()
				if err := c.sync(context.Background(), "ocp/legacy-payload"); err != nil {
					t.Fatal(err)
				}
				if len(client.Actions()) != 0 {
					t.Errorf("populated coordinates caused another write: %v", client.Actions())
				}
			}
		})
	}
}

func newLegacyCoordinatesTestController(t *testing.T, f legacyCoordinatesFixture) (*LegacyReleaseCoordinatesController, *fake.Clientset, releasepayloadinformer.ReleasePayloadInformer) {
	t.Helper()
	client := fake.NewSimpleClientset(f.payload)
	informer := releasepayloadinformers.NewSharedInformerFactory(client, 0).Release().V1alpha1().ReleasePayloads()
	imageInformer := imageinformers.NewSharedInformerFactory(imagefake.NewSimpleClientset(), 0).Image().V1().ImageStreams()
	c, err := NewLegacyReleaseCoordinatesController(informer, client.ReleaseV1alpha1(), imageInformer, events.NewInMemoryRecorder("test", clock.RealClock{}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.queue.ShutDown)
	if err := informer.Informer().GetIndexer().Add(f.payload); err != nil {
		t.Fatal(err)
	}
	if f.target != nil {
		if err := imageInformer.Informer().GetIndexer().Add(f.target); err != nil {
			t.Fatal(err)
		}
	}
	return c, client, informer
}

type legacyCoordinatesEventInformer struct {
	cache.SharedIndexInformer
	handler cache.ResourceEventHandler
	synced  bool
}

func (i *legacyCoordinatesEventInformer) AddEventHandler(handler cache.ResourceEventHandler) (cache.ResourceEventHandlerRegistration, error) {
	i.handler = handler
	return nil, nil
}

func (i *legacyCoordinatesEventInformer) HasSynced() bool { return i.synced }

type legacyCoordinatesPayloadInformer struct {
	releasepayloadinformer.ReleasePayloadInformer
	events *legacyCoordinatesEventInformer
}

func (i legacyCoordinatesPayloadInformer) Informer() cache.SharedIndexInformer { return i.events }

type legacyCoordinatesImageInformer struct {
	imagev1informer.ImageStreamInformer
	events *legacyCoordinatesEventInformer
}

func (i legacyCoordinatesImageInformer) Informer() cache.SharedIndexInformer { return i.events }

func TestLegacyReleaseCoordinatesInformer(t *testing.T) {
	client := fake.NewSimpleClientset()
	payloadInformer := releasepayloadinformers.NewSharedInformerFactory(client, 0).Release().V1alpha1().ReleasePayloads()
	imageInformer := imageinformers.NewSharedInformerFactory(imagefake.NewSimpleClientset(), 0).Image().V1().ImageStreams()
	payloadEvents := &legacyCoordinatesEventInformer{SharedIndexInformer: payloadInformer.Informer()}
	imageEvents := &legacyCoordinatesEventInformer{SharedIndexInformer: imageInformer.Informer()}
	c, err := NewLegacyReleaseCoordinatesController(
		legacyCoordinatesPayloadInformer{payloadInformer, payloadEvents}, client.ReleaseV1alpha1(),
		legacyCoordinatesImageInformer{imageInformer, imageEvents}, events.NewInMemoryRecorder("test", clock.RealClock{}))
	if err != nil {
		t.Fatal(err)
	}
	defer c.queue.ShutDown()
	if len(c.cachesToSync) != 2 {
		t.Fatalf("expected both informer caches, got %d", len(c.cachesToSync))
	}
	payloadEvents.synced = true
	if !c.cachesToSync[0]() || c.cachesToSync[1]() {
		t.Fatal("cache checks must include the payload and image stream informers")
	}
	imageEvents.synced = true
	if !c.cachesToSync[1]() {
		t.Fatal("image stream cache sync was not registered")
	}
	empty := newLegacyCoordinatesFixture().payload
	populated := empty.DeepCopy()
	populated.Spec.ReleaseCoordinates = []v1alpha1.ReleaseCoordinates{{Repository: "keep"}}
	tests := []struct {
		name string
		run  func()
		want int
	}{
		{"add nil", func() { payloadEvents.handler.OnAdd(empty, true) }, 1},
		{"add empty", func() {
			p := empty.DeepCopy()
			p.Spec.ReleaseCoordinates = []v1alpha1.ReleaseCoordinates{}
			payloadEvents.handler.OnAdd(p, false)
		}, 1},
		{"add populated", func() { payloadEvents.handler.OnAdd(populated, false) }, 0},
		{"update empty", func() { payloadEvents.handler.OnUpdate(empty, empty.DeepCopy()) }, 1},
		{"update to populated", func() { payloadEvents.handler.OnUpdate(empty, populated) }, 0},
		{"update to empty", func() { payloadEvents.handler.OnUpdate(populated, empty) }, 1},
		{"delete empty", func() { payloadEvents.handler.OnDelete(empty) }, 0},
		{"wrong object", func() { payloadEvents.handler.OnAdd(&imagev1.ImageStream{}, false) }, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.run()
			if c.queue.Len() != tc.want {
				t.Fatalf("expected %d queued payloads, got %d", tc.want, c.queue.Len())
			}
			if tc.want > 0 {
				key, _ := c.queue.Get()
				c.queue.Done(key)
				if key != "ocp/legacy-payload" {
					t.Errorf("unexpected key %q", key)
				}
			}
		})
	}
}

type legacyCoordinatesFailingImageLister struct {
	imagev1lister.ImageStreamLister
	err error
}

func (l legacyCoordinatesFailingImageLister) ImageStreams(string) imagev1lister.ImageStreamNamespaceLister {
	return l
}
func (l legacyCoordinatesFailingImageLister) Get(string) (*imagev1.ImageStream, error) {
	return nil, l.err
}

type legacyCoordinatesFailingPayloadLister struct {
	releasepayloadlister.ReleasePayloadLister
	err error
}

func (l legacyCoordinatesFailingPayloadLister) ReleasePayloads(string) releasepayloadlister.ReleasePayloadNamespaceLister {
	return l
}
func (l legacyCoordinatesFailingPayloadLister) Get(string) (*v1alpha1.ReleasePayload, error) {
	return nil, l.err
}

func TestLegacyReleaseCoordinatesOperationalErrors(t *testing.T) {
	for _, location := range []string{"payload", "target", "deleted", "invalid key"} {
		t.Run(location, func(t *testing.T) {
			f := newLegacyCoordinatesFixture()
			c, client, informer := newLegacyCoordinatesTestController(t, f)
			wantError := fmt.Errorf("%s lookup failed", location)
			key := "ocp/legacy-payload"
			switch location {
			case "payload":
				c.releasePayloadLister = legacyCoordinatesFailingPayloadLister{err: wantError}
			case "target":
				c.imageStreamLister = legacyCoordinatesFailingImageLister{err: wantError}
			case "deleted":
				if err := informer.Informer().GetIndexer().Delete(f.payload); err != nil {
					t.Fatal(err)
				}
				wantError = nil
			case "invalid key":
				key = "too/many/parts"
				wantError = nil
			}
			if err := c.sync(context.Background(), key); !errors.Is(err, wantError) {
				t.Errorf("expected %v, got %v", wantError, err)
			}
			if len(client.Actions()) != 0 {
				t.Errorf("unexpected writes: %v", client.Actions())
			}
		})
	}
}
