package main

import (
	"sync"

	"k8s.io/apimachinery/pkg/util/sets"
)

// expectations track an upcoming change to a named resource related
// to a release. This is a thread safe object but callers assume
// responsibility for ensuring expectations do not leak.
type expectations struct {
	lock   sync.Mutex
	expect map[queueKey]sets.String
}

// newExpectations returns a tracking object for upcoming events
// that the controller may expect to happen.
func newExpectations() *expectations {
	return &expectations{
		expect: make(map[queueKey]sets.String),
	}
}

// Expect that an event will happen in the future for the given release
// and a named resource related to that release.
func (e *expectations) Expect(namespace, parentName, name string) {
	e.lock.Lock()
	defer e.lock.Unlock()
	key := queueKey{namespace: namespace, name: parentName}
	set, ok := e.expect[key]
	if !ok {
		set = sets.NewString()
		e.expect[key] = set
	}
	set.Insert(name)
}

// Satisfied clears the expectation for the given resource name on an
// release.
func (e *expectations) Satisfied(namespace, parentName, name string) {
	e.lock.Lock()
	defer e.lock.Unlock()
	key := queueKey{namespace: namespace, name: parentName}
	set := e.expect[key]
	set.Delete(name)
	if set.Len() == 0 {
		delete(e.expect, key)
	}
}

// Expecting returns true if the provided release is still waiting to
// see changes.
func (e *expectations) Expecting(namespace, parentName string) bool {
	e.lock.Lock()
	defer e.lock.Unlock()
	key := queueKey{namespace: namespace, name: parentName}
	return e.expect[key].Len() > 0
}

// Clear indicates that all expectations for the given release should
// be cleared.
func (e *expectations) Clear(namespace, parentName string) {
	e.lock.Lock()
	defer e.lock.Unlock()
	key := queueKey{namespace: namespace, name: parentName}
	delete(e.expect, key)
}
