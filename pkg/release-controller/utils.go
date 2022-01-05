package releasecontroller

func StringSliceContains(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

func ContainsString(arr []string, s string) bool {
	for _, str := range arr {
		if s == str {
			return true
		}
	}
	return false
}
