package releasecontroller

import "slices"

func StringSliceContains(slice []string, s string) bool {
	return slices.Contains(slice, s)
}

func ContainsString(arr []string, s string) bool {
	return slices.Contains(arr, s)
}
