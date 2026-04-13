package releasecontroller

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// MachineOSStreamInfo describes one machine-OS image stream (base tag + optional display name from payload).
type MachineOSStreamInfo struct {
	Tag         string `json:"tag"`
	DisplayName string `json:"displayName,omitempty"`
}

// releaseInfoImageRefs is the subset of `oc adm release info -o json` needed to list payload tags.
type releaseInfoImageRefs struct {
	References struct {
		Spec struct {
			Tags []struct {
				Name        string            `json:"name"`
				Annotations map[string]string `json:"annotations"`
			} `json:"tags"`
		} `json:"spec"`
	} `json:"references"`
}

const versionDisplayNamesKey = "io.openshift.build.version-display-names"

// ListMachineOSStreams returns machine-OS base tags discovered by pairing each *coreos* extensions
// image with its base tag. Display names come from io.openshift.build.version-display-names
// (machine-os=...) on the base image, as used in current OCP payloads (e.g. 4.21 nightlies).
// Convention: extensions tag is "<base>-extensions" (rhel-coreos-extensions, rhel-coreos-10-extensions).
func (r *ExecReleaseInfo) ListMachineOSStreams(releaseImage string) ([]MachineOSStreamInfo, error) {
	raw, err := r.ReleaseInfo(releaseImage)
	if err != nil {
		return nil, err
	}
	return machineOSStreamsFromReleaseJSON(raw)
}

// machineOSStreamsFromReleaseJSON parses release JSON for tests and shared logic.
func machineOSStreamsFromReleaseJSON(raw string) ([]MachineOSStreamInfo, error) {
	var ri releaseInfoImageRefs
	if err := json.Unmarshal([]byte(raw), &ri); err != nil {
		return nil, err
	}

	tagSet := make(map[string]struct{}, len(ri.References.Spec.Tags))
	annByTag := make(map[string]map[string]string, len(ri.References.Spec.Tags))
	for _, t := range ri.References.Spec.Tags {
		if t.Name == "" {
			continue
		}
		tagSet[t.Name] = struct{}{}
		if len(t.Annotations) > 0 {
			annByTag[t.Name] = t.Annotations
		}
	}

	var bases []string
	for name := range tagSet {
		if !strings.HasSuffix(name, "-extensions") {
			continue
		}
		if !strings.Contains(name, "coreos") {
			continue
		}
		base := strings.TrimSuffix(name, "-extensions")
		if base == "" {
			continue
		}
		if _, ok := tagSet[base]; !ok {
			continue
		}
		bases = append(bases, base)
	}

	sortMachineOSTags(bases)
	out := make([]MachineOSStreamInfo, 0, len(bases))
	for _, base := range bases {
		dn := ""
		if a, ok := annByTag[base]; ok {
			dn = machineOSDisplayNameFromAnnotations(a)
		}
		out = append(out, MachineOSStreamInfo{Tag: base, DisplayName: dn})
	}
	return out, nil
}

// machineOSDisplayNameFromAnnotations extracts the machine-os display name from the
// io.openshift.build.version-display-names annotation. Returns empty string if not found.
// Typical format: "machine-os=Red Hat Enterprise Linux CoreOS" (comma-separated key=value pairs).
func machineOSDisplayNameFromAnnotations(annotations map[string]string) string {
	v := strings.TrimSpace(annotations[versionDisplayNamesKey])
	if v == "" {
		return ""
	}
	// Typical: "machine-os=Red Hat Enterprise Linux CoreOS" (single pair).
	for _, part := range strings.Split(v, ",") {
		part = strings.TrimSpace(part)
		const prefix = "machine-os="
		if strings.HasPrefix(part, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(part, prefix))
		}
	}
	return ""
}

// MachineOSTitle returns a markdown subsection title for a stream (display name + tag in backticks).
func MachineOSTitle(s MachineOSStreamInfo) string {
	if s.DisplayName != "" {
		return fmt.Sprintf("%s (`%s`)", s.DisplayName, s.Tag)
	}
	switch s.Tag {
	case "rhel-coreos":
		return "Red Hat Enterprise Linux CoreOS (`rhel-coreos`)"
	case "rhel-coreos-10":
		return "Red Hat Enterprise Linux CoreOS 10 (`rhel-coreos-10`)"
	case "stream-coreos":
		return "Stream CoreOS (`stream-coreos`)"
	default:
		return fmt.Sprintf("Machine OS (`%s`)", s.Tag)
	}
}

// sortMachineOSTags sorts a slice of machine OS tag names in-place using a stable sort.
// rhel-coreos and stream-coreos are prioritized first, other tags are sorted lexicographically.
func sortMachineOSTags(tags []string) {
	sort.SliceStable(tags, func(i, j int) bool {
		return machineOSTagLess(tags[i], tags[j])
	})
}

// machineOSTagLess is a comparison function for machine OS tag names.
// Returns true if a should sort before b. Prioritizes rhel-coreos (0) and stream-coreos (1),
// then falls back to lexicographic ordering for all other tags.
func machineOSTagLess(a, b string) bool {
	prio := map[string]int{
		"rhel-coreos":   0,
		"stream-coreos": 1,
	}
	pa, okA := prio[a]
	pb, okB := prio[b]
	switch {
	case okA && okB:
		return pa < pb
	case okA:
		return true
	case okB:
		return false
	default:
		return a < b
	}
}
