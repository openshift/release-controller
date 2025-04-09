package main

import (
	"bytes"
	"slices"

	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	kv1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/klog"
)

func (c *Controller) PruneGraph(secretClient kv1core.SecretInterface, ns, name string, printOption string, confirm bool) error {
	stopCh := wait.NeverStop

	releasecontroller.LoadUpgradeGraph(c.graph, secretClient, ns, name, stopCh)

	imageStreams, err := c.releaseLister.ImageStreams(ns).List(labels.Everything())
	if err != nil {
		return err
	}

	var stableTagList []string

	for _, imageStream := range imageStreams {
		r, ok, err := releasecontroller.ReleaseDefinition(imageStream, c.parsedReleaseConfigCache, c.eventRecorder, *c.releaseLister)
		if err != nil || !ok {
			continue
		}
		if r.Config.As == releasecontroller.ReleaseConfigModeStable {
			for _, tag := range imageStream.Spec.Tags {
				stableTagList = append(stableTagList, tag.Name)
			}
		}
	}

	// To tags that are not present in the stable tags
	toMissingList := make([]string, 0, len(c.graph.To))
	for tag := range c.graph.To {
		if !slices.Contains(stableTagList, tag) {
			toMissingList = append(toMissingList, tag)
		}
	}

	// From tags that are not present in the stable tags
	fromMissingList := make([]string, 0, len(c.graph.From))
	for tag := range c.graph.From {
		if !slices.Contains(stableTagList, tag) {
			fromMissingList = append(fromMissingList, tag)
		}
	}

	// Combine the 2 lists into a set to eliminate duplicates
	pruneTagList := sets.NewString(append(toMissingList, fromMissingList...)...).UnsortedList()
	klog.V(2).Infof("Pruning %d/%d tags from release controller graph\n", len(pruneTagList), len(c.graph.To))

	c.graph.PruneTags(pruneTagList)

	if confirm {
		buf := &bytes.Buffer{}
		err = releasecontroller.SaveUpgradeGraph(buf, c.graph, secretClient, ns, name)
		if err != nil {
			return err
		}
	} else {
		switch printOption {
		case releasecontroller.PruneGraphPrintDebug:
			c.graph.PrettyPrint()
		case releasecontroller.PruneGraphPrintSecret:
			c.graph.PrintSecretPayload()
		}
	}

	return nil
}
