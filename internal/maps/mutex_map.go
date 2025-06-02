package maps

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// MutexMap provides a locking mechanism based on the passed in reference name
type MutexMap struct {
	mutex sync.Mutex
	locks map[string]*wrappedMutex
}

// NewMutexMap creates a new MutexMap
func NewMutexMap() *MutexMap {
	return &MutexMap{
		locks: make(map[string]*wrappedMutex),
	}
}

// Lock locks a mutex with the given name. If it doesn't exist, one is created
func (l *MutexMap) Lock(name string) {
	l.mutex.Lock()
	if l.locks == nil {
		l.locks = make(map[string]*wrappedMutex)
	}

	nameLock, exists := l.locks[name]
	if !exists {
		nameLock = &wrappedMutex{}
		l.locks[name] = nameLock
	}

	// increment the nameLock waiters while inside the main mutex
	// this makes sure that the wrappedMutex isn't deleted if `Lock` and `Unlock` are called concurrently
	nameLock.inc()
	l.mutex.Unlock()

	// Lock the nameLock outside the main mutex so we don't block other operations
	// once locked then we can decrement the number of waiters for this wrappedMutex
	nameLock.Lock()
	nameLock.dec()
}

// Unlock unlocks the mutex with the given name
// If the given wrappedMutex is not being waited on by any other callers, it is deleted
func (l *MutexMap) Unlock(name string) error {
	l.mutex.Lock()
	nameLock, exists := l.locks[name]
	if !exists {
		l.mutex.Unlock()
		return fmt.Errorf("no mutex found for name %s", name)
	}

	if nameLock.count() == 0 {
		delete(l.locks, name)
	}
	nameLock.Unlock()

	l.mutex.Unlock()
	return nil
}

// wrappedMutex adds a count of waiters to the wrappedMutex
// so that the map knows when to clean up a wrapped mutex
type wrappedMutex struct {
	mu sync.Mutex
	// waiters is the number of waiters waiting to acquire the wrappedMutex
	// this is int32 instead of uint32 so we can add `-1` in `dec()`
	waiters int32
}

func (l *wrappedMutex) inc() {
	atomic.AddInt32(&l.waiters, 1)
}

func (l *wrappedMutex) dec() {
	atomic.AddInt32(&l.waiters, -1)
}

func (l *wrappedMutex) count() int32 {
	return atomic.LoadInt32(&l.waiters)
}

func (l *wrappedMutex) Lock() {
	l.mu.Lock()
}

func (l *wrappedMutex) Unlock() {
	l.mu.Unlock()
}
