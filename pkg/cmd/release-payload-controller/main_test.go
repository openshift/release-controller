package release_payload_controller

import (
	"os"
	"testing"
)

// TestMain disables the client-go WatchListClient feature gate for the whole
// package. As of client-go v0.35+ that gate defaults to on, which makes
// SharedInformer reflectors use the streaming (WatchList) list path. The
// generated fake clientset used throughout these tests never emits the
// bookmark event that marks the end of the initial event stream, so
// WaitForCacheSync would block forever and the tests hang. Forcing the gate
// off restores the classic list+watch path that the fake client supports.
//
// The gate is read once, lazily, from the environment on first query, so this
// must run before any informer is created.
func TestMain(m *testing.M) {
	if err := os.Setenv("KUBE_FEATURE_WatchListClient", "false"); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}
