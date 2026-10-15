package releasecontroller

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	imagev1 "github.com/openshift/api/image/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/validation"
)

// Variables that may be referenced from the spec.data of a konfluxRelease publish step.
// They use shell style ${...} rather than the {{ ... }} that Konflux itself uses for
// mapping tags, so that the two expansions cannot be confused for one another: these are
// expanded by the release controller before the Release is created, while {{ ... }} is
// expanded by the Konflux release pipeline afterwards.
const (
	// KonfluxVarReleaseTag expands to the name of the accepted release tag. Referencing it
	// from a mapping tag is how the release tag is carried onto the image Konflux pushes.
	KonfluxVarReleaseTag = "RELEASE_TAG"
	// KonfluxVarReleaseName expands to the name of the release stream.
	KonfluxVarReleaseName = "RELEASE_NAME"
	// KonfluxVarSnapshot expands to the name of the Konflux Snapshot being released.
	KonfluxVarSnapshot = "SNAPSHOT"
)

// KonfluxDataVariables are the variables that may be referenced from the spec.data of a
// konfluxRelease publish step.
var KonfluxDataVariables = []string{
	KonfluxVarReleaseTag,
	KonfluxVarReleaseName,
	KonfluxVarSnapshot,
}

// konfluxVariableReference matches anything shaped like a variable reference, rather than
// only well formed ones, so that a misspelling such as ${RELEASE-TAG} is reported at
// validation time instead of being passed through to Konflux as a literal image tag.
var konfluxVariableReference = regexp.MustCompile(`\$\{[^}]*\}`)

// KonfluxReleaseAnnotation returns the image stream tag annotation that records the Konflux
// Release created for a tag by the named publish step.
func KonfluxReleaseAnnotation(stepName string) string {
	return ReleaseAnnotationKonfluxReleasePrefix + stepName
}

// KonfluxSnapshotForTag returns the Konflux Snapshot that the image behind the tag was built
// from, as stamped on the tag by whatever promoted the image into the stream. Returns an
// empty string when the tag carries no Snapshot.
func KonfluxSnapshotForTag(tag *imagev1.TagReference) string {
	if tag == nil {
		return ""
	}
	return tag.Annotations[ReleaseAnnotationKonfluxSnapshot]
}

// ValidateKonfluxReleasePublish checks a konfluxRelease publish step. It is shared by
// ParseReleaseConfig and by the offline config validator, which do not otherwise share a
// code path. The mode of the release stream is passed in because the step is only
// meaningful for streams that adopt tags created outside the release controller.
func ValidateKonfluxReleasePublish(stepName, as string, publish *PublishKonfluxRelease) error {
	// The step name becomes part of an annotation key, which must be a DNS1123 label.
	if errs := validation.IsDNS1123Label(stepName); len(errs) > 0 {
		return fmt.Errorf("konfluxRelease publish for %s has an invalid name: %s", stepName, strings.Join(errs, ", "))
	}
	if as != ReleaseConfigModeStable && as != ReleaseConfigModeLayered {
		// Every other mode has its tags created by the release controller itself, with a
		// fixed set of annotations, so nothing can ever stamp a Snapshot onto them.
		return fmt.Errorf("konfluxRelease publish for %s requires 'as' to be 'Stable' or 'Layered'", stepName)
	}
	if len(publish.Namespace) == 0 {
		return fmt.Errorf("konfluxRelease publish for %s has no namespace", stepName)
	}
	if len(publish.ReleasePlan) == 0 {
		return fmt.Errorf("konfluxRelease publish for %s has no releasePlan", stepName)
	}
	if len(publish.SecretName) == 0 {
		return fmt.Errorf("konfluxRelease publish for %s has no secretName", stepName)
	}
	if len(publish.CLIImage) == 0 {
		return fmt.Errorf("konfluxRelease publish for %s has no cliImage", stepName)
	}
	if err := ValidateKonfluxData(publish.Data); err != nil {
		return fmt.Errorf("konfluxRelease publish for %s has invalid data: %w", stepName, err)
	}
	return nil
}

// ValidateKonfluxData returns an error when data is not a JSON object or references a
// variable that ExpandKonfluxData does not know how to expand.
func ValidateKonfluxData(data json.RawMessage) error {
	if len(data) == 0 {
		return nil
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return fmt.Errorf("must be a JSON object: %w", err)
	}
	unknown := sets.New[string]()
	walkKonfluxStrings(parsed, func(s string) string {
		for _, match := range konfluxVariableReference.FindAllString(s, -1) {
			if !slices.Contains(KonfluxDataVariables, strings.TrimSuffix(strings.TrimPrefix(match, "${"), "}")) {
				unknown.Insert(match)
			}
		}
		return s
	})
	if unknown.Len() > 0 {
		names := unknown.UnsortedList()
		sort.Strings(names)
		return fmt.Errorf("references unknown variables %s, supported variables are ${%s}",
			strings.Join(names, ", "), strings.Join(KonfluxDataVariables, "}, ${"))
	}
	return nil
}

// ExpandKonfluxData parses the configured spec.data and substitutes the supported variables
// into every string it contains. Substitution is done on the parsed structure rather than on
// the raw JSON so that a value containing JSON metacharacters cannot corrupt the document.
func ExpandKonfluxData(data json.RawMessage, vars map[string]string) (map[string]any, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("unable to parse konfluxRelease data: %w", err)
	}
	replacements := make([]string, 0, 2*len(vars))
	for _, name := range KonfluxDataVariables {
		replacements = append(replacements, "${"+name+"}", vars[name])
	}
	replacer := strings.NewReplacer(replacements...)
	walkKonfluxStrings(parsed, replacer.Replace)
	return parsed, nil
}

// walkKonfluxStrings applies fn to every string leaf reachable from value, rewriting maps and
// slices in place. Non string leaves are left alone.
func walkKonfluxStrings(value any, fn func(string) string) {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			if s, ok := item.(string); ok {
				typed[key] = fn(s)
				continue
			}
			walkKonfluxStrings(item, fn)
		}
	case []any:
		for i, item := range typed {
			if s, ok := item.(string); ok {
				typed[i] = fn(s)
				continue
			}
			walkKonfluxStrings(item, fn)
		}
	}
}
