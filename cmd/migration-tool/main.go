package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	v1 "github.com/openshift/api/image/v1"
	imageclientset "github.com/openshift/client-go/image/clientset/versioned"
	imageclient "github.com/openshift/client-go/image/clientset/versioned/typed/image/v1"
	"github.com/openshift/release-controller/pkg/apis/release/v1alpha1"
	"github.com/openshift/release-controller/pkg/client/clientset/versioned"
	releasev1alpha1 "github.com/openshift/release-controller/pkg/client/clientset/versioned/typed/release/v1alpha1"
	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
	"github.com/spf13/cobra"
	"io/ioutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// defaultAggregateProwJobName the default ProwJob to call if no override is specified
	defaultAggregateProwJobName = "release-openshift-release-analysis-aggregator"
)

type options struct {
	Execute       bool
	Name          string
	Namespace     string
	ProwNamespace string
	Location      string
}

func main() {
	original := flag.CommandLine
	klog.InitFlags(original)
	original.Set("alsologtostderr", "true")
	original.Set("v", "2")

	opt := &options{
		Namespace:     "ocp",
		Name:          "release",
		ProwNamespace: "ci",
	}

	cmd := &cobra.Command{
		Run: func(cmd *cobra.Command, arguments []string) {
			if err := opt.Run(); err != nil {
				klog.Exitf("Run error: %v", err)
			}
		},
	}

	flagset := cmd.Flags()
	flagset.BoolVar(&opt.Execute, "execute", opt.Execute, "Create ReleasePayload resources on currently configured cluster")
	flagset.StringVar(&opt.Namespace, "namespace", opt.Namespace, "Namespace where the image stream, containing releases, resides")
	flagset.StringVar(&opt.Name, "name", opt.Name, "Name of the image stream, containing releases, to process")
	flagset.StringVar(&opt.ProwNamespace, "prow-namespace", opt.ProwNamespace, "The namespace where Prow jobs are executed.")
	flagset.StringVar(&opt.Location, "location", opt.Location, "The path to a folder containing the release payloads from a previous invocation.")
	flagset.AddGoFlag(original.Lookup("v"))

	if err := cmd.Execute(); err != nil {
		klog.Exitf("Execute error: %v", err)
	}
}

func (o *options) Run() error {
	ctx := context.Background()
	ctxWithCancel, cancelFunction := context.WithCancel(ctx)

	defer func() {
		cancelFunction()
	}()

	stopCh := make(chan bool)

	inClusterCfg, err := loadClusterConfig(&clientcmd.ConfigOverrides{})
	imageClient, err := imageclientset.NewForConfig(inClusterCfg)
	if err != nil {
		return fmt.Errorf("unable to create image client: %v", err)
	}

	releasePayloadClient, err := versioned.NewForConfig(inClusterCfg)
	if err != nil {
		return fmt.Errorf("unable to create release payload client: %v", err)
	}

	go mainProcessLoop(ctxWithCancel, stopCh, imageClient.ImageV1(), releasePayloadClient.ReleaseV1alpha1(), o.Namespace, o.Name, o.ProwNamespace, o.Execute, o.Location)

	select {
	case <-ctx.Done():
		klog.V(5).Infof("Context has been cancelled")
	case <-stopCh:
		klog.V(5).Infof("mainProcessLoop returned")
	}
	return nil
}

func loadClusterConfig(overrides *clientcmd.ConfigOverrides) (*rest.Config, error) {
	cfg := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(clientcmd.NewDefaultClientConfigLoadingRules(), overrides)
	clusterConfig, err := cfg.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("could not load client configuration: %v", err)
	}
	return clusterConfig, nil
}

func mainProcessLoop(ctx context.Context, stopCh chan bool, imageClient imageclient.ImageV1Interface, releasePayloadClient releasev1alpha1.ReleaseV1alpha1Interface, namespace, name, prowNamespace string, execute bool, location string) {
	ctxWithTimeout, cancelFunction := context.WithTimeout(ctx, time.Duration(15)*time.Second)

	defer func() {
		cancelFunction()
		stopCh <- true
	}()

	var errors []error
	var payloads []*v1alpha1.ReleasePayload
	controller := newController(ctxWithTimeout, imageClient, namespace, name, prowNamespace)

	if !execute {
		klog.Infof("Running in Dry-Run mode.  No objects will be created on cluster.")
	}

	if len(location) > 0 {
		payloads = readPayloads(location)

		if execute {
			errors = createPayloads(releasePayloadClient, payloads)
		}
	} else {
		releaseConfigs, err := controller.processImagestreams()
		if err != nil {
			klog.Errorf("Unable to gather release configs: %v", err)
			return
		}
		releases, skipped, problems := controller.processReleaseImagestream()
		payloads = controller.generatePayloads(releases, releaseConfigs)

		klog.Infof("Successfully generated %d ReleasePayload definitions", len(payloads))
		klog.Warningf("The following %d tag(s) have problems:\n%s", len(problems), strings.Join(problems, ","))
		klog.Warningf("The following %d tag(s) cannot be migrated:\n%s", len(skipped), strings.Join(skipped, ","))

		if execute {
			errors = createPayloads(releasePayloadClient, payloads)
		} else {
			writePayloads(payloads)
		}
	}

	if len(errors) > 0 {
		klog.Info("The following errors occurred:\n")
		for _, e := range errors {
			klog.Warningf(e.Error())
		}
	}
}

type Controller struct {
	Context       context.Context
	ImageClient   imageclient.ImageV1Interface
	Namespace     string
	Name          string
	ProwNamespace string
}

func newController(ctx context.Context, client imageclient.ImageV1Interface, namespace, name, prowNamespace string) *Controller {
	return &Controller{
		Context:       ctx,
		ImageClient:   client,
		Namespace:     namespace,
		Name:          name,
		ProwNamespace: prowNamespace,
	}
}

func (c Controller) processImagestreams() (map[string]*releasecontroller.ReleaseConfig, error) {
	configs := make(map[string]*releasecontroller.ReleaseConfig)
	list, err := c.ImageClient.ImageStreams(c.Namespace).List(c.Context, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	for _, is := range list.Items {
		var content string
		var ok bool
		if content, ok = is.Annotations[releasecontroller.ReleaseAnnotationConfig]; !ok {
			continue
		}
		releaseConfig := &releasecontroller.ReleaseConfig{}
		if err = json.Unmarshal([]byte(content), &releaseConfig); err != nil {
			klog.Errorf("Unable to unmarshal release config for %s: %v", is.Name, err)
			continue
		}
		configs[fmt.Sprintf("%s/%s", is.Namespace, is.Name)] = releaseConfig
	}
	klog.V(4).Infof("Found %d ReleaseConfigs", len(configs))
	return configs, nil
}

type MigrationData struct {
	Name                       string
	ReleaseStream              string
	Phase                      string
	SourceImageStreamName      string
	SourceImageStreamNamespace string
	Status                     releasecontroller.VerificationStatusMap
	Message                    string
	Reason                     string
}

func (c Controller) processReleaseImagestream() ([]MigrationData, []string, []string) {
	is, err := c.ImageClient.ImageStreams(c.Namespace).Get(c.Context, c.Name, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("Failed to pull imagestream: %v", err)
		return nil, nil, nil
	}

	klog.V(2).Infof("Processing ImageStream: %s/%s", is.Namespace, is.Name)

	var skipped []string
	var problems []string
	var releases []MigrationData

	for _, tag := range is.Spec.Tags {
		if tag.From != nil && tag.From.Kind == "ImageStreamTag" {
			klog.V(5).Infof("Found ImageStreamTag reference...Skipping tag: %s", tag.Name)
			continue
		}

		klog.V(4).Infof("Processing tag: %s", tag.Name)

		releaseStream, err := getAnnotation(tag, releasecontroller.ReleaseAnnotationName, true)
		if err != nil {
			klog.Warningf("Unable to determine releaseStream annotation: %v", err)
			skipped = append(skipped, tag.Name)
			continue
		}
		klog.V(4).Infof("    releaseStream: %s", releaseStream)

		phase, err := getAnnotation(tag, releasecontroller.ReleaseAnnotationPhase, true)
		if err != nil {
			klog.Warningf("Unable to determine phase annotation: %v", err)
			skipped = append(skipped, tag.Name)
			continue
		}
		klog.V(4).Infof("            phase: %s", phase)

		sourceImageStream, err := getAnnotation(tag, releasecontroller.ReleaseAnnotationSource, true)
		if err != nil {
			klog.Warningf("Unable to determine sourceImageStream annotation: %v", err)
			skipped = append(skipped, tag.Name)
			continue
		}
		klog.V(4).Infof("sourceImageStream: %s", sourceImageStream)

		parts := strings.Split(sourceImageStream, "/")
		if len(parts) != 2 {
			klog.Warningf("Unable to determine the namespace/name for %s: %s", tag.Name, sourceImageStream)
			skipped = append(skipped, tag.Name)
			continue
		}
		sourceImageStreamNamespace := parts[0]
		sourceImageStreamName := parts[1]

		if c.Namespace != sourceImageStreamNamespace {
			klog.Errorf("Validation error: inconsistent namespaces: %s != %s", c.Namespace, sourceImageStreamNamespace)
			problems = append(problems, tag.Name)
			continue
		}

		status, err := getAnnotation(tag, releasecontroller.ReleaseAnnotationVerify, true)
		if err != nil {
			klog.Warningf("Unable to determine verification status annotation: %v", err)
			skipped = append(skipped, tag.Name)
			continue
		}

		verifyStatus := make(releasecontroller.VerificationStatusMap)
		if err = json.Unmarshal([]byte(status), &verifyStatus); err != nil {
			klog.Warningf("Unable to unmarshal verification status for %s: %v", tag.Name, err)
			skipped = append(skipped, tag.Name)
			continue
		}

		message, err := getAnnotation(tag, releasecontroller.ReleaseAnnotationMessage, false)
		if err != nil {
			klog.Warningf("Unable to determine message annotation: %v", err)
		}
		klog.V(4).Infof("          message: %s", message)

		reason, err := getAnnotation(tag, releasecontroller.ReleaseAnnotationReason, false)
		if err != nil {
			klog.Warningf("Unable to determine reason annotation: %v", err)
		}
		klog.V(4).Infof("           reason: %s", reason)

		releases = append(releases, MigrationData{
			Name:                       tag.Name,
			ReleaseStream:              releaseStream,
			Phase:                      phase,
			SourceImageStreamName:      sourceImageStreamName,
			SourceImageStreamNamespace: sourceImageStreamNamespace,
			Status:                     verifyStatus,
			Message:                    message,
			Reason:                     reason,
		})
	}

	return releases, skipped, problems
}

func getAnnotation(tag v1.TagReference, annotation string, required bool) (string, error) {
	var ok bool
	var content string
	if content, ok = tag.Annotations[annotation]; !ok {
		if required {
			return "", fmt.Errorf("unable to locate '%s' annotation in tag: %s", annotation, tag.Name)
		}
		content = ""
	}
	return content, nil
}

func (c Controller) generatePayloads(releases []MigrationData, configs map[string]*releasecontroller.ReleaseConfig) []*v1alpha1.ReleasePayload {
	var payloads []*v1alpha1.ReleasePayload
	for _, release := range releases {
		config := configs[fmt.Sprintf("%s/%s", release.SourceImageStreamNamespace, release.SourceImageStreamName)]
		payloads = append(payloads, newReleasePayload(release, config, c.ProwNamespace))
	}
	return payloads
}

func newReleasePayload(release MigrationData, config *releasecontroller.ReleaseConfig, prowNamespace string) *v1alpha1.ReleasePayload {
	payload := v1alpha1.ReleasePayload{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ReleasePayload",
			APIVersion: "release.openshift.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        release.Name,
			Namespace:   release.SourceImageStreamNamespace,
			Labels:      map[string]string{},
			Annotations: map[string]string{},
		},
		Spec: v1alpha1.ReleasePayloadSpec{
			PayloadCoordinates: v1alpha1.PayloadCoordinates{
				Namespace:          release.SourceImageStreamNamespace,
				ImagestreamName:    config.To,
				ImagestreamTagName: release.Name,
			},
			PayloadCreationConfig: v1alpha1.PayloadCreationConfig{
				ReleaseCreationCoordinates: v1alpha1.ReleaseCreationCoordinates{
					Namespace:              "",
					ReleaseCreationJobName: "",
				},
				ProwCoordinates: v1alpha1.ProwCoordinates{
					Namespace: "",
				},
			},
			PayloadOverride: v1alpha1.ReleasePayloadOverride{
				Override: "",
				Reason:   "",
			},
			PayloadVerificationConfig: v1alpha1.PayloadVerificationConfig{
				BlockingJobs:  []v1alpha1.CIConfiguration{},
				InformingJobs: []v1alpha1.CIConfiguration{},
			},
		},
		Status: v1alpha1.ReleasePayloadStatus{
			BlockingJobResults:  []v1alpha1.JobStatus{},
			InformingJobResults: []v1alpha1.JobStatus{},
		},
	}

	for alias, job := range config.Verify {
		if job.Disabled {
			continue
		}
		for k, v := range release.Status {
			var _ *v1alpha1.JobStatus
			if k != alias {
				continue
			}

			jobName := job.ProwJob.Name

			if job.AggregatedProwJob != nil {
				jobName = fmt.Sprintf("%s-%s", alias, defaultAggregateProwJobName)
			}

			_ = v1alpha1.JobRunResult{
				Coordinates: v1alpha1.JobRunCoordinates{
					Name:      jobName,
					Namespace: prowNamespace,
				},
				State:               v1alpha1.JobRunState(v.State),
				HumanProwResultsURL: v.URL,
			}

			/*
				status = payload.Status.FindJob(job.ProwJob.Name, alias, job.Optional, job.MaxRetries)
				if status == nil {
					newJobStatus := v1alpha1.JobStatus{
						JobAlias:   alias,
						JobName:    job.ProwJob.Name,
						MaxRetries: job.MaxRetries,
						Optional:   job.Optional,
						JobRunResults: []v1alpha1.JobRunResult{
							result,
						},
					}
					switch {
					case job.AggregatedProwJob != nil:
						// The Aggregator job can be blocking or informing
						if job.Optional {
							payload.Status.InformingJobResults = append(payload.Status.InformingJobResults, newJobStatus)
						} else {
							payload.Status.BlockingJobResults = append(payload.Status.BlockingJobResults, newJobStatus)
						}
						// The Analysis jobs have their own category
						analysisJobStatus := newJobStatus.DeepCopy()
						analysisJobStatus.JobName = job.ProwJob.Name
						analysisJobStatus.JobAlias = alias
						analysisJobStatus.JobRunResults = []v1alpha1.JobRunResult{}
						payload.Status.AnalysisJobResults = append(payload.Status.AnalysisJobResults, *analysisJobStatus)
					case job.Optional:
						payload.Status.InformingJobResults = append(payload.Status.InformingJobResults, newJobStatus)
					case !job.Optional:
						payload.Status.BlockingJobResults = append(payload.Status.BlockingJobResults, newJobStatus)
					default:
						klog.Warningf("Unable to classify job type: [%s] %s", config.Name, alias)
					}
				} else {
					status.JobRunResults = append(status.JobRunResults, result)
				}
			*/
		}
	}

	/*
		condition := v1alpha1.ReleasePayloadCondition{
			Status:             "True",
			LastTransitionTime: metav1.Now(),
			Message:            "ReleasePayload manually created by migration-tool",
		}

		switch release.Phase {
		case "Accepted":
			condition.Type = v1alpha1.PayloadAccepted
		case "Failed":
			condition.Type = v1alpha1.PayloadFailed
		case "Rejected":
			condition.Type = v1alpha1.PayloadRejected
		case "Ready":
			condition.Type = v1alpha1.PayloadCreated
		default:
			klog.Fatalf("Unable to determine release state: %s", release.Phase)
		}

		if len(release.Message) > 0 {
			condition.Message = release.Message
		}
		if len(release.Reason) > 0 {
			condition.Reason = release.Reason
		}
		payload.Status.Conditions = append(payload.Status.Conditions, condition)

	*/
	return &payload
}

func writePayloads(payloads []*v1alpha1.ReleasePayload) {
	dir, err := ioutil.TempDir(os.TempDir(), "payloads-*")
	if err != nil {
		klog.Fatal(err)
	}
	klog.Infof("Writing %d ReleasePayloads to: %s", len(payloads), dir)

	for _, payload := range payloads {
		f, err := os.Create(filepath.Join(dir, fmt.Sprintf("%s.json", payload.Name)))
		if err != nil {
			klog.Fatalf("Unable to create file (%s): %v", fmt.Sprintf("%s.json", payload.Name), err)
		}

		defer f.Close()

		data, err := json.MarshalIndent(&payload, "", "    ")
		if err != nil {
			klog.Fatalf("Unable to marshal payload (%s): %v", f.Name(), err)
		}

		_, err = f.Write(data)
		if err != nil {
			klog.Fatalf("Unable to write payload (%s): %v", f.Name(), err)
		}
	}
}

func readPayloads(location string) []*v1alpha1.ReleasePayload {
	klog.Infof("Reading ReleasePayloads from: %s", location)
	files, err := ioutil.ReadDir(location)
	if err != nil {
		klog.Fatalf("Unable to read from location (%s): %v", location, err)
	}

	var payloads []*v1alpha1.ReleasePayload
	for _, file := range files {
		fullPath := filepath.Join(location, file.Name())
		content, err := ioutil.ReadFile(fullPath)
		if err != nil {
			klog.Fatalf("Unable to read file (%s): %v", fullPath, err)
		}

		payload := v1alpha1.ReleasePayload{}
		err = json.Unmarshal(content, &payload)
		if err != nil {
			klog.Fatalf("Unable to unmarshal payload (%s): %v", fullPath, err)
			continue
		}
		payloads = append(payloads, &payload)
	}
	return payloads
}

func createPayloads(client releasev1alpha1.ReleaseV1alpha1Interface, payloads []*v1alpha1.ReleasePayload) []error {
	klog.Infof("Creating ReleasePayloads")
	var errors []error

	for _, payload := range payloads {
		if len(errors) >= 5 {
			klog.Error("Maximum error count reached")
			return errors
		}
		_, err := client.ReleasePayloads(payload.Namespace).Create(context.TODO(), payload, metav1.CreateOptions{})
		if err != nil {
			errors = append(errors, fmt.Errorf("unable to create ReleasePayload (%s/%s): %v", payload.Namespace, payload.Name, err))
			continue
		}
	}
	return errors
}
