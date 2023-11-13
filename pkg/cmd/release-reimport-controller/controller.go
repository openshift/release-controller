package release_reimport_controller

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	imageclientset "github.com/openshift/client-go/image/clientset/versioned"
	imageinformers "github.com/openshift/client-go/image/informers/externalversions"
	imagelisters "github.com/openshift/client-go/image/listers/image/v1"
	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

type ImageReimportController struct {
	releaseLister *releasecontroller.MultiImageStreamLister
	dryRun        bool
}

func NewImageReimportController(imageClient *imageclientset.Clientset, namespaces []string, dryRun bool) *ImageReimportController {
	c := &ImageReimportController{
		releaseLister: &releasecontroller.MultiImageStreamLister{Listers: make(map[string]imagelisters.ImageStreamNamespaceLister)},
		dryRun:        dryRun,
	}
	var hasSynced []cache.InformerSynced
	stopCh := wait.NeverStop
	for _, ns := range namespaces {
		klog.V(3).Infof("Adding %s namespace to reimport controller", ns)
		factory := imageinformers.NewSharedInformerFactoryWithOptions(imageClient, 10*time.Minute, imageinformers.WithNamespace(ns))
		streams := factory.Image().V1().ImageStreams()
		c.releaseLister.Listers[ns] = streams.Lister().ImageStreams(ns)
		hasSynced = append(hasSynced, streams.Informer().HasSynced)
		factory.Start(stopCh)
	}
	cache.WaitForCacheSync(stopCh, hasSynced...)

	return c
}

func (c *ImageReimportController) Run(ctx context.Context, interval time.Duration) {
	wait.Until(c.sync, interval, ctx.Done())
}

func (c *ImageReimportController) sync() {
	klog.V(3).Info("Checking if images need reimport")
	streams, err := c.releaseLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("Failed to list releases: %s", err)
	}
	isNames := ""
	for _, stream := range streams {
		isNames += stream.Name + ", "
	}
	klog.V(3).Infof("List of imagestreams being checked: %s", strings.TrimSuffix(isNames, ", "))
	for _, stream := range streams {
		// only handle release imagestreams
		if _, ok := stream.Annotations[releasecontroller.ReleaseAnnotationConfig]; !ok {
			klog.V(4).Infof("Imagestream %s does not have release annotation", stream.Name)
			continue
		}
		klog.V(4).Infof("Checking tags for imagestream %s", stream.Name)
		for _, tag := range stream.Status.Tags {
			if len(tag.Conditions) == 0 {
				klog.V(4).Infof("%s/%s has no conditions", stream.Name, tag.Tag)
			} else {
				klog.V(4).Infof("%s/%s has the following conditions: %+v", stream.Name, tag.Tag, tag.Conditions)
			}
			for _, condition := range tag.Conditions {
				if condition.Type == "ImportSuccess" && condition.Status == "False" {
					klog.V(3).Infof("Reimporting %s:%s", stream.Name, tag.Tag)
					commandSlice := []string{"import-image"}
					if c.dryRun {
						commandSlice = append(commandSlice, "--dry-run")
					}
					commandSlice = append(commandSlice, fmt.Sprintf("%s:%s", stream.Name, tag.Tag))
					cmd := exec.Command("oc", commandSlice...)
					out, err := cmd.Output()
					if err != nil {
						klog.Errorf("Failed to run `%s`: %v", cmd.String(), err)
						continue
					}
					klog.V(4).Infof("Output of `%s`: %s", cmd.String(), out)
				}
			}
		}
	}
	klog.V(3).Infof("Finished image reimporting")
}
