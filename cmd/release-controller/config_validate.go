package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/openshift/release-controller/pkg/release-controller"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/robfig/cron.v2"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
)

func validateConfigs(configDir string) error {
	errors := []error{}
	releaseConfigs := []releasecontroller.ReleaseConfig{}
	err := filepath.Walk(configDir, func(path string, info os.FileInfo, err error) error {
		if info != nil && filepath.Ext(info.Name()) == ".json" {
			raw, err := ioutil.ReadFile(path)
			if err != nil {
				return err
			}
			config := releasecontroller.ReleaseConfig{}
			dec := json.NewDecoder(bytes.NewReader(raw))
			dec.DisallowUnknownFields() // Force errors on unknown fields
			if err := dec.Decode(&config); err != nil {
				errors = append(errors, fmt.Errorf("failed to unmarshal release configuration file %s: %v", info.Name(), err))
				return nil
			}
			releaseConfigs = append(releaseConfigs, config)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("error encountered while trying to read config files: %w", err)
	}
	errors = append(errors, verifyPeriodicFields(releaseConfigs)...)
	errors = append(errors, findDuplicatePeriodics(releaseConfigs)...)
	return utilerrors.NewAggregate(errors)
}

func validateUpgradeJobs(releaseConfigs []releasecontroller.ReleaseConfig) []error {
	errors := []error{}
	for _, config := range releaseConfigs {
		for name, verify := range config.Verify {
			if len(verify.UpgradeFrom) > 0 && verify.UpgradeFromRelease != nil {
				errors = append(errors, fmt.Errorf("%s: verification job %s cannot have both upgradeFrom and upgradeFromRelease set", config.Name, name))
			}
		}
		for name, periodic := range config.Periodic {
			if len(periodic.UpgradeFrom) > 0 && periodic.UpgradeFromRelease != nil {
				errors = append(errors, fmt.Errorf("%s: periodic job %s cannot have both upgradeFrom and upgradeFromRelease set", config.Name, name))
			}
		}
	}
	return errors
}

func verifyPeriodicFields(releaseConfigs []releasecontroller.ReleaseConfig) []error {
	errors := []error{}
	for _, config := range releaseConfigs {
		for stepName, periodic := range config.Periodic {
			if periodic.Cron == "" && periodic.Interval == "" {
				errors = append(errors, fmt.Errorf("%s: periodic %s: must specify a cron or interval", config.Name, stepName))
			}
			if periodic.Cron != "" && periodic.Interval != "" {
				errors = append(errors, fmt.Errorf("%s: periodic %s: cannot have both cron and interval specified", config.Name, stepName))
			}
			if periodic.Interval != "" {
				if _, err := time.ParseDuration(periodic.Interval); err != nil {
					errors = append(errors, fmt.Errorf("%s: periodic %s: cannot parse interval: %w", config.Name, stepName, err))
				}
			}
			if periodic.Cron != "" {
				if _, err := cron.Parse(periodic.Cron); err != nil {
					errors = append(errors, fmt.Errorf("%s: periodic %s: cannot parse cron: %w", config.Name, stepName, err))
				}
			}
		}
	}
	return errors
}

func findDuplicatePeriodics(releaseConfigs []releasecontroller.ReleaseConfig) []error {
	seen := make(map[string][]string)
	for _, config := range releaseConfigs {
		for stepName, periodic := range config.Periodic {
			steps, ok := seen[periodic.ProwJob.Name]
			if !ok {
				steps = []string{}
			}
			steps = append(steps, fmt.Sprintf("[%s: periodic: %s]", config.Name, stepName))
			seen[periodic.ProwJob.Name] = steps
		}
	}
	var duplicates []error
	for job, steps := range seen {
		if len(steps) == 1 {
			continue
		}
		duplicates = append(duplicates, fmt.Errorf("found job %s in multiple locations: %v", job, steps))
	}
	return duplicates
}
