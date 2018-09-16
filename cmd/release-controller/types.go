package main

import (
	"time"

	imagev1 "github.com/openshift/api/image/v1"
)

type Release struct {
	Source  *imagev1.ImageStream
	Target  *imagev1.ImageStream
	Config  *ReleaseConfig
	Expires time.Duration
}

type ReleaseConfig struct {
	Name string `json:"name"`
}
