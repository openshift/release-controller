package release_mirror_cleanup_controller

import (
	"context"
	"strings"
	"time"

	imageclientset "github.com/openshift/client-go/image/clientset/versioned"
	imageinformers "github.com/openshift/client-go/image/informers/externalversions"
	imagelisters "github.com/openshift/client-go/image/listers/image/v1"
	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"

	lru "github.com/hashicorp/golang-lru"
	"github.com/regclient/regclient"
	ref "github.com/regclient/regclient/types/ref"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

type MirrorCleanupController struct {
	releaseLister  *releasecontroller.MultiImageStreamLister
	lruCache       *lru.Cache
	registryClient *regclient.RegClient
	dryRun         bool
}

func NewMirrorCleanupController(imageClient *imageclientset.Clientset, namespaces []string, dryRun bool) *MirrorCleanupController {
	parsedReleaseConfigCache, err := lru.New(50)
	if err != nil {
		// this shouldn't ever happen
		panic(err)
	}
	c := &MirrorCleanupController{
		releaseLister:  &releasecontroller.MultiImageStreamLister{Listers: make(map[string]imagelisters.ImageStreamNamespaceLister)},
		dryRun:         dryRun,
		lruCache:       parsedReleaseConfigCache,
		registryClient: regclient.New(),
	}
	var hasSynced []cache.InformerSynced
	stopCh := wait.NeverStop
	for _, ns := range namespaces {
		klog.V(3).Infof("Adding %s namespace to mirror cleanup controller", ns)
		factory := imageinformers.NewSharedInformerFactoryWithOptions(imageClient, 6*time.Hour, imageinformers.WithNamespace(ns))
		streams := factory.Image().V1().ImageStreams()
		c.releaseLister.Listers[ns] = streams.Lister().ImageStreams(ns)
		hasSynced = append(hasSynced, streams.Informer().HasSynced)
		factory.Start(stopCh)
	}
	cache.WaitForCacheSync(stopCh, hasSynced...)

	return c
}

func (c *MirrorCleanupController) Run(ctx context.Context, interval time.Duration) {
	wait.Until(func() { c.sync(ctx) }, interval, ctx.Done())
}

func (c *MirrorCleanupController) sync(ctx context.Context) {
	klog.V(3).Info("Creating list of releases in CI registry")
	streams, err := c.releaseLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("Failed to list releases: %s", err)
	}
	isNames := ""
	for _, stream := range streams {
		isNames += stream.Name + ", "
	}
	klog.V(3).Infof("List of imagestreams being checked: %s", strings.TrimSuffix(isNames, ", "))
	klog.V(3).Infof("Identifying release tags and alternate repos")
	alternateRepos := sets.New[string]()
	validTags := sets.New[string]()
	for _, stream := range streams {
		// only handle release imagestreams
		if _, ok := stream.Annotations[releasecontroller.ReleaseAnnotationConfig]; !ok {
			klog.V(4).Infof("Imagestream %s does not have release annotation", stream.Name)
			continue
		}
		releaseDefinition, err := releasecontroller.ParseReleaseConfig(stream.Annotations[releasecontroller.ReleaseAnnotationConfig], c.lruCache)
		if err != nil {
			klog.Errorf("failed to parse release definition for imagestream %s: %v", stream.Name, err)
			continue
		}
		// "Stable" streams contain release tags
		if releaseDefinition.As == "Stable" {
			klog.V(4).Infof("Adding tags from %s/%s to valid tags list", stream.Namespace, stream.Name)
			for _, tags := range stream.Status.Tags {
				validTags.Insert(tags.Tag)
			}
		}
		// Keep set of alternate repos to look at
		if releaseDefinition.AlternateImageRepository != "" {
			klog.V(4).Infof("%s/%s has alternate mirror %s", stream.Namespace, stream.Name, releaseDefinition.AlternateImageRepository)
			alternateRepos.Insert(releaseDefinition.AlternateImageRepository)
		}
	}
	klog.V(3).Infof("Trimming alternate mirror tags")
	for repo := range alternateRepos {
		reference, err := ref.New(repo)
		if err != nil {
			klog.Errorf("failed to parse remote registry reference for %s: %v", repo, err)
			continue
		}
		tagList, err := c.registryClient.TagList(ctx, reference)
		if err != nil {
			klog.Errorf("failed to get remote tags for repo %s: %v", repo, err)
			continue
		}
		for _, tagName := range tagList.Tags {
			if !validTags.Has(tagName) {
				klog.V(3).Infof("Will remove tag %s from remote registry %s", tagName, repo)
				if c.dryRun {
					klog.V(3).Infof("Dry run enabled, not deleting tag %s from remote registry %s", tagName, repo)
				} else {
					if err := c.registryClient.TagDelete(ctx, reference.SetTag(tagName)); err != nil {
						klog.Errorf("failed to delete tag %s from remote registry %s: %v", tagName, repo, err)
						continue
					}
					klog.Infof("Deleted tag %s from remote registry %s", tagName, repo)
				}
			}
		}
	}
	klog.V(3).Infof("Finished mirror cleanup")
}
