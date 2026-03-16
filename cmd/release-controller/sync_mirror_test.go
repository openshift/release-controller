package main

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	imagev1 "github.com/openshift/api/image/v1"
	releasecontroller "github.com/openshift/release-controller/pkg/release-controller"
)

func Test_calculateMirrorImageStream(t *testing.T) {
	tests := []struct {
		name    string
		source  string
		release *releasecontroller.Release
		is      *imagev1.ImageStream
		wantErr bool
		expects string
	}{
		{
			name: "current functionality - standard imagestream",
			release: &releasecontroller.Release{
				Source: &imagev1.ImageStream{},
				Target: &imagev1.ImageStream{},
				Config: &releasecontroller.ReleaseConfig{
					ReferenceMode: "source",
				},
			},
			is:      &imagev1.ImageStream{},
			source:  "testdata/4.23.json",
			wantErr: false,
			expects: "testdata/4.23-mirror.json",
		},
		{
			name: "current functionality - QCI reference imagestream",
			release: &releasecontroller.Release{
				Source: &imagev1.ImageStream{},
				Target: &imagev1.ImageStream{},
				Config: &releasecontroller.ReleaseConfig{
					ReferenceMode: "source",
				},
			},
			is:      &imagev1.ImageStream{},
			source:  "testdata/4.23-quay-references.json",
			wantErr: false,
			expects: "testdata/4.23-quay-references-mirror.json",
		},
		{
			name: "current functionality - QCI SHA256 reference imagestream",
			release: &releasecontroller.Release{
				Source: &imagev1.ImageStream{},
				Target: &imagev1.ImageStream{},
				Config: &releasecontroller.ReleaseConfig{
					ReferenceMode: "source",
				},
			},
			is:      &imagev1.ImageStream{},
			source:  "testdata/4.23-quay-references-sha256.json",
			wantErr: false,
			expects: "testdata/4.23-quay-references-sha256-mirror.json",
		},
		{
			name: "current functionality - mixed bag imagestream",
			release: &releasecontroller.Release{
				Source: &imagev1.ImageStream{},
				Target: &imagev1.ImageStream{},
				Config: &releasecontroller.ReleaseConfig{
					ReferenceMode: "source",
				},
			},
			is:      &imagev1.ImageStream{},
			source:  "testdata/4.23-mixed-bag.json",
			wantErr: false,
			expects: "testdata/4.23-mixed-bag-mirror.json",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Get the Source imagestream
			data, err := os.ReadFile(tt.source)
			if err != nil {
				t.Fatalf("failed to read source file: %v", err)
			}

			var source *imagev1.ImageStream
			if err := json.Unmarshal(data, &source); err != nil {
				t.Fatalf("failed to unmarshal source ImageStream: %v", err)
			}
			tt.release.Source = source

			// Get the Expected imagestream
			data, err = os.ReadFile(tt.expects)
			if err != nil {
				t.Fatalf("failed to read expects file: %v", err)
			}

			var expects *imagev1.ImageStream
			if err = json.Unmarshal(data, &expects); err != nil {
				t.Fatalf("failed to unmarshal expects ImageStream: %v", err)
			}

			if err = calculateMirrorImageStream(tt.release, tt.is); (err != nil) != tt.wantErr {
				t.Errorf("calculateMirrorImageStream() error = %v, wantErr %v", err, tt.wantErr)
			}

			//b, err := json.MarshalIndent(tt.is, "", "  ")
			//if err != nil {
			//	t.Fatalf("unable to marshal output: %v", err)
			//}
			//fmt.Println(string(b))

			if !reflect.DeepEqual(tt.is, expects) {
				t.Errorf("calculateMirrorImageStream() got = %v, want %v", tt.is, expects)
			}
		})
	}
}
