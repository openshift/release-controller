package jobrunresult

import "github.com/openshift/release-controller/pkg/apis/release/v1alpha1"

// ByCoordinatesName sorts a list of JobRunResults by their Coordinates Name
type ByCoordinatesName []v1alpha1.JobRunResult

func (in ByCoordinatesName) Less(i, j int) bool {
	return in[i].Coordinates.Name < in[j].Coordinates.Name
}

func (in ByCoordinatesName) Len() int {
	return len(in)
}

func (in ByCoordinatesName) Swap(i, j int) {
	in[i], in[j] = in[j], in[i]
}
