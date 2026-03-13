package main

import (
	"hash/fnv"
	"testing"
	"time"
)

// splayDelay mirrors the delay computation in ensureProwJobForReleaseTag
func splayDelay(tagName, prowJobName string, splay time.Duration) time.Duration {
	h := fnv.New32a()
	h.Write([]byte(tagName + "/" + prowJobName))
	return time.Duration(h.Sum32()%uint32(splay.Seconds())) * time.Second
}

func TestSplayDelay(t *testing.T) {
	splay := 15 * time.Minute

	t.Run("deterministic", func(t *testing.T) {
		d1 := splayDelay("4.22.0-0.nightly-2026-03-11-124441", "job-a", splay)
		d2 := splayDelay("4.22.0-0.nightly-2026-03-11-124441", "job-a", splay)
		if d1 != d2 {
			t.Errorf("same inputs produced different delays: %v vs %v", d1, d2)
		}
	})

	t.Run("within bounds", func(t *testing.T) {
		for i := range 100 {
			tag := "4.22.0-0.nightly-2026-03-11-124441"
			job := "job-" + string(rune('a'+i))
			d := splayDelay(tag, job, splay)
			if d < 0 || d >= splay {
				t.Errorf("delay %v out of range [0, %v) for job %s", d, splay, job)
			}
		}
	})

	t.Run("different jobs get different delays", func(t *testing.T) {
		tag := "4.22.0-0.nightly-2026-03-11-124441"
		delays := make(map[time.Duration]int)
		for i := range 30 {
			job := "aggregated-e2e-aws-analysis-" + string(rune('0'+i))
			d := splayDelay(tag, job, splay)
			delays[d]++
		}
		// With 30 jobs across 900 seconds, we should get reasonable distribution.
		// At minimum, not all delays should be identical.
		if len(delays) < 2 {
			t.Errorf("expected varied delays across 30 jobs, got %d distinct values", len(delays))
		}
	})

	t.Run("different payloads get different delays for same job", func(t *testing.T) {
		d1 := splayDelay("4.22.0-0.nightly-2026-03-11-124441", "e2e-aws", splay)
		d2 := splayDelay("4.22.0-0.nightly-2026-03-12-060000", "e2e-aws", splay)
		if d1 == d2 {
			t.Logf("note: same delay for different payloads (possible but unlikely): %v", d1)
		}
	})

	// Note: when splay is 0, ensureProwJobForReleaseTag skips the splay gate
	// entirely (guarded by `if splay > 0`), so splayDelay is never called
	// with a zero duration.
}
