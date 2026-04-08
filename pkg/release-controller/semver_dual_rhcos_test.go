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
	}
	for _, tt := range tests {
		t.Run(tt.tag, func(t *testing.T) {
			if got := ReleaseTagIsDualRHCOS(tt.tag); got != tt.want {
				t.Errorf("ReleaseTagIsDualRHCOS(%q) = %v, want %v", tt.tag, got, tt.want)
			}
		})
	}
}
