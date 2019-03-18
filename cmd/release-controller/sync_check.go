package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	imagev1 "github.com/openshift/api/image/v1"
)

func checkConsistentImages(release, parent *Release) ReleaseCheckResult {
	var result ReleaseCheckResult
	source := parent.Source
	target := release.Source
	statusTags := make(map[string]*imagev1.NamedTagEventList)
	for i := range source.Status.Tags {
		statusTags[source.Status.Tags[i].Tag] = &source.Status.Tags[i]
	}
	var (
		errUnableToImport     map[string]string
		errDoesNotExist       []string
		errStaleBuild         []string
		warnOlderThanUpstream []string
		warnNoDownstream      []string
	)

	now := time.Now()

	for _, tag := range target.Status.Tags {
		if isTagEventConditionNotImported(&tag) {
			source := "image"
			if ref := findTagReference(target, tag.Tag); ref != nil {
				if ref.From != nil {
					source = ref.From.Name
				}
			}
			if errUnableToImport == nil {
				errUnableToImport = make(map[string]string)
			}
			errUnableToImport[tag.Tag] = source
			continue
		}
		if len(tag.Items) == 0 {
			// TODO: check something here?
			continue
		}
		sourceTag := statusTags[tag.Tag]
		delete(statusTags, tag.Tag)

		if sourceTag == nil || len(sourceTag.Items) == 0 {
			if now.Sub(tag.Items[0].Created.Time) > 10*24*time.Hour {
				errStaleBuild = append(errStaleBuild, tag.Tag)
			} else {
				errDoesNotExist = append(errDoesNotExist, tag.Tag)
			}
			continue
		}
		delta := sourceTag.Items[0].Created.Sub(tag.Items[0].Created.Time)
		if delta > 0 {
			// source tag is newer than current tag
			if delta > 5*24*time.Hour {
				warnOlderThanUpstream = append(warnOlderThanUpstream, tag.Tag)
				continue
			}
		}
	}
	for _, tag := range statusTags {
		if len(tag.Items) == 0 {
			continue
		}
		if now.Sub(tag.Items[0].Created.Time) > 24*time.Hour {
			warnNoDownstream = append(warnNoDownstream, tag.Tag)
			continue
		}
	}

	if len(errUnableToImport) > 0 {
		var keys []string
		for k := range errUnableToImport {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		result.Errors = append(result.Errors, fmt.Sprintf("Unable to import the following tags: %s", strings.Join(keys, ", ")))
	}
	if len(errStaleBuild) > 0 {
		sort.Strings(errStaleBuild)
		result.Errors = append(result.Errors, fmt.Sprintf("Found old tags downstream that are not built upstream (not in %s/%s): %s", parent.Source.Namespace, parent.Source.Name, strings.Join(errStaleBuild, ", ")))
	}
	if len(errDoesNotExist) > 0 {
		sort.Strings(errDoesNotExist)
		result.Errors = append(result.Errors, fmt.Sprintf("Found recent tags downstream that are not built upstream (not in %s/%s): %s", parent.Source.Namespace, parent.Source.Name, strings.Join(errDoesNotExist, ", ")))
	}
	if len(warnOlderThanUpstream) > 0 {
		sort.Strings(warnOlderThanUpstream)
		result.Warnings = append(result.Warnings, fmt.Sprintf("Some tags are significantly older than the upstream: %s", strings.Join(warnOlderThanUpstream, ", ")))
	}
	if len(warnNoDownstream) > 0 {
		sort.Strings(warnNoDownstream)
		result.Warnings = append(result.Warnings, fmt.Sprintf("No downstream builds have been pushed for these upstream tags: %s", strings.Join(warnNoDownstream, ", ")))
	}
	return result
}

func findReleaseStream(page *ReleasePage, name string) *ReleaseStream {
	for i := range page.Streams {
		if page.Streams[i].Release.Config.Name == name {
			return &page.Streams[i]
		}
	}
	return nil
}
