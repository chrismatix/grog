package maps

import (
	"sync"
	"testing"
	"time"
)

func TestUnlockUnknownKey(t *testing.T) {
	m := NewMutexMap()
	err := m.Unlock("missing")
	if err == nil {
		t.Fatal("expected error unlocking missing key, got nil")
	}
}

func TestConcurrentLockUnlockSameName(t *testing.T) {
	m := NewMutexMap()
	const name = "shared"
	const n = 10

	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			m.Lock(name)
			// simulate some work
			time.Sleep(5 * time.Millisecond)
			if err := m.Unlock(name); err != nil {
				t.Errorf("unlock error: %v", err)
			}
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for goroutines")
	}

	if len(m.locks) != 0 {
		t.Fatalf("expected locks map empty, got %d entries", len(m.locks))
	}
}
