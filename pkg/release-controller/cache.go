package releasecontroller

import (
	"fmt"
	"sort"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru"
	imagev1 "github.com/openshift/api/image/v1"
	imagelisters "github.com/openshift/client-go/image/listers/image/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog"
)

// latestImageCache tries to find the first valid tag matching
// the requested image stream with the matching name (or the first
// one when looking across all lexigraphically).
type latestImageCache struct {
	imageStream string
	tag         string
	interval    time.Duration

	cache       *lru.Cache
	lock        sync.Mutex
	lister      imagelisters.ImageStreamNamespaceLister
	last        string
	lastChecked time.Time
}

func NewLatestImageCache(imageStream string, tag string) *latestImageCache {
	cache, _ := lru.New(64)
	return &latestImageCache{
		imageStream: imageStream,
		tag:         tag,
		interval:    10 * time.Minute,
		cache:       cache,
	}
}

func (c *latestImageCache) SetLister(lister imagelisters.ImageStreamNamespaceLister) {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.lister = lister
}

func (c *latestImageCache) Get() (string, error) {
	c.lock.Lock()
	defer c.lock.Unlock()
	if c.lister == nil {
		return "", fmt.Errorf("not yet started")
	}
	if len(c.last) > 0 && c.lastChecked.After(time.Now().Add(-c.interval)) {
		return c.last, nil
	}

	// Find the first image stream matching the desired stream name name, or the first
	// one that isn't a stable image stream and has the requested tag. Stable image
	// streams
	var preferred *imagev1.ImageStream
	items, _ := c.lister.List(labels.Everything())
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	for _, item := range items {
		if len(c.imageStream) > 0 {
			if c.imageStream == item.Name {
				preferred = item
				break
			}
			continue
		}

		value, ok := item.Annotations[ReleaseAnnotationConfig]
		if !ok {
			continue
		}
		if spec := FindImagePullSpec(item, c.tag); len(spec) == 0 {
			continue
		}
		config, err := ParseReleaseConfig(value, c.cache)
		if err != nil {
			continue
		}
		if config.As == ReleaseConfigModeStable {
			continue
		}

		if preferred == nil {
			preferred = item
			continue
		}
		if len(c.imageStream) > 0 && c.imageStream == item.Name {
			preferred = item
			break
		}
	}

	if preferred != nil {
		if spec := FindImagePullSpec(preferred, c.tag); len(spec) > 0 {
			c.last = spec
			c.lastChecked = time.Now()
			klog.V(4).Infof("Resolved %s:%s to %s", c.imageStream, c.tag, spec)
			return spec, nil
		}
	}

	return "", fmt.Errorf("could not find a release image stream with :%s (tools=%s)", c.tag, c.imageStream)
}
