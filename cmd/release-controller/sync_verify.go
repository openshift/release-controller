package main

import (
	"encoding/json"
	"fmt"

	"github.com/golang/glog"

	imagev1 "github.com/openshift/api/image/v1"
)

func (c *Controller) ensureVerificationJobs(release *Release, releaseTag *imagev1.TagReference) (VerificationStatusMap, error) {
	var verifyStatus VerificationStatusMap
	for name, verifyType := range release.Config.Verify {
		if verifyType.Disabled {
			glog.V(2).Infof("Release verification step %s is disabled, ignoring", name)
			continue
		}

		switch {
		case verifyType.ProwJob != nil:
			if verifyStatus == nil {
				if data := releaseTag.Annotations[releaseAnnotationVerify]; len(data) > 0 {
					verifyStatus = make(VerificationStatusMap)
					if err := json.Unmarshal([]byte(data), &verifyStatus); err != nil {
						glog.Errorf("Release %s has invalid verification status, ignoring: %v", releaseTag.Name, err)
					}
				}
			}

			if status, ok := verifyStatus[name]; ok {
				switch status.State {
				case releaseVerificationStateFailed, releaseVerificationStateSucceeded:
					// we've already processed this, continue
					continue
				case releaseVerificationStatePending:
					// we need to process this
				default:
					glog.V(2).Infof("Unrecognized verification status %q for type %s on release %s", status.State, name, releaseTag.Name)
				}
			}

			job, err := c.ensureProwJobForReleaseTag(release, name, verifyType, releaseTag)
			if err != nil {
				return nil, err
			}
			status, ok := prowJobVerificationStatus(job)
			if !ok {
				return nil, fmt.Errorf("unexpected error accessing prow job definition")
			}
			if status.State == releaseVerificationStateSucceeded {
				glog.V(2).Infof("Prow job %s for release %s succeeded, logs at %s", name, releaseTag.Name, status.URL)
			}
			if verifyStatus == nil {
				verifyStatus = make(VerificationStatusMap)
			}
			verifyStatus[name] = status

		default:
			// manual verification
		}
	}
	return verifyStatus, nil
}

