package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/golang/glog"
	"k8s.io/apimachinery/pkg/api/errors"

	imagev1 "github.com/openshift/api/image/v1"
)

func (c *Controller) releaseDefinition(is *imagev1.ImageStream) (*Release, bool, error) {
	src, ok := is.Annotations[releaseAnnotationConfig]
	if !ok {
		return nil, false, nil
	}
	cfg, err := c.parseReleaseConfig(src)
	if err != nil {
		return nil, false, fmt.Errorf("the %s annotation for %s is invalid: %v", releaseAnnotationConfig, is.Name, err)
	}

	targetImageStream, err := c.imageStreamLister.ImageStreams(c.releaseNamespace).Get(releaseImageStreamName)
	if errors.IsNotFound(err) {
		// TODO: something special here?
		glog.V(2).Infof("The release image stream %s/%s does not exist", c.releaseNamespace, releaseImageStreamName)
		return nil, false, terminalError{fmt.Errorf("the output release image stream %s/%s does not exist", releaseImageStreamName, is.Name)}
	}
	if err != nil {
		return nil, false, fmt.Errorf("unable to lookup release image stream: %v", err)
	}

	if len(is.Status.Tags) == 0 {
		glog.V(4).Infof("The release input has no status tags, waiting")
		return nil, false, nil
	}

	r := &Release{
		Source: is,
		Target: targetImageStream,
		Config: cfg,
	}
	return r, true, nil
}

func (c *Controller) parseReleaseConfig(data string) (*ReleaseConfig, error) {
	if len(data) > 4*1024 {
		return nil, fmt.Errorf("release config must be less than 4k")
	}
	obj, ok := c.parsedReleaseConfigCache.Get(data)
	if ok {
		cfg := obj.(ReleaseConfig)
		return &cfg, nil
	}
	cfg := &ReleaseConfig{}
	if err := json.Unmarshal([]byte(data), cfg); err != nil {
		return nil, err
	}
	if len(cfg.Name) == 0 {
		return nil, fmt.Errorf("release config must have a valid name")
	}
	copied := *cfg
	c.parsedReleaseConfigCache.Add(data, copied)
	return cfg, nil
}

func releaseGenerationFromObject(name string, annotations map[string]string) (int64, bool) {
	_, ok := annotations[releaseAnnotationSource]
	if !ok {
		return 0, false
	}
	s, ok := annotations[releaseAnnotationGeneration]
	if !ok {
		glog.V(4).Infof("Can't check job %s, no generation", name)
		return 0, false
	}
	generation, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		glog.V(4).Infof("Can't check job %s, generation is invalid: %v", name, err)
		return 0, false
	}
	return generation, true
}

func hashSpecTagImageDigests(is *imagev1.ImageStream) string {
	h := sha256.New()
	for _, tag := range is.Status.Tags {
		if len(tag.Items) == 0 {
			continue
		}
		latest := tag.Items[0]
		input := latest.Image
		if len(input) == 0 {
			input = latest.DockerImageReference
		}
		h.Write([]byte(input))
	}
	return fmt.Sprintf("sha256:%x", h.Sum(nil))
}

func queueKeyFor(annotation string) (queueKey, bool) {
	if len(annotation) == 0 {
		return queueKey{}, false
	}
	parts := strings.SplitN(annotation, "/", 2)
	if len(parts) != 2 {
		return queueKey{}, false
	}
	return queueKey{namespace: parts[0], name: parts[1]}, true
}

func tagNames(refs []*imagev1.TagReference) []string {
	names := make([]string, 0, len(refs))
	for _, ref := range refs {
		names = append(names, ref.Name)
	}
	return names
}

func int32p(i int32) *int32 {
	return &i
}

func containsTagReference(tags []*imagev1.TagReference, name string) bool {
	for _, tag := range tags {
		if name == tag.Name {
			return true
		}
	}
	return false
}

func findTagReference(is *imagev1.ImageStream, name string) *imagev1.TagReference {
	for i := range is.Spec.Tags {
		tag := &is.Spec.Tags[i]
		if tag.Name == name {
			return tag
		}
	}
	return nil
}
