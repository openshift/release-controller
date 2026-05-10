package releasecontroller

import "testing"

func TestReleaseTagIsDualRHCOS(t *testing.T) {
	tests := []struct {
		tag  string
		want bool
	}{
		{"4.21.0-ec.1", true},
		{"4.21.0", true},
		{"4.22.1", true},
		{"4.20.0", false},
		{"4.20.0-ec.0", false},
		{"not-a-version", false},
		{"5.0.0", true},
		{"5.0.0-ec.1", true},
		{"5.1.0", true},
		{"5.2.3", true},
	}
	for _, tt := range tests {
		t.Run(tt.tag, func(t *testing.T) {
			if got := ReleaseTagIsDualRHCOS(tt.tag); got != tt.want {
				t.Errorf("ReleaseTagIsDualRHCOS(%q) = %v, want %v", tt.tag, got, tt.want)
			}
		})
	}
}

func TestPreferredMachineOSTag(t *testing.T) {
	tests := []struct {
		tag  string
		want string
	}{
		// 4.Y releases prefer rhel-coreos (RHCOS 9)
		{"4.21.0-ec.1", "rhel-coreos"},
		{"4.21.0", "rhel-coreos"},
		{"4.22.1", "rhel-coreos"},
		{"4.20.0", "rhel-coreos"},
		{"4.20.0-ec.0", "rhel-coreos"},
		{"4.30.0", "rhel-coreos"},
		// 5.Y+ releases prefer rhel-coreos-10 (RHCOS 10)
		{"5.0.0", "rhel-coreos-10"},
		{"5.0.0-ec.1", "rhel-coreos-10"},
		{"5.1.0", "rhel-coreos-10"},
		{"5.2.3", "rhel-coreos-10"},
		{"6.0.0", "rhel-coreos-10"},
		// Invalid versions return empty string
		{"not-a-version", ""},
	}
	for _, tt := range tests {
		t.Run(tt.tag, func(t *testing.T) {
			if got := PreferredMachineOSTag(tt.tag); got != tt.want {
				t.Errorf("PreferredMachineOSTag(%q) = %q, want %q", tt.tag, got, tt.want)
			}
		})
	}
}
