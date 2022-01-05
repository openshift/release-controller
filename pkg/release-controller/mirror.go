package releasecontroller

import (
	"fmt"
	"strings"

	imagev1 "github.com/openshift/api/image/v1"
)

func GetMirror(release *Release, releaseTagName string, lister *MultiImageStreamLister) (*imagev1.ImageStream, error) {
	return lister.ImageStreams(release.Source.Namespace).Get(MirrorName(release, releaseTagName))
}

func MirrorName(release *Release, releaseTagName string) string {
	switch release.Config.As {
	case ReleaseConfigModeStable:
		return releaseTagName
	default:
		suffix := strings.TrimPrefix(releaseTagName, release.Config.Name)
		if len(release.Config.MirrorPrefix) > 0 {
			return fmt.Sprintf("%s%s", release.Config.MirrorPrefix, suffix)
		}
		return fmt.Sprintf("%s%s", release.Source.Name, suffix)
	}
}
