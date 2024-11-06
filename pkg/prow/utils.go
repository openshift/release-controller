package prow

import (
	"crypto/sha256"
	"encoding/base32"
	"fmt"
	"strings"
)

const (
	MaxProwJobNameLength = 63
)

// oneWayEncoding can be used to encode hex to a 62-character set (0 and 1 are duplicates) for use in
// short display names that are safe for use in kubernetes as resource names.
var oneWayNameEncoding = base32.NewEncoding("bcdfghijklmnpqrstvwxyz0123456789").WithPadding(base32.NoPadding)

func ProwjobSafeHash(values ...string) string {
	hash := sha256.New()

	// the inputs form a part of the hash
	for _, s := range values {
		hash.Write([]byte(s))
	}

	// Object names can't be too long so we truncate
	// the hash. This increases chances of collision
	// but we can tolerate it as our input space is
	// tiny.
	return oneWayNameEncoding.EncodeToString(hash.Sum(nil)[:4])
}

func GenerateSafeProwJobName(name, suffix string) string {
	if suffix != "" && !strings.HasPrefix(suffix, "-") {
		suffix = "-" + suffix
	}
	jobName := fmt.Sprintf("%s%s", name, suffix)
	if len(jobName) > MaxProwJobNameLength {
		s := fmt.Sprintf("-%s%s", ProwjobSafeHash(name), suffix)
		truncated := strings.TrimSuffix(name[0:MaxProwJobNameLength-len(s)], "-")
		jobName = fmt.Sprintf("%s%s", truncated, s)
	}
	return jobName
}
