package main

import (
	"encoding/json"
	"maps"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	imagev1 "github.com/openshift/api/image/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	batchlisters "k8s.io/client-go/listers/batch/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"

	imagefake "github.com/openshift/client-go/image/clientset/versioned/fake"
	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
)

const (
	konfluxTestTag      = "4.21.0-0.nightly-2026-01-02-030405"
	konfluxTestStep     = "konflux-prod"
	konfluxTestSnapshot = "hypershift-abcde"
	konfluxTestJobNS    = "ci"
)

func konfluxTestConfig() *releasecontroller.PublishKonfluxRelease {
	return &releasecontroller.PublishKonfluxRelease{
		Namespace:   "ocp-hypershift-tenant",
		ReleasePlan: "hypershift-prod-release-plan",
		SecretName:  "konflux-kubeconfig",
		CLIImage:    "quay.io/openshift/cli:latest",
	}
}

// konfluxTestRelease builds a layered release holding a single accepted tag carrying the
// given annotations on top of the accepted phase.
func konfluxTestRelease(annotations map[string]string) *releasecontroller.Release {
	tagAnnotations := map[string]string{
		releasecontroller.ReleaseAnnotationPhase: releasecontroller.ReleasePhaseAccepted,
	}
	maps.Copy(tagAnnotations, annotations)
	is := &imagev1.ImageStream{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ocp", Name: "hypershift-ci", Generation: 7},
		Spec: imagev1.ImageStreamSpec{Tags: []imagev1.TagReference{{
			Name:        konfluxTestTag,
			Annotations: tagAnnotations,
		}}},
	}
	return &releasecontroller.Release{
		Source: is,
		Target: is,
		Config: &releasecontroller.ReleaseConfig{Name: "hypershift-ci", As: releasecontroller.ReleaseConfigModeLayered},
	}
}

// konfluxTestController wires up a Controller with the fakes ensureKonfluxRelease needs. Any
// jobs passed are seeded into both the lister and the client, as though they already existed.
func konfluxTestController(release *releasecontroller.Release, jobs ...*batchv1.Job) (*Controller, *record.FakeRecorder) {
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	objects := []runtime.Object{}
	for _, job := range jobs {
		if err := indexer.Add(job); err != nil {
			panic(err)
		}
		objects = append(objects, job)
	}
	recorder := record.NewFakeRecorder(10)
	return &Controller{
		imageClient:   imagefake.NewSimpleClientset(release.Target).ImageV1(),
		jobClient:     k8sfake.NewSimpleClientset(objects...).BatchV1(),
		jobLister:     batchlisters.NewJobLister(indexer),
		eventRecorder: recorder,
		jobNamespace:  konfluxTestJobNS,
	}, recorder
}

func TestEnsureKonfluxRelease(t *testing.T) {
	jobName := konfluxReleaseName(konfluxTestTag, konfluxTestStep, konfluxTestSnapshot)
	releaseName := konfluxReleaseName(konfluxTestTag, konfluxTestStep, konfluxTestSnapshot)

	job := func(mutate func(*batchv1.Job)) *batchv1.Job {
		j := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Namespace: konfluxTestJobNS, Name: jobName}}
		if mutate != nil {
			mutate(j)
		}
		return j
	}

	testCases := []struct {
		name string
		// tagAnnotations are added to the accepted tag before the call.
		tagAnnotations map[string]string
		existingJobs   []*batchv1.Job
		expectJob      bool
		expectStamped  string
		expectEvent    string
	}{
		{
			name:           "already created, nothing to do",
			tagAnnotations: map[string]string{releasecontroller.KonfluxReleaseAnnotation(konfluxTestStep): "some-release"},
			expectJob:      false,
			expectStamped:  "some-release",
		},
		{
			name:           "no snapshot annotation reports an event and does nothing",
			tagAnnotations: nil,
			expectJob:      false,
			expectEvent:    "KonfluxSnapshotMissing",
		},
		{
			name:           "creates the job when the snapshot is present",
			tagAnnotations: map[string]string{releasecontroller.ReleaseAnnotationKonfluxSnapshot: konfluxTestSnapshot},
			expectJob:      true,
		},
		{
			name:           "does not stamp while the job is running",
			tagAnnotations: map[string]string{releasecontroller.ReleaseAnnotationKonfluxSnapshot: konfluxTestSnapshot},
			existingJobs:   []*batchv1.Job{job(nil)},
			expectJob:      true,
		},
		{
			name:           "stamps the tag once the job succeeds",
			tagAnnotations: map[string]string{releasecontroller.ReleaseAnnotationKonfluxSnapshot: konfluxTestSnapshot},
			existingJobs: []*batchv1.Job{job(func(j *batchv1.Job) {
				j.Status.CompletionTime = &metav1.Time{}
			})},
			expectJob:     true,
			expectStamped: releaseName,
			expectEvent:   "KonfluxReleaseCreated",
		},
		{
			name:           "reports an event when the job fails and does not stamp",
			tagAnnotations: map[string]string{releasecontroller.ReleaseAnnotationKonfluxSnapshot: konfluxTestSnapshot},
			existingJobs: []*batchv1.Job{job(func(j *batchv1.Job) {
				j.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue}}
			})},
			expectJob:   true,
			expectEvent: "KonfluxReleaseFailed",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			release := konfluxTestRelease(tc.tagAnnotations)
			c, recorder := konfluxTestController(release, tc.existingJobs...)

			tag := releasecontroller.FindTagReference(release.Target, konfluxTestTag)
			if err := c.ensureKonfluxRelease(release, konfluxTestStep, konfluxTestConfig(), tag); err != nil {
				t.Fatalf("ensureKonfluxRelease: unexpected error: %v", err)
			}

			jobs, err := c.jobClient.Jobs(konfluxTestJobNS).List(t.Context(), metav1.ListOptions{})
			if err != nil {
				t.Fatalf("listing jobs: %v", err)
			}
			if got := len(jobs.Items) > 0; got != tc.expectJob {
				t.Errorf("expected job to exist=%v, got %v", tc.expectJob, got)
			}

			// setReleaseAnnotation writes through release.Target, so read the stamp back from
			// the release rather than from the stale tag pointer.
			stamped := releasecontroller.FindTagReference(release.Target, konfluxTestTag).
				Annotations[releasecontroller.KonfluxReleaseAnnotation(konfluxTestStep)]
			if stamped != tc.expectStamped {
				t.Errorf("expected tag annotation %q, got %q", tc.expectStamped, stamped)
			}

			assertKonfluxEvent(t, recorder, tc.expectEvent)
		})
	}
}

// TestEnsureKonfluxReleaseStampsATagThatIsStillAnnotatedReady covers the phase annotation on
// the tag lagging the phase of the ReleasePayload. syncAccepted selects tags through
// GetTagPhase, which prefers the payload, and once the payload reports Accepted the tag is no
// longer listed as ready, so syncReady stops reaching it and never writes Accepted onto the
// tag. Recording the Konflux Release must not depend on that annotation having caught up.
func TestEnsureKonfluxReleaseStampsATagThatIsStillAnnotatedReady(t *testing.T) {
	release := konfluxTestRelease(map[string]string{
		releasecontroller.ReleaseAnnotationKonfluxSnapshot: konfluxTestSnapshot,
	})
	// The payload has accepted the release, which is why syncAccepted is looking at it, but
	// the tag itself is still annotated as ready.
	tag := releasecontroller.FindTagReference(release.Target, konfluxTestTag)
	tag.Annotations[releasecontroller.ReleaseAnnotationPhase] = releasecontroller.ReleasePhaseReady
	release.PayloadPhases = map[string]string{konfluxTestTag: releasecontroller.ReleasePhaseAccepted}

	jobName := konfluxReleaseName(konfluxTestTag, konfluxTestStep, konfluxTestSnapshot)
	c, _ := konfluxTestController(release, &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Namespace: konfluxTestJobNS, Name: jobName},
		Status:     batchv1.JobStatus{CompletionTime: &metav1.Time{}},
	})

	if err := c.ensureKonfluxRelease(release, konfluxTestStep, konfluxTestConfig(), tag); err != nil {
		t.Fatalf("ensureKonfluxRelease: unexpected error: %v", err)
	}

	stamped := releasecontroller.FindTagReference(release.Target, konfluxTestTag).
		Annotations[releasecontroller.KonfluxReleaseAnnotation(konfluxTestStep)]
	if expected := konfluxReleaseName(konfluxTestTag, konfluxTestStep, konfluxTestSnapshot); stamped != expected {
		t.Errorf("expected the tag to be stamped with %q, got %q", expected, stamped)
	}
}

// TestEnsureKonfluxReleaseDoesNotReuseAJobForAnotherSnapshot covers a tag whose Snapshot
// changes while a job from the previous Snapshot is still around. The Release is named after
// the Snapshot, so the job has to be too: reusing the old job would stamp the tag with the
// name of a Release that was never created, and the guard would then stop the new Snapshot
// from ever being released.
func TestEnsureKonfluxReleaseDoesNotReuseAJobForAnotherSnapshot(t *testing.T) {
	const previousSnapshot = "hypershift-previous"

	release := konfluxTestRelease(map[string]string{
		releasecontroller.ReleaseAnnotationKonfluxSnapshot: konfluxTestSnapshot,
	})
	// A completed job for the Snapshot this tag used to carry. The tag was never stamped,
	// so nothing short circuits before the job is looked up.
	previousJob := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: konfluxTestJobNS,
			Name:      konfluxReleaseName(konfluxTestTag, konfluxTestStep, previousSnapshot),
		},
		Status: batchv1.JobStatus{CompletionTime: &metav1.Time{}},
	}
	c, _ := konfluxTestController(release, previousJob)

	tag := releasecontroller.FindTagReference(release.Target, konfluxTestTag)
	if err := c.ensureKonfluxRelease(release, konfluxTestStep, konfluxTestConfig(), tag); err != nil {
		t.Fatalf("ensureKonfluxRelease: unexpected error: %v", err)
	}

	if stamped := releasecontroller.FindTagReference(release.Target, konfluxTestTag).
		Annotations[releasecontroller.KonfluxReleaseAnnotation(konfluxTestStep)]; stamped != "" {
		t.Errorf("tag was stamped with %q on the strength of a job for another snapshot, so the current snapshot would never be released", stamped)
	}

	jobs, err := c.jobClient.Jobs(konfluxTestJobNS).List(t.Context(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("listing jobs: %v", err)
	}
	var found bool
	for _, job := range jobs.Items {
		if job.Name == konfluxReleaseName(konfluxTestTag, konfluxTestStep, konfluxTestSnapshot) {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a job for the current snapshot to be created, got %d jobs", len(jobs.Items))
	}
}

// TestEnsureKonfluxReleaseIsExactlyOnce is the regression test for the central risk of this
// publish step: unlike every other one, creating a Konflux Release twice releases twice, and
// syncAccepted re-enters it on every sync of the stream.
func TestEnsureKonfluxReleaseIsExactlyOnce(t *testing.T) {
	release := konfluxTestRelease(map[string]string{
		releasecontroller.ReleaseAnnotationKonfluxSnapshot: konfluxTestSnapshot,
	})
	c, recorder := konfluxTestController(release)
	config := konfluxTestConfig()

	call := func(t *testing.T) {
		t.Helper()
		tag := releasecontroller.FindTagReference(release.Target, konfluxTestTag)
		if err := c.ensureKonfluxRelease(release, konfluxTestStep, config, tag); err != nil {
			t.Fatalf("ensureKonfluxRelease: unexpected error: %v", err)
		}
	}

	// First pass creates the job.
	call(t)
	created, err := c.jobClient.Jobs(konfluxTestJobNS).List(t.Context(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("listing jobs: %v", err)
	}
	if len(created.Items) != 1 {
		t.Fatalf("expected exactly one job, got %d", len(created.Items))
	}
	jobName := created.Items[0].Name

	// Resyncing while the job runs must not create a second one.
	call(t)

	// The job completes and the tag is stamped.
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	done := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Namespace: konfluxTestJobNS, Name: jobName},
		Status:     batchv1.JobStatus{CompletionTime: &metav1.Time{}},
	}
	if err := indexer.Add(done); err != nil {
		t.Fatalf("seeding job: %v", err)
	}
	c.jobLister = batchlisters.NewJobLister(indexer)
	call(t)

	stamped := releasecontroller.FindTagReference(release.Target, konfluxTestTag).
		Annotations[releasecontroller.KonfluxReleaseAnnotation(konfluxTestStep)]
	if stamped == "" {
		t.Fatalf("expected the tag to be stamped once the job succeeded")
	}

	// The job is garbage collected two hours after it completes, and the generation of the
	// stream moves on. Neither may cause the release to be created again.
	c.jobLister = batchlisters.NewJobLister(cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}))
	if err := c.jobClient.Jobs(konfluxTestJobNS).Delete(t.Context(), jobName, metav1.DeleteOptions{}); err != nil {
		t.Fatalf("deleting job: %v", err)
	}
	release.Target.Generation = 99

	for range 3 {
		call(t)
	}

	remaining, err := c.jobClient.Jobs(konfluxTestJobNS).List(t.Context(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("listing jobs: %v", err)
	}
	if len(remaining.Items) != 0 {
		t.Errorf("job was recreated after garbage collection, which would release a second time: %d jobs", len(remaining.Items))
	}
	drainEvents(recorder)
}

func TestKonfluxReleaseJobSpec(t *testing.T) {
	release := konfluxTestRelease(map[string]string{
		releasecontroller.ReleaseAnnotationKonfluxSnapshot: konfluxTestSnapshot,
	})
	c, _ := konfluxTestController(release)
	config := konfluxTestConfig()
	tag := releasecontroller.FindTagReference(release.Target, konfluxTestTag)
	releaseName := konfluxReleaseName(konfluxTestTag, konfluxTestStep, konfluxTestSnapshot)

	job, err := c.ensureKonfluxReleaseJob(release, config, tag, konfluxTestSnapshot, releaseName)
	if err != nil {
		t.Fatalf("ensureKonfluxReleaseJob: unexpected error: %v", err)
	}

	if job.Spec.Template.Spec.Containers[0].Image != config.CLIImage {
		t.Errorf("expected image %q, got %q", config.CLIImage, job.Spec.Template.Spec.Containers[0].Image)
	}

	var mounted bool
	for _, volume := range job.Spec.Template.Spec.Volumes {
		if volume.Secret != nil && volume.Secret.SecretName == config.SecretName {
			mounted = true
		}
	}
	if !mounted {
		t.Errorf("expected the kubeconfig secret %q to be mounted", config.SecretName)
	}

	var kubeconfig string
	for _, env := range job.Spec.Template.Spec.Containers[0].Env {
		if env.Name == "KUBECONFIG" {
			kubeconfig = env.Value
		}
	}
	if kubeconfig != konfluxKubeconfigMountPath+"/kubeconfig" {
		t.Errorf("expected KUBECONFIG to point at the mounted secret, got %q", kubeconfig)
	}

	// This job talks to the Konflux cluster, not to a registry, so it must not inherit the
	// registry login preamble that newReleaseJobBase returns.
	script := strings.Join(job.Spec.Template.Spec.Containers[0].Command, " ")
	if strings.Contains(script, "oc registry login") {
		t.Errorf("job should not log in to a registry:\n%s", script)
	}
	if !strings.Contains(script, "already exists") {
		t.Errorf("job should tolerate an existing Release so a retry cannot release twice:\n%s", script)
	}

	// garbageCollectSync keys off ReleaseTag, and processJob re-enqueues the stream off Source.
	for _, key := range []string{
		releasecontroller.ReleaseAnnotationSource,
		releasecontroller.ReleaseAnnotationTarget,
		releasecontroller.ReleaseAnnotationGeneration,
		releasecontroller.ReleaseAnnotationReleaseTag,
	} {
		if len(job.Annotations[key]) == 0 {
			t.Errorf("expected job annotation %s to be set", key)
		}
	}
}

func TestKonfluxReleaseManifest(t *testing.T) {
	release := konfluxTestRelease(nil)
	tag := releasecontroller.FindTagReference(release.Target, konfluxTestTag)

	t.Run("without data", func(t *testing.T) {
		manifest, err := konfluxReleaseManifest(release, konfluxTestConfig(), tag, konfluxTestSnapshot, "my-release")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		spec := manifestSpec(t, manifest)
		expected := map[string]any{
			"snapshot":    konfluxTestSnapshot,
			"releasePlan": "hypershift-prod-release-plan",
		}
		if diff := cmp.Diff(expected, spec); diff != "" {
			t.Errorf("unexpected spec (-want +got):\n%s", diff)
		}
	})

	t.Run("release tag is expanded into the mapping", func(t *testing.T) {
		config := konfluxTestConfig()
		config.Data = json.RawMessage(`{"mapping": {"components": [{"name": "hypershift", "tags": ["${RELEASE_TAG}"]}]}}`)

		manifest, err := konfluxReleaseManifest(release, config, tag, konfluxTestSnapshot, "my-release")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		spec := manifestSpec(t, manifest)
		expectedData := map[string]any{
			"mapping": map[string]any{
				"components": []any{map[string]any{
					"name": "hypershift",
					"tags": []any{konfluxTestTag},
				}},
			},
		}
		if diff := cmp.Diff(expectedData, spec["data"]); diff != "" {
			t.Errorf("unexpected data (-want +got):\n%s", diff)
		}
	})
}

func TestKonfluxReleaseNaming(t *testing.T) {
	// A realistically long tag, to exercise the truncation path.
	longTag := "4.21.0-0.nightly-multi-2026-01-02-030405"

	t.Run("the name is valid and bounded", func(t *testing.T) {
		// It names a job as well as a Release, so it has to satisfy the stricter of the two.
		name := konfluxReleaseName(longTag, konfluxTestStep, konfluxTestSnapshot)
		if len(name) > 63 {
			t.Errorf("name %q is %d characters, over the 63 character limit", name, len(name))
		}
		if errs := validation.IsDNS1123Subdomain(name); len(errs) > 0 {
			t.Errorf("name %q is not a valid DNS1123 subdomain: %v", name, errs)
		}
	})

	t.Run("the release name is stable for the same inputs", func(t *testing.T) {
		first := konfluxReleaseName(konfluxTestTag, konfluxTestStep, konfluxTestSnapshot)
		second := konfluxReleaseName(konfluxTestTag, konfluxTestStep, konfluxTestSnapshot)
		if first != second {
			t.Errorf("expected a deterministic name, got %q then %q", first, second)
		}
	})

	// The job stands in for the Release when deciding whether the Release was created, so a
	// change that gives one of them a new identity has to give the other one too.
	t.Run("a different snapshot gets its own name", func(t *testing.T) {
		first := konfluxReleaseName(konfluxTestTag, konfluxTestStep, konfluxTestSnapshot)
		second := konfluxReleaseName(konfluxTestTag, konfluxTestStep, "hypershift-fghij")
		if first == second {
			t.Errorf("expected a different name for a different snapshot, both were %q", first)
		}
	})

	t.Run("steps do not collide", func(t *testing.T) {
		first := konfluxReleaseName(konfluxTestTag, "konflux-stage", konfluxTestSnapshot)
		second := konfluxReleaseName(konfluxTestTag, "konflux-prod", konfluxTestSnapshot)
		if first == second {
			t.Errorf("expected a different name per publish step, both were %q", first)
		}
	})
}

// TestKonfluxReleaseJobIsNamedAfterItsRelease pins the invariant that the job created for a
// tag is the job for that tag's current Release and no other. ensureKonfluxRelease treats a
// successful job as evidence that the Release exists, which only holds while the two
// identify the same thing, so an existing job under the Release's name must be adopted
// rather than a second one created alongside it.
func TestKonfluxReleaseJobIsNamedAfterItsRelease(t *testing.T) {
	release := konfluxTestRelease(map[string]string{
		releasecontroller.ReleaseAnnotationKonfluxSnapshot: konfluxTestSnapshot,
	})
	releaseName := konfluxReleaseName(konfluxTestTag, konfluxTestStep, konfluxTestSnapshot)
	// A job already running under the name of the Release for this tag and snapshot.
	c, _ := konfluxTestController(release, &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Namespace: konfluxTestJobNS, Name: releaseName},
	})

	tag := releasecontroller.FindTagReference(release.Target, konfluxTestTag)
	if err := c.ensureKonfluxRelease(release, konfluxTestStep, konfluxTestConfig(), tag); err != nil {
		t.Fatalf("ensureKonfluxRelease: unexpected error: %v", err)
	}

	jobs, err := c.jobClient.Jobs(konfluxTestJobNS).List(t.Context(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("listing jobs: %v", err)
	}
	if len(jobs.Items) != 1 || jobs.Items[0].Name != releaseName {
		t.Errorf("expected the job named after the Release %q to be adopted, got %d jobs: %v", releaseName, len(jobs.Items), jobNames(jobs.Items))
	}
}

func jobNames(jobs []batchv1.Job) []string {
	names := make([]string, 0, len(jobs))
	for _, job := range jobs {
		names = append(names, job.Name)
	}
	return names
}

func manifestSpec(t *testing.T, manifest string) map[string]any {
	t.Helper()
	var parsed map[string]any
	if err := json.Unmarshal([]byte(manifest), &parsed); err != nil {
		t.Fatalf("manifest is not valid JSON: %v\n%s", err, manifest)
	}
	if got := parsed["apiVersion"]; got != konfluxReleaseAPIVersion {
		t.Errorf("expected apiVersion %q, got %v", konfluxReleaseAPIVersion, got)
	}
	if got := parsed["kind"]; got != "Release" {
		t.Errorf("expected kind Release, got %v", got)
	}
	metadata, ok := parsed["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("manifest has no metadata:\n%s", manifest)
	}
	// The job is found by the name of the Release it creates, so the manifest has to ask for
	// the name it was given rather than deriving one of its own.
	if got := metadata["name"]; got != "my-release" {
		t.Errorf("expected the Release to be named %q, got %v", "my-release", got)
	}
	spec, ok := parsed["spec"].(map[string]any)
	if !ok {
		t.Fatalf("manifest has no spec:\n%s", manifest)
	}
	return spec
}

func assertKonfluxEvent(t *testing.T, recorder *record.FakeRecorder, reason string) {
	t.Helper()
	events := drainEvents(recorder)
	if reason == "" {
		if len(events) > 0 {
			t.Errorf("expected no events, got %v", events)
		}
		return
	}
	for _, event := range events {
		if strings.Contains(event, reason) {
			return
		}
	}
	t.Errorf("expected an event mentioning %s, got %v", reason, events)
}

func drainEvents(recorder *record.FakeRecorder) []string {
	var events []string
	for {
		select {
		case event := <-recorder.Events:
			events = append(events, event)
		default:
			return events
		}
	}
}
