package releasecontroller

import (
	"reflect"
	"testing"
)

func TestMachineOSStreamsFromReleaseJSON_nightly421(t *testing.T) {
	// Subset of oc adm release info -o json for registry.ci.openshift.org/ocp/release:4.21.0-0.nightly-2026-03-30-143812
	const raw = `{
  "references": {
    "spec": {
      "tags": [
        {
          "name": "rhel-coreos",
          "annotations": {
            "io.openshift.build.version-display-names": "machine-os=Red Hat Enterprise Linux CoreOS",
            "io.openshift.build.versions": "machine-os=9.6.20260327-0"
          }
        },
        {
          "name": "rhel-coreos-10",
          "annotations": {
            "io.openshift.build.version-display-names": "machine-os=Red Hat Enterprise Linux CoreOS 10.2",
            "io.openshift.build.versions": "machine-os=10.2.20260328-0"
          }
        },
        {
          "name": "rhel-coreos-10-extensions",
          "annotations": {}
        },
        {
          "name": "rhel-coreos-extensions",
          "annotations": {}
        }
      ]
    }
  }
}`

	got, err := machineOSStreamsFromReleaseJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	want := []MachineOSStreamInfo{
		{Tag: "rhel-coreos", DisplayName: "Red Hat Enterprise Linux CoreOS"},
		{Tag: "rhel-coreos-10", DisplayName: "Red Hat Enterprise Linux CoreOS 10.2"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("machineOSStreamsFromReleaseJSON() = %#v, want %#v", got, want)
	}
}

func TestMachineOSDisplayNameFromAnnotations(t *testing.T) {
	tests := []struct {
		ann  map[string]string
		want string
	}{
		{nil, ""},
		{map[string]string{versionDisplayNamesKey: "machine-os=Foo Bar"}, "Foo Bar"},
		{map[string]string{versionDisplayNamesKey: " machine-os=Foo Bar "}, "Foo Bar"},
		{map[string]string{versionDisplayNamesKey: "other=x, machine-os=CoreOS 10"}, "CoreOS 10"},
	}
	for _, tt := range tests {
		if got := machineOSDisplayNameFromAnnotations(tt.ann); got != tt.want {
			t.Errorf("machineOSDisplayNameFromAnnotations(%v) = %q, want %q", tt.ann, got, tt.want)
		}
	}
}

func TestMachineOSTitle(t *testing.T) {
	if got := MachineOSTitle(MachineOSStreamInfo{Tag: "rhel-coreos", DisplayName: "Red Hat Enterprise Linux CoreOS"}); got != "Red Hat Enterprise Linux CoreOS (`rhel-coreos`)" {
		t.Errorf("got %q", got)
	}
	if got := MachineOSTitle(MachineOSStreamInfo{Tag: "custom-stream", DisplayName: ""}); got != "Machine OS (`custom-stream`)" {
		t.Errorf("got %q", got)
	}
}
