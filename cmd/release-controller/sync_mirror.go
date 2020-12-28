package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"

	imagev1 "github.com/openshift/api/image/v1"
	imagereference "github.com/openshift/library-go/pkg/image/reference"
)

func (c *Controller) ensureReleaseMirror(release *Release, releaseTagName, inputImageHash string) (*imagev1.ImageStream, error) {
	mirrorName := mirrorName(release, releaseTagName)
	is, err := c.releaseLister.ImageStreams(release.Source.Namespace).Get(mirrorName)
	if err == nil {
		return is, nil
	}
	if !errors.IsNotFound(err) {
		return nil, err
	}

	is = &imagev1.ImageStream{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mirrorName,
			Namespace: release.Source.Namespace,
			Annotations: map[string]string{
				releaseAnnotationSource:     fmt.Sprintf("%s/%s", release.Source.Namespace, release.Source.Name),
				releaseAnnotationTarget:     fmt.Sprintf("%s/%s", release.Target.Namespace, release.Target.Name),
				releaseAnnotationReleaseTag: releaseTagName,
				releaseAnnotationImageHash:  inputImageHash,
				releaseAnnotationGeneration: strconv.FormatInt(release.Target.Generation, 10),
			},
		},
	}

	switch release.Config.As {
	case releaseConfigModeStable:
		// stream will be populated later
	default:
		if err := calculateMirrorImageStream(release, is); err != nil {
			return nil, err
		}
	}

	klog.V(2).Infof("Mirroring release images in %s/%s to %s/%s", release.Source.Namespace, release.Source.Name, is.Namespace, is.Name)
	is, err = c.imageClient.ImageStreams(is.Namespace).Create(context.TODO(), is, metav1.CreateOptions{})
	if err != nil {
		if !errors.IsAlreadyExists(err) {
			return nil, err
		}
		for {
			klog.V(4).Infof("Performing a live lookup of release images in %s/%s", is.Namespace, is.Name)
			// perform live reads, ensure we have observed a public image repo
			is, err = c.imageClient.ImageStreams(is.Namespace).Get(context.TODO(), is.Name, metav1.GetOptions{})
			if err != nil || (is != nil && is.Status.PublicDockerImageRepository != "") {
				break
			}
		}
	}
	return is, err
}

func (c *Controller) getMirror(release *Release, releaseTagName string) (*imagev1.ImageStream, error) {
	return c.releaseLister.ImageStreams(release.Source.Namespace).Get(mirrorName(release, releaseTagName))
}

func mirrorName(release *Release, releaseTagName string) string {
	switch release.Config.As {
	case releaseConfigModeStable:
		return releaseTagName
	default:
		suffix := strings.TrimPrefix(releaseTagName, release.Config.Name)
		if len(release.Config.MirrorPrefix) > 0 {
			return fmt.Sprintf("%s%s", release.Config.MirrorPrefix, suffix)
		}
		return fmt.Sprintf("%s%s", release.Source.Name, suffix)
	}
}

func calculateMirrorImageStream(release *Release, is *imagev1.ImageStream) error {
	// this block is mostly identical to the logic in openshift/origin pkg/oc/cli/admin/release/new which
	// calculates the spec tags - it preserves the desired source location of the image and errors when
	// we can't resolve or the result might be ambiguous
	forceExternal := release.Config.ReferenceMode == "public" || release.Config.ReferenceMode == ""
	internal := release.Source.Status.DockerImageRepository
	external := release.Source.Status.PublicDockerImageRepository
	if forceExternal && len(external) == 0 {
		return fmt.Errorf("only image streams with public image repositories can be the source for releases when using the default referenceMode")
	}

	for _, tag := range release.Source.Status.Tags {
		if len(tag.Items) == 0 {
			continue
		}
		source := tag.Items[0].DockerImageReference
		latest := tag.Items[0]
		if len(latest.Image) == 0 {
			continue
		}
		// eliminate status tag references that point to the outside
		if len(source) > 0 {
			if len(internal) > 0 && strings.HasPrefix(latest.DockerImageReference, internal) {
				klog.V(2).Infof("Can't use tag %q source %s because it points to the internal registry", tag.Tag, source)
				source = ""
			}
		}
		ref := findSpecTag(release.Source.Spec.Tags, tag.Tag)
		if ref == nil {
			ref = &imagev1.TagReference{Name: tag.Tag}
		} else {
			// prevent unimported images from being skipped
			if ref.Generation != nil && *ref.Generation > tag.Items[0].Generation {
				return fmt.Errorf("the tag %q in the source input stream has not been imported yet", tag.Tag)
			}
			// use the tag ref as the source
			if ref.From != nil && ref.From.Kind == "DockerImage" && !strings.HasPrefix(ref.From.Name, internal) {
				if from, err := imagereference.Parse(ref.From.Name); err == nil {
					from.Tag = ""
					from.ID = tag.Items[0].Image
					source = from.Exact()
				} else {
					klog.V(2).Infof("Can't use tag %q from %s because it isn't a valid image reference", tag.Tag, ref.From.Name)
				}
			}
			ref = ref.DeepCopy()
		}
		// default to the external registry name
		if (forceExternal || len(source) == 0) && len(external) > 0 {
			source = external + "@" + tag.Items[0].Image
		}
		if len(source) == 0 {
			return fmt.Errorf("can't use tag %q because we cannot locate or calculate a source location", tag.Tag)
		}
		sourceRef, err := imagereference.Parse(source)
		if err != nil {
			return fmt.Errorf("the tag %q points to source %q which is not valid", tag.Tag, source)
		}
		sourceRef.Tag = ""
		sourceRef.ID = tag.Items[0].Image
		source = sourceRef.Exact()

		if strings.HasPrefix(source, external+"@") {
			ref.From = &corev1.ObjectReference{
				Kind:      "ImageStreamImage",
				Namespace: release.Source.Namespace,
				Name:      fmt.Sprintf("%s@%s", release.Source.Name, latest.Image),
			}
		} else {
			ref.From = &corev1.ObjectReference{
				Kind: "DockerImage",
				Name: source,
			}
		}
		ref.ImportPolicy.Scheduled = false
		is.Spec.Tags = append(is.Spec.Tags, *ref)
	}
	return nil
}
